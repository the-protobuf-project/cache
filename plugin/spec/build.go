package spec

// build.go walks a built IR and assembles the spec: which resources are cached,
// under what namespace, with which indexes and edges.
//
// Nothing here decides a name. Every neutral name — database, schema, table,
// column, primary key — arrives already decided on the IR, because this plugin
// registers no StructureReader and so has no way to have influenced one. What is
// decided here is strictly cache-shaped: strategy, lease, namespace, scope.

import (
	"fmt"
	"sort"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/cachev1"
	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1"
)

// Build derives the spec from a built IR.
//
// It returns the first hard error it finds rather than accumulating: the one
// hard error this generator has is a tenancy boundary, and a report listing
// several of those alongside a summary is a report someone skims.
func Build(ir *protokit.IR) (*Spec, error) {
	out := &Spec{}
	for _, db := range ir.Databases {
		d := &Database{Name: db.Name}
		for _, s := range db.Schemas {
			sc, err := buildSchema(ir, s)
			if err != nil {
				return nil, err
			}
			if sc != nil {
				d.Schemas = append(d.Schemas, sc)
			}
		}
		// A database whose every schema is uncached produces no output at all,
		// rather than an empty directory.
		if len(d.Schemas) > 0 {
			out.Databases = append(out.Databases, d)
		}
	}
	return out, nil
}

// buildSchema returns the cached resources of one schema, or nil when it has
// none.
func buildSchema(ir *protokit.IR, s *schema.Schema) (*Schema, error) {
	var resources []*Resource
	tables := map[schema.NodeID]*schema.Table{}
	for _, t := range s.Tables {
		if t.Node != "" {
			tables[t.Node] = t
		}
		r, err := buildResource(ir, t)
		if err != nil {
			return nil, err
		}
		if r != nil {
			resources = append(resources, r)
		}
	}
	if len(resources) == 0 {
		return nil, nil
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Node < resources[j].Node })

	storePkg := naming.GoPackage(s.Name)
	out := &Schema{
		Name:           s.Name,
		StoreGoPackage: storePkg,
		GoPackage:      storePkg + "cache",
		Resources:      resources,
	}
	out.Edges = buildEdges(out, tables)

	// Hand each resource the edges it fires. The set is small and the duplication
	// is deliberate: a template that had to filter the schema-wide set would be a
	// template doing the graph walk this design exists to avoid.
	byFrom := map[schema.NodeID][]Edge{}
	for _, e := range out.Edges {
		byFrom[e.From] = append(byFrom[e.From], e)
	}
	for _, r := range out.Resources {
		r.Outgoing = byFrom[r.Node]
	}
	return out, nil
}

// buildResource returns the spec for one table, or nil when it is not cached.
func buildResource(ir *protokit.IR, t *schema.Table) (*Resource, error) {
	opts, ok := protokit.Facet[*cachepbv1.CacheOptions](ir, cachev1.Key, t.Node)
	if !ok || !opts.GetEnabled() {
		return nil, nil
	}
	// A synthesized table (an m2m join) has no message to annotate, so it cannot
	// reach here — but it also has no descriptor to read a pattern from, and a
	// nil Source below would be a confusing panic rather than a clear refusal.
	if t.Source == nil {
		return nil, fmt.Errorf("cache: %s has no proto message behind it, so its resource pattern "+
			"cannot be read; a synthesized table cannot be cached", t.Name)
	}

	defaults := fileDefaults(ir, t.Source.ParentFile())
	res := resourceDescriptor(t.Source)

	r := &Resource{
		Node:         t.Node,
		Model:        t.ModelName,
		Table:        t.Name,
		PKColumn:     t.PKColumn,
		ResourceType: res.GetType(),
		Strategy:     strategyOf(opts, defaults),
		TTL:          durationOf(opts.GetTtl(), defaults.GetTtl()),
		Stale:        durationOf(opts.GetStale(), defaults.GetStale()),
		NegativeTTL:  durationOf(opts.GetNegativeTtl(), defaults.GetNegativeTtl()),
	}
	if p := res.GetPattern(); len(p) > 0 {
		r.Pattern = p[0]
	}

	ns, err := buildNamespace(ir, t, r, opts, defaults)
	if err != nil {
		return nil, err
	}
	r.Namespace = ns

	if err := buildIndexes(ir, t, r); err != nil {
		return nil, err
	}
	r.Requires = requirements(r)
	return r, nil
}

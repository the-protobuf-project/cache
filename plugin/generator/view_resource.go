package generator

// view_resource.go turns one spec.Resource into the strings the template writes.
//
// Every Go identifier the output contains is decided here rather than in the
// template, and that is the rule the whole package is arranged around: a template
// that computed a name could emit a namespace the spec never validated, and the
// one thing this generator must never do quietly is cache under a namespace
// nobody checked.

import (
	"strconv"
	"strings"
	"time"

	"github.com/the-protobuf-project/protokit/naming"

	"github.com/the-protobuf-project/cache/plugin/spec"
)

func buildResource(r *spec.Resource, storePackage string) resourceView {
	rv := resourceView{
		Node:         string(r.Node),
		Model:        r.Model,
		Iface:        r.Model + "StoreIface",
		Table:        r.Table,
		Strategy:     string(r.Strategy),
		StorePackage: storePackage,
		PKField:      goFieldOf(r.PKColumn),
		Warnings:     r.Warnings,
	}
	rv.Doc = resourceDoc(r)

	rv.NamespaceFunc = r.Model + "Namespace"
	rv.HasScope = len(r.Namespace.Scope) > 0
	rv.ScopeType = r.Model + "Scope"
	if rv.HasScope {
		rv.NamespaceArgs = "scope " + rv.ScopeType
		rv.NamespaceCall = "scope"
		for _, b := range r.Namespace.Scope {
			rv.ScopeFields = append(rv.ScopeFields, scopeFieldView{
				GoField: b.GoField,
				Segment: b.Segment,
				Doc:     scopeDoc(b),
			})
		}
	}
	for _, p := range r.Namespace.Parts {
		if p.ScopeField != "" {
			rv.NamespaceExpr = append(rv.NamespaceExpr, "scope."+p.ScopeField)
			continue
		}
		rv.NamespaceExpr = append(rv.NamespaceExpr, strconv.Quote(p.Literal))
	}
	if !rv.HasScope {
		// With nothing to parameterize, the whole namespace is known now, so it
		// is emitted as one constant rather than a join of literals assembled at
		// run time. The parts are joined here — not in the template, and not
		// element-by-element — because a namespace that rendered only its first
		// segment would still compile, still be a valid namespace, and silently
		// address a keyspace shared with every other resource under the same
		// prefix.
		rv.NamespaceConst = lowerFirst(r.Model) + "Namespace"
		rv.NamespaceLiteral = strconv.Quote(joinLiterals(r.Namespace.Parts))
	}

	for _, c := range r.Requires {
		switch c {
		case spec.CoreSets:
			rv.AssertSets = true
		case spec.CoreLeases:
			rv.AssertLeases = true
		}
	}
	// The sets probe names a real index field when there is one, so a failure
	// message points at something the reader can find in their proto.
	rv.SetsProbe = "__cache_capability_probe"
	if len(r.Indexes) > 0 {
		rv.SetsProbe = r.Indexes[0].Field
	}

	rv.HasIndexes = len(r.Indexes) > 0
	for _, ix := range r.Indexes {
		rv.Indexes = append(rv.Indexes, indexView{
			Field:    ix.Field,
			Column:   ix.Column,
			GoField:  ix.GoField,
			Accessor: ix.Accessor,
			Optional: ix.Optional,
			Doc:      indexDoc(r, ix),
		})
	}

	rv.HasEdges = len(r.Outgoing) > 0
	rv.EdgesType = r.Model + "Edges"
	for _, e := range r.Outgoing {
		rv.EdgeWires = append(rv.EdgeWires, edgeWireView{
			Field:       e.ToModel,
			TargetModel: e.ToModel,
			ViaGoField:  e.ViaGoField,
			Doc:         e.Reason,
		})
	}

	if r.TTL > 0 {
		rv.AsideOptions = append(rv.AsideOptions, "cache.TTL("+durationLit(r.TTL)+")")
	}
	if r.Stale > 0 {
		rv.AsideOptions = append(rv.AsideOptions, "cache.Stale("+durationLit(r.Stale)+")")
	}
	return rv
}

// durationLit renders a duration as a Go expression that reads like the
// annotation did: 300s becomes 5*time.Minute, not 300000000000.
func durationLit(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return strconv.FormatInt(int64(d/time.Hour), 10) + "*time.Hour"
	case d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "*time.Minute"
	case d%time.Second == 0:
		return strconv.FormatInt(int64(d/time.Second), 10) + "*time.Second"
	case d%time.Millisecond == 0:
		return strconv.FormatInt(int64(d/time.Millisecond), 10) + "*time.Millisecond"
	default:
		return strconv.FormatInt(int64(d), 10) + "*time.Nanosecond"
	}
}

// goFieldOf is the store's Go field name for a column. It calls the same
// conversion protoc-gen-store's model renderer does, which is why "author_id"
// lands on "AuthorID" here and in the model without either side knowing about
// the other.
func goFieldOf(column string) string {
	if column == "" {
		return "ID"
	}
	return naming.PascalGo(column)
}

// joinLiterals concatenates a namespace's parts, which for an unscoped resource
// are all literals. A scope part would be a bug reaching here, so it is dropped
// rather than rendered as an empty segment that would look like a valid name.
func joinLiterals(parts []spec.NamespacePart) string {
	var lits []string
	for _, p := range parts {
		if p.Literal != "" {
			lits = append(lits, p.Literal)
		}
	}
	return strings.Join(lits, ".")
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

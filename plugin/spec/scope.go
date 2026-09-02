package spec

// scope.go binds the parent segments of a resource pattern into its namespace,
// and holds the one refusal this generator has.
//
// Everything here exists because of a single asymmetry: a cache answers before
// the store is consulted. Every row-level guard the store applies runs inside the
// loader, and the loader runs only on a miss — so on a hit, the store's scoping
// does not run at all, and the only thing separating one tenant from another is
// whether the key was different. Binding is what makes it different.

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"

	"google.golang.org/genproto/googleapis/api/annotations"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/cachev1"
	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1"
)

// bindScope resolves every parent segment of the resource pattern to a binding,
// and appends any field forced into scope that no segment names.
func bindScope(ir *protokit.IR, t *schema.Table, r *Resource, opts *cachepbv1.CacheOptions) ([]ScopeBinding, error) {
	fields := scopeFields(ir, t)
	used := map[string]bool{} // column -> consumed by a segment

	var out []ScopeBinding
	for _, segment := range t.Parents {
		if f, ok := matchSegment(segment, fields, used); ok {
			used[f.column] = true
			out = append(out, ScopeBinding{
				Segment:     segment,
				GoField:     f.goField,
				Column:      f.column,
				FromPattern: true,
			})
			continue
		}
		// A namespace template may bind a segment on its own, for a resource
		// whose parent is not a column at all.
		if strings.Contains(opts.GetNamespace(), "{"+segment+"}") {
			out = append(out, ScopeBinding{
				Segment:     segment,
				GoField:     naming.PascalGo(segment),
				FromPattern: true,
			})
			continue
		}
		return nil, unboundSegment(r, segment, fields)
	}

	// Fields forced into scope that no parent segment claimed. These are the
	// "force this field into the namespace" case: a tenant column that is not
	// part of the AIP pattern but is still what separates one caller's entries
	// from another's.
	var extra []ScopeBinding
	for _, f := range fields {
		if used[f.column] {
			continue
		}
		extra = append(extra, ScopeBinding{
			Segment: f.column,
			GoField: f.goField,
			Column:  f.column,
		})
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Column < extra[j].Column })
	return append(out, extra...), nil
}

// scopeField is one field carrying (cache.v1.cache_scope).
type scopeField struct {
	column string
	// goField is the store model's field for this column, resolved through the
	// same helper protoc-gen-store uses so a foreign key lands on the same name
	// in both trees.
	goField string
	// segment is the explicitly declared segment, or "" to be inferred.
	segment string
	// refType is the field's google.api.resource_reference type, if any.
	refType string
}

// scopeFields collects the table's scope-annotated columns, in column order.
func scopeFields(ir *protokit.IR, t *schema.Table) []scopeField {
	var out []scopeField
	for _, c := range t.Columns {
		if c.Node == "" {
			continue // a synthesized column carries no annotation
		}
		f, ok := protokit.Facet[*cachev1.FieldFacet](ir, cachev1.Key, c.Node)
		if !ok || f.Scope == nil {
			continue
		}
		out = append(out, scopeField{
			column:  c.Name,
			goField: GoFieldOf(c),
			segment: f.Scope.GetSegment(),
			refType: referenceType(c),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].column < out[j].column })
	return out
}

// matchSegment finds the scope field binding one parent segment.
//
// Explicit wins over inferred, always: a declared segment is the author saying
// which parent this is, and a name-based guess that disagreed with it would be
// the generator quietly overruling the schema.
func matchSegment(segment string, fields []scopeField, used map[string]bool) (scopeField, bool) {
	for _, f := range fields {
		if !used[f.column] && f.segment == segment {
			return f, true
		}
	}
	for _, f := range fields {
		if used[f.column] || f.segment != "" {
			continue
		}
		if f.column == segment || f.column == segment+"_id" {
			return f, true
		}
		// A resource_reference naming the parent's type is the strongest
		// inference available: "bookstore.v1/Author" for segment "author".
		if leaf := refLeaf(f.refType); leaf != "" && strings.EqualFold(leaf, naming.PascalGo(segment)) {
			return f, true
		}
	}
	return scopeField{}, false
}

// referenceType returns a column's (google.api.resource_reference) type, or "".
func referenceType(c *schema.Column) string {
	if c.Source == nil || !proto.HasExtension(c.Source.Options(), annotations.E_ResourceReference) {
		return ""
	}
	ref, ok := proto.GetExtension(c.Source.Options(), annotations.E_ResourceReference).(*annotations.ResourceReference)
	if !ok || ref == nil {
		return ""
	}
	if ref.GetType() != "" {
		return ref.GetType()
	}
	return ref.GetChildType()
}

// refLeaf is the resource name from a type ("bookstore.v1/Author" → "Author").
func refLeaf(refType string) string {
	if i := strings.LastIndex(refType, "/"); i >= 0 {
		return refType[i+1:]
	}
	return refType
}

// unboundSegment is the generator's one refusal.
//
// The message has to do real work: whoever hits it is usually not the person who
// wrote the pattern, and "unbound parent segment" alone tells them nothing about
// why a cache generator cares. So it names the resource, quotes the pattern, says
// what the consequence is, and shows the annotation that fixes it — using the
// column that is most likely the right one, when there is a plausible candidate.
func unboundSegment(r *Resource, segment string, fields []scopeField) error {
	var b strings.Builder
	fmt.Fprintf(&b, "cache: %s: the resource pattern %q nests this resource under {%s}, "+
		"but no field binds that segment into the cache namespace.\n\n", r.Node, r.Pattern, segment)

	fmt.Fprintf(&b, "A cache answers before the store is consulted, so on a hit the store's row-level\n"+
		"scoping never runs. With {%s} unbound, every parent's entries share one\n"+
		"keyspace and the first hit serves whichever was cached first — one tenant reading\n"+
		"another's rows. This is the one thing this generator refuses to emit.\n\n", segment)

	b.WriteString("Bind it on the field that carries the parent's id:\n\n")
	fmt.Fprintf(&b, "\tstring %s_id = N [(cache.v1.cache_scope) = {}];\n\n", segment)

	if len(fields) > 0 {
		var have []string
		for _, f := range fields {
			have = append(have, f.column)
		}
		fmt.Fprintf(&b, "Fields already marked (cache.v1.cache_scope) on this message: %s.\n"+
			"None of them binds {%s} — a field binds a segment when its column is %q or %q,\n"+
			"when its (google.api.resource_reference) names the parent's type, or when it says\n"+
			"so outright with (cache.v1.cache_scope) = {segment: %q}.\n",
			strings.Join(have, ", "), segment, segment, segment+"_id", segment)
	}
	return fmt.Errorf("%s", b.String())
}

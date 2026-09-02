package generator

// view_doc.go builds the doc comments the generated file carries.
//
// They are assembled here, not in the template, for the same reason every other
// string is: a comment that has to be correct about a TTL or a scope is reading
// the same spec fields the code does, and keeping the two next to each other is
// what stops them drifting apart.

import (
	"fmt"

	"github.com/the-protobuf-project/cache/plugin/spec"
)

func resourceDoc(r *spec.Resource) []string {
	out := []string{
		fmt.Sprintf("%s caching: %s over %s.", r.Model, r.Strategy, r.Table),
	}
	if r.Pattern != "" {
		out = append(out, fmt.Sprintf("Resource pattern: %s", r.Pattern))
	}
	switch {
	case r.TTL > 0 && r.Stale > 0:
		out = append(out, fmt.Sprintf("Entries live %s and may be served up to %s past that while they refresh.", r.TTL, r.Stale))
	case r.TTL > 0:
		out = append(out, fmt.Sprintf("Entries live %s; readers block on the loader when one expires.", r.TTL))
	default:
		out = append(out, "No TTL: entries persist until invalidated, which for a read-through cache means it keeps every id it has ever loaded. Set (cache.v1.cache).ttl unless that is deliberate.")
	}
	return out
}

func scopeDoc(b spec.ScopeBinding) string {
	if b.FromPattern {
		return fmt.Sprintf("%s is the {%s} segment of the resource pattern. Every key this decorator reads or writes is namespaced by it, which is what keeps one %s's entries unreachable from another's.",
			b.GoField, b.Segment, b.Segment)
	}
	return fmt.Sprintf("%s is the %s column, forced into the namespace by (cache.v1.cache_scope).", b.GoField, b.Column)
}

func indexDoc(r *spec.Resource, ix spec.IndexField) string {
	if !ix.Unique {
		return fmt.Sprintf("filed under %q, which nothing marks unique — every %s sharing a value lands in one key.", ix.Field, r.Model)
	}
	return fmt.Sprintf("filed under %q.", ix.Field)
}

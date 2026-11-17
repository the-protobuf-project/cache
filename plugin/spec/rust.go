package spec

// rust.go holds the refusals only Rust output has.
//
// Every refusal in the rest of this package applies to both languages, because
// both render the same spec: a parent segment left unbound is refused before
// either renderer runs. Rust adds one, and it is the same refusal from the
// other side.
//
// Go's decorator is constructed per scope: the caller hands it the scope values
// and it builds one namespace for its lifetime. A Rust repository serves every
// parent at once and is asked for a record by its resource name alone, so the
// decorator derives the namespace from that name on every call. A parent
// segment is in the name. A field forced into scope that is *not* a segment of
// the pattern is not: on a get, nothing in hand says what its value is. Caching
// such a resource anyway would mean either a namespace without that value --
// every tenant's entries in one keyspace, the failure the scope exists to
// prevent -- or reading the record to learn which namespace to read it from.

import "fmt"

// CheckRust returns the first reason sp cannot be rendered for Rust, or nil.
func CheckRust(sp *Spec) error {
	for _, db := range sp.Databases {
		for _, s := range db.Schemas {
			for _, r := range s.Resources {
				if err := checkRustResource(r); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkRustResource(r *Resource) error {
	for _, b := range r.Namespace.Scope {
		if b.FromPattern {
			continue
		}
		return fmt.Errorf("cache: %s: lang=rust: the field %q is bound into the cache namespace by "+
			"(cache.v1.cache_scope), but it is not a segment of the resource pattern %q.\n\n"+
			"A Rust repository is asked for a record by its resource name alone, so the generated "+
			"decorator derives the namespace from the name on every call. {%s} is not in the name: "+
			"on a get nothing says what its value is, and a namespace without it puts every %s's "+
			"entries in one keyspace -- one tenant reading another's rows. This is the one thing "+
			"this generator refuses to emit.\n\n"+
			"Make it a parent segment of the pattern, or generate this resource's cache for Go only.",
			r.Node, b.Column, r.Pattern, b.Segment, b.Segment)
	}
	if r.Pattern == "" && len(r.Namespace.Scope) > 0 {
		return fmt.Errorf("cache: %s: lang=rust: the cache namespace is scoped, but the resource has "+
			"no google.api.resource pattern to read the scope out of a record's name with", r.Node)
	}
	if r.NameField == "" {
		return fmt.Errorf("cache: %s: lang=rust: the message has no resource name field (\"name\", or "+
			"the google.api.resource name_field); the generated decorator reads a record's name from it", r.Node)
	}
	return nil
}

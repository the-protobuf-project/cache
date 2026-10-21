package spec

// list.go derives the spec for a resource whose List pages are cached:
// (cache.v1.cache).list.
//
// It is a separate path from buildResource, not a strategy of it, because the
// two caches share nothing but a backend. A record cache sits over the store a
// record is loaded from, under a namespace that binds every parent segment of
// the pattern; a list cache sits over the repository a page is listed from,
// and binds the parent by putting the whole parent name into every key it
// writes. So the tenancy refusal in namespace.go -- the one thing that
// generator will not emit -- cannot arise here: two parents' pages are under
// two parents' names by construction.

import (
	"fmt"
	"strings"
	"time"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/cachev1"
	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1"
)

// ListResource is one resource whose List pages are cached.
type ListResource struct {
	// Node is the resource's proto message, which the renderer resolves to its
	// generated Go type.
	Node schema.NodeID

	// Model is the repository's name for the resource (Table.LocalName), which
	// names its interface: <Model>Repository.
	Model string

	// ResourceType and Pattern are the AIP resource type and its first pattern.
	ResourceType string
	Pattern      string

	// Collections are the pattern's collection segments in order
	// ("organizationalUnits", "reservations"). A written record's name is
	// matched against them to find its parent.
	Collections []string

	// Parented reports a pattern with a parent: List takes the parent name.
	// It follows from the pattern exactly as protoc-gen-store's repository
	// target decides it, so the decorator's List has the interface's
	// signature.
	Parented bool

	// TTL is how long a page may be served.
	TTL time.Duration

	// Namespace is the fixed namespace pages are cached under: the record
	// cache's base name with ".list" after it, so the two never share keys.
	Namespace string
}

// buildList returns the list spec for one table, or nil when its pages are
// not cached.
func buildList(ir *protokit.IR, t *schema.Table) (*ListResource, error) {
	opts, ok := protokit.Facet[*cachepbv1.CacheOptions](ir, cachev1.Key, t.Node)
	if !ok || !opts.GetList().GetEnabled() {
		return nil, nil
	}
	if t.Source == nil {
		return nil, fmt.Errorf("cache: %s has no proto message behind it, so it has no List to cache", t.Name)
	}
	res := resourceDescriptor(t.Source)
	patterns := res.GetPattern()
	if len(patterns) == 0 {
		return nil, fmt.Errorf("cache: %s caches its list but has no google.api.resource pattern; "+
			"a list is listed under a parent the pattern names", t.Source.FullName())
	}
	collections, err := patternCollections(patterns[0])
	if err != nil {
		return nil, fmt.Errorf("cache: %s: %w", t.Source.FullName(), err)
	}

	ttl := durationOf(opts.GetList().GetTtl(), fileDefaults(ir, t.Source.ParentFile()).GetTtl())
	if ttl <= 0 {
		return nil, fmt.Errorf("cache: %s caches its list with no ttl.\n\n"+
			"A page is dropped when a write through the generated repositories reaches its parent. "+
			"A write that does not go through them -- SQL by hand, another service on the same "+
			"table -- is seen by nothing, and ttl is then the only thing that ends a stale page. "+
			"Set (cache.v1.cache).list.ttl, or the file's (cache.v1.cache_defaults).ttl.",
			t.Source.FullName())
	}

	r := &ListResource{
		Node:         t.Node,
		Model:        t.LocalName,
		ResourceType: res.GetType(),
		Pattern:      patterns[0],
		Collections:  collections,
		Parented:     len(collections) > 1,
		TTL:          ttl,
	}
	r.Namespace = baseName(&Resource{ResourceType: r.ResourceType, Model: t.ModelName}, t) + ".list"
	return r, nil
}

// patternCollections returns a pattern's collection segments, refusing a
// shape the generated repositories do not serve: anything but
// collection/{var} pairs, such as an AIP-156 singleton.
func patternCollections(pattern string) ([]string, error) {
	segs := strings.Split(pattern, "/")
	if len(segs)%2 != 0 {
		return nil, fmt.Errorf("the pattern %q is not collection/{id} pairs; only those have a "+
			"generated repository List to cache", pattern)
	}
	var out []string
	for i := 0; i < len(segs); i += 2 {
		v := segs[i+1]
		if segs[i] == "" || strings.ContainsAny(segs[i], "{}") || !strings.HasPrefix(v, "{") || !strings.HasSuffix(v, "}") {
			return nil, fmt.Errorf("the pattern %q is not collection/{id} pairs; only those have a "+
				"generated repository List to cache", pattern)
		}
		out = append(out, segs[i])
	}
	return out, nil
}

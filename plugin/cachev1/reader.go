// Package cache is protoc-gen-cache's reader over the cache.v1 vocabulary, and
// the entry point a generator embedding this plugin registers.
//
// It mirrors the shape of store's entity reader deliberately, because the two are
// the same kind of thing: a FacetReader over an annotation module, in the
// repository that owns that module. protokit imports neither.
package cachev1

// reader.go is the schema.FacetReader over cache.v1.
//
// Note what it is *not*: a schema.StructureReader. That omission is the single
// most load-bearing decision in this file, so it is worth stating rather than
// leaving to be inferred from an absent method set.
//
// Structure is what protokit acts on while building — the database, the schema,
// the table, the column, the primary key. cache.v1 has opinions about none of
// them. It says which resources are cached and how; it cannot say what anything
// is called. So this reader contributes facets and stops, and the consequence is
// that golden.IRAgreement between protoc-gen-store and protoc-gen-cache is not a
// property anyone has to maintain: this plugin has no mechanism by which it could
// move a name, because the interface through which names are moved is one it does
// not implement.
//
// The neutral names still have to come from somewhere, and they come from
// entity.Reader() — imported from the store repository, not reimplemented here.
// See Readers below for why that import is the whole guarantee.

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/schema"
	"github.com/the-protobuf-project/store/plugin/entity"

	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1"
)

// Key is the facet key cache.v1 is registered under. It names the vocabulary,
// not the plugin — a target reading these values names this string.
//
// It sorts after "entity.v1", which is the order that matters: the neutral
// vocabulary is resolved first and this one can only ever annotate what it
// decided.
const Key = "cache.v1"

// Reader returns the reader for cache.v1: a protokit.FacetReader supplying the
// per-node cache configuration this plugin's target reads back.
//
// It is stateless. cache.v1 is read straight off the descriptors, with no config
// and no per-run state, so the zero value is the whole thing.
func Reader() protokit.FacetReader { return reader{} }

type reader struct{}

// Compile-time proof of what this reader is — and, by omission, of what it is
// not. There is no schema.StructureReader assertion here because implementing
// that interface is how a vocabulary gains the ability to move a neutral name,
// and cache.v1 must not have it. If someone adds ReadTable to this type, this
// file compiles and the invariant is gone; TestReaderIsNotAStructureReader in
// reader_test.go is what actually catches that.
var _ schema.FacetReader = reader{}

// Key namespaces this reader's facets.
func (reader) Key() string { return Key }

// Readers returns the reader set a protoc-gen-cache build registers: the shared
// neutral vocabulary, then this plugin's own.
//
// entity.Reader() is imported from github.com/the-protobuf-project/store/plugin/entity
// rather than reimplemented, and that import is not a convenience — it is the
// mechanism by which two plugins agree on what a table is called.
//
// protokit reads no annotation module, so nothing in the engine enforces the
// agreement. Two plugins derive the same names because they run the same code
// over the same options. A second implementation of this vocabulary would agree
// with the first right up until the first edge case — an empty schema override, a
// resource whose plural is irregular, an id strategy interacting with a
// field_behavior — and then it would disagree silently, and a decorator generated
// here would address rows that protoc-gen-store never persisted under that name.
// That is the failure the whole split exists to prevent, and importing is what
// prevents it.
func Readers() []protokit.FacetReader {
	return []protokit.FacetReader{
		entity.Reader(),
		Reader(),
	}
}

// The schema.FacetReader methods follow. Each returns (nil, nil) for an
// unannotated node, which costs no map entry and is the normal result: most
// nodes in a schema are not cached.

// ReadFile returns the file's (cache.v1.cache_defaults), or nil.
func (reader) ReadFile(d protoreflect.FileDescriptor) (any, error) {
	if d == nil || !proto.HasExtension(d.Options(), cachepbv1.E_CacheDefaults) {
		return nil, nil
	}
	return proto.GetExtension(d.Options(), cachepbv1.E_CacheDefaults).(*cachepbv1.CacheDefaults), nil
}

// ReadMessage returns the message's (cache.v1.cache), or nil.
func (reader) ReadMessage(d protoreflect.MessageDescriptor) (any, error) {
	if d == nil || !proto.HasExtension(d.Options(), cachepbv1.E_Cache) {
		return nil, nil
	}
	return proto.GetExtension(d.Options(), cachepbv1.E_Cache).(*cachepbv1.CacheOptions), nil
}

// FieldFacet carries both field-level annotations for one field.
//
// They are two extensions rather than one (see annotations.proto) but one facet,
// because a facet is keyed by node and a field is one node. Either half may be
// nil; a field carrying neither produces no facet at all.
type FieldFacet struct {
	// Index is set when the field carries (cache.v1.cache_index).
	Index *cachepbv1.IndexOptions

	// Scope is set when the field carries (cache.v1.cache_scope).
	Scope *cachepbv1.ScopeOptions
}

// ReadField returns the field's cache.v1 annotations as a *FieldFacet, or nil
// when it carries none.
func (reader) ReadField(d protoreflect.FieldDescriptor) (any, error) {
	if d == nil {
		return nil, nil
	}
	opts := d.Options()
	hasIndex := proto.HasExtension(opts, cachepbv1.E_CacheIndex)
	hasScope := proto.HasExtension(opts, cachepbv1.E_CacheScope)
	if !hasIndex && !hasScope {
		return nil, nil
	}

	f := &FieldFacet{}
	if hasIndex {
		f.Index = proto.GetExtension(opts, cachepbv1.E_CacheIndex).(*cachepbv1.IndexOptions)
	}
	if hasScope {
		f.Scope = proto.GetExtension(opts, cachepbv1.E_CacheScope).(*cachepbv1.ScopeOptions)
	}
	return f, nil
}

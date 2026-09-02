package cachev1

// storev1.go is the *optional* half of what this plugin reads: store.v1's
// per-column `unique`.
//
// It is optional in the precise sense the manifest means by facets.optional_reads
// — a build without store.v1 generates less, not wrong. Every function here
// returns a (value, known) pair rather than a bare bool, and every caller is
// written to do something sensible with known == false. A proto annotated only in
// entity.v1 and cache.v1 is a supported input.
//
// # Why a cache generator cares whether a column is unique
//
// Two reasons, and only the second is about correctness.
//
// The first is advice. runtime-go's Indexed contract is blunt about where it
// stops scaling: "an index on something low-cardinality — a tenant, a status, a
// version — puts every entry sharing that value into one key, which is the case
// where this stops scaling first and does it quietly." A unique column cannot be
// that case, because one value maps to at most one id. So `unique` is the one
// signal available at generation time that distinguishes a sound index from the
// one that will quietly become a hot key, and it is worth a diagnostic.
//
// The second is that protoc-gen-store emits a typed finder per unique column
// (GetByName, GetByISBN). Those are reads, so the decorator has to decide what to
// do with each one, and "is this column unique?" is exactly the question that
// decides whether a lookup can be served from a secondary index at all.
//
// # Why this reads the annotation rather than the IR
//
// schema.Column.Unique exists and is populated — by store's *Enricher*, which
// runs only in a protoc-gen-store build. This plugin registers entity.Reader()
// and its own, not store's, so in a protoc-gen-cache build that field is always
// false. Reading the annotation directly is not a shortcut around the IR; it is
// the only place the answer exists here.

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/store/plugin/pb/storepbv1"
)

// ColumnUnique reports whether store.v1 marks this field's column UNIQUE.
//
// known is false when the field carries no (store.v1.column) at all — which
// covers both "store.v1 is not in this build" and "this particular field says
// nothing". The two are deliberately not distinguished: a caller that must
// degrade cleanly degrades the same way for either.
func ColumnUnique(d protoreflect.FieldDescriptor) (unique, known bool) {
	if d == nil || !proto.HasExtension(d.Options(), storepbv1.E_Column) {
		return false, false
	}
	o, ok := proto.GetExtension(d.Options(), storepbv1.E_Column).(*storepbv1.ColumnOptions)
	if !ok || o == nil {
		return false, false
	}
	return o.GetUnique(), true
}

package cachev1

import (
	"testing"

	"github.com/the-protobuf-project/protokit/schema"
)

// TestReaderIsNotAStructureReader pins the omission that carries this plugin's
// half of golden.IRAgreement.
//
// A StructureReader is how a vocabulary tells protokit what things are called.
// cache.v1 must never be one: it says how a resource is cached, and a plugin that
// could also move a table name could generate a decorator addressing rows
// protoc-gen-store persisted somewhere else.
//
// The agreement test would eventually catch that, but only for names some case's
// protos actually exercise, and only as a confusing diff. This catches it as a
// one-line failure naming the rule, at the moment the method is added.
func TestReaderIsNotAStructureReader(t *testing.T) {
	if _, ok := Reader().(schema.StructureReader); ok {
		t.Fatal("cache.Reader() implements schema.StructureReader.\n\n" +
			"cache.v1 must not supply structure. A StructureReader decides neutral names —\n" +
			"database, schema, table, column — and this plugin's whole claim to agreeing with\n" +
			"protoc-gen-store over the same protos is that it has no way to decide one.\n\n" +
			"If a cache option genuinely needs to change what something is called, it does not\n" +
			"belong in cache.v1; it belongs in entity.v1, where every plugin reads it through\n" +
			"the one shared reader.")
	}
}

// TestReaderKeySortsAfterEntity pins the resolution order the reader set depends
// on. protokit consults readers in sorted Key order, so "cache.v1" sorting after
// "entity.v1" is what makes the neutral vocabulary authoritative and this one
// merely additive. It is true by spelling today, which is exactly the kind of
// thing that stops being true when a key is renamed.
func TestReaderKeySortsAfterEntity(t *testing.T) {
	const entityKey = "entity.v1"
	if Key >= entityKey {
		t.Errorf("Key = %q, which does not sort before %q — protokit resolves readers in "+
			"sorted Key order, and the neutral vocabulary must be resolved first", Key, entityKey)
	}
}

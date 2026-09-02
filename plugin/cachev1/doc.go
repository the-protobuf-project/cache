// Package cachev1 is the reader over the cache.v1 vocabulary, and the plugin's
// declaration of itself.
//
// It is the layer that touches proto descriptors and nothing else: it reads
// annotations off them and hands back values. It decides nothing — which
// resources are cached under which strategy is spec's job, and rendering is
// generator's — so nothing here can fail on a schema, only on a descriptor that
// does not contain what it claims to.
//
//	reader.go     the FacetReader over cache.v1, and the reader set a build
//	              registers (this one plus store's shared entity.Reader)
//	storev1.go    the optional store.v1 read: one field, ColumnOptions.unique
//	manifest.go   plugin.yaml, parsed as protokit's schema plus one key protokit
//	              does not have
//
// # The one thing to understand before changing anything here
//
// This package must never implement schema.StructureReader.
//
// A StructureReader is how a vocabulary tells protokit what things are *called* —
// database, schema, table, column. cache.v1 says how a resource is cached, and
// the generated decorator addresses rows protoc-gen-store persisted. If this
// plugin could move a name, it could generate a decorator caching under a name
// the store never wrote, and nothing downstream would catch it.
//
// So the agreement between the two plugins is not maintained, it is structural:
// this plugin has no mechanism by which it could disagree. TestReaderIsNotA
// StructureReader pins that, because the golden and agreement tests would only
// catch a violation on names some case happens to exercise.
//
// The neutral names still have to come from somewhere, and they come from
// entity.Reader() — imported from the store repository, never reimplemented. See
// Readers in reader.go for why that import is the whole guarantee.
package cachev1

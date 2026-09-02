// protoc-gen-cache: the cache.v1 vocabulary, its reader, and the generator that
// emits typed read-through decorators over protoc-gen-store's generated stores.
//
// The dependency set is deliberately small and is worth reading as a statement of
// what this plugin is allowed to know:
//
//	protokit              the neutral IR engine. Pinned, not floated: the whole
//	                      claim of golden.IRAgreement is that two plugins on one
//	                      engine version derive one set of names.
//	store                 two things, one module. plugin/entity is the *shared*
//	                      neutral reader — imported, never reimplemented, see
//	                      plugin/cachev1/reader.go for why a second implementation
//	                      would silently undo the guarantee. plugin/pb/storepbv1 is
//	                      the store.v1 stubs, read optionally to learn which
//	                      columns are unique.
//
//	                      plugin/entity used to be a module of its own and is not
//	                      any more; the store repository folded it into the root.
//	                      The import path did not change, so nothing here did
//	                      either beyond dropping the vestigial requirement. See
//	                      docs/boundary-findings.md, finding 5.
//
//	                      Depending on the root module for one boolean is heavier
//	                      than it should be; see finding 3.
//
// Notably absent: runtime-go/cache. This plugin *emits* code that imports the
// runtime; it does not link against it. Only examples/ does.
//
// Everything here resolves from the module proxy. That was not true until
// recently and is worth recording: store is now tagged as
// github.com/the-protobuf-project/store v1.5.1, under its own module path, so
// this repository no longer needs a workspace to build. See
// docs/boundary-findings.md, finding 5.
//
// One requirement must never appear below: github.com/the-protobuf-project/store/plugin/entity.
// It was a nested module once, and old commits of it are still resolvable, so
// adding it puts two modules in the graph that both contain plugin/entity and
// every build fails with `ambiguous import`. The package comes from the store
// root module now. Require store, never the nested path.
//
module github.com/the-protobuf-project/cache

go 1.26.5

require (
	github.com/the-protobuf-project/protokit v1.2.1
	github.com/the-protobuf-project/store v1.5.1
	google.golang.org/genproto/googleapis/api v0.0.0-20260720211330-0afa2a65878a
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/agnivade/levenshtein v1.2.1 // indirect
	github.com/bufbuild/protocompile v0.14.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/the-protobuf-project/telemetry/telemetry-go v0.0.0-20260817061725-884f94d7858d // indirect
	github.com/vektah/gqlparser/v2 v2.5.36 // indirect
	golang.org/x/sync v0.21.0 // indirect
)

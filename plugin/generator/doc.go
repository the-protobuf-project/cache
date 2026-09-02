// Package generator renders the typed read-through decorators, and assembles the
// protokit.Plugin that drives them.
//
// One Go file per schema, at <out>/<database>/<schema>cache/cache.go. The layout
// mirrors protoc-gen-store's gorm output file for file, and the store package's
// name comes from the same naming.GoPackage call, so the generated import of it
// is correct by construction rather than by a convention two repositories would
// have to keep in step.
//
//	generator.go      the plugin: target registry, reader set, layout
//	config.go         cache.yaml — which is entity.LayoutConfig, shared with store
//	provenance.go     the banner, carrying the full version lock
//	target.go         the schema.Target: spec in, files out
//	view*.go          the spec rendered down to strings the template writes
//	templates/        one partial per thing the generated file contains
//
// # Nothing here writes into a path protoc-gen-store owns
//
// The decorator is a new package that imports the store's generated tree. That is
// exactly the seam the store's own generated doc comment describes:
//
//	It exists so a decorator — caching, tracing, retries, a test double — can be
//	written in its own package against this contract, without that package needing
//	to edit anything in this tree.
//
// # Why every string is decided in Go rather than in a template
//
// A template that computed a Go identifier could emit output the spec never
// authorized, and the one thing this generator must never do quietly is cache
// under a namespace nobody validated. So the view files decide all of it, where
// it can be tested without rendering, and the templates stay transcriptions.
package generator

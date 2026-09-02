// Package cache is the root of protoc-gen-cache. It holds no code — only the
// repository's own boundary test, and this map of where everything lives.
//
// The layout follows the convention the plugins in this project use: all
// generator Go code under plugin/, the annotation protos under protobuf/, and a
// plugin.yaml at the root declaring what the plugin is.
//
// protoc-gen-store is the one to read alongside this. It shares the layout and,
// more importantly, the architecture: it drives protokit.Build, registers readers
// over its own vocabulary, and ships a plugin.yaml. A reader who knows that plugin
// knows this one.
//
// protoc-gen-mcp shares the directory layout and nothing below it. It is a plain
// protogen plugin — it imports protokit/naming and protokit/factory as utilities,
// but it builds no IR, registers no reader, and has no manifest. That is a
// reasonable choice for what it does (it generates from services and methods, not
// from a persisted schema), and it is worth knowing before treating it as a model
// for a plugin on this SPI. As of today protoc-gen-cache is the only second-party
// consumer of the StructureReader/FacetReader seam, which is what makes
// TestIRAgreement the load-bearing test it is.
//
//	protobuf/cache/v1/          the cache.v1 vocabulary — the only thing published
//	    annotations.proto           the four extensions (52000-52003)
//	    cache.proto                 the option messages and the Strategy enum
//
//	plugin/                     everything that runs at generation time
//	    cmd/protoc-gen-cache/       the binary: flags in, protokit.RunPlugin out
//	    pb/cachepbv1/               generated stubs for cache.v1
//	    cachev1/                    the vocabulary: reader, optional store.v1
//	                                read, and the plugin.yaml parser
//	    spec/                       what the generator decided, before rendering.
//	                                Everything that can fail, fails here
//	    generator/                  the renderer: plugin assembly, config,
//	                                provenance, the target, and templates/
//
//	examples/                   its own module: protos, generated output for both
//	                            plugins, a runnable Redis program, and the tests
//	                            that prove the generated guards fire
//
//	docs/boundary-findings.md   every place the plugin boundary was thinner than
//	                            it looked, and how each was worked around without
//	                            editing another repository
//
// # Why the split between cachev1, spec and generator
//
// They fail differently, and separating them is what keeps each failure legible.
//
// cachev1 reads annotations and cannot fail on anything but a malformed
// descriptor. spec decides — strategy, namespace, scope, indexes — and is where
// every refusal lives, so an error message there has the proto in hand and no
// template context to confuse it. generator renders, and by the time it runs
// there is nothing left to check, which is what keeps the templates free of
// validation.
//
// The dependency runs one way: generator → spec → cachev1. Nothing imports back.
package cache

// Command protoc-gen-cache is a protoc plugin that reads proto descriptors
// annotated with google.api.*, entity.v1.* and cache.v1.* options, then generates
// typed read-through cache decorators over protoc-gen-store's generated stores.
//
// It is the second plugin on protokit's StructureReader SPI, and it is built
// without a single change to the store repository — which is the point of it as
// much as the caching is. It imports store's entity reader (never reimplementing
// it), reads store.v1 optionally for what it can learn about unique columns, and
// writes only into its own output tree.
//
// # Install
//
//	go install github.com/the-protobuf-project/cache/plugin/cmd/protoc-gen-cache@latest
//
// # Usage via buf.gen.yaml
//
//	plugins:
//	  - local: protoc-gen-cache
//	    out: generated/cache
//	    opt:
//	      - store_module=github.com/me/gen/gorm
//
// # What it generates, and what it deliberately does not
//
// It selects a strategy per resource, binds index fields, derives the namespace,
// and emits typed wiring. It does not generate key templates —
// runtime-go/cache/core/keyspace.go owns key layout, and a second implementation
// of it would drift from the runtime's on the first change. It does not implement
// singleflight, negative caching, TTL enforcement, index maintenance or
// stale-while-revalidate; all of those are already in core.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/header"

	"github.com/the-protobuf-project/cache/plugin/generator"
)

// version is the build version, injected at release time via
// -ldflags "-X main.version=...".
var version = "dev"

// resolveVersion returns the build version stamped into generated files. A
// release sets `version` via ldflags and wins outright; otherwise it is recovered
// from the build info the Go toolchain embeds. Only a genuine local build
// (reported as "(devel)") falls back to "dev".
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, dep := range bi.Deps {
		if dep.Path == "github.com/the-protobuf-project/cache" && dep.Version != "" {
			return dep.Version
		}
	}
	return version
}

func main() {
	v := resolveVersion()

	// When invoked directly with -version (not by protoc), print and exit before
	// protogen tries to read a CodeGeneratorRequest from stdin.
	if len(os.Args) == 2 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Printf("protoc-gen-cache %s\n", v)
		return
	}

	header.SetTool("protoc-gen-cache")

	var flags flag.FlagSet
	storeModule := flags.String("store_module", "",
		"Go import path of protoc-gen-store's gorm output directory (e.g. github.com/me/gen/gorm); "+
			"the generated decorator imports the store interfaces from under it")
	configPath := flags.String("config", "",
		"path to a cache.yaml mapping proto packages to databases/schemas")
	strict := flags.String("strict", "",
		"per-rule severity for schema problems: \"\"=all warn, \"true\"=all error, "+
			"or \"ref:error,collision:warn,index:error,lint:warn\"")

	protogen.Options{ParamFunc: flags.Set}.Run(func(p *protogen.Plugin) error {
		// Proto3 `optional` is fully supported (presence is read via
		// field_behavior, not synthetic oneofs); declare it so buf/protoc do not
		// warn.
		p.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

		cfg, err := generator.LoadConfig(*configPath)
		if err != nil {
			return err
		}

		// The same constructor the golden harness uses, so a real run and a test
		// run differ in their inputs and in nothing else.
		//
		// The dependency versions are resolved here rather than inside the
		// renderer, because build info describes what *this process* linked
		// against — a fact about an invocation. Resolving it at render time made
		// every golden file record the build environment that produced it.
		return protokit.RunPlugin(p,
			protokit.Options{Target: generator.TargetName, Strict: *strict, Version: v},
			generator.Plugin(*storeModule, generator.ResolveVersions(v), cfg.Layout()))
	})
}

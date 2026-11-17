package generator

// target_rust.go is the lang=rust target: one cache.rs per proto package,
// wrapping protoc-gen-store's Rust repositories (lang=rust target=repository).
//
// It renders the same spec the Go target does, so every refusal in spec applies
// to it unchanged, plus the one only Rust has (spec.CheckRust). What it emits
// is data, not a decorator: the decorator is runtime-rs's
// database::repository::cached, written once, and each resource contributes a
// Plan naming its keyspace, strategy, leases, index fields, edges and list
// cache, with one plain function per proto field the decorator reads.

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/header"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/spec"
)

// RustTargetName is the target lang=rust selects.
const RustTargetName = "rustcache"

// RustModules are the Rust module paths the generated cache.rs names. Each may
// hold "{package}", replaced by the proto package as a module path
// ("bookstore.v1" -> "bookstore::v1").
type RustModules struct {
	// Repository is where protoc-gen-store's Rust repository target put the
	// package's Repositories struct (rust_repository_module).
	Repository string

	// Proto is where the package's prost types are (rust_proto_module).
	Proto string
}

// The defaults: a crate with a `repository` module and a `pb` module, each
// mirroring the proto package tree.
const (
	DefaultRustRepositoryModule = "crate::repository::{package}"
	DefaultRustProtoModule      = "crate::pb::{package}"
)

func (m RustModules) withDefaults() RustModules {
	if m.Repository == "" {
		m.Repository = DefaultRustRepositoryModule
	}
	if m.Proto == "" {
		m.Proto = DefaultRustProtoModule
	}
	return m
}

var rustTmpl = template.Must(template.New("cache.rs.tmpl").ParseFS(templates, "templates/cache.rs.tmpl"))

// RustTarget renders the Rust cache wiring.
type RustTarget struct {
	Modules  RustModules
	Versions Versions
}

// NewRust returns the lang=rust target.
func NewRust(mods RustModules, versions Versions) *RustTarget {
	return &RustTarget{Modules: mods.withDefaults(), Versions: versions}
}

// Name is the target's registry key.
func (*RustTarget) Name() string { return RustTargetName }

// Generate refuses the facet-less path, as the Go target does.
func (*RustTarget) Generate(*protogen.Plugin, []*schema.Database) error {
	return fmt.Errorf("cache: the rustcache target needs the full IR to read cache.v1 facets; " +
		"drive it with protokit.Build and GenerateIR")
}

// GenerateIR builds the spec and renders one cache.rs per proto package with
// a cached record or list.
func (t *RustTarget) GenerateIR(p *protogen.Plugin, ir *protokit.IR) error {
	sp, err := spec.Build(ir)
	if err != nil {
		return err
	}
	if err := spec.CheckRust(sp); err != nil {
		return err
	}
	files, err := buildRustFiles(sp, t.Modules)
	if err != nil {
		return err
	}
	for _, f := range files {
		f.Banner = RenderFor("//", header.Info{
			ProtocVersion: protocVersion(p),
			Source:        SourceLine(f.sources(ir)),
			Database:      strings.Join(f.databases, ", "),
			Schema:        strings.Join(f.schemas, ", "),
		}, t.Versions, rustRuntime)
		var buf bytes.Buffer
		if err := rustTmpl.Execute(&buf, f); err != nil {
			return fmt.Errorf("cache: render %s cache.rs: %w", f.Package, err)
		}
		rel := path.Join(strings.ReplaceAll(f.Package, ".", "/"), "cache.rs")
		if _, err := p.NewGeneratedFile(rel, "").Write(buf.Bytes()); err != nil {
			return fmt.Errorf("cache: write %s: %w", rel, err)
		}
	}
	return nil
}

// rustRuntime is the banner's runtime line for Rust output.
const rustRuntime = "github.com/the-protobuf-project/runtime-rs (tpp-database repository::cached)"

// sources returns the protos behind the file's schemas, for the banner.
func (f *rustFileView) sources(ir *protokit.IR) []string {
	seen := map[string]bool{}
	var out []string
	for _, pair := range f.pairs {
		for _, p := range sourceProtos(ir, pair[0], pair[1]) {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

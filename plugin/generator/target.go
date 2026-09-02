package generator

// target.go is the schema.Target: it builds the spec, renders one file per schema
// that has cached resources, and writes nothing else.

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"path"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/header"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/spec"
)

// TargetName is the buf.gen.yaml `target` opt this plugin answers to.
const TargetName = "gocache"

// templates holds the rendering, split by what each partial emits:
//
//	cache.go.tmpl      the file: banner, package, imports, and the composition
//	preamble.go.tmpl   package-level types shared by every resource
//	asserts.go.tmpl    the construction-time guards, emitted only when needed
//	namespace.go.tmpl  per resource: namespace, scope, Open<X>Cache
//	decorator.go.tmpl  per resource: the struct, constructor and methods
//	indexes.go.tmpl    per resource: refile plus the typed index accessors
//
// One partial per thing the generated file contains, rather than one file for all
// of it. The split is not only length: a partial has a name, so a change to how
// indexes are filed is a diff in indexes.go.tmpl and nowhere else.
//
// Each per-resource partial is invoked with the resource alone, so everything it
// needs is on resourceView — including a copy of the store package's name. A
// partial cannot reach the enclosing file view, and threading a wrapper type
// through every one of them would cost more than repeating one string.
//
//go:embed templates/*.tmpl
var templates embed.FS

// tmpl is parsed once. A parse failure is a bug in this package rather than in
// anyone's protos, so it panics at init rather than surfacing as a generation
// error someone might read as being about their schema.
var tmpl = template.Must(
	template.New("cache.go.tmpl").ParseFS(templates, "templates/*.tmpl"))

// Target renders the cache decorators.
type Target struct {
	// StoreModule is the Go import path of protoc-gen-store's gorm output
	// directory. The decorator cannot name the interface it wraps without it,
	// which is why generation fails rather than guessing when it is unset.
	StoreModule string

	// Versions are the module versions stamped into every banner. They are
	// supplied rather than resolved here so that rendering is a pure function of
	// its inputs — see provenance.go for what reading them at render time cost.
	Versions Versions
}

// New returns a target rendering against the store output at storeModule.
func New(storeModule string, versions Versions) *Target {
	return &Target{StoreModule: storeModule, Versions: versions}
}

// Name is the value of the buf.gen.yaml `target` opt this target answers to.
func (*Target) Name() string { return TargetName }

// Generate satisfies schema.Target for the facet-less path. It cannot do the job
// — every decision this generator makes comes from a cache.v1 facet — so it says
// so rather than emitting an empty tree that looks like "nothing was cached".
func (*Target) Generate(*protogen.Plugin, []*schema.Database) error {
	return fmt.Errorf("cache: the gocache target needs the full IR to read cache.v1 facets; " +
		"drive it with protokit.Build and GenerateIR")
}

// GenerateIR builds the spec and renders one file per schema that has cached
// resources.
func (t *Target) GenerateIR(p *protogen.Plugin, ir *protokit.IR) error {
	if t.StoreModule == "" {
		return fmt.Errorf("cache: the store_module opt is required: it is the Go import path of " +
			"protoc-gen-store's gorm output (e.g. store_module=github.com/me/gen/gorm), and the " +
			"generated decorator has no way to name the store interface it wraps without it")
	}

	sp, err := spec.Build(ir)
	if err != nil {
		return err
	}

	for _, db := range sp.Databases {
		for _, s := range db.Schemas {
			if err := t.renderSchema(p, ir, db, s); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderSchema writes one schema's cache.go.
func (t *Target) renderSchema(p *protogen.Plugin, ir *protokit.IR, db *spec.Database, s *spec.Schema) error {
	storeImport := path.Join(t.StoreModule, db.Name, s.StoreGoPackage)

	banner := Render("//", header.Info{
		ProtocVersion: protocVersion(p),
		Source:        SourceLine(sourceProtos(ir, db.Name, s.Name)),
		Database:      db.Name,
		Schema:        s.Name,
	}, t.Versions)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, buildFile(banner, storeImport, db.Name, s)); err != nil {
		return fmt.Errorf("cache: render %s/%s: %w", db.Name, s.GoPackage, err)
	}

	// gofmt the result rather than shipping whatever the template produced. A
	// template's whitespace is a template's business; the committed golden is
	// compared byte for byte, and formatting here is what keeps a blank-line
	// change in a partial from rewriting every golden file.
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("cache: %s/%s: generated code does not parse: %w\n\n%s",
			db.Name, s.GoPackage, err, buf.String())
	}

	rel := path.Join(db.Name, s.GoPackage, "cache.go")
	if _, err := p.NewGeneratedFile(rel, "").Write(src); err != nil {
		return fmt.Errorf("cache: write %s: %w", rel, err)
	}
	return nil
}

// sourceProtos returns the distinct proto import paths contributing tables to a
// schema, for the banner's source line. It reads the IR rather than the spec
// because a schema's provenance is every proto behind it, cached or not.
func sourceProtos(ir *protokit.IR, database, schemaName string) []string {
	for _, db := range ir.Databases {
		if db.Name != database {
			continue
		}
		for _, s := range db.Schemas {
			if s.Name == schemaName {
				return s.SourceProtos()
			}
		}
	}
	return nil
}

// protocVersion formats the compiler version from the request, matching
// protoc-gen-go's rendering.
func protocVersion(p *protogen.Plugin) string {
	v := p.Request.GetCompilerVersion()
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("v%d.%d.%d", v.GetMajor(), v.GetMinor(), v.GetPatch())
	if suffix := v.GetSuffix(); suffix != "" {
		s += "-" + suffix
	}
	return s
}

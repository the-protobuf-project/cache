package generator_test

// agreement_test.go holds the acceptance test this whole plugin exists to
// satisfy: protoc-gen-store and protoc-gen-cache, over the same protos, derive
// the same neutral names.
//
// That claim is what makes the generated decorator meaningful at all. The cache
// addresses rows the store persisted; if the two disagreed about a database, a
// schema, a table or a column name — even in one edge case — the decorator would
// cache something the store never wrote under that name, and nothing downstream
// would catch it.
//
// # Why this is tested against the real plugin
//
// store's own agreement test compares itself against a *stub* registering only
// entity.Reader(), because on its first day there was no second plugin to compare
// against. Its comment says as much: "Registering only entity.Reader() is what a
// cache or streams generator's proto source would actually look like on its first
// day."
//
// This is that generator, and it exists, so the comparison is made against
// protoc-gen-store itself rather than against a model of it. The difference is
// not decorative. A stub agrees with entity.Reader() by construction — it *is*
// entity.Reader(). The real plugin also runs store.v1's reader, its compat
// reader, its telemetry reader, and its Enricher, any of which could move a name;
// that is the thing worth proving does not happen.
//
// It is affordable here because this plugin already depends on the store module
// for the store.v1 stubs. Note this file is a _test.go: boundary_test.go's
// TestNoStoreGeneratorImports forbids the *compiled* surface from reaching into
// store's generator, and exempts tests precisely so this comparison can be made.

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/store/plugin/factory/source/proto/backend"
	"github.com/the-protobuf-project/store/plugin/factory/wire"

	"github.com/the-protobuf-project/cache/plugin/generator"
)

// TestIRAgreement requires the two plugins to derive identical neutral facts —
// database, schema, table and column names, primary keys, foreign-key resolution
// — for every golden case.
//
// Facets are not compared. Differing there is the entire point of having two
// plugins: one emits SQL column types, the other emits cache namespaces, and
// neither should be able to move a name while doing it.
func TestIRAgreement(t *testing.T) {
	for _, dir := range cases(t, "cases") {
		t.Run(baseName(dir), func(t *testing.T) {
			assertNonVacuous(t, dir)
			golden.IRAgreement(t, dir, cachePlugin(), storePlugin())
		})
	}
}

// assertNonVacuous is the guard on the guard.
//
// IRAgreement compares two projections and reports their differences, so two
// *empty* projections agree perfectly. A case whose protos stopped compiling into
// tables — a renamed directory, a dropped annotation, a "source" file pointing at
// nothing — would turn the acceptance test for this entire plugin into a test
// that asserts nothing, and it would keep passing.
//
// So the case is required to produce tables and columns before the comparison is
// believed. The numbers are deliberately not pinned: this is a check that the
// comparison had something to compare, not a second golden test.
func assertNonVacuous(t *testing.T, dir string) {
	t.Helper()

	req := golden.BuildRequest(t, dir)
	p, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	pl := cachePlugin()
	ir, err := protokit.Build(p, protokit.Options{}, pl.Readers, pl.Layout)
	if err != nil {
		t.Fatalf("build IR: %v", err)
	}

	tables, columns := 0, 0
	for _, db := range ir.Databases {
		for _, s := range db.Schemas {
			for _, tbl := range s.Tables {
				tables++
				columns += len(tbl.Columns)
			}
		}
	}
	if tables == 0 || columns == 0 {
		t.Fatalf("case %s produced %d table(s) and %d column(s): IRAgreement compares two "+
			"projections, so two empty ones agree and this test would pass while asserting "+
			"nothing. Check the case's protos and its \"source\" file.", dir, tables, columns)
	}
}

// cachePlugin is this plugin, configured as a real run would be.
func cachePlugin() protokit.Plugin {
	return generator.Plugin(storeModule, testVersions, nil)
}

// storePlugin is protoc-gen-store, with its full reader set: entity.v1, its
// compat reader, store.v1 and telemetry.v1.
//
// Both sides get a nil layout, and that is the distinction the LayoutResolver
// split exists to draw. The same protos generated under two different layouts
// *should* produce different database and schema names, because a layout is a
// deployment's choice; the same protos read by two different plugins should not,
// because an annotation is the schema's. Handing both sides the same layout is
// what leaves only the second difference visible — which is the one under test.
func storePlugin() protokit.Plugin {
	reader := backend.New(nil, "example.com/test/gen", true, false, false, false)
	return wire.Plugin(backend.Readers(reader), nil)
}

// baseName is the case directory's leaf, for the subtest name.
func baseName(dir string) string {
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			return dir[i+1:]
		}
	}
	return dir
}

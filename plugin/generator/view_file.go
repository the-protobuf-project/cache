package generator

// view_file.go assembles the per-file half of the view: the imports, the shared
// edge table, and the flags that decide which shared helpers the template emits.

import (
	"sort"
	"strconv"

	"github.com/the-protobuf-project/cache/plugin/spec"
)

func buildFile(banner, storeImport, database string, s *spec.Schema) fileView {
	f := fileView{
		Banner:       banner,
		Package:      s.GoPackage,
		StorePackage: s.StoreGoPackage,
		StoreImport:  storeImport,
		Database:     database,
		Schema:       s.Name,
	}

	for _, e := range s.Edges {
		f.Edges = append(f.Edges, edgeView{
			From:   string(e.From),
			To:     string(e.To),
			Via:    e.ViaColumn,
			Reason: e.Reason,
		})
	}

	for _, r := range s.Resources {
		rv := buildResource(r, s.StoreGoPackage)
		f.NeedsSets = f.NeedsSets || rv.AssertSets
		f.NeedsLeases = f.NeedsLeases || rv.AssertLeases
		f.Resources = append(f.Resources, rv)
	}

	f.StdImports = stdImports(f)
	// No aliases. The store package's name differs from this one's by a suffix
	// chosen for exactly this reason, so both can be named plainly — matching the
	// convention the cache runtime's own example states outright: "No driver, and
	// no alias — a program that caches names the cache and the backend it wants,
	// and nothing else."
	f.Imports = []string{
		`"github.com/the-protobuf-project/runtime-go/cache"`,
		strconv.Quote(storeImport),
	}
	return f
}

// stdImports returns the stdlib imports the rendered file actually uses.
//
// It is computed rather than fixed because an unused import does not compile,
// and a generator that emitted one would fail on exactly the inputs nobody has a
// golden for — a schema with no scoped resource, or none with a TTL.
func stdImports(f fileView) []string {
	need := map[string]bool{
		"context": true, // every constructor and method takes one
		"errors":  true, // nil-argument refusals, and errors.Is on ErrUnsupported
		"fmt":     true, // every assertion message
		// namespaceFuncName is emitted unconditionally by the assertion helpers
		// and calls strings.LastIndex, so this cannot be gated on HasScope the
		// way namespace assembly could: a file whose resources are all unscoped
		// still renders the call and would not compile without the import. The
		// doc comment above anticipated the unused-import direction; this is the
		// missing-import one, and no golden covered it because the only fixture
		// with a cached resource scopes it.
		"strings": true,
	}
	for _, r := range f.Resources {
		if len(r.AsideOptions) > 0 {
			need["time"] = true
		}
	}
	var out []string
	for k := range need {
		out = append(out, strconv.Quote(k))
	}
	sort.Strings(out)
	return out
}

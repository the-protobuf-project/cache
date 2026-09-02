package cache

// boundary_test.go enforces which annotation modules protoc-gen-cache is allowed
// to link against, mirroring protokit's boundary_test.go with the allowlist
// inverted for a plugin rather than for the engine.
//
// protokit's rule has no exemptions: the engine imports no annotation module at
// all. A plugin's rule cannot be that, because a plugin exists to read one. So
// the question here is a different one — *which* vocabularies, and why each.
//
//	cache.v1    this plugin owns it. Obviously permitted.
//	entity.v1   the neutral vocabulary. Permitted, and more than permitted:
//	            importing store's reader rather than reimplementing it is what
//	            makes golden.IRAgreement structural instead of aspirational.
//	store.v1    read optionally, for one field (ColumnOptions.unique). Permitted
//	            because it is a *read* of a vocabulary another plugin owns, which
//	            is the composition this architecture is for.
//
// And nothing else. A third plugin's vocabulary appearing in this list would mean
// protoc-gen-cache had grown an opinion about a domain it does not own — the
// failure mode where "the cache plugin" slowly becomes "the plugin that knows
// about everything", and where two plugins start disagreeing about a value
// neither of them owns.
//
// The rule is easy to state and easy to violate by accident: importing a
// generated stub package is one line, and it compiles. So it is checked here
// rather than documented.

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// apiVersion matches a trailing path segment that is an *API* version rather than
// a Go module major version. The two are spelled identically and mean opposite
// things:
//
//	github.com/acme/entity/gen/entity/v1   an annotation module
//	github.com/vektah/gqlparser/v2         a Go major version — ordinary dependency
//
// Go's semantic import versioning never emits "/v1" and never emits a channel
// suffix, so "v1", "v1beta1" and "v2beta1" are unambiguously API versions.
var apiVersion = regexp.MustCompile(`^(v1|v[0-9]+(alpha|beta)[0-9]*)$`)

// goStubs matches the flattened package name protoc-gen-go's module= option
// produces for a versioned proto package: cache/v1 becomes cachepbv1, and plain
// bindings become somethingpb.
//
// This is deliberately wider than "*/pb/*" and "*/v1", because those two miss the
// case that actually matters. A plugin's stubs are generated the ordinary way —
// cache.v1 compiles to cachepbv1, store's to storepbv1, entity's to entitypbv1 —
// and none has a "pb" path segment or a trailing "/v1".
var goStubs = regexp.MustCompile(`pb(v[0-9]+((alpha|beta)[0-9]*)?)?$`)

// allowedPrefixes are the proto module trees this plugin may import.
//
// The three annotation modules are listed by their exact stub package path rather
// than by repository prefix, which matters for store: allowing
// "github.com/the-protobuf-project/store" wholesale would permit any future
// vocabulary that repository adds, and the point of naming store.v1 is that this
// plugin reads store.v1 and not whatever comes next.
var allowedPrefixes = []string{
	// The protobuf runtime and the AIP annotations — neither belongs to a plugin.
	"google.golang.org/protobuf",
	"google.golang.org/genproto/googleapis/api",

	// This plugin's own vocabulary.
	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1",

	// The neutral vocabulary, and the shared reader over it. Importing this is
	// the mechanism, not a convenience — see Readers in reader.go.
	"github.com/the-protobuf-project/store/plugin/entity",

	// store.v1, read optionally for ColumnOptions.unique.
	"github.com/the-protobuf-project/store/plugin/pb/storepbv1",
}

// TestNoForeignPluginProtoImports walks every non-test Go file in the module and
// fails on any proto-shaped import that is not one of the four above.
//
// Test files are exempt: a test may legitimately construct a descriptor from a
// vocabulary it is exercising, and the invariant is about what this plugin's
// compiled surface depends on.
func TestNoForeignPluginProtoImports(t *testing.T) {
	type violation struct {
		file   string
		imp    string
		lineNo int
	}
	var found []violation

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "examples", "node_modules":
				// testdata holds case fixtures and examples is a separate module;
				// neither is part of this module's compiled surface.
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if !protoShaped(imp) || allowed(imp) {
				continue
			}
			found = append(found, violation{
				file:   filepath.ToSlash(path),
				imp:    imp,
				lineNo: fset.Position(spec.Pos()).Line,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if len(found) == 0 {
		return
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].file != found[j].file {
			return found[i].file < found[j].file
		}
		return found[i].lineNo < found[j].lineNo
	})

	var b strings.Builder
	b.WriteString("protoc-gen-cache imports an annotation module it does not read:\n\n")
	for _, v := range found {
		fmt.Fprintf(&b, "\t%s:%d\timports %s\n", v.file, v.lineNo, v.imp)
	}
	b.WriteString("\nThis plugin may import exactly three vocabularies: cache.v1 (its own),\n" +
		"entity.v1 (the neutral one, through store's shared reader), and store.v1\n" +
		"(optionally, for ColumnOptions.unique).\n\n" +
		"A fourth means this plugin has grown an opinion about a domain it does not own.\n" +
		"If you need a value another plugin's vocabulary carries, read it the way store.v1\n" +
		"is read here — optionally, degrading cleanly when it is absent — and add it to\n" +
		"allowedPrefixes with a comment saying which field and why.\n\n" +
		"See docs/boundary-findings.md.")
	t.Error(b.String())
}

// TestNoStoreGeneratorImports fails if this plugin imports protoc-gen-store's
// *generator* — its targets, its factory, its config.
//
// This is the boundary the whole exercise is about, and it is not covered above:
// none of those paths is proto-shaped, so the import walk would let every one of
// them through. Reaching into store's renderer would be the easy way to make this
// plugin work — the gorm target already knows how to name a model field and
// decide whether a column is a pointer — and doing so would make "zero changes to
// the store repo" true only until the first time store refactored an unexported
// helper.
//
// The two permitted store paths are the vocabulary (plugin/pb/storepbv1) and the
// shared neutral reader (plugin/entity). Both are stable, published surfaces that
// exist to be depended on. Everything else in that repository is store's own
// business.
func TestNoStoreGeneratorImports(t *testing.T) {
	const storeRoot = "github.com/the-protobuf-project/store"
	permitted := []string{
		storeRoot + "/plugin/entity",
		storeRoot + "/plugin/pb/",
	}

	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "examples", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil || !strings.HasPrefix(imp, storeRoot) {
				continue
			}
			ok := false
			for _, p := range permitted {
				if imp == strings.TrimSuffix(p, "/") || strings.HasPrefix(imp, p) {
					ok = true
					break
				}
			}
			if !ok {
				found = append(found, fmt.Sprintf("\t%s:%d\timports %s",
					filepath.ToSlash(path), fset.Position(spec.Pos()).Line, imp))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if len(found) == 0 {
		return
	}
	sort.Strings(found)
	t.Errorf("protoc-gen-cache reaches into protoc-gen-store's generator:\n\n%s\n\n"+
		"Only two paths in the store repository are for other plugins to import:\n\n"+
		"\t%s/plugin/entity      the shared neutral reader\n"+
		"\t%s/plugin/pb/...      the store.v1 vocabulary\n\n"+
		"Everything else there is store's own implementation. Depending on it would make\n"+
		"\"zero changes to the store repo\" hold only until store next refactored — and the\n"+
		"break would surface here, as a compile error in a plugin its authors do not build.\n\n"+
		"If you need a rule store's renderer implements (how a column becomes a Go field,\n"+
		"whether it is a pointer), restate it and let the example's compile be the alarm.\n"+
		"See spec.GoFieldOf and spec.isPointer.",
		strings.Join(found, "\n"), storeRoot, storeRoot)
}

// protoShaped reports whether an import path looks like a proto module.
func protoShaped(path string) bool {
	segs := strings.Split(path, "/")
	last := segs[len(segs)-1]

	if apiVersion.MatchString(last) || goStubs.MatchString(last) {
		return true
	}
	return slices.Contains(segs[:len(segs)-1], "pb")
}

// allowed reports whether a proto-shaped import is one this plugin may depend on.
func allowed(path string) bool {
	for _, p := range allowedPrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// TestBoundaryPatterns pins the classifier itself. The walk above is only as good
// as what protoShaped recognizes, and a test that silently stopped matching
// anything would pass forever while enforcing nothing.
func TestBoundaryPatterns(t *testing.T) {
	violations := []string{
		"github.com/the-protobuf-project/web3/protobuf/web3pbv1",
		"github.com/the-protobuf-project/streams/plugin/pb/streamspbv1",
		"github.com/the-protobuf-project/opentelementry/opentelementry-go/protobuf/telemetry/v1/telemetrypbv1",
		"github.com/acme/entity/gen/entity/v1",
		"github.com/acme/entity/gen/pb/entity",
		"example.com/thing/gen/thingpb",
		"example.com/api/v2beta1",
	}
	for _, imp := range violations {
		t.Run(imp, func(t *testing.T) {
			if !protoShaped(imp) {
				t.Errorf("protoShaped(%q) = false, want true", imp)
			}
			if allowed(imp) {
				t.Errorf("allowed(%q) = true, want false", imp)
			}
		})
	}

	permitted := []string{
		"google.golang.org/protobuf/compiler/protogen",
		"google.golang.org/protobuf/reflect/protoreflect",
		"google.golang.org/genproto/googleapis/api/annotations",
		"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1",
		"github.com/the-protobuf-project/store/plugin/entity",
		"github.com/the-protobuf-project/store/plugin/pb/storepbv1",
	}
	for _, imp := range permitted {
		t.Run(imp, func(t *testing.T) {
			if protoShaped(imp) && !allowed(imp) {
				t.Errorf("allowed(%q) = false, want true", imp)
			}
		})
	}

	// Ordinary imports are not proto-shaped at all, so they never reach the
	// allowlist and cannot be whitelisted into it by accident. A Go module major
	// version is the case that matters: "/v2" is how every Go dependency past v1
	// is spelled, and flagging it would make the gate unusable.
	for _, imp := range []string{
		"strings",
		"github.com/the-protobuf-project/protokit",
		"github.com/the-protobuf-project/protokit/schema",
		"github.com/the-protobuf-project/runtime-go/cache",
		"github.com/vektah/gqlparser/v2",
		"gopkg.in/yaml.v3",
	} {
		if protoShaped(imp) {
			t.Errorf("protoShaped(%q) = true, want false — that is not an annotation module", imp)
		}
	}
}

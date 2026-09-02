package generator_test

// errors_test.go pins the generator's refusals.
//
// These assert on the *message*, not merely that an error occurred, and that is
// the whole point of the file. A test that only checks `err != nil` passes when
// the refusal fires for the wrong reason, passes when the message stops naming
// the resource, and passes when someone replaces a considered explanation with
// "invalid configuration". The unbound-parent refusal in particular is read by
// people who did not write the proto and have no reason to know why a cache
// generator cares about an AIP pattern — so what it says is the feature.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"

	"github.com/the-protobuf-project/cache/plugin/generator"
)

func TestGenerationRefusals(t *testing.T) {
	for _, tc := range []struct {
		// dir is the case under testdata/errors.
		dir string

		// why states what the refusal is for, quoted into a failure so a broken
		// test explains itself.
		why string

		// wants are substrings the message must contain. Each one is a claim
		// about what a reader needs: which resource, what the consequence is,
		// and how to fix it.
		wants []string
	}{
		{
			dir: "unbound_parent",
			why: "a nested resource whose parent segment is not bound into the cache namespace",
			wants: []string{
				// The resource, so the reader knows where to look.
				"tenancy.v1.Document",
				// The pattern and the segment, quoted from their proto.
				"users/{user}/documents/{document}",
				"{user}",
				// The consequence, because "unbound parent segment" alone does
				// not explain why a *cache* generator refuses.
				"before the store is consulted",
				"one tenant reading",
				// The fix, spelled rather than described.
				"(cache.v1.cache_scope)",
			},
		},
		{
			dir: "index_without_indexed",
			why: "a cache_index field on a resource whose strategy maintains no index",
			wants: []string{
				"catalog.v1.Product",
				"sku",
				"STRATEGY_INDEXED",
			},
		},
		{
			dir: "index_non_string",
			why: "a cache_index field on a non-string column",
			wants: []string{
				"metrics.v1.Sample",
				"bucket",
				"keyed by string",
			},
		},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			err := generate(t, filepath.Join("testdata", "errors", tc.dir))
			if err == nil {
				t.Fatalf("generation succeeded, want a refusal: this case is %s", tc.why)
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not mention %q.\n\nThis case is %s, and the message "+
						"is what the person hitting it has to act on.\n\nGot:\n%s", want, tc.why, err)
				}
			}
		})
	}
}

// TestWarningsAreEmittedAndTargeted pins the non-fatal diagnostics.
//
// A warning that stopped being emitted breaks no build and fails no other test —
// it just quietly stops warning, which is the failure mode of every diagnostic
// nobody pins. Both halves are checked: that the advice appears where it should,
// and that it does *not* appear where it should not. A generator that warned on
// every index would pass the first half and be worse than one that never warned,
// because advice nobody can act on is advice everyone learns to skip.
func TestWarningsAreEmittedAndTargeted(t *testing.T) {
	warnings := goldenFile(t, "warnings", "tenancy_db/tenancycache/cache.go")

	// status has no uniqueness guarantee anywhere: the hot-key case.
	if !strings.Contains(warnings, `index on "status"`) {
		t.Errorf("no warning for the index on %q, which nothing marks unique.\n\n"+
			"That is the case runtime-go's Indexed contract singles out as where it "+
			"'stops scaling first and does it quietly'.", "status")
	}
	// token is store.v1-unique, so one value maps to at most one entry.
	if strings.Contains(warnings, `index on "token"`) {
		t.Errorf("warned about the index on %q, which store.v1 marks unique. Warning on a "+
			"sound index is how a diagnostic becomes noise people filter out.", "token")
	}
	// INDEXED with nothing indexed is DOCUMENT plus bookkeeping nobody reads.
	if !strings.Contains(warnings, "no field is marked (cache.v1.cache_index)") {
		t.Error("no warning for an INDEXED resource with no index fields")
	}

	// The bookstore case indexes only a unique column, so it must be clean. This
	// is the guard against a regression that warns unconditionally.
	if bookstore := goldenFile(t, "bookstore", "bookstore_db/bookstorev1cache/cache.go"); strings.Contains(bookstore, "WARNING") {
		t.Errorf("the bookstore output carries a warning, but its only index is on a unique "+
			"column:\n%s", bookstore)
	}
}

// goldenFile reads one file out of a case's committed golden tree.
func goldenFile(t *testing.T, caseName, rel string) string {
	t.Helper()
	path := filepath.Join("testdata", "cases", caseName, "golden", generator.TargetName, rel)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return string(b)
}

// generate runs the real plugin entry point over a case and returns its error.
func generate(t *testing.T, dir string) error {
	t.Helper()
	req := golden.BuildRequest(t, dir)
	p, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	return protokit.RunPlugin(p,
		protokit.Options{Target: generator.TargetName},
		generator.Plugin(storeModule, testVersions, nil))
}

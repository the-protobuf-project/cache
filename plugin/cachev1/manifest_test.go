package cachev1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPluginManifest parses the committed plugin.yaml.
//
// It is worth a test because the file is validated by two decoders that disagree
// about what is legal: protokit's, which rejects requires_capability outright, and
// this repository's, which understands it. Only the second is exercised in
// ordinary use, so nothing else would notice if the round trip through the first
// stopped working — until whatever eventually composes plugins tried to read it.
func TestPluginManifest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "plugin.yaml"))
	if err != nil {
		t.Fatalf("read plugin.yaml: %v", err)
	}
	m, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("plugin.yaml is invalid: %v", err)
	}

	if m.Provides != "cache" {
		t.Errorf("provides = %q, want \"cache\"", m.Provides)
	}

	// The requirement the generated code asserts and the requirement the manifest
	// declares are two statements of one fact, made in different places for
	// different readers — a scheduler reads this one before anything is written,
	// and the constructor's assertion is the only one that sees the driver a
	// process actually wired up. They have to agree.
	if got := m.RequiresCapability["INDEXED"]; len(got) != 1 || got[0] != "core.Sets" {
		t.Errorf("requires_capability.INDEXED = %v, want [core.Sets] — this must match the "+
			"capability the generated constructor asserts for an INDEXED resource", got)
	}
}

// TestManifestRejectsUnknownCapability pins the extra validation this repository
// adds, since protokit cannot do it: a capability name that is not a real core
// interface would otherwise declare a requirement no scheduler could ever match.
func TestManifestRejectsUnknownCapability(t *testing.T) {
	_, err := ParseManifest([]byte(`
provides: cache
requires: [store]
outputs: ['**/*.go']
requires_capability:
  INDEXED: [core.Set]
`))
	if err == nil {
		t.Fatal("accepted core.Set, want a refusal — it is a typo for core.Sets, and a " +
			"requirement nothing will match is worse than no requirement")
	}
	if !strings.Contains(err.Error(), "core.Set") {
		t.Errorf("the error does not quote the offending capability:\n%s", err)
	}
}

// TestManifestRejectsUnknownStrategy is the same argument for the key side: a
// strategy cache.v1 does not have cannot be reached by any resource, so a
// requirement filed under one is dead text that looks live.
func TestManifestRejectsUnknownStrategy(t *testing.T) {
	_, err := ParseManifest([]byte(`
provides: cache
requires: [store]
outputs: ['**/*.go']
requires_capability:
  WRITE_BEHIND: [core.Sets]
`))
	if err == nil {
		t.Fatal("accepted WRITE_BEHIND, want a refusal — the runtime has no write-behind path " +
			"and cache.v1 deliberately cannot express one")
	}
}

// TestManifestStillValidatedByProtokit proves the delegation is real.
//
// The risk this guards is specific: ParseManifest could stop calling
// manifest.Parse — by a refactor, or by someone "simplifying" the double decode —
// and every test above would still pass, because none of them exercises a rule
// protokit owns. A manifest listing a facet as both required and optional is such
// a rule, and this plugin implements none of that checking itself.
func TestManifestStillValidatedByProtokit(t *testing.T) {
	_, err := ParseManifest([]byte(`
provides: cache
requires: [store]
outputs: ['**/*.go']
facets:
  reads: [store.v1]
  optional_reads: [store.v1]
`))
	if err == nil {
		t.Fatal("accepted a facet listed as both required and optional. That is protokit's rule, " +
			"not this plugin's — so ParseManifest is no longer delegating to manifest.Parse, and " +
			"every other manifest rule protokit owns is now unenforced here too")
	}
}

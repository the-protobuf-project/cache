package main

import "testing"

// TestCanonicalVersion: every version a build can report is spelled with a
// leading "v", so a banner reads the same whether a release (goreleaser's bare
// "0.3.1") or a `go install` ("v0.3.1") wrote it.
func TestCanonicalVersion(t *testing.T) {
	for in, want := range map[string]string{
		"0.3.1":       "v0.3.1",
		"v0.3.1":      "v0.3.1",
		"0.3.1+dirty": "v0.3.1+dirty",
		"dev":         "dev",
		"":            "",
	} {
		if got := canonicalVersion(in); got != want {
			t.Errorf("canonicalVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

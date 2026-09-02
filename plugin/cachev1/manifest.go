package cachev1

// manifest.go parses plugin.yaml.
//
// It exists because of a gap rather than a preference, and the gap is worth
// stating precisely so this file can be deleted when it closes.
//
// protokit's manifest package defines what a plugin manifest may contain, and it
// decodes strictly: an unknown key is an error, not a silently ignored line.
// That strictness is right — a manifest is hand-written, rarely read back, and
// `optional_read` for `optional_reads` would otherwise parse cleanly and declare
// nothing. But protokit v1.2.1's schema has no requires_capability field, and this
// plugin needs one: whether a resource can run at all depends on capabilities of a
// driver chosen long after generation, and something scheduling a multi-plugin run
// should be able to refuse a plan before anything is written.
//
// So the manifest is decoded twice. Once here, into a superset that understands
// the extra key; then the remainder is re-encoded and handed to protokit for the
// validation every plugin should share. The re-encode is what keeps this from
// becoming a fork of the schema: nothing in this file validates provides,
// requires, annotations, facets or outputs, and if protokit's rules change, this
// plugin's manifest is held to the new ones without a line changing here.
//
// The right long-term fix is a first-class field upstream. See
// docs/boundary-findings.md, finding 1.

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/the-protobuf-project/protokit/manifest"
)

// Manifest is protokit's manifest plus this plugin's capability requirements.
type Manifest struct {
	// Manifest is everything protokit defines. Inlined so the YAML shape is the
	// standard one with an extra key, not a nested document.
	manifest.Manifest `yaml:",inline"`

	// RequiresCapability maps a strategy name to the runtime-go/cache/core
	// capabilities a resource using it needs of its driver.
	//
	// It is keyed by strategy rather than by resource because that is what is
	// knowable without the protos: which strategies exist, and what each one
	// needs. Which resources use which strategy is a property of a particular
	// build, and belongs in the generated output — where it is, as a
	// construction-time assertion naming the resource.
	RequiresCapability map[string][]string `yaml:"requires_capability"`
}

// ParseManifest decodes and validates plugin.yaml.
//
// Decoding is strict at both layers: an unknown key fails here, and the subset
// protokit owns is validated by protokit.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("cache: manifest: parse: %w", err)
	}

	// Re-encode the protokit half and validate it through protokit, so this
	// plugin is held to the shared rules rather than to a copy of them.
	core, err := yaml.Marshal(m.Manifest)
	if err != nil {
		return nil, fmt.Errorf("cache: manifest: re-encode: %w", err)
	}
	if _, err := manifest.Parse(core); err != nil {
		return nil, err
	}

	if err := m.validateCapabilities(); err != nil {
		return nil, err
	}
	return &m, nil
}

// knownStrategies are the strategy names requires_capability may key on. They
// are cache.v1's enum without its prefix, and the list is closed for the same
// reason the enum is: runtime-go implements four strategies, and a manifest that
// could name a fifth would be declaring requirements nothing will ever check.
var knownStrategies = map[string]bool{
	"ASIDE":    true,
	"INDEXED":  true,
	"DOCUMENT": true,
	"VOLATILE": true,
}

// knownCapabilities are the core capabilities this plugin can require. Listing
// them means a typo — "core.Set" for "core.Sets" — fails here instead of
// declaring a requirement no scheduler will ever match.
var knownCapabilities = map[string]bool{
	"core.Sets":       true,
	"core.Leases":     true,
	"core.SetScanner": true,
	"core.Scanner":    true,
	"core.Bulk":       true,
	"core.Fenced":     true,
}

// validateCapabilities reports every problem it finds in one error, matching
// protokit's own behavior: a manifest is edited by hand and checked in a batch,
// so surfacing one problem per run turns a single fix into several round trips.
func (m *Manifest) validateCapabilities() error {
	var problems []string
	for strategy, caps := range m.RequiresCapability {
		if !knownStrategies[strategy] {
			problems = append(problems, fmt.Sprintf(
				"requires_capability has key %q, which is not a cache.v1 strategy (want ASIDE, INDEXED, DOCUMENT or VOLATILE)",
				strategy))
		}
		if len(caps) == 0 {
			problems = append(problems, fmt.Sprintf(
				"requires_capability.%s lists no capabilities; drop the key or name one", strategy))
		}
		for _, c := range caps {
			if !knownCapabilities[c] {
				problems = append(problems, fmt.Sprintf(
					"requires_capability.%s names %q, which is not a runtime-go/cache/core capability",
					strategy, c))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems) // deterministic problem order
	return fmt.Errorf("cache: manifest: %d problem(s):\n  - %s", len(problems), strings.Join(problems, "\n  - "))
}

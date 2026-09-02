// Package config loads cache.yaml: the naming policy this plugin resolves from
// its own configuration.
//
// The only thing in it today is the layout — which proto packages land in which
// database and schema — and that block is not this plugin's own type. It is
// entity.LayoutConfig, embedded inline, so protoc-gen-store and protoc-gen-cache
// read the same keys with the same meanings through the same code.
//
// That sharing is what makes golden.IRAgreement a fair test. Layout is deployment
// policy: the same protos generated under two different layouts *should* produce
// different database and schema names. If each plugin implemented "match a package
// glob, template a schema name, strip a trailing version" for itself, the two
// would agree until the first edge case and then disagree for a reason that looks
// exactly like an annotation bug.
package generator

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/store/plugin/entity"
)

// Config is cache.yaml.
type Config struct {
	// LayoutConfig is the shared naming policy: datasources, strip_version,
	// dedupe_schema_table. Declared in the entity module, not here.
	entity.LayoutConfig `yaml:",inline"`
}

// Load reads cache.yaml from path. An empty path means "no config", which is a
// valid build: protokit's package-path defaults apply.
//
// Decoding is strict. A config file is hand-written, rarely read back, and its
// keys decide where every table in a build lands — exactly the shape of file where
// `strip_versions` for `strip_version` would go unnoticed while silently changing
// every schema name.
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cache: read config %s: %w", path, err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("cache: parse config %s: %w", path, err)
	}
	return &c, nil
}

// Layout returns the LayoutResolver for c, or nil when there is no config.
//
// It returns entity.Layout — the shared implementation — rather than one of this
// plugin's own. See the package doc.
func (c *Config) Layout() protokit.LayoutResolver {
	if c == nil {
		return nil
	}
	return entity.Layout(&c.LayoutConfig)
}

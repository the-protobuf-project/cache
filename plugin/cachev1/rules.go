package cachev1

// rules.go is cache.v1 for protos that cannot carry it: a schema module owned
// upstream, whose files a consumer may not annotate. A rule in cache.yaml says
// what (cache.v1.cache) would have said, for every resource message its match
// names, and the reader hands it on as the same facet. Nothing downstream can
// tell a rule from an annotation, which is the point: the spec, the namespace
// checks and the capability probes hold a configured resource to exactly what
// they hold an annotated one to.
//
// An annotation, where there is one, wins. A rule is deployment policy laid over
// a schema; an annotation is the schema's own statement, and the more specific
// source wins here as it does for the layout's datasources.

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1"
)

// Rule is one entry of cache.yaml's resources: the (cache.v1.cache) options of
// every resource message Match names.
type Rule struct {
	// Match is a dotted glob over a message's full name: "*" matches one
	// segment, a trailing "**" any remaining ones, anything else literally
	// ("shop.cart.v1.Cart", "shop.*.v1.*", "shop.**"). Only messages carrying
	// (google.api.resource) match; a value object is never cached on its own.
	Match string `yaml:"match"`

	// Enabled is CacheOptions.enabled; unset is true, since a rule exists to
	// cache something. Set false to keep a considered rule switched off.
	Enabled *bool `yaml:"enabled"`

	// Strategy is ASIDE, INDEXED, DOCUMENT or VOLATILE; unset takes the file
	// default, then ASIDE.
	Strategy string `yaml:"strategy"`

	// TTL, Stale and NegativeTTL are Go durations ("5m", "30s").
	TTL         string `yaml:"ttl"`
	Stale       string `yaml:"stale"`
	NegativeTTL string `yaml:"negative_ttl"`

	// Namespace overrides the derived namespace, as CacheOptions.namespace.
	Namespace string `yaml:"namespace"`

	// List caches the resource's List pages, as CacheOptions.list.
	List *ListRule `yaml:"list"`
}

// ListRule is a Rule's list block.
type ListRule struct {
	Enabled bool   `yaml:"enabled"`
	TTL     string `yaml:"ttl"`
}

// Options are the CacheOptions r stands for, or an error naming the key that
// does not parse.
func (r Rule) Options() (*cachepbv1.CacheOptions, error) {
	if r.Match == "" {
		return nil, fmt.Errorf("cache: a resources rule has no match")
	}
	o := &cachepbv1.CacheOptions{Enabled: r.Enabled == nil || *r.Enabled, Namespace: r.Namespace}
	if r.Strategy != "" {
		v, ok := cachepbv1.Strategy_value["STRATEGY_"+strings.ToUpper(r.Strategy)]
		if !ok {
			return nil, fmt.Errorf("cache: rule %q: unknown strategy %q (ASIDE, INDEXED, DOCUMENT or VOLATILE)", r.Match, r.Strategy)
		}
		o.Strategy = cachepbv1.Strategy(v)
	}
	var err error
	if o.Ttl, err = ruleDuration(r.Match, "ttl", r.TTL); err != nil {
		return nil, err
	}
	if o.Stale, err = ruleDuration(r.Match, "stale", r.Stale); err != nil {
		return nil, err
	}
	if o.NegativeTtl, err = ruleDuration(r.Match, "negative_ttl", r.NegativeTTL); err != nil {
		return nil, err
	}
	if r.List != nil {
		ttl, err := ruleDuration(r.Match, "list.ttl", r.List.TTL)
		if err != nil {
			return nil, err
		}
		o.List = &cachepbv1.ListCacheOptions{Enabled: r.List.Enabled, Ttl: ttl}
	}
	return o, nil
}

// ruleDuration parses one duration key of the rule matching match; empty is
// unset.
func ruleDuration(match, key, s string) (*durationpb.Duration, error) {
	if s == "" {
		return nil, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("cache: rule %q: %s: %w", match, key, err)
	}
	return durationpb.New(d), nil
}

// ruleFor returns the options of the first rule matching resource message d,
// or nil.
func ruleFor(rules []Rule, d protoreflect.MessageDescriptor) (*cachepbv1.CacheOptions, error) {
	if len(rules) == 0 || !proto.HasExtension(d.Options(), annotations.E_Resource) {
		return nil, nil
	}
	name := strings.Split(string(d.FullName()), ".")
	for _, r := range rules {
		if matchName(strings.Split(r.Match, "."), name) {
			return r.Options()
		}
	}
	return nil, nil
}

// matchName reports whether the dotted glob pattern matches the name's
// segments.
func matchName(pattern, name []string) bool {
	for i, p := range pattern {
		if p == "**" && i == len(pattern)-1 {
			return len(name) >= i
		}
		if i >= len(name) || (p != "*" && p != name[i]) {
			return false
		}
	}
	return len(pattern) == len(name)
}

package spec

// template.go expands an explicit (cache.v1.cache).namespace, and holds the one
// character check the runtime keyspace imposes on every literal in a namespace.

import (
	"fmt"
	"strings"
)

// expandTemplate turns an explicit (cache.v1.cache).namespace into parts,
// substituting "{segment}" for the matching scope binding.
//
// A "{segment}" naming nothing is an error rather than a literal. The whole
// purpose of a template here is to bind a parent, so a typo in one is a namespace
// that silently stops isolating — the failure this package exists to prevent,
// arriving through the very annotation meant to prevent it.
func expandTemplate(r *Resource, tmpl string, scope []ScopeBinding) ([]NamespacePart, error) {
	bySegment := map[string]ScopeBinding{}
	for _, b := range scope {
		bySegment[b.Segment] = b
	}

	var parts []NamespacePart
	rest := tmpl
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			break
		}
		close := strings.Index(rest[open:], "}")
		if close < 0 {
			return nil, fmt.Errorf("cache: %s: (cache.v1.cache).namespace %q has an unclosed '{'", r.Node, tmpl)
		}
		close += open

		if lit := strings.Trim(rest[:open], "."); lit != "" {
			if err := checkSegment("(cache.v1.cache).namespace", lit); err != nil {
				return nil, err
			}
			parts = append(parts, NamespacePart{Literal: lit})
		}
		name := rest[open+1 : close]
		b, ok := bySegment[name]
		if !ok {
			return nil, fmt.Errorf("cache: %s: (cache.v1.cache).namespace references {%s}, which is neither a "+
				"parent segment of the pattern %q nor a field marked (cache.v1.cache_scope)",
				r.Node, name, r.Pattern)
		}
		parts = append(parts, NamespacePart{ScopeField: b.GoField})
		rest = rest[close+1:]
	}
	if lit := strings.Trim(rest, "."); lit != "" {
		if err := checkSegment("(cache.v1.cache).namespace", lit); err != nil {
			return nil, err
		}
		parts = append(parts, NamespacePart{Literal: lit})
	}
	return parts, nil
}

// checkSegment rejects a literal the runtime's keyspace could not carry.
//
// The ban on ':' is core.CheckNamespace's, restated at generation time so it is
// caught while someone is looking at the annotation rather than at the first
// SetDatabase of a deployment. See runtime-go/cache/core/keyspace.go: the
// character joins the prefix to the name, so allowing one lets two configurations
// that were never meant to meet address the same keys.
func checkSegment(where, lit string) error {
	if strings.Contains(lit, ":") {
		return fmt.Errorf("cache: %s value %q contains ':', which the cache keyspace uses to join "+
			"the prefix to the namespace — two configurations spelled differently would address "+
			"the same keys. Use '.' instead", where, lit)
	}
	return nil
}

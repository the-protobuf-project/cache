package generator

// view_rust.go turns the spec into what cache.rs.tmpl writes. As for Go, every
// Rust identifier and expression is decided here and none in the template.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/the-protobuf-project/cache/plugin/spec"
)

// rustFileView is one proto package's cache.rs.
type rustFileView struct {
	Banner string

	// Package is the proto package ("bookstore.v1").
	Package string

	// RepoModule and ProtoModule are the resolved module paths.
	RepoModule  string
	ProtoModule string
	// ProtoUse is the use of ProtoModule as `pb`.
	ProtoUse string

	// CrateUses are the uses of the prost and repository modules, in the order
	// rustfmt sorts them, so a crate formatting the file sees no diff.
	CrateUses []string

	Keyspaces []rustKeyspace
	Resources []rustResource
	Edges     []edgeView

	// HasRecord, HasIndexes, HasEdges and HasLists say which runtime names the
	// file imports, so it compiles without unused-import warnings.
	HasRecord, HasIndexes, HasEdges, HasLists, HasDuration bool

	// Imports is the runtime names the file uses, sorted as rustfmt sorts them.
	Imports       string
	ImportsInline bool

	// pairs are the (database, schema) pairs behind the file, for the banner.
	pairs     [][2]string
	databases []string
	schemas   []string
}

// rustKeyspace is one resource's record keyspace static.
type rustKeyspace struct {
	Static  string
	Node    string
	Model   string
	Pattern string
	Parts   []string
	// Inline reports that the namespace fits on one line.
	Inline bool
	// Namespace is the namespace spelled out, {segment} for each scope part,
	// for the doc comment.
	Namespace string
	// Foreign marks a keyspace emitted only because an edge in this file
	// targets it; its own package's cache.rs emits it too.
	Foreign bool
}

// rustResource is one wrapped repository field.
type rustResource struct {
	Node     string
	Type     string // pb::Book
	Field    string // books
	PlanFn   string // book_plan
	NameExpr string // |m| m.name.as_str()
	// WrapInline reports that the wrapping call fits on one line.
	WrapInline bool
	Record     *rustRecord
	List       *rustList
	Warnings   []string
}

type rustRecord struct {
	Keyspace string
	Strategy string
	TTL      string
	Stale    string
	Indexes  []rustIndex
	Edges    []rustEdge
}

type rustIndex struct{ Field, Value string }

type rustEdge struct {
	Target string
	Via    string
	Reason string
}

type rustList struct {
	Namespace   string
	Collections string
	Parented    bool
	TTL         string
}

// buildRustFiles groups the spec's resources by proto package.
func buildRustFiles(sp *spec.Spec, mods RustModules) ([]*rustFileView, error) {
	files := map[string]*rustFileView{}
	file := func(pkg, db, schemaName string) *rustFileView {
		f, ok := files[pkg]
		if !ok {
			pkgPath := rustModulePath(pkg)
			f = &rustFileView{
				Package:     pkg,
				RepoModule:  mods.Repository + "::" + pkgPath,
				ProtoModule: mods.Proto + "::" + pkgPath,
			}
			f.ProtoUse = f.ProtoModule + " as pb"
			files[pkg] = f
		}
		pair := [2]string{db, schemaName}
		for _, p := range f.pairs {
			if p == pair {
				return f
			}
		}
		f.pairs = append(f.pairs, pair)
		return f
	}

	// Every cached record resource, by node, so an edge can find its target.
	records := map[string]*spec.Resource{}
	for _, db := range sp.Databases {
		for _, s := range db.Schemas {
			for _, r := range s.Resources {
				records[string(r.Node)] = r
			}
		}
	}

	resources := map[string]map[string]*rustResource{} // package -> node -> resource
	resource := func(pkg, node, message, plural, nameField string) (*rustResource, error) {
		if resources[pkg] == nil {
			resources[pkg] = map[string]*rustResource{}
		}
		if r, ok := resources[pkg][node]; ok {
			return r, nil
		}
		if nameField == "" {
			return nil, fmt.Errorf("cache: %s: lang=rust: the message has no resource name field; "+
				"the generated decorator reads a record's name from it", node)
		}
		r := &rustResource{
			Node:     node,
			Type:     "pb::" + rustTypePath(message),
			Field:    rustPluralField(plural, message),
			PlanFn:   rustSnake(strings.ReplaceAll(message, ".", "_")) + "_plan",
			NameExpr: "|m| m." + rustField(nameField) + ".as_str()",
		}
		resources[pkg][node] = r
		return r, nil
	}

	for _, db := range sp.Databases {
		for _, s := range db.Schemas {
			for _, r := range s.Resources {
				f := file(r.Package, db.Name, s.Name)
				rr, err := resource(r.Package, string(r.Node), r.Message, r.Plural, r.NameField)
				if err != nil {
					return nil, err
				}
				f.Keyspaces = append(f.Keyspaces, keyspaceOf(r, false))
				rr.Warnings = r.Warnings
				rec := &rustRecord{
					Keyspace: keyspaceStatic(r),
					Strategy: rustStrategy(r.Strategy),
					TTL:      rustOptDuration(r.TTL),
					Stale:    rustOptDuration(r.Stale),
				}
				for _, ix := range r.Indexes {
					rec.Indexes = append(rec.Indexes, rustIndex{
						Field: ix.Field,
						Value: rustStrValue(ix.ProtoField, ix.ProtoOptional, ix.Optional),
					})
				}
				for _, e := range r.Outgoing {
					target := records[string(e.To)]
					if target == nil {
						continue
					}
					if target.Package != r.Package {
						f.Keyspaces = append(f.Keyspaces, keyspaceOf(target, true))
					}
					rec.Edges = append(rec.Edges, rustEdge{
						Target: keyspaceStatic(target),
						Via:    rustStrValue(e.ViaProtoField, e.ViaProtoOptional, e.ViaOptional),
						Reason: e.Reason,
					})
					f.Edges = append(f.Edges, edgeView{
						From: string(e.From), To: string(e.To), Via: e.ViaColumn, Reason: e.Reason,
					})
				}
				rr.Record = rec
				f.HasRecord = true
				f.HasIndexes = f.HasIndexes || len(rec.Indexes) > 0
				f.HasEdges = f.HasEdges || len(rec.Edges) > 0
				f.HasDuration = f.HasDuration || r.TTL > 0 || r.Stale > 0
			}
			for _, l := range s.Lists {
				f := file(l.Package, db.Name, s.Name)
				rr, err := resource(l.Package, string(l.Node), l.Message, l.Plural, l.NameField)
				if err != nil {
					return nil, err
				}
				quoted := make([]string, len(l.Collections))
				for i, c := range l.Collections {
					quoted[i] = strconv.Quote(c)
				}
				rr.List = &rustList{
					Namespace:   strconv.Quote(l.Namespace),
					Collections: "&[" + strings.Join(quoted, ", ") + "]",
					Parented:    l.Parented,
					TTL:         rustDuration(l.TTL),
				}
				f.HasLists = true
				f.HasDuration = true
			}
		}
	}

	var out []*rustFileView
	for pkg, f := range files {
		for _, r := range resources[pkg] {
			line := fmt.Sprintf("    repos.%s = cached::wrap_with(repos.%s, cache.clone(), %s(), hooks.clone());",
				r.Field, r.Field, r.PlanFn)
			r.WrapInline = len(line) <= rustMaxWidth
			f.Resources = append(f.Resources, *r)
		}
		names := []string{"DeclaredEdge", "Hooks", "Plan"}
		if f.HasRecord {
			names = append(names, "Keyspace", "Part", "Record", "Strategy")
		}
		if f.HasIndexes {
			names = append(names, "Index")
		}
		if f.HasEdges {
			names = append(names, "Edge")
		}
		if f.HasLists {
			names = append(names, "List")
		}
		sort.Strings(names)
		f.CrateUses = []string{f.ProtoUse, f.RepoModule + "::Repositories"}
		sort.Strings(f.CrateUses)
		f.Imports = "self, " + strings.Join(names, ", ")
		f.ImportsInline = len("use database::repository::cached::{"+f.Imports+"};") <= rustMaxWidth
		sort.Slice(f.Resources, func(i, j int) bool { return f.Resources[i].Node < f.Resources[j].Node })
		sort.SliceStable(f.Keyspaces, func(i, j int) bool { return f.Keyspaces[i].Node < f.Keyspaces[j].Node })
		f.Keyspaces = dedupeKeyspaces(f.Keyspaces)
		sort.Slice(f.Edges, func(i, j int) bool {
			a, b := f.Edges[i], f.Edges[j]
			if a.From != b.From {
				return a.From < b.From
			}
			if a.To != b.To {
				return a.To < b.To
			}
			return a.Via < b.Via
		})
		sort.Slice(f.pairs, func(i, j int) bool {
			return f.pairs[i][0]+"/"+f.pairs[i][1] < f.pairs[j][0]+"/"+f.pairs[j][1]
		})
		seenDB := map[string]bool{}
		for _, p := range f.pairs {
			if !seenDB[p[0]] {
				seenDB[p[0]] = true
				f.databases = append(f.databases, p[0])
			}
			f.schemas = append(f.schemas, p[1])
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })

	// Two resources of one package wrapping one Repositories field would
	// assign it twice; refuse rather than emit code that drops one.
	for _, f := range out {
		fields := map[string]string{}
		for _, r := range f.Resources {
			if other, ok := fields[r.Field]; ok {
				return nil, fmt.Errorf("cache: %s and %s: lang=rust: both are the Repositories field %q "+
					"of %s; give one a distinct google.api.resource plural", other, r.Node, r.Field, f.Package)
			}
			fields[r.Field] = r.Node
		}
	}
	return out, nil
}

// rustMaxWidth is rustfmt's default max_width, which the output keeps to so a
// crate running rustfmt over it sees no diff.
const rustMaxWidth = 100

func dedupeKeyspaces(in []rustKeyspace) []rustKeyspace {
	var out []rustKeyspace
	for _, k := range in {
		if len(out) > 0 && out[len(out)-1].Static == k.Static {
			if !k.Foreign {
				out[len(out)-1] = k
			}
			continue
		}
		out = append(out, k)
	}
	return out
}

func keyspaceStatic(r *spec.Resource) string {
	return strings.ToUpper(rustSnake(strings.ReplaceAll(r.Message, ".", "_"))) + "_KEYSPACE"
}

func keyspaceOf(r *spec.Resource, foreign bool) rustKeyspace {
	bySegment := map[string]string{} // GoField -> segment
	for _, b := range r.Namespace.Scope {
		bySegment[b.GoField] = b.Segment
	}
	k := rustKeyspace{
		Static:  keyspaceStatic(r),
		Node:    string(r.Node),
		Model:   r.Model,
		Pattern: r.Pattern,
		Foreign: foreign,
	}
	var spelled []string
	for _, p := range r.Namespace.Parts {
		if p.ScopeField != "" {
			seg := bySegment[p.ScopeField]
			k.Parts = append(k.Parts, "Part::Segment("+strconv.Quote(seg)+")")
			spelled = append(spelled, "{"+seg+"}")
			continue
		}
		k.Parts = append(k.Parts, "Part::Literal("+strconv.Quote(p.Literal)+")")
		spelled = append(spelled, p.Literal)
	}
	k.Namespace = strings.Join(spelled, ".")
	k.Inline = len("    namespace: &["+strings.Join(k.Parts, ", ")+"],") <= rustMaxWidth
	return k
}

func rustStrategy(s spec.Strategy) string {
	switch s {
	case spec.Indexed:
		return "Strategy::Indexed"
	case spec.Document:
		return "Strategy::Document"
	case spec.Volatile:
		return "Strategy::Volatile"
	default:
		return "Strategy::Aside"
	}
}

// rustStrValue is a closure reading a string field as Option<&str>.
//
// presence is a proto3 `optional` field, which prost renders as Option<String>.
// nullable is Go's reading of the same column: the store renders it as a
// pointer, nil when unset, and Go's decorator skips a nil one. A plain proto
// string has no nil, so the empty string stands in for it.
func rustStrValue(field string, presence, nullable bool) string {
	f := "m." + rustField(field)
	switch {
	case presence:
		return "|m| " + f + ".as_deref().filter(|v| !v.is_empty())"
	case nullable:
		return "|m| Some(" + f + ".as_str()).filter(|v| !v.is_empty())"
	default:
		return "|m| Some(" + f + ".as_str())"
	}
}

func rustOptDuration(d time.Duration) string {
	if d <= 0 {
		return "None"
	}
	return "Some(" + rustDuration(d) + ")"
}

func rustDuration(d time.Duration) string {
	switch {
	case d%time.Second == 0:
		return fmt.Sprintf("Duration::from_secs(%d)", int64(d/time.Second))
	case d%time.Millisecond == 0:
		return fmt.Sprintf("Duration::from_millis(%d)", int64(d/time.Millisecond))
	default:
		return fmt.Sprintf("Duration::from_nanos(%d)", int64(d))
	}
}

// rustModulePath is a proto package as a module path, as prost-build nests it.
func rustModulePath(pkg string) string {
	parts := strings.Split(pkg, ".")
	for i, p := range parts {
		parts[i] = rustIdent(rustSnake(p))
	}
	return strings.Join(parts, "::")
}

// rustTypePath is a message's prost type relative to its package module: a
// nested message lives in a module named after its parent.
func rustTypePath(message string) string {
	parts := strings.Split(message, ".")
	for i := range parts[:len(parts)-1] {
		parts[i] = rustIdent(rustSnake(parts[i]))
	}
	parts[len(parts)-1] = rustUpperCamel(parts[len(parts)-1])
	return strings.Join(parts, "::")
}

// rustField is a proto field's prost field name.
func rustField(name string) string { return rustIdent(rustSnake(name)) }

// rustPluralField is the Repositories field for a resource: its plural, snake
// case, or the message name with an "s" when the resource declares none.
func rustPluralField(plural, message string) string {
	if plural == "" {
		parts := strings.Split(message, ".")
		plural = parts[len(parts)-1] + "s"
	}
	return rustIdent(rustSnake(plural))
}

// rustWords splits an identifier into words the way heck does: at
// underscores, at a lower-to-upper change, and before the last capital of an
// acronym that a lowercase letter follows.
func rustWords(s string) []string {
	var words []string
	var cur []rune
	rs := []rune(s)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}
	for i, r := range rs {
		switch {
		case r == '_' || r == '-' || r == ' ':
			flush()
			continue
		case unicode.IsUpper(r) && len(cur) > 0:
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

func rustSnake(s string) string {
	words := rustWords(s)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, "_")
}

func rustUpperCamel(s string) string {
	words := rustWords(s)
	for i, w := range words {
		rs := []rune(strings.ToLower(w))
		rs[0] = unicode.ToUpper(rs[0])
		words[i] = string(rs)
	}
	return strings.Join(words, "")
}

// rustIdent escapes a keyword as prost does: raw where Rust allows it, and an
// underscore suffix for the four names a raw identifier cannot spell.
func rustIdent(s string) string {
	switch s {
	case "self", "super", "crate", "Self":
		return s + "_"
	case "as", "break", "const", "continue", "else", "enum", "false", "fn", "for", "if", "impl",
		"in", "let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return", "static",
		"struct", "trait", "true", "type", "unsafe", "use", "where", "while", "dyn", "abstract",
		"become", "box", "do", "final", "macro", "override", "priv", "typeof", "unsized",
		"virtual", "yield", "async", "await", "try", "gen":
		return "r#" + s
	}
	return s
}

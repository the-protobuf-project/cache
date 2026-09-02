package spec

// namespace.go derives the namespace a resource's keyspace is built under, and
// refuses to derive one that would not isolate tenants.
//
// This is the only place in the generator that hard-errors, and the reason is
// worth stating plainly rather than leaving in a commit message.
//
// A cache answers before the store is consulted. Every row-level guard the store
// applies — a WHERE clause on a parent id, a policy on a connection, a tenant
// column — runs inside the loader, and the loader runs only on a miss. So on a
// hit, the store's scoping does not run at all, and the only thing standing
// between one tenant and another's rows is whether the key was different. If a
// resource's AIP pattern nests it under a parent and that parent is not bound
// into the namespace, then two tenants' entries share a keyspace and the first
// hit serves the wrong one. Nothing downstream can recover from that: not a
// review, not a test that happens to use one tenant, not a runtime check the
// runtime has no way to make.
//
// Hence a refusal rather than a warning. A warning would be printed into a build
// log next to a hundred others, and the output would still be generated, and it
// would still compile, and it would still be wrong.
//
// # What this does not generate
//
// Keys. runtime-go/cache/core/keyspace.go owns key layout entirely —
// {head}cache:aside:entry:{id} and the rest — and a second implementation of that
// layout would agree with the first until the first change to either. This
// generates the {head}: the namespace the keyspace is built under, handed to
// Provider.SetDatabase, and never a key inside it.

import (
	"strings"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1"
)

// Namespace is a resource's keyspace head, as a sequence of parts to join.
//
// It is a part list rather than a format string because the two things being
// joined have different trust: a literal is decided here, and a scope value
// arrives at runtime from a caller. Keeping them distinct in the model means the
// renderer cannot accidentally interpolate one as the other, and means the
// generated code can validate exactly the parts that need it.
type Namespace struct {
	// Parts are joined with "." in order.
	Parts []NamespacePart

	// Scope is the ordered set of bindings this namespace is parameterized by —
	// one field on the generated scope struct each. Empty for a resource whose
	// pattern has no parent segment and which forces no field into scope.
	Scope []ScopeBinding
}

// NamespacePart is one segment of a namespace: either a literal decided at
// generation time, or the value of a scope binding supplied at construction.
type NamespacePart struct {
	// Literal is set for a fixed segment.
	Literal string

	// ScopeField is set for a segment filled from the scope struct, and names
	// the Go field to read.
	ScopeField string
}

// ScopeBinding is one parent segment (or forced field) bound into the namespace.
type ScopeBinding struct {
	// Segment is the pattern variable this binds ("author" for
	// "authors/{author}/books/{book}"), or the column name for a field forced
	// into scope that no segment names.
	Segment string

	// GoField is the field on the generated scope struct. It is named after the
	// column where there is one, so a caller passes the value it already holds
	// under the name it already knows it by.
	GoField string

	// Column is the neutral column the value comes from, or empty when the
	// binding came from a namespace template rather than a field.
	Column string

	// FromPattern reports whether this binding satisfies a parent segment of the
	// resource pattern (as opposed to a field forced into scope).
	FromPattern bool
}

// buildNamespace derives the namespace for one resource, or fails.
func buildNamespace(ir *protokit.IR, t *schema.Table, r *Resource, opts *cachepbv1.CacheOptions, def *cachepbv1.CacheDefaults) (Namespace, error) {
	scope, err := bindScope(ir, t, r, opts)
	if err != nil {
		return Namespace{}, err
	}

	var parts []NamespacePart
	add := func(lit string) {
		if lit != "" {
			parts = append(parts, NamespacePart{Literal: lit})
		}
	}

	// No prefix is applied here, deliberately. The runtime already has one —
	// cache.Config.Prefix, set where the client is built — and a prefix is a fact
	// about a deployment rather than about a schema. cache.v1 used to carry one
	// too; see the reserved field in cache.proto for why it does not any more.

	if explicit := opts.GetNamespace(); explicit != "" {
		tmpl, err := expandTemplate(r, explicit, scope)
		if err != nil {
			return Namespace{}, err
		}
		parts = append(parts, tmpl...)
	} else {
		add(baseName(r, t))
		// Each binding contributes its segment name and then its value, so
		// "book.author.<id>" cannot be confused with a resource literally named
		// "book.author" — a namespace is compared for equality at construction,
		// and an ambiguous encoding would make that comparison meaningless.
		for _, b := range scope {
			add(b.Segment)
			parts = append(parts, NamespacePart{ScopeField: b.GoField})
		}
	}

	return Namespace{Parts: parts, Scope: scope}, nil
}

// baseName is the namespace's fixed portion, derived from the AIP resource type
// ("bookstore.v1/Book" → "bookstore.v1.book").
//
// It comes from the resource type rather than the table name on purpose. The
// table name is an output — an explicit override, a de-stuttering pass and a
// global-namespace qualification can all move it — while the resource type is
// what the schema author wrote and what every other system addressing this
// resource already agrees on. A namespace that moved when a table was renamed
// would silently orphan a live cache on deploy.
func baseName(r *Resource, t *schema.Table) string {
	if r.ResourceType != "" {
		return strings.ToLower(strings.ReplaceAll(r.ResourceType, "/", "."))
	}
	// A cached message with no google.api.resource is unusual but legal; fall
	// back to coordinates that are still stable under a table rename.
	return strings.ToLower(t.PgSchema + "." + naming.SnakeCase(r.Model))
}

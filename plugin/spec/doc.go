// Package spec is what the generator decided, separated from how it renders.
//
// Everything that can fail, fails here — an unbound parent segment, a namespace
// that cannot be spelled, an index field on a resource that maintains no index.
// By the time the renderer runs there is nothing left to check, which is what
// keeps the templates free of validation and the error messages free of template
// context.
//
//	model.go      the types, and nothing that computes one
//	build.go      walking a built IR into a Spec
//	strategy.go   strategy, leases, and the driver capabilities they imply
//	namespace.go  the keyspace head a resource's entries live under
//	scope.go      binding parent segments — and the one refusal
//	index.go      cache_index fields
//	edges.go      the invalidation edge set
//
// # This is not the IR
//
// protokit's IR describes a schema; this describes a cache over one, and the two
// disagree about what a "resource" is in exactly one way that matters. The IR has
// a table per persisted message; this has an entry only for a message someone
// annotated as cached. A schema whose messages are all uncached produces no spec
// at all, and therefore no file — rather than an empty package that looks like a
// generator that ran and found nothing to say.
//
// # Nothing here decides a name
//
// Every neutral name — database, schema, table, column, primary key — arrives
// already decided on the IR, because this plugin registers no StructureReader and
// so had no way to influence one. What is decided here is strictly cache-shaped:
// strategy, lease, namespace, scope, index, edge.
package spec

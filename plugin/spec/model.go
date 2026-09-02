package spec

// model.go holds the types the rest of the package fills in, and nothing that
// computes one. See doc.go for how the package is arranged.

import (
	"time"

	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"
)

// Strategy is the cache shape a resource is served through. It mirrors
// cache.v1's enum and runtime-go's four strategies, as a local type so the
// renderer never has to name a proto enum.
type Strategy string

// The four strategies runtime-go implements. There is no fifth, and a spec that
// could hold one would be a spec that could describe output nothing will honor.
const (
	Aside    Strategy = "ASIDE"
	Indexed  Strategy = "INDEXED"
	Document Strategy = "DOCUMENT"
	Volatile Strategy = "VOLATILE"
)

// Capability names a runtime-go/cache/core capability a resource's configuration
// requires of whatever driver it is eventually wired to.
//
// The generator cannot know the driver — that is a deployment's choice, made long
// after generation — so a requirement discovered here becomes two things: an
// assertion in the generated constructor, and a line in plugin.yaml for whatever
// eventually schedules a multi-plugin run.
type Capability string

// The capabilities this generator can require. Both are behavioral gates in
// core: without them the strategy does not degrade, it refuses.
const (
	// CoreSets is server-side sets. Without them a backend cannot enumerate and
	// cannot index; memcached has none.
	CoreSets Capability = "core.Sets"

	// CoreLeases is a protocol that reports a remaining TTL. Memcache stores an
	// expiry and honors it but will never say what is left of it, so a stale
	// window cannot be implemented against it at all.
	CoreLeases Capability = "core.Leases"
)

// Spec is one build's decisions, in deterministic order.
type Spec struct {
	Databases []*Database
}

// Database groups schemas exactly as the IR does, so the output tree lines up
// with protoc-gen-store's file for file.
type Database struct {
	Name    string
	Schemas []*Schema
}

// Schema is one output package: the cached resources of one IR schema.
type Schema struct {
	// Name is the IR schema name ("bookstore_v1").
	Name string

	// StoreGoPackage is the store's package name for this schema, from
	// naming.GoPackage — the same function protoc-gen-store's gorm target calls.
	// Sharing it is what makes the generated import of the store's package
	// correct by construction rather than by a convention two repositories would
	// have to remember.
	StoreGoPackage string

	// GoPackage is *this* package's name, and it is deliberately not the same.
	//
	// The obvious choice — matching the store's, since both describe one schema —
	// would make every generated file a package importing another of the same
	// name, which Go can only express with an import alias. One suffix here
	// removes an alias from every generated file and from every consumer that
	// holds both, which is the whole of why it is worth the redundancy.
	GoPackage string

	// Resources are the cached resources, sorted by proto message name.
	Resources []*Resource

	// Edges is every invalidation edge originating in this schema, sorted. It is
	// emitted as data so the blast radius of a write is readable in the output
	// and so a CDC consumer can act on it without regenerating.
	Edges []Edge
}

// Resource is one cached resource: everything the renderer needs and nothing it
// has to re-derive.
type Resource struct {
	// Node is the fully-qualified proto message name, the coordinate every
	// diagnostic is reported against.
	Node schema.NodeID

	// Model is the Go type protoc-gen-store generated ("Book"), and so also the
	// prefix of the interface this decorator implements ("BookStoreIface").
	Model string

	// Table and PKColumn are the neutral names, carried for diagnostics and for
	// the generated doc comments. Nothing here builds a key out of them.
	Table    string
	PKColumn string

	// ResourceType and Pattern are the AIP resource's identity and shape. The
	// pattern is what parent segments are read from, and it is quoted verbatim
	// in the unbound-segment error because that is where the reader has to look.
	ResourceType string
	Pattern      string

	Strategy    Strategy
	TTL         time.Duration
	Stale       time.Duration
	NegativeTTL time.Duration

	// Namespace is the keyspace head this resource's entries live under. The
	// generator emits this and never a key inside it.
	Namespace Namespace

	// Indexes are the cache_index fields, sorted by column.
	Indexes []IndexField

	// Requires are the driver capabilities this configuration needs, sorted.
	Requires []Capability

	// Outgoing are the invalidation edges a write to this resource fires, in the
	// order the decorator fires them. Each becomes a named field on the
	// resource's Edges struct, so wiring one is a decision the caller makes in
	// the open rather than a graph the runtime discovers.
	Outgoing []Edge

	// Warnings are non-fatal diagnostics raised while building this resource.
	Warnings []string
}

// IndexField is one secondary index on an INDEXED resource.
type IndexField struct {
	// Field is the index name in the runtime's keyspace — the column name
	// unless (cache.v1.cache_index).name overrode it.
	Field string

	// Column is the neutral column name.
	Column string

	// GoField is the field on the generated model, for the typed accessor.
	GoField string

	// Accessor is the generated method-name fragment ("ISBN" in ByISBN).
	Accessor string

	// Optional reports whether the store's model renders this column as a
	// pointer, which it does for a nullable scalar. The generated code has to
	// nil-check before filing, so this has to be known here rather than guessed
	// at in a template.
	Optional bool

	// Unique reports whether store.v1 marked the column UNIQUE, and Known
	// whether store.v1 said anything at all. A non-unique index is legal and
	// warned about; an unknown one is neither.
	Unique bool
	Known  bool
}

// GoFieldOf returns the Go struct field protoc-gen-store generates for a column.
//
// It calls the same two protokit helpers the gorm target's gormFieldName does,
// in the same order, rather than approximating them with PascalGo alone. The
// difference shows up exactly on foreign keys: a column named "author" that
// carries an FK becomes AuthorID in the model, and a decorator that guessed
// "Author" would fail to compile against the very output it exists to wrap.
func GoFieldOf(c *schema.Column) string {
	return naming.PascalGo(naming.FKFieldBase(c.Name, c.FKModel != ""))
}

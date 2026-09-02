package generator

// view.go holds the types the templates range over, and nothing that computes
// one. The three files that do are:
//
//	view_file.go      the per-file half: imports, the edge table, helper flags
//	view_resource.go  one resource's identifiers, namespace parts and options
//	view_doc.go       the doc comments the generated file carries

// fileView is one generated file: one schema's cached resources.
type fileView struct {
	Banner string

	// Package is this file's package name, and StorePackage is the name of the
	// store package it imports. They are deliberately different — see
	// spec.Schema.GoPackage — so the import needs no alias and every reference
	// below reads as the package it actually is.
	Package      string
	StorePackage string

	StoreImport string
	Database    string
	Schema      string

	// Imports are the non-stdlib imports, already formatted as import lines.
	Imports []string

	// StdImports are the stdlib imports this file actually uses.
	StdImports []string

	Resources []resourceView
	Edges     []edgeView

	// NeedsSets and NeedsLeases report whether any resource in the file asserts
	// that capability, so the shared helper is emitted only when used.
	NeedsSets   bool
	NeedsLeases bool
}

// resourceView is one cached resource's rendering.
type resourceView struct {
	// Node is the fully-qualified proto name, quoted into every error message
	// the generated constructor can produce.
	Node     string
	Model    string
	Iface    string
	Table    string
	Strategy string

	// StorePackage is the store package's name, repeated from the file view.
	//
	// The repetition is what lets each per-resource template partial be rendered
	// with the resource alone: a partial invoked as {{ template "x" . }} sees only
	// what it was handed, so a field it needs from the enclosing file view is a
	// field it cannot reach. Copying one string is cheaper than threading a
	// wrapper type through every partial.
	StorePackage string

	// Doc is the resource's generated doc comment body, already line-wrapped
	// and prefixed.
	Doc []string

	// PKField is the Go field holding the primary key on the store's model.
	PKField string

	// NamespaceConst is the const name holding the fixed part, when the
	// namespace takes no scope.
	NamespaceConst string
	// NamespaceLiteral is the whole namespace, quoted, for the unscoped case.
	NamespaceLiteral string
	NamespaceExpr    []string // strings.Join parts, as Go expressions
	NamespaceFunc    string
	NamespaceArgs    string // "" or "scope <Model>Scope"
	NamespaceCall    string // "" or "scope"

	HasScope    bool
	ScopeType   string
	ScopeFields []scopeFieldView

	HasEdges  bool
	EdgesType string
	EdgeWires []edgeWireView

	HasIndexes bool
	Indexes    []indexView

	// AssertSets and AssertLeases carry the arguments for the construction-time
	// capability assertions, or are empty when the resource requires neither.
	AssertSets   bool
	SetsProbe    string
	AssertLeases bool

	// Options rendered into the aside calls, e.g. `cache.TTL(120*time.Second)`.
	AsideOptions []string

	// Warnings are rendered into the file as comments beside the resource, so a
	// diagnostic survives the build log it was printed into.
	Warnings []string
}

type scopeFieldView struct {
	GoField string
	Segment string
	Doc     string
}

type indexView struct {
	Field    string
	Column   string
	GoField  string
	Accessor string
	// Optional reports that the store's model field is a pointer, so filing
	// this index has to nil-check before dereferencing.
	Optional bool
	Doc      string
}

type edgeWireView struct {
	// Field is the field on the resource's Edges struct.
	Field string
	// TargetModel is the resource invalidated.
	TargetModel string
	// ViaGoField is the field on this resource's model holding the target id.
	ViaGoField string
	Doc        string
}

type edgeView struct {
	From   string
	To     string
	Via    string
	Reason string
}

// buildFile assembles one schema's view.

package spec

// index.go resolves the cache_index fields of a resource.

import (
	"fmt"
	"sort"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/cachev1"
)

// stringy reports whether a column renders as a Go string in the store's model.
//
// The three types are the ones protokit maps to string: a proto string, and the
// two synthesized surrogate keys. A repeated field is excluded whatever its
// element type — the model renders it as a slice, and a slice has no single
// value to file an entry under.
func stringy(c *schema.Column) bool {
	if c.List {
		return false
	}
	switch c.Type {
	case schema.TypeString, schema.TypeULID, schema.TypeUUID:
		return true
	default:
		return false
	}
}

// isPointer reports whether the store's model renders this column as a pointer.
//
// The gorm target's rule is: a nullable column becomes *T unless its base type is
// already slice-backed ([]byte, json.RawMessage, a pq array), because nil in
// those already encodes NULL. Every column reaching here is string-backed —
// stringy rejected the rest — so the exclusions cannot apply and nullability is
// the whole of it.
//
// The rule is restated rather than imported because it lives in an unexported
// helper inside protoc-gen-store's gorm target, which this plugin has no business
// reaching into. That makes it a duplicated fact, and duplicated facts drift: if
// the store ever renders a nullable string as something other than *string, the
// example stops compiling, which is the cheapest available alarm.
func isPointer(c *schema.Column) bool {
	return c.Optional && stringy(c)
}

// buildIndexes fills r.Indexes from the table's (cache.v1.cache_index) fields,
// and refuses an index the chosen strategy cannot serve.
func buildIndexes(ir *protokit.IR, t *schema.Table, r *Resource) error {
	for _, c := range t.Columns {
		if c.Node == "" {
			continue
		}
		f, ok := protokit.Facet[*cachev1.FieldFacet](ir, cachev1.Key, c.Node)
		if !ok || f.Index == nil {
			continue
		}

		// An index on a strategy with no index is not a preference the generator
		// can quietly ignore: every accessor it emitted would report
		// ErrUnsupported on every backend, which is a compile-time-shaped mistake
		// discovered at runtime.
		if r.Strategy != Indexed {
			return fmt.Errorf("cache: %s: field %q is marked (cache.v1.cache_index), but the resource's "+
				"strategy is %s, which maintains no secondary index.\n\n"+
				"Set strategy: STRATEGY_INDEXED on (cache.v1.cache), or drop the annotation. "+
				"Only INDEXED files entries under a field other than the id.",
				r.Node, c.Name, r.Strategy)
		}

		// The runtime's index is keyed by string: core files an entry under
		// "field=value" members of a set, and there is no conversion anywhere in
		// it. Rendering an int or a timestamp into one here would mean this
		// generator inventing a format the runtime never agreed to — and the
		// first thing to read that index by a hand-written key would disagree
		// with it. So the restriction is stated rather than papered over.
		if !stringy(c) {
			return fmt.Errorf("cache: %s: field %q is marked (cache.v1.cache_index), but its column "+
				"is not string-typed.\n\n"+
				"The cache's secondary index is keyed by string end to end — core files ids under "+
				"a \"field=value\" set member — and this generator will not invent a rendering for "+
				"another type, because anything else reading that index by a hand-written key would "+
				"then have to guess the same one. Index a string, ULID or UUID column.",
				r.Node, c.Name)
		}

		field := f.Index.GetName()
		if field == "" {
			field = c.Name
		}
		storeUnique, storeSaid := cachev1.ColumnUnique(c.Source)

		// Uniqueness has two sources here, and using only one was a mistake worth
		// naming: store.v1's `unique`, and protokit's own AIP inference, which
		// marks a column unique when the resource's IDENTIFIER is demoted to a
		// lookup key (see protokit/keys.go). Reading only store.v1 meant a column
		// that AIP already guarantees unique looked unknown, and the warning
		// below fired essentially never.
		unique := c.Unique || c.PrimaryKey || storeUnique

		r.Indexes = append(r.Indexes, IndexField{
			Field:    field,
			Column:   c.Name,
			GoField:  GoFieldOf(c),
			Accessor: naming.PascalGo(c.Name),
			Optional: isPointer(c),
			Unique:   unique,
			Known:    storeSaid,
		})

		// Advice, not a refusal: a low-cardinality index is legal, sometimes
		// deliberate, and only its author knows the cardinality. But it is the
		// failure runtime-go's Indexed contract singles out as the one that
		// "stops scaling first and does it quietly", so silence would be worse.
		//
		// The condition is "nothing says this is unique" rather than "something
		// says it is not". That warns on a column nobody annotated, which is the
		// point: an index on a column with no uniqueness guarantee anywhere is
		// exactly the one that becomes a hot key, and the fix — confirming it is
		// fine, or marking it unique — is cheap either way.
		if !unique {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"%s: index on %q, which nothing marks unique — neither store.v1 nor the AIP "+
					"annotations. Every entry sharing a value lands in one key, so an index on "+
					"something low-cardinality (a tenant, a status, a version) becomes a hot key "+
					"that every write to that field must reach.",
				r.Node, c.Name))
		}
	}

	sort.Slice(r.Indexes, func(i, j int) bool { return r.Indexes[i].Column < r.Indexes[j].Column })

	// INDEXED with nothing indexed is Document plus the cost of an index nobody
	// reads. Worth saying, because it usually means an annotation was dropped.
	if r.Strategy == Indexed && len(r.Indexes) == 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"%s: strategy is STRATEGY_INDEXED but no field is marked (cache.v1.cache_index). "+
				"That is STRATEGY_DOCUMENT with extra bookkeeping — either index a field or "+
				"change the strategy.", r.Node))
	}
	return nil
}

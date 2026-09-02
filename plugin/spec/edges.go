package spec

// edges.go derives the invalidation edge set: which writes make which other
// resources' cached entries wrong.
//
// The edges are *declared*, not walked. Nothing in the generated output loads a
// graph and follows it at runtime — each edge becomes a named field and a
// straight-line call, and the whole set is emitted as data beside the decorator.
// That is a deliberate trade of flexibility for legibility: the blast radius of a
// write is the thing reviewers most need to see and the thing a runtime walk
// hides best. If writing a Book invalidates an Author, that fact should be
// readable in the generated file, not inferable from a traversal.
//
// Emitting it as data has a second purpose the brief for this plugin names
// directly: a CDC consumer that wants to invalidate from the write-ahead log,
// rather than from the decorator, can read this set without regenerating
// anything.

import (
	"fmt"
	"sort"

	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"
)

// Edge is one invalidation relationship: a write to From makes To's cached
// entries stale, and the id to invalidate is read from From's ViaGoField.
type Edge struct {
	// From is the resource being written.
	From      schema.NodeID
	FromModel string

	// To is the resource whose entries the write invalidates.
	To      schema.NodeID
	ToModel string

	// ViaColumn is the foreign-key column carrying the relationship, and
	// ViaGoField is the field on From's generated model that holds the target's
	// id at the moment of the write.
	ViaColumn  string
	ViaGoField string

	// Reason is a short human-readable justification, rendered into the
	// generated data so the set explains itself without this package.
	Reason string
}

// buildEdges returns the invalidation edges originating in one schema, sorted.
//
// Only edges whose target is itself cached are emitted. An edge to an uncached
// resource is not an omission — there is no entry to invalidate, so declaring one
// would be describing work that does not exist.
func buildEdges(s *Schema, tables map[schema.NodeID]*schema.Table) []Edge {
	cached := map[schema.NodeID]*Resource{}
	for _, r := range s.Resources {
		cached[r.Node] = r
	}

	var out []Edge
	for _, r := range s.Resources {
		t := tables[r.Node]
		if t == nil {
			continue
		}
		for _, fk := range t.ForeignKeys {
			target, ok := cached[nodeOfReference(fk, tables)]
			if !ok || target.Node == r.Node {
				continue
			}
			out = append(out, Edge{
				From:       r.Node,
				FromModel:  r.Model,
				To:         target.Node,
				ToModel:    target.Model,
				ViaColumn:  fk.Column,
				ViaGoField: naming.PascalGo(fk.Column),
				Reason: fmt.Sprintf("%s.%s references %s, so writing a %s changes what that %s preloads",
					r.Model, fk.Column, target.Model, r.Model, target.Model),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].ViaColumn < out[j].ViaColumn
	})
	return out
}

// nodeOfReference resolves a foreign key's target to the proto node it belongs
// to.
//
// The IR names a foreign key's target by generated model and table, not by node,
// because a foreign key is resolved after names are decided. Matching back to a
// node here — rather than carrying the model name forward — keeps the edge set
// keyed by the same coordinate as everything else in this package, which is what
// makes an edge survive a table rename.
func nodeOfReference(fk *schema.ForeignKey, tables map[schema.NodeID]*schema.Table) schema.NodeID {
	// ReferencedProto is set when the target was resolved within this build and
	// is the exact answer.
	if fk.ReferencedProto != "" {
		return schema.NodeID(fk.ReferencedProto)
	}
	// Otherwise fall back to the model name, which is unique within a database.
	for node, t := range tables {
		if t.ModelName == fk.ReferencedModel {
			return node
		}
	}
	return ""
}

package spec

// strategy.go resolves the per-resource cache settings that are pure lookups:
// which of the four runtime strategies, which leases, and which driver
// capabilities those choices imply.
//
// The capability half is the part worth reading twice. A requirement discovered
// here cannot be checked here — the driver is a deployment's choice, made long
// after generation — so it travels two ways: into the generated constructor as an
// assertion, and into plugin.yaml for whatever eventually schedules a run.

import (
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"

	"google.golang.org/genproto/googleapis/api/annotations"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/schema"

	"github.com/the-protobuf-project/cache/plugin/cachev1"
	"github.com/the-protobuf-project/cache/plugin/pb/cachepbv1"
)

// requirements resolves the driver capabilities a resource's configuration needs.
//
// DOCUMENT is here alongside INDEXED, which the brief for this plugin did not
// call for, because core/document.go gates on the same field: Keys and List
// report ErrUnsupported without sets, and enumeration is the whole reason to
// choose DOCUMENT over VOLATILE. Requiring sets for a strategy whose defining
// operation cannot run without them is not scope creep; omitting it would emit a
// decorator whose List always fails on memcached with no warning anywhere.
func requirements(r *Resource) []Capability {
	var out []Capability
	switch r.Strategy {
	case Indexed, Document:
		out = append(out, CoreSets)
	}
	if r.Stale > 0 {
		out = append(out, CoreLeases)
	}
	slices.Sort(out)
	return out
}

// strategyOf resolves the strategy: the resource's own, then the file default,
// then ASIDE.
func strategyOf(o *cachepbv1.CacheOptions, def *cachepbv1.CacheDefaults) Strategy {
	s := o.GetStrategy()
	if s == cachepbv1.Strategy_STRATEGY_UNSPECIFIED {
		s = def.GetStrategy()
	}
	switch s {
	case cachepbv1.Strategy_STRATEGY_INDEXED:
		return Indexed
	case cachepbv1.Strategy_STRATEGY_DOCUMENT:
		return Document
	case cachepbv1.Strategy_STRATEGY_VOLATILE:
		return Volatile
	default:
		return Aside
	}
}

// durationOf takes the resource's value, then the file default, then zero.
func durationOf(v, def *durationpb.Duration) time.Duration {
	if v != nil {
		return v.AsDuration()
	}
	if def != nil {
		return def.AsDuration()
	}
	return 0
}

// fileDefaults returns the file's (cache.v1.cache_defaults), never nil — the
// getters below all tolerate an empty message, so callers need no presence check.
func fileDefaults(ir *protokit.IR, fd protoreflect.FileDescriptor) *cachepbv1.CacheDefaults {
	if fd == nil {
		return &cachepbv1.CacheDefaults{}
	}
	d, ok := protokit.Facet[*cachepbv1.CacheDefaults](ir, cachev1.Key, schema.NodeIDOfFile(fd))
	if !ok || d == nil {
		return &cachepbv1.CacheDefaults{}
	}
	return d
}

// resourceDescriptor returns the message's google.api.resource, never nil.
func resourceDescriptor(md protoreflect.MessageDescriptor) *annotations.ResourceDescriptor {
	if md == nil || !proto.HasExtension(md.Options(), annotations.E_Resource) {
		return &annotations.ResourceDescriptor{}
	}
	r, ok := proto.GetExtension(md.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor)
	if !ok || r == nil {
		return &annotations.ResourceDescriptor{}
	}
	return r
}

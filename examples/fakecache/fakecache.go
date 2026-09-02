// Package fakecache is an in-memory core.Driver whose capabilities are chosen
// per instance.
//
// It exists to test the generated constructors' refusals. Those refusals are
// about what a *backend* can do — whether it has server-side sets, whether its
// protocol reports a remaining lease — and the whole point of them is that they
// fire on backends the developer is not running locally. A test that needed a
// real memcached to prove the memcached path would be a test nobody runs.
//
// core.Build accepts any Driver and resolves its capabilities by type assertion
// at construction, so "a backend without sets" is expressible here as a type that
// simply does not have the methods.
package fakecache

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/the-protobuf-project/runtime-go/cache"
	"github.com/the-protobuf-project/runtime-go/cache/core"
)

// Driver is the base: the eight Driver methods and no optional capability.
//
// This is the memcached-shaped case — no sets, no leases — and it is the one the
// generated assertions have to catch.
type Driver struct {
	name string

	mu     sync.Mutex
	values map[string][]byte
	sets   map[string]map[string]bool
}

// New returns a driver with no optional capabilities.
func New(name string) *Driver {
	return &Driver{
		name:   name,
		values: map[string][]byte{},
		sets:   map[string]map[string]bool{},
	}
}

var _ core.Driver = (*Driver)(nil)

func (d *Driver) Name() string { return d.name }

func (d *Driver) Get(_ context.Context, key string) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.values[key]
	if !ok {
		return nil, core.ErrMiss
	}
	return v, nil
}

func (d *Driver) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.values[key] = value
	return nil
}

func (d *Driver) Add(_ context.Context, key string, value []byte, _ time.Duration) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.values[key]; ok {
		return false, nil
	}
	d.values[key] = value
	return true, nil
}

func (d *Driver) Replace(_ context.Context, key string, value []byte, _ time.Duration) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.values[key]; !ok {
		return false, nil
	}
	d.values[key] = value
	return true, nil
}

func (d *Driver) Delete(_ context.Context, keys ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, k := range keys {
		delete(d.values, k)
	}
	return nil
}

func (d *Driver) Exists(_ context.Context, key string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.values[key]
	return ok, nil
}

func (d *Driver) Touch(context.Context, string, time.Duration) error { return nil }

// Capable is Driver plus sets and leases — the redis-shaped case.
//
// It is a distinct type rather than a flag on Driver because core decides
// capabilities by type assertion. A flag would still assert true and the
// "backend without sets" case would be untestable, which is the case that
// matters.
type Capable struct{ *Driver }

// NewCapable returns a driver implementing core.Sets and core.Leases.
func NewCapable(name string) *Capable { return &Capable{Driver: New(name)} }

var (
	_ core.Sets   = (*Capable)(nil)
	_ core.Leases = (*Capable)(nil)
)

func (d *Capable) SetAdd(_ context.Context, key string, members ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sets[key] == nil {
		d.sets[key] = map[string]bool{}
	}
	for _, m := range members {
		d.sets[key][m] = true
	}
	return nil
}

func (d *Capable) SetRemove(_ context.Context, key string, members ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, m := range members {
		delete(d.sets[key], m)
	}
	return nil
}

func (d *Capable) SetMembers(_ context.Context, key string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := slices.Collect(maps.Keys(d.sets[key]))
	slices.Sort(out)
	return out, nil
}

// TTL reports a fixed remaining lease. The value is not the point — that the
// method exists is, since that is what core asserts for.
func (d *Capable) TTL(_ context.Context, key string) (time.Duration, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.values[key]; !ok {
		return 0, core.ErrMiss
	}
	return time.Minute, nil
}

// Open builds a *cache.DB over driver, namespaced under name.
//
// It goes through core.Build — the same constructor every real backend uses — so
// the DB under test differs from a production one only in which driver is behind
// it.
func Open(driver core.Driver, name string) *cache.DB {
	db := core.Build(driver, core.Spec{Namespace: name})
	return db
}

// Keys returns every key written, sorted, for a test that wants to see what a
// decorator actually did.
func (d *Driver) Keys() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := slices.Collect(maps.Keys(d.values))
	slices.Sort(out)
	return out
}

// HasKeyContaining reports whether any key contains sub — used to assert that a
// scoped decorator's keys carry the scope, without this test asserting a key
// layout core owns.
func (d *Driver) HasKeyContaining(sub string) bool {
	for _, k := range d.Keys() {
		if strings.Contains(k, sub) {
			return true
		}
	}
	return false
}

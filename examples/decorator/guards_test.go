package decorator_test

// guards_test.go covers the two guarantees that exist only at construction time.
//
// Both are about deployment facts — which backend is behind the cache, which
// namespace the database was opened under — so neither can be checked at
// generation time, and both would otherwise be discovered in production or not at
// all. Each has a positive twin: a guard that refused everything would pass the
// negative test and be useless.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/the-protobuf-project/runtime-go/cache"

	"github.com/the-protobuf-project/cache/examples/fakecache"
	"github.com/the-protobuf-project/cache/examples/fakestore"
	"github.com/the-protobuf-project/cache/examples/generated/cache/bookstore_db/bookstorev1cache"
)

// TestIndexedRefusesBackendWithoutSets is the acceptance test for the capability
// precondition.
//
// Book is INDEXED, which core cannot serve without server-side sets: every index
// operation reports ErrUnsupported, per call, forever. A decorator built on such
// a backend would compile, construct, and then fail every lookup that is the
// reason the resource is INDEXED in the first place — on a path a developer
// running Redis locally would never reach.
//
// So the constructor refuses, and the message has to name three things, because
// whoever reads it in a deploy log has none of this context: which resource,
// which strategy, and which capability is missing.
func TestIndexedRefusesBackendWithoutSets(t *testing.T) {
	ctx := context.Background()
	scope := bookstorev1cache.BookScope{AuthorID: "author-1"}

	// A driver with no sets and no leases — the memcached shape.
	db := fakecache.Open(fakecache.New("memcache"), bookstorev1cache.BookNamespace(scope))

	_, err := bookstorev1cache.NewCachedBookStore(ctx, fakestore.NewBooks(), db, scope, bookstorev1cache.BookEdges{})
	if err == nil {
		t.Fatal("constructed an INDEXED decorator over a backend with no sets.\n\n" +
			"Every index operation on it reports ErrUnsupported, so this decorator would fail " +
			"exactly the lookups it exists to serve — and only in whatever environment runs the " +
			"setless backend.")
	}
	for _, want := range []string{"bookstore.v1.Book", "INDEXED", "core.Sets", "memcache"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q — it is read in a deploy log by someone "+
				"with none of this context.\n\nGot:\n%s", want, err)
		}
	}
	if !errors.Is(err, cache.ErrUnsupported) {
		t.Errorf("the refusal does not wrap cache.ErrUnsupported, so a caller cannot tell "+
			"'this backend cannot do it' from any other startup failure.\n\nGot: %v", err)
	}
}

// TestIndexedAcceptsBackendWithSets is the other half, and it is not a formality:
// an assertion that refused every backend would pass the test above and be
// useless. This proves the probe distinguishes.
func TestIndexedAcceptsBackendWithSets(t *testing.T) {
	ctx := context.Background()
	scope := bookstorev1cache.BookScope{AuthorID: "author-1"}
	db := fakecache.Open(fakecache.NewCapable("redis"), bookstorev1cache.BookNamespace(scope))

	if _, err := bookstorev1cache.NewCachedBookStore(ctx, fakestore.NewBooks(), db, scope, bookstorev1cache.BookEdges{}); err != nil {
		t.Fatalf("refused a backend that does implement core.Sets: %v", err)
	}
}

// TestStaleRefusesBackendWithoutLeases is the same argument for core.Leases.
//
// Author sets a stale window, which is served by reading how much of an entry's
// lease is left. A backend whose protocol never reports one cannot do that, and
// would silently fall back to blocking every reader at the moment a hot key
// expires — the traffic spike stale exists to absorb.
func TestStaleRefusesBackendWithoutLeases(t *testing.T) {
	ctx := context.Background()
	db := fakecache.Open(fakecache.New("memcache"), bookstorev1cache.AuthorNamespace())

	_, err := bookstorev1cache.NewCachedAuthorStore(ctx, fakestore.NewAuthors(), db)
	if err == nil {
		t.Fatal("constructed a decorator with a stale window over a backend that cannot report " +
			"a remaining lease; stale would silently do nothing")
	}
	for _, want := range []string{"bookstore.v1.Author", "core.Leases", "stale"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q.\n\nGot:\n%s", want, err)
		}
	}
}

// TestRefusesMismatchedNamespace is the acceptance test for the scope guarantee.
//
// A *cache.DB arrives already bound to whatever namespace the caller selected it
// under, and nothing in its type says which. If that is not the namespace the
// decorator's keys were derived for, every read and write addresses a keyspace
// the generator never reasoned about — and for a scoped resource that means the
// isolation is simply absent, silently, on a cache that otherwise works.
func TestRefusesMismatchedNamespace(t *testing.T) {
	ctx := context.Background()
	scope := bookstorev1cache.BookScope{AuthorID: "author-1"}

	// Opened under a *different* author's namespace than the decorator is built
	// for — the exact shape of a wiring bug that leaks one tenant's rows.
	other := bookstorev1cache.BookNamespace(bookstorev1cache.BookScope{AuthorID: "author-2"})
	db := fakecache.Open(fakecache.NewCapable("redis"), other)

	_, err := bookstorev1cache.NewCachedBookStore(ctx, fakestore.NewBooks(), db, scope, bookstorev1cache.BookEdges{})
	if err == nil {
		t.Fatal("accepted a cache database namespaced for a different scope than the decorator " +
			"was built for. Every key would address the wrong author's keyspace.")
	}
	if !strings.Contains(err.Error(), "bookstore.v1.Book") {
		t.Errorf("the refusal does not name the resource:\n%s", err)
	}
}

// TestEmptyScopeRefused pins the case an assertion on the namespace alone would
// miss: an empty scope value produces a *consistent* namespace, so the decorator
// and the database agree and the check above passes. What it does not produce is
// isolation — every author with an unset id collapses into one keyspace.
func TestEmptyScopeRefused(t *testing.T) {
	ctx := context.Background()
	empty := bookstorev1cache.BookScope{}
	db := fakecache.Open(fakecache.NewCapable("redis"), bookstorev1cache.BookNamespace(empty))

	_, err := bookstorev1cache.NewCachedBookStore(ctx, fakestore.NewBooks(), db, empty, bookstorev1cache.BookEdges{})
	if err == nil {
		t.Fatal("accepted an empty scope. The namespace is self-consistent, so the namespace " +
			"assertion passes — and every parent collapses into one keyspace, which is the " +
			"isolation failure the scope exists to prevent.")
	}
}

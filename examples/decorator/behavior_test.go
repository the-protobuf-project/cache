package decorator_test

// behavior_test.go covers what the decorator does when nothing is wrong: a miss
// loads, a second read is served from the cache, a write reaches both, and the
// declared invalidation edges fire.

import (
	"context"
	"testing"

	"github.com/the-protobuf-project/cache/examples/fakecache"
	"github.com/the-protobuf-project/cache/examples/fakestore"
	"github.com/the-protobuf-project/cache/examples/generated/cache/bookstore_db/bookstorev1cache"
	"github.com/the-protobuf-project/cache/examples/generated/gorm/bookstore_db/bookstorev1"
)

// TestReadThroughAndInvalidation checks the decorator does the thing it is for:
// a second read is served without touching the store, and a write makes the next
// read see the new value.
func TestReadThroughAndInvalidation(t *testing.T) {
	ctx := context.Background()
	inner := fakestore.NewAuthors(&bookstorev1.Author{ID: "a1", DisplayName: "First"})

	db := fakecache.Open(fakecache.NewCapable("redis"), bookstorev1cache.AuthorNamespace())
	store, err := bookstorev1cache.NewCachedAuthorStore(ctx, inner, db)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	got, err := store.GetByID(ctx, "a1")
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if got.DisplayName != "First" {
		t.Errorf("first read = %q, want %q", got.DisplayName, "First")
	}
	if inner.Loads != 1 {
		t.Fatalf("first read caused %d store loads, want 1", inner.Loads)
	}

	if _, err := store.GetByID(ctx, "a1"); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if inner.Loads != 1 {
		t.Errorf("second read caused %d store loads in total, want 1 — it should have been "+
			"served from the cache", inner.Loads)
	}

	// A write refreshes rather than invalidating, so the new value is already
	// there for the next reader instead of every reader missing at once.
	if err := store.Update(ctx, &bookstorev1.Author{ID: "a1", DisplayName: "Second"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = store.GetByID(ctx, "a1")
	if err != nil {
		t.Fatalf("read after update: %v", err)
	}
	if got.DisplayName != "Second" {
		t.Errorf("read after update = %q, want %q — the write did not reach the cache",
			got.DisplayName, "Second")
	}
}

// TestScopeReachesTheKeyspace is the end-to-end form of the isolation claim.
//
// The assertions above check that a *mismatched* scope is refused. This checks
// the positive: that the scope value actually reaches the keys, so two decorators
// for two authors cannot see each other's entries. It asserts on the scope value
// appearing in a key rather than on the key's shape — core owns the layout, and a
// test that pinned it here would be a second copy of it.
func TestScopeReachesTheKeyspace(t *testing.T) {
	ctx := context.Background()
	driver := fakecache.NewCapable("redis")
	scope := bookstorev1cache.BookScope{AuthorID: "author-1"}
	db := fakecache.Open(driver, bookstorev1cache.BookNamespace(scope))

	inner := fakestore.NewBooks(&bookstorev1.Book{ID: "b1", Title: "A Book", AuthorID: "author-1"})

	store, err := bookstorev1cache.NewCachedBookStore(ctx, inner, db, scope, bookstorev1cache.BookEdges{})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, err := store.GetByID(ctx, "b1"); err != nil {
		t.Fatalf("read: %v", err)
	}

	if !driver.HasKeyContaining("author-1") {
		t.Errorf("no key written carries the scope value.\n\nKeys: %v\n\n"+
			"The scope is what separates one author's entries from another's; if it does not "+
			"reach the keyspace, the namespace assertion is guarding nothing.", driver.Keys())
	}
}

// TestInvalidationEdgesAreDeclared checks that the edge set is emitted as data.
//
// Not machinery — the decorator fires edges through named fields, not by reading
// this. It is emitted so the blast radius of a write is legible in the output and
// so a CDC consumer can act on it without regenerating, which is only true if it
// is actually there and actually populated.
func TestInvalidationEdgesAreDeclared(t *testing.T) {
	if len(bookstorev1cache.InvalidationEdges) == 0 {
		t.Fatal("InvalidationEdges is empty, but Book has a foreign key to Author and both " +
			"are cached")
	}
	var found bool
	for _, e := range bookstorev1cache.InvalidationEdges {
		if e.From == "bookstore.v1.Book" && e.To == "bookstore.v1.Author" {
			found = true
			if e.Via != "author_id" {
				t.Errorf("edge Book→Author is via %q, want \"author_id\"", e.Via)
			}
			if e.Reason == "" {
				t.Error("edge Book→Author has no Reason; the set is meant to explain itself " +
					"without the generator")
			}
		}
	}
	if !found {
		t.Errorf("no Book→Author edge in %v", bookstorev1cache.InvalidationEdges)
	}
}

// TestEdgeFires checks the wiring: a write to Book invalidates the Author entry
// it was wired to, through the declared edge.
func TestEdgeFires(t *testing.T) {
	ctx := context.Background()

	authorInner := fakestore.NewAuthors(&bookstorev1.Author{ID: "author-1", DisplayName: "First"})
	authorDB := fakecache.Open(fakecache.NewCapable("redis"), bookstorev1cache.AuthorNamespace())
	authors, err := bookstorev1cache.NewCachedAuthorStore(ctx, authorInner, authorDB)
	if err != nil {
		t.Fatalf("construct authors: %v", err)
	}

	// Warm the Author entry so there is something for the edge to invalidate.
	if _, err := authors.GetByID(ctx, "author-1"); err != nil {
		t.Fatalf("warm author: %v", err)
	}
	if authorInner.Loads != 1 {
		t.Fatalf("warm caused %d loads, want 1", authorInner.Loads)
	}

	scope := bookstorev1cache.BookScope{AuthorID: "author-1"}
	bookDB := fakecache.Open(fakecache.NewCapable("redis"), bookstorev1cache.BookNamespace(scope))
	books, err := bookstorev1cache.NewCachedBookStore(ctx, fakestore.NewBooks(), bookDB, scope,
		// The decorator is itself an Invalidator, so wiring an edge is passing
		// one store to the other.
		bookstorev1cache.BookEdges{Author: authors.(bookstorev1cache.Invalidator)})
	if err != nil {
		t.Fatalf("construct books: %v", err)
	}

	if err := books.Create(ctx, &bookstorev1.Book{ID: "b1", Title: "A Book", AuthorID: "author-1"}); err != nil {
		t.Fatalf("create book: %v", err)
	}

	// The Author entry should now be gone, so the next read reloads.
	if _, err := authors.GetByID(ctx, "author-1"); err != nil {
		t.Fatalf("read author after book write: %v", err)
	}
	if authorInner.Loads != 2 {
		t.Errorf("the author was loaded %d time(s) in total, want 2 — writing a Book should "+
			"have invalidated the Author entry through the declared edge", authorInner.Loads)
	}
}

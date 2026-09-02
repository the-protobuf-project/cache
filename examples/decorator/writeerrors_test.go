package decorator_test

// writeerrors_test.go pins what a decorator does when the store write committed
// and the cache work after it did not.
//
// This is the one place the generated code deliberately swallows an error, so it
// is the one place a test has to say so out loud. The decision and its reasoning
// live on options.afterWrite in the generated file; what is pinned here is that
// the decision is actually in force, in both directions, because the default is
// invisible by construction — a test that only checked the happy path would pass
// identically whichever default was chosen.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/the-protobuf-project/cache/examples/fakecache"
	"github.com/the-protobuf-project/cache/examples/fakestore"
	"github.com/the-protobuf-project/cache/examples/generated/cache/bookstore_db/bookstorev1cache"
	"github.com/the-protobuf-project/cache/examples/generated/gorm/bookstore_db/bookstorev1"
)

// errEdgeDown stands in for a cache that is briefly unreachable.
var errEdgeDown = errors.New("cache backend unreachable")

// failingInvalidator is an edge target whose invalidation always fails. It is the
// cheapest way to reach the post-commit path: an edge runs after s.inner has
// already written the row, which is the whole situation under test.
type failingInvalidator struct{ calls int }

func (f *failingInvalidator) Invalidate(context.Context, string) error {
	f.calls++
	return errEdgeDown
}

// newBookStoreWithFailingEdge wires a Book decorator whose Author edge always
// fails, and returns it alongside the inner store so a test can check what
// actually got written.
func newBookStoreWithFailingEdge(t *testing.T, opts ...bookstorev1cache.Option) (
	bookstorev1.BookStoreIface, *fakestore.Books, *failingInvalidator,
) {
	t.Helper()
	ctx := context.Background()

	scope := bookstorev1cache.BookScope{AuthorID: "author-1"}
	db := fakecache.Open(fakecache.NewCapable("redis"), bookstorev1cache.BookNamespace(scope))
	inner := fakestore.NewBooks()
	edge := &failingInvalidator{}

	books, err := bookstorev1cache.NewCachedBookStore(ctx, inner, db, scope,
		bookstorev1cache.BookEdges{Author: edge}, opts...)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	return books, inner, edge
}

// TestPostCommitCacheFailureDoesNotFailTheWrite is the default, and it is the
// behavior that matters most.
//
// The row is committed before any cache work runs. Returning the cache's error
// would tell the caller their write failed when it did not, and the ordinary
// response to that is a retry — which for a Create means a second row. So the
// error does not travel, and the write reports success because the write
// succeeded.
func TestPostCommitCacheFailureDoesNotFailTheWrite(t *testing.T) {
	ctx := context.Background()
	books, inner, edge := newBookStoreWithFailingEdge(t)

	err := books.Create(ctx, &bookstorev1.Book{ID: "b1", Title: "A Book", AuthorID: "author-1"})
	if err != nil {
		t.Fatalf("Create returned %v, want nil.\n\n"+
			"The row was already committed before the edge fired, so a failing edge must not "+
			"make Create report a failure the caller would retry.", err)
	}
	if edge.calls != 1 {
		t.Errorf("the edge fired %d time(s), want 1 — the test proved nothing if it never ran", edge.calls)
	}
	if _, ok := inner.Rows["b1"]; !ok {
		t.Error("the row is not in the store, so this test is not exercising the post-commit path")
	}
}

// TestWriteErrorsObservesWhatIsSwallowed pins the other half of the default.
//
// Swallowing is defensible only because it is observable. Without an observer a
// cache whose invalidations have been failing for a week looks exactly like one
// that is working, so the handler has to receive the resource, the operation and
// the error — enough to alert on without reading the generated file.
func TestWriteErrorsObservesWhatIsSwallowed(t *testing.T) {
	ctx := context.Background()

	type report struct{ resource, op string }
	var got []report
	var gotErr error

	books, _, _ := newBookStoreWithFailingEdge(t,
		bookstorev1cache.WithWriteErrors(func(_ context.Context, resource, op string, err error) {
			got = append(got, report{resource, op})
			gotErr = err
		}))

	if err := books.Create(ctx, &bookstorev1.Book{ID: "b1", Title: "A Book", AuthorID: "author-1"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("the observer was called %d time(s), want 1: %+v", len(got), got)
	}
	if got[0].resource != "bookstore.v1.Book" {
		t.Errorf("observed resource %q, want %q — an alert has to name what is stale",
			got[0].resource, "bookstore.v1.Book")
	}
	if !strings.Contains(got[0].op, "Author") {
		t.Errorf("observed op %q, want it to name the edge target — a write fires several "+
			"cache operations and the handler has to be able to tell them apart", got[0].op)
	}
	if !errors.Is(gotErr, errEdgeDown) {
		t.Errorf("observed error %v, want it to wrap %v", gotErr, errEdgeDown)
	}
}

// TestStrictWritesFailsTheWrite pins the opt-in opposite.
//
// It exists for callers who would rather return an error than serve a stale read.
// The error has to say that the store write already happened, because the caller
// deciding what to do next needs to know that retrying writes twice.
func TestStrictWritesFailsTheWrite(t *testing.T) {
	ctx := context.Background()
	books, inner, _ := newBookStoreWithFailingEdge(t, bookstorev1cache.WithStrictWrites())

	err := books.Create(ctx, &bookstorev1.Book{ID: "b1", Title: "A Book", AuthorID: "author-1"})
	if err == nil {
		t.Fatal("Create returned nil, want an error: WithStrictWrites is the option that makes " +
			"a post-commit cache failure reach the caller")
	}
	if !errors.Is(err, errEdgeDown) {
		t.Errorf("error %v does not wrap the underlying cause %v", err, errEdgeDown)
	}
	if !strings.Contains(err.Error(), "already committed") {
		t.Errorf("error %q does not say the store write already happened.\n\n"+
			"That is the fact the caller needs: the row exists, so a retry writes it twice.", err)
	}

	// The row is still there. Strict mode reports the failure; it cannot undo it.
	if _, ok := inner.Rows["b1"]; !ok {
		t.Error("the row is gone, but nothing here rolls a write back — strict mode reports, " +
			"it does not revert")
	}
}

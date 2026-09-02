// Command bookstore is the whole cache lifecycle as one readable sequence:
// open a client, select a database per resource, build the decorators, use them,
// and release everything in reverse.
//
// It is written as one straight run of steps on purpose. This is a demonstration
// of what the generated code looks like in use, so the value is in reading it top
// to bottom — a real program would factor this, and factoring it here would hide
// the one thing it exists to show.
//
//	docker run --rm -p 6379:6379 redis:7
//	go run ./cmd/bookstore
//
// Dragonfly is a drop-in: swap the import and the two constructors on steps 1
// and 2, and nothing else changes. memcached deliberately is not — Book is
// STRATEGY_INDEXED and memcached has no server-side sets, so step 8 fails at
// construction rather than handing back a decorator whose index lookups would
// all fail at runtime.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/the-protobuf-project/runtime-go/cache"
	"github.com/the-protobuf-project/runtime-go/cache/redis"

	"github.com/the-protobuf-project/cache/examples/fakestore"
	"github.com/the-protobuf-project/cache/examples/generated/cache/bookstore_db/bookstorev1cache"
	"github.com/the-protobuf-project/cache/examples/generated/gorm/bookstore_db/bookstorev1"
)

func main() {
	// main only exists so the defers below actually run: log.Fatal calls
	// os.Exit, which skips them, and skipping them is exactly what this program
	// is here to show you not doing.
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	const authorID = "author-ursula"

	// 1. THE CLIENT — yours, and yours to close.
	//
	// Hand the same one to the database and streams layers and all three share a
	// pool. Nothing below ever closes it, which is why the defer is here.
	fmt.Println("1. open redis client")
	client, err := redis.NewClient(ctx, redis.Config{Address: "localhost:6379"})
	if err != nil {
		return fmt.Errorf("redis: %w (is a server listening on :6379?)", err)
	}
	defer func() {
		fmt.Println("   close redis client")
		_ = client.Close()
	}()

	// 2. THE PROVIDER — owns nothing, and has no cache methods.
	//
	// Until a database is chosen there is nothing to read or write. Prefix
	// separates this program's keys from its neighbours' in a shared server;
	// DefaultTTL is only a backstop, since the real leases come from the protos
	// and the decorator passes them on every call.
	fmt.Println("2. build provider")
	provider := redis.New(client, cache.Config{
		Prefix:     "bookstore",
		DefaultTTL: 5 * time.Minute,
	})

	// 3. OPEN THE AUTHOR CACHE DATABASE.
	//
	// OpenAuthorCache selects it under the namespace this decorator's keys are
	// derived for. Assembling that name by hand is the one way to get a decorator
	// whose keys do not match its scope, and the constructor on step 5 would then
	// refuse it — correctly, but after the fact.
	fmt.Println("3. open author cache database")
	authorDB, err := bookstorev1cache.OpenAuthorCache(ctx, provider)
	if err != nil {
		return fmt.Errorf("open author cache: %w", err)
	}
	defer func() {
		// Close only. The entries are deliberately left in Redis so they can be
		// read back with redis-cli after the run — see the end of this function.
		//
		// Close is a no-op for a database selected by name, and is called anyway
		// because SelectIndex derives a pool that this is what releases.
		fmt.Println("   close author cache database")
		_ = authorDB.Close()
	}()

	// 4. THE STORE BEHIND THE CACHE.
	//
	// Normally bookstorev1.NewAuthorStore(gormDB). A fake keeps this runnable
	// without a Postgres, and counts reads that reach it so the steps below can
	// show a hit rather than claim one.
	fmt.Println("4. seed the inner author store")
	innerAuthors := fakestore.NewAuthors(&bookstorev1.Author{
		ID: authorID, Name: "authors/ursula", DisplayName: "Ursula K. Le Guin",
	})

	// 5. THE AUTHOR DECORATOR — ASIDE, unscoped.
	//
	// WithNotFound turns a missing row into a cached absence; without it
	// everything works and negative caching simply does not engage.
	// WithWriteErrors surfaces cache failures that happen after a store write has
	// already committed — those do not fail the write, so nothing would show them.
	fmt.Println("5. build cached author store")
	authors, err := bookstorev1cache.NewCachedAuthorStore(ctx, innerAuthors, authorDB,
		bookstorev1cache.WithNotFound(fakestore.NotFound),
		bookstorev1cache.WithWriteErrors(func(_ context.Context, resource, op string, err error) {
			log.Printf("cache write-side failure: resource=%s op=%s err=%v", resource, op, err)
		}))
	if err != nil {
		return fmt.Errorf("cached author store: %w", err)
	}

	// 6. THE SCOPE.
	//
	// Book's resource pattern nests it under {author}, so its namespace is
	// parameterized by that id and a decorator exists per author. This is the
	// tenancy boundary: the decorator below cannot reach another author's entries,
	// because their keys are under a different head.
	fmt.Println("6. bind the book scope")
	scope := bookstorev1cache.BookScope{AuthorID: authorID}

	// 7. OPEN THE BOOK CACHE DATABASE, under that scope.
	fmt.Println("7. open book cache database")
	bookDB, err := bookstorev1cache.OpenBookCache(ctx, provider, scope)
	if err != nil {
		return fmt.Errorf("open book cache: %w", err)
	}
	defer func() {
		fmt.Println("   close book cache database")
		_ = bookDB.Close()
	}()

	// 8. THE BOOK DECORATOR — INDEXED, scoped, with the edge wired.
	//
	// BookEdges.Author says a write to Book invalidates the Author entry that
	// preloads it. A nil field would be a decision not to, made in the open.
	//
	// This constructor also probes for core.Sets before returning, because Book is
	// INDEXED. On memcached it fails right here, naming the resource and the
	// missing capability.
	fmt.Println("8. build cached book store (asserts core.Sets)")
	books, err := bookstorev1cache.NewCachedBookStore(ctx, fakestore.NewBooks(), bookDB, scope,
		bookstorev1cache.BookEdges{Author: authors.(bookstorev1cache.Invalidator)},
		bookstorev1cache.WithNotFound(fakestore.NotFound))
	if err != nil {
		return fmt.Errorf("cached book store: %w", err)
	}

	// The constructor returns BookStoreIface — the contract the decorator is
	// substitutable for. INDEXED also generates ByISBN, IDsByISBN and
	// DeleteByISBN; naming them is how a caller who wants them asks.
	byIndex := books.(interface {
		ByISBN(ctx context.Context, v string) ([]bookstorev1.Book, error)
	})

	fmt.Println()

	// 9. FIRST READ — a miss on a cold cache, loading through to the store.
	//
	// Whether it actually missed is read off the counter rather than asserted:
	// nothing is dropped on the way out, so a second run against the same Redis
	// finds this entry already there and a hardcoded "miss" would be a lie.
	loadsBefore := innerAuthors.Loads
	author, err := authors.GetByID(ctx, authorID)
	if err != nil {
		return fmt.Errorf("get author: %w", err)
	}
	outcome := "hit, cache was already warm"
	if innerAuthors.Loads > loadsBefore {
		outcome = "miss, loaded from store"
	}
	fmt.Printf("9.  read author  -> %s   (%s, store loads: %d)\n",
		author.DisplayName, outcome, innerAuthors.Loads)

	// 10. SECOND READ — served from Redis either way, so the counter cannot move.
	//
	// Concurrent misses on one id would have collapsed into a single load. That
	// is core's singleflight, not anything this program or the generated file does.
	loadsBefore = innerAuthors.Loads
	if _, err := authors.GetByID(ctx, authorID); err != nil {
		return fmt.Errorf("get author again: %w", err)
	}
	if innerAuthors.Loads > loadsBefore {
		return fmt.Errorf("second read reached the store: the cache is not serving hits")
	}
	fmt.Printf("10. read author  -> %s   (hit, store loads: %d)\n",
		author.DisplayName, innerAuthors.Loads)

	// 11. WRITE A BOOK — refreshes its own entry, files it under the isbn index,
	//     and fires the declared edge against the Author decorator above.
	isbn := "9780441013593"
	if err := books.Create(ctx, &bookstorev1.Book{
		ID: "book-lhd", Name: "authors/ursula/books/lhd",
		Title: "The Left Hand of Darkness", AuthorID: authorID, ISBN: &isbn,
		Genre: bookstorev1.GenreSciFi,
	}); err != nil {
		return fmt.Errorf("create book: %w", err)
	}
	fmt.Println("11. write book   -> created, edge fired at bookstore.v1.Author")

	// 12. READ THE AUTHOR AGAIN — the counter moving is the edge having actually
	//     dropped the entry, rather than this program saying it did.
	loadsBefore = innerAuthors.Loads
	if _, err := authors.GetByID(ctx, authorID); err != nil {
		return fmt.Errorf("get author after book write: %w", err)
	}
	if innerAuthors.Loads == loadsBefore {
		return fmt.Errorf("the author entry survived a Book write: the invalidation edge did not fire")
	}
	fmt.Printf("12. read author  -> reloaded (store loads: %d — the edge worked)\n",
		innerAuthors.Loads)

	// 13. THE INDEX ACCESSOR — reads the cache only. "Which entries match?" is a
	//     different question from "fetch me this one", and answering it from the
	//     store would mean a query this decorator has no way to write.
	found, err := byIndex.ByISBN(ctx, isbn)
	if err != nil {
		return fmt.Errorf("by isbn: %w", err)
	}
	fmt.Printf("13. by isbn      -> %d book(s) filed under %s\n", len(found), isbn)

	// 14. THE EDGE SET — emitted as data, so a CDC consumer can read the blast
	//     radius of a write without regenerating anything.
	fmt.Println("14. declared invalidation edges:")
	for _, e := range bookstorev1cache.InvalidationEdges {
		fmt.Printf("      %s -> %s (via %s)\n", e.From, e.To, e.Via)
	}

	// 15. WHAT IS LEFT IN REDIS.
	//
	// Nothing is dropped on the way out, so the keys this run wrote are still
	// there to be read back. The namespaces below are the whole of what this
	// plugin generates about key layout — everything after "cache:" belongs to
	// runtime-go/cache/core.
	fmt.Println("15. entries left in redis, by namespace:")
	fmt.Printf("      author: %s\n", authorDB.Name)
	fmt.Printf("      book:   %s\n", bookDB.Name)
	fmt.Println()
	fmt.Println("    inspect:  redis-cli --scan --pattern 'bookstore:*'")
	fmt.Println("    read one: redis-cli GET 'bookstore:" + authorDB.Name + ":cache:aside:entry:" + authorID + "'")
	fmt.Println("    clear:    redis-cli --scan --pattern 'bookstore:*' | xargs redis-cli DEL")
	fmt.Println()
	fmt.Println("    a second run starts warm, so step 9 is a hit rather than a miss;")
	fmt.Println("    clear first to see the miss again.")

	// Everything above unwinds here, newest first: book database, author
	// database, then the client. Releasing, not deleting — the entries stay.
	fmt.Println("\nteardown (reverse order):")
	return nil
}

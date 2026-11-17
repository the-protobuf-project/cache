//! The generated Rust cache wiring, over an in-memory repository and tpp-cache's in-memory
//! driver. Ports the Go decorator's behavioural tests (examples/decorator) that apply to a
//! repository: read-through, absence, scoping, edges, index refiling, the write-error
//! policy, and the list cache.

use std::collections::BTreeMap;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use cache::core::{
    Capabilities, DatabaseSpec, Driver, Keyspace, MemoryDriver, MemorySets, build_database,
};
use cache_rust_example::pb::bookstore::v1::{Author, Book, Shelf};
use cache_rust_example::pb::shelving::v1 as shelving;
use cache_rust_example::{bookstore, bookstore_cache, shelving as shelving_store, shelving_cache};
use database::repository::cached::Hooks;
use database::repository::{ListInput, Page, Repository};
use database::{Error, Result};
use prost::Message;

/// An in-memory repository counting the reads that reach it.
struct Fake<M> {
    rows: Mutex<BTreeMap<String, M>>,
    name: fn(&M) -> String,
    gets: AtomicUsize,
    lists: AtomicUsize,
}

impl<M: Clone> Fake<M> {
    fn new(name: fn(&M) -> String) -> Arc<Self> {
        Arc::new(Self {
            rows: Mutex::new(BTreeMap::new()),
            name,
            gets: AtomicUsize::new(0),
            lists: AtomicUsize::new(0),
        })
    }

    /// A write that bypasses the decorator: what another service would do.
    fn put(&self, m: M) {
        self.rows.lock().unwrap().insert((self.name)(&m), m);
    }

    fn gets(&self) -> usize {
        self.gets.load(Ordering::SeqCst)
    }

    fn lists(&self) -> usize {
        self.lists.load(Ordering::SeqCst)
    }
}

#[async_trait::async_trait]
impl<M: Clone + Send + Sync + 'static> Repository<M> for Fake<M> {
    async fn create(&self, m: &M) -> Result<M> {
        let mut rows = self.rows.lock().unwrap();
        let name = (self.name)(m);
        if rows.contains_key(&name) {
            return Err(Error::AlreadyExists);
        }
        rows.insert(name, m.clone());
        Ok(m.clone())
    }

    async fn get(&self, name: &str) -> Result<M> {
        self.gets.fetch_add(1, Ordering::SeqCst);
        self.rows
            .lock()
            .unwrap()
            .get(name)
            .cloned()
            .ok_or(Error::NotFound)
    }

    async fn list(&self, input: &ListInput) -> Result<Page<M>> {
        self.lists.fetch_add(1, Ordering::SeqCst);
        let rows = self.rows.lock().unwrap();
        let size = if input.page_size <= 0 {
            usize::MAX
        } else {
            input.page_size as usize
        };
        // Under `parent`, `-` matching any id; empty is every parent.
        let under = |name: &str| {
            if input.parent.is_empty() {
                return true;
            }
            let want: Vec<&str> = input.parent.split('/').collect();
            let have: Vec<&str> = name.split('/').collect();
            have.len() == want.len() + 2 && want.iter().zip(&have).all(|(w, h)| *w == "-" || w == h)
        };
        Ok(Page {
            items: rows
                .iter()
                .filter(|(name, _)| under(name))
                .map(|(_, m)| m.clone())
                .take(size)
                .collect(),
            next_page_token: String::new(),
        })
    }

    async fn count(&self, _: &ListInput) -> Result<i64> {
        Ok(self.rows.lock().unwrap().len() as i64)
    }

    async fn update(&self, m: &M, _: &[String]) -> Result<M> {
        let mut rows = self.rows.lock().unwrap();
        let name = (self.name)(m);
        if !rows.contains_key(&name) {
            return Err(Error::NotFound);
        }
        rows.insert(name, m.clone());
        Ok(m.clone())
    }

    async fn update_output_only(&self, m: &M, paths: &[String]) -> Result<M> {
        self.update(m, paths).await
    }

    async fn delete(&self, name: &str, _: Option<&str>) -> Result<()> {
        self.rows
            .lock()
            .unwrap()
            .remove(name)
            .map(|_| ())
            .ok_or(Error::NotFound)
    }

    async fn undelete(&self, _: &str, _: Option<&str>) -> Result<M> {
        Err(Error::Invalid("no soft delete".into()))
    }

    async fn purge(&self, name: &str) -> Result<()> {
        self.delete(name, None).await
    }
}

const PREFIX: &str = "bookstore";

/// A cache database over the in-memory driver, with sets and leases as Redis has them.
/// Its own name is deliberately unrelated to any resource's namespace.
fn cache_db() -> (Arc<cache::DB>, Arc<MemoryDriver>) {
    let driver = Arc::new(MemoryDriver::new());
    let db = build_database(
        driver.clone(),
        Capabilities::new()
            .with_sets(Arc::new(MemorySets::new()))
            .with_leases(driver.clone()),
        DatabaseSpec {
            prefix: PREFIX.into(),
            namespace: "wiring".into(),
            default_ttl: Duration::from_secs(60),
            ..DatabaseSpec::default()
        },
    );
    (Arc::new(db), driver)
}

struct Bookstore {
    authors: Arc<Fake<Author>>,
    books: Arc<Fake<Book>>,
    repos: bookstore::v1::Repositories,
    shelves: Arc<dyn Repository<Shelf>>,
    db: Arc<cache::DB>,
    driver: Arc<MemoryDriver>,
}

fn bookstore(hooks: Hooks) -> Bookstore {
    let authors = Fake::new(|m: &Author| m.name.clone());
    let books = Fake::new(|m: &Book| m.name.clone());
    let shelves: Arc<dyn Repository<Shelf>> = Fake::new(|m: &Shelf| m.name.clone());
    let (db, driver) = cache_db();
    let repos = bookstore_cache::cache_repositories_with(
        bookstore::v1::Repositories {
            authors: authors.clone(),
            books: books.clone(),
            shelves: shelves.clone(),
        },
        db.clone(),
        hooks,
    );
    Bookstore {
        authors,
        books,
        repos,
        shelves,
        db,
        driver,
    }
}

fn ursula() -> Author {
    Author {
        name: "authors/ursula".into(),
        display_name: "Ursula K. Le Guin".into(),
        bio: String::new(),
    }
}

fn lhd(author_id: &str) -> Book {
    Book {
        name: "authors/ursula/books/lhd".into(),
        title: "The Left Hand of Darkness".into(),
        author_id: author_id.into(),
        isbn: "9780441013593".into(),
        published_year: 1969,
    }
}

/// The key Go's decorator reads, built with the runtime's own keyspace.
fn aside_key(namespace: &str, id: &str) -> String {
    Keyspace::new(PREFIX, namespace, 0, false).aside_entry(id)
}

#[tokio::test]
async fn read_through_hit_and_invalidation() {
    let s = bookstore(Hooks::default());
    s.authors.put(ursula());

    assert_eq!(
        s.repos.authors.get("authors/ursula").await.unwrap(),
        ursula()
    );
    assert_eq!(s.authors.gets(), 1, "a miss loads from the repository");
    assert_eq!(
        s.repos.authors.get("authors/ursula").await.unwrap(),
        ursula()
    );
    assert_eq!(s.authors.gets(), 1, "a hit does not");

    // The entry is where Go's decorator puts it.
    assert!(
        s.driver
            .get(&aside_key("bookstore.v1.author", "authors/ursula"))
            .await
            .is_ok()
    );

    // Update refreshes (one load, Go's "SELECT it back"), so the next read is a hit.
    let mut renamed = ursula();
    renamed.display_name = "U. K. Le Guin".into();
    s.repos.authors.update(&renamed, &[]).await.unwrap();
    assert_eq!(s.authors.gets(), 2);
    assert_eq!(
        s.repos.authors.get("authors/ursula").await.unwrap(),
        renamed
    );
    assert_eq!(s.authors.gets(), 2);

    // Delete drops it.
    s.repos
        .authors
        .delete("authors/ursula", None)
        .await
        .unwrap();
    assert!(matches!(
        s.repos.authors.get("authors/ursula").await,
        Err(Error::NotFound)
    ));
}

#[tokio::test]
async fn absence_is_remembered_and_a_create_clears_it() {
    let s = bookstore(Hooks::default());
    assert!(matches!(
        s.repos.authors.get("authors/nobody").await,
        Err(Error::NotFound)
    ));
    assert!(matches!(
        s.repos.authors.get("authors/nobody").await,
        Err(Error::NotFound)
    ));
    assert_eq!(
        s.authors.gets(),
        1,
        "a remembered absence does not reach the repository"
    );

    // A row that appears behind the cache's back stays invisible until the absence expires...
    let mut nobody = ursula();
    nobody.name = "authors/nobody".into();
    s.authors.put(nobody.clone());
    assert!(matches!(
        s.repos.authors.get("authors/nobody").await,
        Err(Error::NotFound)
    ));

    // ...but a create through the decorator replaces the absence at once.
    s.authors.rows.lock().unwrap().clear();
    s.repos.authors.create(&nobody).await.unwrap();
    assert_eq!(s.repos.authors.get("authors/nobody").await.unwrap(), nobody);
}

#[tokio::test]
async fn scope_reaches_the_keyspace() {
    let s = bookstore(Hooks::default());
    let mut other = lhd("authors/other");
    other.name = "authors/other/books/lhd".into();
    s.books.put(lhd("authors/ursula"));
    s.books.put(other);

    s.repos.books.get("authors/ursula/books/lhd").await.unwrap();
    s.repos.books.get("authors/other/books/lhd").await.unwrap();
    assert_eq!(
        s.books.gets(),
        2,
        "one book id under two authors is two entries"
    );

    for (ns, id) in [
        (
            "bookstore.v1.book.author.ursula",
            "authors/ursula/books/lhd",
        ),
        ("bookstore.v1.book.author.other", "authors/other/books/lhd"),
    ] {
        assert!(
            s.driver.get(&aside_key(ns, id)).await.is_ok(),
            "no entry under {ns}"
        );
    }
    // Nothing under the namespace the cache database itself was selected with.
    assert!(
        s.driver
            .get(&aside_key("wiring", "authors/ursula/books/lhd"))
            .await
            .is_err()
    );
}

#[tokio::test]
async fn a_name_outside_the_pattern_is_not_cached() {
    let s = bookstore(Hooks::default());
    assert!(matches!(
        s.repos.books.get("books/lhd").await,
        Err(Error::NotFound)
    ));
    assert!(matches!(
        s.repos.books.get("books/lhd").await,
        Err(Error::NotFound)
    ));
    assert_eq!(
        s.books.gets(),
        2,
        "read through, never under a namespace it cannot derive"
    );
}

#[tokio::test]
async fn an_edge_fires_on_write() {
    for reference in ["authors/ursula", "ursula"] {
        let s = bookstore(Hooks::default());
        s.authors.put(ursula());
        s.repos.authors.get("authors/ursula").await.unwrap();
        s.repos.authors.get("authors/ursula").await.unwrap();
        assert_eq!(s.authors.gets(), 1);

        s.repos.books.create(&lhd(reference)).await.unwrap();
        s.repos.authors.get("authors/ursula").await.unwrap();
        assert_eq!(
            s.authors.gets(),
            2,
            "writing a Book drops its Author ({reference})"
        );
    }
    assert_eq!(bookstore_cache::INVALIDATION_EDGES.len(), 1);
    assert_eq!(
        bookstore_cache::INVALIDATION_EDGES[0].to,
        "bookstore.v1.Author"
    );
}

#[tokio::test]
async fn an_indexed_write_refiles() {
    let s = bookstore(Hooks::default());
    let book = lhd("authors/ursula");
    s.repos.books.create(&book).await.unwrap();

    let indexed =
        s.db.under("bookstore.v1.book.author.ursula")
            .unwrap()
            .indexed;
    assert_eq!(
        indexed.ids_by_index("isbn", "9780441013593").await.unwrap(),
        vec![book.name.clone()]
    );
    // The value is Go's encoding of a []byte: the wire bytes as a base64 JSON string.
    let values = indexed.by_index("isbn", "9780441013593").await.unwrap();
    let want = serde_json::to_vec(&BASE64.encode(book.encode_to_vec())).unwrap();
    assert_eq!(values, vec![want]);

    let mut moved = book.clone();
    moved.isbn = "0000000000".into();
    s.repos.books.update(&moved, &[]).await.unwrap();
    assert!(
        indexed
            .ids_by_index("isbn", "9780441013593")
            .await
            .unwrap()
            .is_empty()
    );
    assert_eq!(
        indexed.ids_by_index("isbn", "0000000000").await.unwrap(),
        vec![book.name.clone()]
    );

    // An empty ISBN, NULL in Go's model, is left unfiled rather than filed under "".
    let mut none = moved.clone();
    none.isbn.clear();
    s.repos.books.update(&none, &[]).await.unwrap();
    assert!(indexed.ids_by_index("isbn", "").await.unwrap().is_empty());

    s.repos.books.delete(&book.name, None).await.unwrap();
    assert!(
        indexed
            .ids_by_index("isbn", "0000000000")
            .await
            .unwrap()
            .is_empty()
    );
}

/// Records every failure the hooks are told of.
/// `(resource, operation)` per observed failure.
type Seen = Arc<Mutex<Vec<(String, String)>>>;

fn observer() -> (Hooks, Seen) {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let into = seen.clone();
    let hooks = Hooks {
        write_errors: Some(Arc::new(move |resource: &str, op: &str, _: &Error| {
            into.lock()
                .unwrap()
                .push((resource.to_owned(), op.to_owned()));
        })),
        ..Hooks::default()
    };
    (hooks, seen)
}

#[tokio::test]
async fn a_failure_after_commit_does_not_fail_the_write_but_is_observed() {
    let (hooks, seen) = observer();
    let s = bookstore(hooks);
    // A reference that names no Author: its edge cannot be fired.
    s.repos.books.create(&lhd("not/an/author")).await.unwrap();
    assert!(
        s.books
            .rows
            .lock()
            .unwrap()
            .contains_key("authors/ursula/books/lhd")
    );
    assert_eq!(
        *seen.lock().unwrap(),
        vec![(
            "bookstore.v1.Book".to_owned(),
            "invalidate Author".to_owned()
        )]
    );
}

#[tokio::test]
async fn strict_writes_fail_the_call_after_the_commit() {
    let (mut hooks, seen) = observer();
    hooks.strict_writes = true;
    let s = bookstore(hooks);
    let err = s
        .repos
        .books
        .create(&lhd("not/an/author"))
        .await
        .unwrap_err();
    assert!(err.to_string().contains("already committed"), "{err}");
    assert!(
        s.books
            .rows
            .lock()
            .unwrap()
            .contains_key("authors/ursula/books/lhd")
    );
    assert_eq!(seen.lock().unwrap().len(), 1);
}

#[tokio::test]
async fn an_uncached_resource_is_left_as_it_was() {
    let s = bookstore(Hooks::default());
    assert!(Arc::ptr_eq(&s.repos.shelves, &s.shelves));
}

struct Shelving {
    shelves: Arc<Fake<shelving::Shelf>>,
    notes: Arc<Fake<shelving::Note>>,
    repos: shelving_store::v1::Repositories,
    driver: Arc<MemoryDriver>,
}

fn shelving_repos() -> Shelving {
    let shelves = Fake::new(|m: &shelving::Shelf| m.name.clone());
    let notes = Fake::new(|m: &shelving::Note| m.name.clone());
    let (db, driver) = cache_db();
    let repos = shelving_cache::cache_repositories(
        shelving_store::v1::Repositories {
            shelves: shelves.clone(),
            notes: notes.clone(),
        },
        db,
    );
    Shelving {
        shelves,
        notes,
        repos,
        driver,
    }
}

#[tokio::test]
async fn a_list_is_cached_until_a_write_moves_its_version_on() {
    let s = shelving_repos();
    s.shelves.put(shelving::Shelf {
        name: "shelves/a".into(),
    });
    let input = ListInput {
        page_size: 10,
        ..ListInput::default()
    };

    let first = s.repos.shelves.list(&input).await.unwrap();
    assert_eq!(s.repos.shelves.list(&input).await.unwrap(), first);
    assert_eq!(
        s.shelves.lists(),
        1,
        "the second page is served from the cache"
    );

    // Another request is another page.
    s.repos
        .shelves
        .list(&ListInput {
            page_size: 5,
            ..ListInput::default()
        })
        .await
        .unwrap();
    assert_eq!(s.shelves.lists(), 2);

    // A write behind the cache's back is not seen until the ttl ends...
    s.shelves.put(shelving::Shelf {
        name: "shelves/b".into(),
    });
    assert_eq!(s.repos.shelves.list(&input).await.unwrap().items.len(), 1);
    // ...a write through it is.
    s.repos
        .shelves
        .create(&shelving::Shelf {
            name: "shelves/c".into(),
        })
        .await
        .unwrap();
    assert_eq!(s.repos.shelves.list(&input).await.unwrap().items.len(), 3);
    assert_eq!(s.shelves.lists(), 3);
    // Shelf's records are not cached: a get always reaches the repository.
    s.repos.shelves.get("shelves/a").await.unwrap();
    s.repos.shelves.get("shelves/a").await.unwrap();
    assert_eq!(s.shelves.gets(), 2);
}

#[tokio::test]
async fn a_nested_list_moves_on_with_a_write_under_any_parent() {
    let s = shelving_repos();
    let input = ListInput::default();
    s.repos.notes.list(&input).await.unwrap();
    s.repos.notes.list(&input).await.unwrap();
    assert_eq!(s.notes.lists(), 1);

    s.repos
        .notes
        .create(&shelving::Note {
            name: "shelves/a/notes/1".into(),
        })
        .await
        .unwrap();
    assert_eq!(s.repos.notes.list(&input).await.unwrap().items.len(), 1);
    assert_eq!(s.notes.lists(), 2);

    s.repos
        .notes
        .delete("shelves/a/notes/1", None)
        .await
        .unwrap();
    assert!(s.repos.notes.list(&input).await.unwrap().items.is_empty());
    assert_eq!(s.notes.lists(), 3);

    // Note's records are cached under the template namespace "shelving.v1.note.{shelf}".
    s.notes.put(shelving::Note {
        name: "shelves/a/notes/2".into(),
    });
    let before = s.notes.gets(); // the create above refreshed, which is a load
    s.repos.notes.get("shelves/a/notes/2").await.unwrap();
    s.repos.notes.get("shelves/a/notes/2").await.unwrap();
    assert_eq!(s.notes.gets(), before + 1);
}

/// The Volatile key of a list page, built with the runtime's own keyspace.
fn page_key(namespace: &str, version: &str, digest: &str) -> String {
    Keyspace::new(PREFIX, namespace, 0, false).vol_entry(&format!("page:{version}:{digest}"))
}

/// Go's `listKey` digests for the requests below, computed by Go's `encoding/json` and
/// `crypto/sha256` over `struct{Parent string; In repox.ListInput}`:
/// `List(ctx, "shelves/a", repox.ListInput{PageSize: 10})` and
/// `List(ctx, "shelves/-", repox.ListInput{})`.
const GO_NESTED: &str = "505ccbbb04506f5e02921c529b3512d39c65dfb08951cb22e2d40de310f18b5d";
const GO_WILDCARD: &str = "a380b2e7a1f90f0c7e324a117a1dfb995adadbf4aa09aa378852b120ccf6e7e8";

#[tokio::test]
async fn a_nested_page_is_where_go_caches_it_and_versioned_per_parent() {
    let s = shelving_repos();
    s.notes.put(shelving::Note {
        name: "shelves/a/notes/1".into(),
    });
    s.notes.put(shelving::Note {
        name: "shelves/b/notes/1".into(),
    });
    let under_a = ListInput {
        page_size: 10,
        parent: "shelves/a".into(),
        ..ListInput::default()
    };
    let any = ListInput {
        parent: "shelves/-".into(),
        ..ListInput::default()
    };

    assert_eq!(s.repos.notes.list(&under_a).await.unwrap().items.len(), 1);
    assert_eq!(s.repos.notes.list(&any).await.unwrap().items.len(), 2);
    assert_eq!(s.notes.lists(), 2);

    // Before any write a parent's version is "0", so the keys are Go's, byte for byte.
    let ns = "shelving.v1.note.list";
    assert!(s.driver.get(&page_key(ns, "0", GO_NESTED)).await.is_ok());
    assert!(s.driver.get(&page_key(ns, "0", GO_WILDCARD)).await.is_ok());

    // A write under b moves b's version and the wildcard's, not a's.
    s.repos
        .notes
        .create(&shelving::Note {
            name: "shelves/b/notes/2".into(),
        })
        .await
        .unwrap();
    s.repos.notes.list(&under_a).await.unwrap();
    assert_eq!(s.notes.lists(), 2, "a's page is still current");
    assert_eq!(s.repos.notes.list(&any).await.unwrap().items.len(), 3);
    assert_eq!(s.notes.lists(), 3, "the wildcard page moved on");

    // A write under a moves a's.
    s.repos
        .notes
        .create(&shelving::Note {
            name: "shelves/a/notes/2".into(),
        })
        .await
        .unwrap();
    assert_eq!(s.repos.notes.list(&under_a).await.unwrap().items.len(), 2);
    assert_eq!(s.notes.lists(), 4);
}

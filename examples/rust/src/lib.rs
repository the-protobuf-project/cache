//! protoc-gen-cache `lang=rust` output, compiled.
//!
//! The generated modules are the committed golden files themselves, included by path, so
//! what the generator's golden tests pin is exactly what compiles and is tested here. The
//! two modules they name stand in for the other generators' output:
//!
//! - `pb`: the prost types prost-build generates from the same protos, written out by
//!   hand (only the fields the tests use).
//! - `bookstore::v1`, `shelving::v1`: the `Repositories` struct protoc-gen-store
//!   `lang=rust target=repository` generates per package, at the crate root.

use std::sync::Arc;

use database::repository::Repository;

/// The prost types, laid out as prost-build nests them (`crate::pb::<package>`).
pub mod pb {
    /// `bookstore.v1`.
    pub mod bookstore {
        /// `bookstore.v1`.
        pub mod v1 {
            /// `bookstore.v1.Author`.
            #[derive(Clone, PartialEq, prost::Message)]
            pub struct Author {
                /// The resource name, `authors/{author}`.
                #[prost(string, tag = "1")]
                pub name: String,
                /// Shown name.
                #[prost(string, tag = "2")]
                pub display_name: String,
                /// Biography.
                #[prost(string, tag = "3")]
                pub bio: String,
            }

            /// `bookstore.v1.Book`.
            #[derive(Clone, PartialEq, prost::Message)]
            pub struct Book {
                /// The resource name, `authors/{author}/books/{book}`.
                #[prost(string, tag = "1")]
                pub name: String,
                /// Title.
                #[prost(string, tag = "2")]
                pub title: String,
                /// The author, by resource name.
                #[prost(string, tag = "3")]
                pub author_id: String,
                /// The ISBN, indexed.
                #[prost(string, tag = "4")]
                pub isbn: String,
                /// Year of publication.
                #[prost(int32, tag = "5")]
                pub published_year: i32,
            }

            /// `bookstore.v1.Shelf`: not cached.
            #[derive(Clone, PartialEq, prost::Message)]
            pub struct Shelf {
                /// The resource name, `shelves/{shelf}`.
                #[prost(string, tag = "1")]
                pub name: String,
                /// Theme.
                #[prost(string, tag = "2")]
                pub theme: String,
            }
        }
    }

    /// `shelving.v1`.
    pub mod shelving {
        /// `shelving.v1`.
        pub mod v1 {
            /// `shelving.v1.Shelf`: its list is cached, its records are not.
            #[derive(Clone, PartialEq, prost::Message)]
            pub struct Shelf {
                /// The resource name, `shelves/{shelf}`.
                #[prost(string, tag = "1")]
                pub name: String,
            }

            /// `shelving.v1.Note`: records and list cached.
            #[derive(Clone, PartialEq, prost::Message)]
            pub struct Note {
                /// The resource name, `shelves/{shelf}/notes/{note}`.
                #[prost(string, tag = "1")]
                pub name: String,
            }
        }
    }
}

// What protoc-gen-store `lang=rust target=repository` generates: one module per proto
// package at the crate root (`crate::<package path>`), each with its Repositories struct.
// That is rust_repository_module's default base, `crate`.

/// `bookstore.v1`.
pub mod bookstore {
    /// `bookstore.v1`.
    pub mod v1 {
        use super::super::*;

        /// The package's repositories.
        pub struct Repositories {
            /// `bookstore.v1.Author`.
            pub authors: Arc<dyn Repository<pb::bookstore::v1::Author>>,
            /// `bookstore.v1.Book`.
            pub books: Arc<dyn Repository<pb::bookstore::v1::Book>>,
            /// `bookstore.v1.Shelf`, which no annotation caches.
            pub shelves: Arc<dyn Repository<pb::bookstore::v1::Shelf>>,
        }
    }
}

/// `shelving.v1`.
pub mod shelving {
    /// `shelving.v1`.
    pub mod v1 {
        use super::super::*;

        /// The package's repositories.
        pub struct Repositories {
            /// `shelving.v1.Shelf`.
            pub shelves: Arc<dyn Repository<pb::shelving::v1::Shelf>>,
            /// `shelving.v1.Note`.
            pub notes: Arc<dyn Repository<pb::shelving::v1::Note>>,
        }
    }
}

// protoc-gen-cache's output: the golden files, verbatim. Declared at the crate root
// because a #[path] inside an inline module resolves under a directory that does not exist.

/// `bookstore.v1`'s cache wiring, from `examples/proto`.
#[path = "../../../plugin/generator/testdata/cases/bookstore/golden/rustcache/bookstore/v1/cache.rs"]
pub mod bookstore_cache;

/// `shelving.v1`'s cache wiring, from the generator's `lists` case.
#[path = "../../../plugin/generator/testdata/cases/lists/golden/rustcache/shelving/v1/cache.rs"]
pub mod shelving_cache;

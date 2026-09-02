// Package fakestore is an in-memory stand-in for protoc-gen-store's generated
// stores, so the example and its tests run without a Postgres.
//
// The real thing is bookstorev1.NewAuthorStore(gormDB), which satisfies the same
// interface. Nothing about how a decorator is wired changes when you swap one for
// the other — which is the property the decorator exists to have, and the reason
// one fake serves both the runnable program and the tests.
//
// Each type embeds the store interface, so the methods a decorator does not
// override — List, Count, GetByName — are present without being written. Calling
// one panics, which is the honest outcome: a silent zero value would be worse
// than a stack trace.
//
// The timestamp columns are maintained here rather than left at their zero value.
// The models carry gorm's autoCreateTime and autoUpdateTime tags, so against a
// real database those fields are populated on every write — and a fake that left
// them alone would put "0001-01-01T00:00:00Z" into the cache, which is visible the
// moment anyone reads an entry back with redis-cli. A double that is substitutable
// for the real store has to be substitutable in what it writes, not only in the
// methods it answers to.
package fakestore

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/the-protobuf-project/cache/examples/generated/gorm/bookstore_db/bookstorev1"
)

// Authors is an in-memory AuthorStoreIface.
//
// Loads counts reads that reached it, which is how a caller tells a cache hit
// from a miss without inspecting keys.
type Authors struct {
	bookstorev1.AuthorStoreIface

	Rows  map[string]*bookstorev1.Author
	Loads int
}

// NewAuthors returns a store seeded with rows, stamping any timestamp the caller
// left unset so seeded rows look like rows that were once written.
func NewAuthors(seed ...*bookstorev1.Author) *Authors {
	s := &Authors{Rows: map[string]*bookstorev1.Author{}}
	now := time.Now().UTC()
	for _, a := range seed {
		if a.CreatedAt.IsZero() {
			a.CreatedAt = now
		}
		if a.UpdatedAt.IsZero() {
			a.UpdatedAt = now
		}
		s.Rows[a.ID] = a
	}
	return s
}

func (s *Authors) GetByID(_ context.Context, id string) (*bookstorev1.Author, error) {
	s.Loads++
	a, ok := s.Rows[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return a, nil
}

// Create stamps CreatedAt and UpdatedAt, as gorm's autoCreateTime and
// autoUpdateTime tags do against a real database.
func (s *Authors) Create(_ context.Context, m *bookstorev1.Author) error {
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	s.Rows[m.ID] = m
	return nil
}

// Update stamps UpdatedAt and preserves the original CreatedAt, which the caller
// will not usually have carried on the model it is writing.
func (s *Authors) Update(_ context.Context, m *bookstorev1.Author) error {
	if m.CreatedAt.IsZero() {
		if prev, ok := s.Rows[m.ID]; ok {
			m.CreatedAt = prev.CreatedAt
		} else {
			m.CreatedAt = time.Now().UTC()
		}
	}
	m.UpdatedAt = time.Now().UTC()
	s.Rows[m.ID] = m
	return nil
}

func (s *Authors) DeleteByID(_ context.Context, id string) error {
	delete(s.Rows, id)
	return nil
}

// Books is the same for Book, the INDEXED and scoped resource.
type Books struct {
	bookstorev1.BookStoreIface

	Rows  map[string]*bookstorev1.Book
	Loads int
}

// NewBooks returns a store seeded with rows, stamping any CreateTime the caller
// left unset.
func NewBooks(seed ...*bookstorev1.Book) *Books {
	s := &Books{Rows: map[string]*bookstorev1.Book{}}
	now := time.Now().UTC()
	for _, b := range seed {
		if b.CreateTime.IsZero() {
			b.CreateTime = now
		}
		s.Rows[b.ID] = b
	}
	return s
}

func (s *Books) GetByID(_ context.Context, id string) (*bookstorev1.Book, error) {
	s.Loads++
	b, ok := s.Rows[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return b, nil
}

// Create stamps CreateTime and applies the genre column's default. Book has no
// updated_at column — the proto only asks for create_time — so there is nothing
// to maintain on Update.
//
// The default matters for the same reason the timestamps do: genre is NOT NULL
// with default 'FICTION' and a CHECK constraint listing the four legal values, so
// the empty string a zero-valued model carries is a value the database would
// refuse outright. Caching it would mean the cache holding a row the store could
// never have returned.
func (s *Books) Create(_ context.Context, m *bookstorev1.Book) error {
	if m.CreateTime.IsZero() {
		m.CreateTime = time.Now().UTC()
	}
	if m.Genre == "" {
		m.Genre = bookstorev1.GenreFiction
	}
	s.Rows[m.ID] = m
	return nil
}

func (s *Books) Update(_ context.Context, m *bookstorev1.Book) error {
	if m.CreateTime.IsZero() {
		if prev, ok := s.Rows[m.ID]; ok {
			m.CreateTime = prev.CreateTime
		}
	}
	s.Rows[m.ID] = m
	return nil
}

func (s *Books) DeleteByID(_ context.Context, id string) error {
	delete(s.Rows, id)
	return nil
}

// NotFound reports whether err is this store's "no such row", for WithNotFound.
//
// It lives here rather than at each call site because it is a fact about the
// store, not about the cache: the generated package cannot supply it without
// importing gorm into generated cache code, which is the coupling the whole
// arrangement avoids.
func NotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

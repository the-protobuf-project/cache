// Package decorator holds the tests over the *generated* cache decorator.
//
// It has no code of its own, and that is the point: everything under test is in
// examples/generated/cache, written by protoc-gen-cache. These tests are the only
// place the generated output is exercised as a running thing rather than compared
// byte for byte, so they are what would catch a decorator that compiles, matches
// its golden, and does the wrong thing.
//
//	behavior_test.go     a miss loads, a second read is served, a write fires the
//	                     declared invalidation edges
//	guards_test.go       the two construction-time refusals — a backend without
//	                     the capability a strategy needs, and a database opened
//	                     under the wrong namespace
//	writeerrors_test.go  what a committed write does when the cache work after it
//	                     fails, in both directions
//
// They live in a package of their own rather than at the module root because the
// module root is where go.mod lives, and a directory holding nothing but loose
// _test.go files gives a reader nowhere to start.
//
// The doubles they run against are examples/fakecache (a core.Driver whose
// capabilities are chosen per instance, so the memcached-shaped refusals are
// testable without memcached) and examples/fakestore (the store protoc-gen-store
// would have generated).
package decorator

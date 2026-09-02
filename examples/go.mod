// The example is its own module so the generated output's dependencies — gorm,
// the cache runtime — stay out of the plugin's graph. The plugin emits code that
// imports runtime-go/cache; it must never link against it itself.
module github.com/the-protobuf-project/cache/examples

go 1.26.5

require (
	github.com/the-protobuf-project/runtime-go/cache v0.0.0-20260828174955-a72961a8bb95
	gorm.io/gorm v1.31.2
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/oklog/ulid/v2 v2.1.2 // indirect
	github.com/redis/go-redis/v9 v9.18.0 // indirect
	github.com/the-protobuf-project/runtime-go/telemetry v0.0.0-20260722084318-b90e81eeadb7 // indirect
	github.com/the-protobuf-project/runtime-go/ulid v0.0.0-00010101000000-000000000000 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// runtime-go/cache requires its three siblings at v0.0.0 and resolves that with a
// `replace` in its own go.mod — which Go ignores in a *dependency's* go.mod. None
// of the four is tagged, so the only versions that exist are pseudo-versions, and
// a pseudo-version of v0.0.0 sorts *below* v0.0.0 in semver: a plain `require`
// loses to the unresolvable v0.0.0 under MVS. A replace is what actually pins them.
//
// Delete this block once runtime-go tags a release.
// See docs/boundary-findings.md, finding 5.
replace (
	github.com/the-protobuf-project/runtime-go/observability => github.com/the-protobuf-project/runtime-go/observability v0.0.0-20260828174955-a72961a8bb95
	github.com/the-protobuf-project/runtime-go/telemetry => github.com/the-protobuf-project/runtime-go/telemetry v0.0.0-20260828174955-a72961a8bb95
	github.com/the-protobuf-project/runtime-go/ulid => github.com/the-protobuf-project/runtime-go/ulid v0.0.0-20260828174955-a72961a8bb95
)

# Boundary findings

protoc-gen-cache was built to answer a question about an architecture, not only to
cache things: **can a second plugin be written on protokit's StructureReader SPI
with zero changes to the plugin that came first?**

The answer is yes. Nothing in the store repository was modified, and
`git -C ../protorm status --porcelain` is empty. That is the headline result.

But "it worked" is the least useful part of the report. A boundary that holds
because one person was careful is not a boundary. What follows is every place the
seam was thinner than it looked — each one worked around *inside this repository*,
never by editing another, and each one worth fixing upstream.

---

## 1. `requires_capability` is not in protokit's manifest schema

**Where:** `protokit/manifest/manifest.go:43`

`manifest.Manifest` has no `requires_capability` field, and `Parse` decodes with
`KnownFields(true)`. A `plugin.yaml` carrying the key the brief specifies fails
validation outright.

The strictness is correct — a manifest is hand-written and rarely read back, so a
typo'd key that parsed silently would declare nothing. The gap is that this plugin
has a real requirement to declare: whether a resource can run at all depends on
capabilities of a driver chosen long after generation, and something scheduling a
multi-plugin run should be able to refuse a plan before anything is written.

**Worked around by:** `manifest.go` in this repository decodes into a superset,
then re-encodes the protokit half and hands it to `manifest.Parse` for the shared
validation. Nothing here validates `provides`, `requires`, `annotations`, `facets`
or `outputs` — if protokit's rules change, this plugin is held to the new ones
without a line changing here. `TestManifestStillValidatedByProtokit` pins that the
delegation is real, because a refactor that dropped it would leave every other
test passing.

**Upstream fix:** a first-class field. The shape that works is
`map[string][]string` keyed by whatever the plugin's own vocabulary calls a mode —
protokit should not need to know that "INDEXED" is a cache strategy, only that a
plugin declares requirements under names it owns.

---

## 2. `cache.DB` exposes no capability report

**Where:** `runtime-go/cache/core/build.go:73`, `runtime-go/cache/cache.go:162`

`core.Build` resolves a driver's capabilities once, at construction, by type
assertion — and keeps every result private. `cache.DB` hands back the four
strategy interfaces and nothing else. There is no `db.Supports(core.Sets)`, no
`Capabilities` field, no way to ask.

So the generated construction-time assertion cannot ask either. It **probes**:
calls `IDsByIndex(ctx, field, "")` and checks for `ErrUnsupported`. That works —
`core` refuses before touching the network on a driver with no sets — but it costs
a round trip on every backend that *does* support sets, on every construction, for
information the runtime already computed and discarded.

The cost is worse than "once per construction" makes it sound, because of how
often construction happens. A scoped resource's decorator is built **per scope**,
not per process — `BookScope{AuthorID: ...}` parameterizes the namespace, so a
multi-tenant caller builds one decorator per tenant and quite possibly one per
request. Each of those pays `SetDatabase` plus one or two probe round trips.
`IDsByIndex` short-circuits locally only when sets are *absent* (`core/indexed.go`
returns `unsupported` before touching the driver); on Redis or Dragonfly — the
backends anyone actually runs — it is a real `SMEMBERS`. So the plugin pays a
network round trip on every working backend, per tenant, to re-derive something
`core.Build` computed and discarded.

**Worked around by:** the probe, in `plugin/generator/templates/asserts.go.tmpl`. It is
correct and it is verified (`TestIndexedRefusesBackendWithoutSets`, plus its
positive twin so an assertion that refused everything could not pass).

**Upstream fix:** `DB.Supports(capability) bool`, or a `Capabilities` struct
populated in `Build`. The information exists at exactly the moment it is thrown
away.

---

## 3. store.v1's stubs are not independently importable

**Where:** `store/plugin/pb/storepbv1/` — no `go.mod`

Reading store.v1 means depending on the store **root** module, which brings
gqlparser, protocompile, telemetry-go, levenshtein and yaml into a plugin that
uses exactly one field of it (`ColumnOptions.unique`).

This is precisely the coupling the nested entity module existed to prevent, and
that module's own `go.mod` said so:

> A cache generator or a streams generator needs exactly this and nothing else, so
> it must be possible to depend on it without pulling in gorm, prisma, graphql, or
> any other part of the store plugin.

entity.v1 got that treatment. store.v1 did not — reasonably, since it is the store
plugin's own vocabulary. But the moment a second plugin reads it optionally, it
becomes a shared surface with a private dependency graph.

**Not worked around.** This was a deliberate choice (the alternative was resolving
the extension dynamically by name, which avoids the dependency entirely); the
heavier dependency was accepted because it lets `TestIRAgreement` instantiate the
**real** protoc-gen-store rather than a stand-in, which is a materially stronger
test. See `plugin/generator/agreement_test.go`.

**Upstream fix:** it used to be "nest `plugin/pb` as its own module, mirroring
`plugin/entity`". There is no longer a `plugin/entity` to mirror — see finding 5 —
so the dependency-weight complaint stands but the proposed fix has to be argued on
its own merits rather than by analogy.

---

## 4. `store.v1`'s extension numbers are not where the brief said

**Where:** `store/protobuf/store/v1/annotations.proto:41,47,51`

store.v1 occupies **50010 / 50011 / 50012**, not 50000–50003. entity.v1 is
51000–51002 as documented. cache.v1 at 52000–52003 is clear of both either way, so
nothing was affected — but a plugin author allocating numbers from the brief alone
would be reasoning from a wrong map.

**Fix:** none needed here; noted so the next plugin checks the protos rather than
the prose.

---

## 5. The module graph, in three acts

**Where:** the module graph, and it is the most consequential finding here.

This one has been rewritten twice because the ground moved twice. The history is
the finding — a plugin ecosystem with unpublished modules breaks its consumers in
ways that have nothing to do with their code, and the shape of the breakage
changes as upstream reacts.

**Act one — nothing was published.** `store/plugin/entity` was a nested module
with no tag. The store repository required it at `v0.0.0` and resolved that with a
`replace`, which Go ignores in a *dependency's* go.mod. The store module's own
latest tag (`v1.5.0`) still declared its path as
`github.com/the-protobuf-project/orm`, from before the rename. Together: no
downstream module could build against `github.com/the-protobuf-project/store` at
all. The recommendation here was to tag the nested module.

**Act two — the nested module was deleted instead.** Commit `4a0752d` ("Remove
entity module and related files") dropped `plugin/entity/go.mod`, folding entity
into the root. The import path did not change; only the module providing it did.
That broke this repository outright, because a directory `replace` names a module
and there was no longer a module there:

```
github.com/the-protobuf-project/store/plugin/entity@v0.0.0
  (replaced by ../../protorm/plugin/entity): no such file or directory
```

Fixed by dropping the vestigial requirement and its replace. Nothing in this
repository had changed; a dependency was refactored in another one, correctly, and
this was invisible from here until the next build.

**Act three — store is published, and the ghost bites.** There is now a
`github.com/the-protobuf-project/store v1.5.1`, tagged under its own module path.
The central claim of act one is fixed: this repository builds from the proxy with
no workspace at all, and `GOWORK=off go test ./...` passes.

But the deleted nested module is still resolvable at its old commits, so it is
still a module Go can find. Require it and the build fails a new way:

```
ambiguous import: found package github.com/the-protobuf-project/store/plugin/entity
in multiple modules:
	github.com/the-protobuf-project/store v1.5.1
	github.com/the-protobuf-project/store/plugin/entity v0.0.0-20260812070629-e76c7629c5d9
```

Both contain the package, so neither wins. **A deleted nested module does not stop
existing** — deleting its go.mod removes it from the repository's future, not from
the proxy's memory, and anything that ever recorded a version of it keeps
resolving. This is the trap for anyone consuming this ecosystem, and it is invited
by `go mod tidy` run from an empty go.mod: with nothing else in the graph, Go
resolves the import to the longest matching module path, which is the ghost.

**The rule:** require `github.com/the-protobuf-project/store` and never
`github.com/the-protobuf-project/store/plugin/entity`. With store already in the
graph, tidy resolves the package from it and adds nothing. CI asserts it, in
`test.yaml`'s "Modules build standalone" job, rather than leaving it to be
remembered.

### What is still unpublished

`runtime-go` — cache, ulid, observability, telemetry. Only `examples/` needs any
of them; the plugin does not link against the runtime it generates calls into.

They resolve as pseudo-versions, but consuming them takes one more step than it
looks, and the reason is a semver detail worth stating outright. `runtime-go/cache`
requires its three siblings at `v0.0.0` and resolves that with a `replace` in its
own go.mod, which Go ignores in a dependency. So a consumer has to pin them — and
**a `require` cannot do it**: the only versions that exist are pseudo-versions of
the form `v0.0.0-<timestamp>-<hash>`, and a pseudo-version of `v0.0.0` sorts
*below* `v0.0.0` as a pre-release. Under MVS the unresolvable `v0.0.0` wins and the
build fails on `unknown revision 000000000000`. Only a `replace` overrides it.

`examples/go.mod` therefore carries three replaces, which is act one's workaround
moved from the workspace into the module that actually needs it.

**Upstream fix:** tag runtime-go. One release removes all three.

### A rename arrived with the same tag

`store v1.5.1` swapped `github.com/the-protobuf-project/opentelementry/opentelementry-go`
for `github.com/the-protobuf-project/telemetry/telemetry-go`. It is an indirect
dependency here and nothing in this repository names it, so it cost nothing — but
it is the third module-path change in this ecosystem to reach a consumer through a
version bump rather than an announcement, which is the pattern the tagging fix is
really about.

---

## 6. cache.v1 duplicated a runtime concept, and running it was what showed this

**Where:** `(cache.v1.cache_defaults).prefix`, now `reserved 5` in `cache.proto`

The vocabulary shipped a `prefix` field that namespaced every namespace derived in
a file. The cache runtime already has exactly that, as `cache.Config.Prefix`, set
where the client is built. The first program to set both simply concatenated them:

```
bookstore:bookstore.bookstore.v1.book.author.author-ursula:cache:...
^Config.Prefix
          ^the annotation
```

The stutter is the symptom; the fault is ownership. A prefix separates one
program's caches from another's in a shared server — a fact about a *deployment*,
not about a schema. Two deployments of one set of protos should be able to differ
in it, and an annotation that travels with the proto cannot express that.

Removed, with the field number reserved and the reasoning recorded in the proto.

**Worth noting how it was found.** Every test passed with the field in place —
goldens, determinism, agreement, the capability and namespace assertions. None of
them could have caught it, because each one compares this plugin against itself.
It took wiring the generated code to a real Redis and reading the keys back with
`redis-cli`. A generator's tests can prove its output is *consistent*; only running
the output proves it is *right*.

---

## 7. `cache.Aside` cannot be handed a value it already has

**Where:** `runtime-go/cache/aside.go` — the `Aside` interface

`Aside` has three methods: `GetOrLoad`, `Refresh`, `Invalidate`. There is no
`Put`. `Refresh` is documented as "runs the loader and overwrites the entry", and
that is exactly what it does.

The generated write path is `Create` → `s.inner.Create(ctx, m)` → `after(ctx, m)`
→ `s.aside.Refresh(ctx, m.ID, ...)`, and that `Refresh` calls the loader, which is
`s.inner.GetByID`. So **every Create and every Update re-reads from the database
the row it was just handed.** The value is sitting in the method's parameter.

Refreshing rather than invalidating is the right call — dropping the entry leaves
every reader arriving next to miss at once — so the choice is not between refresh
and invalidate. It is between refreshing from the value in hand and refreshing by
going back to the database for a copy of it, and only the second is expressible.

The asymmetry inside the runtime is what makes this look like an oversight rather
than a design: `Indexed.Update(ctx, id, value, opts...)` takes the value directly,
and the generated `refile` uses it. One strategy accepts a value it is given; the
one used on every write does not.

**Not worked around.** It cannot be, from here. Encoding the entry means naming
its key, and key layout is core's outright — the thing this whole plugin is
arranged around not doing. The alternative, calling `Invalidate` instead, trades a
redundant read for a thundering miss, which is worse.

**Upstream fix:** `Aside.Put(ctx, id, value, opts...)`, storing the value the
caller already holds under the same key `GetOrLoad` would write. The generated
`after` becomes one call with no database round trip, and `Refresh` stays for the
case it is actually for — warming an entry for a value you do *not* have.

---

## 8. An INDEXED resource is stored twice, and only writes file the index

**Where:** `runtime-go/cache/core/keyspace.go`, and the shape of the four
strategies

`{head}cache:aside:entry:{id}` and `{head}cache:idx:entry:{id}` are different
keys. A resource on `STRATEGY_INDEXED` gets both: `GetByID` reads and writes the
**Aside** entry, `ByISBN` reads the **Indexed** one, and `after` writes both. Two
copies of one row, in two keyspaces, with two write paths — and, per finding 7,
the Aside copy is written from a fresh database read while the Indexed copy is
written from the model in hand, so they are not even copies of the same read.

The second half is sharper than the storage cost. **The read path never files the
index.** `GetByID` populates Aside and nothing else, so a row that has been read a
million times is still invisible to `ByISBN`; only rows written through this
decorator since the entry was created are findable by field. `ByISBN`'s generated
doc comment says it reads the cache only and does not load through — which is
true, and is not the surprising part. The surprising part is that the cache it
reads is one the ordinary read path does not populate.

The insert path pays for the split a third time: `refile` calls
`Indexed.Update` first (only Update refiles a changed value, which is right) and
falls back to `Indexed.Create` on `ErrNotFound`, so an insert costs a failed round
trip before the successful one. A `Book` Create is therefore roughly two database
operations and four cache operations.

**Not worked around,** and it is not clear it should be from here. The generator
picks one strategy per resource because cache.v1 offers four and runtime-go
implements four; layering Aside over Indexed for one resource is a decision about
what the runtime's strategies *are*, not about how to generate against them.

**Upstream fix:** one of two, and the choice belongs to runtime-go. Either
`Indexed` grows a read-through `GetOrLoad` — at which point an INDEXED resource
needs no Aside entry at all and the duplication disappears — or `Aside` and
`Indexed` are documented as deliberately separate stores, in which case the
generated `GetByID` for an INDEXED resource should file the index on a miss so
that what `ByISBN` sees matches what has actually been read.

**Worth noting how this was found.** By reading the generated file top to bottom
and asking which key each call touched. Every test passes: the goldens are
byte-correct, the behavior tests exercise a read-through and an edge, the
determinism and agreement tests are green. None of them compares the Aside entry
to the Indexed one, because none of them had a reason to — which is the same shape
as finding 6, and the second time in this document that a whole-system property
escaped a suite that checks each part against itself.

**And then confirmed the same way finding 6 was.** The example program no longer
drops its databases on the way out, so the keys survive the run:

```
$ redis-cli --scan --pattern 'bookstore:*'
bookstore:bookstore.v1.author:cache:aside:entry:author-ursula
bookstore:bookstore.v1.book.author.author-ursula:cache:aside:entry:book-lhd
bookstore:bookstore.v1.book.author.author-ursula:cache:idx:by:isbn:9780441013593
bookstore:bookstore.v1.book.author.author-ursula:cache:idx:entry:book-lhd
bookstore:bookstore.v1.book.author.author-ursula:cache:idx:fields:book-lhd
bookstore:bookstore.v1.book.author.author-ursula:cache:idx:index
```

`book-lhd` is there twice, under `aside:entry:` and under `idx:entry:`, from one
Create of one row. That is the finding, in a form nobody has to take on trust —
and it is the second time in this document that leaving a real backend's keyspace
readable is what turned an argument into an observation.

---

## 9. An INDEXED resource's index sets never expire

**Where:** `runtime-go/cache/core/filing.go:24,29` — `SetAdd` with no expiry

Read the TTLs back after a run and the entries expire while the index sets do not:

```
317   aside:entry:author-ursula
107   aside:entry:book-lhd
107   idx:entry:book-lhd
 -1   idx:by:isbn:9780441013593
 -1   idx:index
 -1   idx:fields:book-lhd
```

Half of that is deliberate and documented. `core` says so directly: "Entries
expire on their own and leave their index members behind, so lookups verify
liveness and sweep as they go." A set cannot expire with its members, because the
members belong to different entries with different lifetimes, so the sweep is
moved to read time. That is a sound trade.

What the design does not account for is the value nobody queries again. Sweeping
happens in `liveMembers`, which runs when someone looks a value up — so
`idx:by:isbn:9780441013593` is cleaned only by a future `ByISBN` for *that ISBN*.
For a unique index, which is the kind this generator recommends and warns you
toward, a repeat lookup of one value is the exception rather than the rule. The
ordinary path is: a row is cached, its entry expires, and its index set survives
holding one dead member, permanently.

So an INDEXED resource leaks, per row whose entry expired without an explicit
delete:

- one `idx:by:{field}:{value}` key per distinct indexed value, and
- one `idx:fields:{id}` key per id.

`DeleteByID` does clean up — the generated code calls `Indexed.Delete` before
invalidating, which unfiles both. The leak is specific to expiry, which is the
common case for a cache with a TTL, and it grows with distinct values written
rather than with rows live.

**Not worked around,** and it cannot be from here. These are keys, and key layout
is `core`'s outright; the generated decorator has no name to sweep by. It is also
not visible from anything this repository can test: the fake driver has no notion
of expiry, so no unit test here would ever see a key outlive its entry.

**Upstream fix:** give the index sets a TTL — the resource's, plus a margin —
refreshed on each `SetAdd`, so a set outlives its newest member and no longer.
Redis expires it for free after that; a sweep on read stays as the correctness
mechanism for members that die early. Failing that, `Indexed` needs a maintenance
call the caller can schedule, and the fact that one is needed belongs in the
`Indexed` contract next to the hot-key warning, which is currently the only
scaling caveat it names.

**Worth noting how it was found.** By running the example against a real Redis and
asking `TTL` about every key it left. This is now the third finding in this
document that survived every test and was caught by reading a live keyspace —
after finding 6's double prefix and finding 8's double storage. The pattern is
consistent enough to be the recommendation: a generator's tests prove its output
is consistent with itself, and nothing but a real backend proves it is right. CI
runs the example against a Redis service container for exactly this reason.

---

## What did *not* need working around

Worth recording, because the parts that held are the design working:

- **`AuthorStoreIface` was already the right seam.** protoc-gen-store emits an
  interface per resource with a doc comment describing this exact use case
  ("so a decorator — caching, tracing, retries, a test double — can be written in
  its own package"). The decorator needed no store change because someone had
  already thought about it.
- **`entity.Reader()` composed exactly as intended.** Imported, not reimplemented;
  `TestIRAgreement` passes against the real protoc-gen-store over the same protos.
- **`entity.Layout` / `entity.LayoutConfig`** are shared too, so both plugins
  resolve `strip_version` and `dedupe_schema_table` through one implementation.
- **cache.v1 supplies no structure at all.** The reader implements `FacetReader`
  and deliberately not `StructureReader`, so this plugin has no *mechanism* by
  which it could move a neutral name. `TestReaderIsNotAStructureReader` pins the
  omission, since the agreement test would only catch a violation on names some
  case happens to exercise.

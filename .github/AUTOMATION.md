# Repository automation

## What runs, and when

| Trigger | Workflow | Does |
|---|---|---|
| push / PR to `main` | `test.yaml` | six jobs — see below |
| push / PR to `main` | `lint.yaml` | Go, proto and AIP lint |
| tag `v*` | `release.yaml` | GoReleaser: the binary for 6 platforms, a Homebrew cask, a GitHub release |
| tag `v*` | `publish.yaml` | `buf push`: the `cache.v1` vocabulary to the BSR |
| Test or Lint completes | `dependabot-auto-merge.yaml` | merges a Dependabot PR once every check on the commit is green |

### The test jobs, and why each exists

| Job | Covers |
|---|---|
| `Test (Go)` | the generator: goldens, IR agreement, refusals, the manifest, the boundary |
| `Test (examples)` | the *generated* code — `examples/` is a module of its own, so the root's `go test ./...` never reaches it |
| `Modules build standalone` | `GOWORK=off` in both modules, plus a guard on the deleted `store/plugin/entity` module |
| `Integration (Redis)` | the generated decorator against a real backend, asserting on the keyspace |
| `Examples reproduce` | the committed output still matches what both plugins emit |
| `Build (os/arch)` | the binary cross-compiles for six platforms |

Three of those are specific to this repository and worth explaining.

**`Integration (Redis)` is the one that earns its keep.** Three of the findings in
[docs/boundary-findings.md](../docs/boundary-findings.md) — the duplicated key
prefix (6), an `INDEXED` resource being stored twice (8), and index sets that
never expire (9) — passed every unit test here and were caught only by running
the generated code against Redis and reading the keyspace back. A generator's
tests prove its output is consistent with itself; nothing but a real backend
proves it is right. So CI runs the example against a service container and
asserts on the keys: that the scope value reached them, that the index set was
filed, and that no key sits outside the `cache:` segment `core` owns.

**`Modules build standalone` protects a property that regresses silently.** This
repository needed a `go.work` to build at all until `store` was tagged under its
own module path. It does not any more, and the way that reverts is quiet —
someone adds a `replace`, the workspace papers over it locally, and consumers
find out at `go get`. `GOWORK=off` is the consumer's view.

That job also guards the ghost module. `store/plugin/entity` was a nested module,
it was deleted upstream and folded into the store root, and its old commits are
*still resolvable* — so requiring it puts two modules in the graph that both
contain that package and every build fails with `ambiguous import`. `go mod tidy`
against a thin `go.mod` is exactly how it gets reintroduced, which makes it worth
asserting rather than remembering. See finding 5.

**`Examples reproduce` installs both plugins.** The demonstration this repository
exists to make is that `protoc-gen-store` and `protoc-gen-cache` agree on every
name without coordinating, so the reproduce job runs both over one set of protos
and diffs the committed tree. `STORE_VERSION` is pinned in `test.yaml` for the
same reason goldens exist: store's output is this plugin's input, and a floating
store would fail this job on someone else's release.

## Dependabot auto-merge

Dependabot opens a pull request; `.github/workflows/dependabot-auto-merge.yaml`
merges it once every check on that commit has passed, and leaves it alone
otherwise. Nothing to configure — it works as committed.

### Why it does not use GitHub's auto-merge

The usual recipe is `gh pr merge --auto`, which delegates the decision to GitHub:
merge when the *required status checks* pass. Two reasons that is not what runs
here.

**It may be unavailable.** On a private repository in a free-plan organization,
branch protection, rulesets and native auto-merge are all withheld, and the API
returns `403: Upgrade to GitHub Pro or make this repository public`. `--auto`
would fail on every run.

**And it degrades quietly where it *is* available.** "Required status checks"
means required *by branch protection*. On a repository whose protection does not
name these checks, a pull request has no required checks, so it is mergeable the
instant it opens and `--auto` merges it before CI has finished. The automation
would look like it was gating on CI while gating on nothing.

So the workflow reads the check results itself. That behaves identically before
and after this repository becomes public, and there is no configuration that can
silently turn the gate off.

### What has to be green

Every check reported for the commit — not a list someone has to maintain. A list
has to be remembered, and the failure mode of forgetting is that auto-merge
quietly stops covering the job you just added. Requiring the whole set inverts
that: a new CI job gates auto-merge the moment it exists.

`skipped` and `neutral` count as passing, since a conditional job that did not
run has not failed.

There is one named list, `REQUIRED_CHECKS`, and it is a *floor* rather than the
gate: it names the checks that must have **reported at all**, so a commit whose
CI has not started yet — and which therefore has no failing checks — does not
sail through.

### Update types

All of them, including majors, gated on CI. Two dependencies are worth knowing
about specifically:

- **`protokit`** is the IR engine, and a major bump of it is the change most
  likely to alter *generated output* rather than break the build. What stands
  between that and a silent change to every consumer's cache is the golden and
  agreement tests.
- **`runtime-go/cache`** is what the *generated* code calls. A bump there can
  change what a decorator writes to a real backend without changing a byte of
  this repository's output, and `Integration (Redis)` is the only job that would
  see it.

Minor and patch updates are grouped into one weekly pull request per module;
majors arrive individually, so a group diff is never where a breaking bump hides.

## Branch protection

Optional — the auto-merge workflow works without it — but worth adding as defence
in depth, since it also stops a *human* merging past red CI:

```sh
gh api -X PUT repos/the-protobuf-project/cache/branches/main/protection \
  --input .github/branch-protection.json
```

The contexts in that file are the job `name:` fields the workflows report, and
they mirror `REQUIRED_CHECKS`. Verify with:

```sh
gh api repos/the-protobuf-project/cache/branches/main/protection/required_status_checks \
  --jq '.contexts'
```

## Secrets

Set under Settings → Secrets and variables → Actions:

| Secret | Used by | What |
|---|---|---|
| `HOMEBREW_TAP_GITHUB_TOKEN` | `release.yaml` | PAT with `repo` scope on `the-protobuf-project/homebrew-tap` |
| `BUF_TOKEN` | `publish.yaml` | BSR token with write access to the organization |

`BUF_TOKEN` is the only env var `buf` reads for BSR credentials, so `publish.yaml`
must export it under exactly that name; a token exported as anything else leaves
the push unauthenticated.

## Licensing

Apache-2.0, matching every other repository in the organization. `LICENSE` is
shipped inside each release archive by `.github/release/goreleaser.yaml`.

There are no per-file license headers, which is the convention the sibling
repositories follow too: the LICENSE file covers the work, and a header on every
generated and hand-written file would be noise in a codebase whose comments are
already doing real work.

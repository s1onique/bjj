# ACT-BJJ-LAB01 — Publication Authority Laboratory

**Status:** IMPL — authorized.

**Project:** Bounded Jujutsu (bjj)

**Repository:** https://github.com/s1onique/bjj

## Mission

Build the reproducible local laboratory in which BJJ's central
security property can later be implemented and falsified.

This ACT does not implement the publication boundary. It establishes
the deterministic laboratory needed to prove it.

The baseline recorded by this ACT is the opposite of the eventual
product invariant:

```text
unguarded raw git push       = PASS (CONTROL)
unguarded raw jj git push    = PASS (CONTROL)
```

Those passes are CONTROL observations, not product success.

## Doctrine established

- `jj` is the local VCS engine. BJJ does not replace it.
- BJJ owns authority transitions, not local VCS operations.
- Git-compatible hosting is infrastructure, not domain. No
  GitHub/GitLab specifics in the core transaction model.
- Hooks are not the security boundary. They may exist as UX.
- Command denial is defense-in-depth, not authority. The eventual
  invariant must hold even when an agent attempts `git push` directly.

## Scope

Implemented:

- `cmd/bjj` with `bjj version` and `bjj version --json`.
- `internal/execx` bounded subprocess seam.
- `internal/version` build-time identity with `-ldflags` injection.
- `internal/lab` publication laboratory:
  - Setup constructs a temp-rooted universe
    (`remote.git/`, `seed/`, `git-client/`, `jj-client/`, `home/`);
  - ControlRawGitPush mutates the disposable remote via raw git;
  - ControlRawJJPush mutates the disposable remote via `jj git push`;
  - NegativeTransport demonstrates that a failed transport does not
    mutate the remote.
- Typed `Evidence` JSON schema for machine auditing.

Deferred (non-goals for this ACT):

PublishPlan, subject digests, Factory gate, evidence freshness,
credential acquisition, SSH agent, token handling, GitHub/GitLab
API, branch protection, `bjj check`, `bjj publish`, `bjj receipt`,
stack verification, `jj run`, pre-push hooks, ClineMM integration,
CI enforcement.

## Acceptance contract

| Gate                              | Status |
|-----------------------------------|--------|
| REPOSITORY_BOOTSTRAPPED           | PASS   |
| GO_TEST_ALL                       | PASS   |
| GO_VET_ALL                        | PASS   |
| BJJ_BUILD                         | PASS   |
| BJJ_VERSION_TEXT                  | PASS   |
| BJJ_VERSION_JSON_STRICT           | PASS   |
| LAB_REPRODUCIBLE                  | PASS   |
| LAB_REMOTE_IS_LOCAL               | PASS   |
| REAL_ORIGIN_NOT_REFERENCED        | PASS   |
| CONTROL_RAW_GIT_PUSH              | PASS   |
| CONTROL_RAW_JJ_PUSH               | PASS   |
| REMOTE_GIT_STATE_VERIFIED         | PASS   |
| REMOTE_JJ_STATE_VERIFIED          | PASS   |
| TRANSPORT_FAILURE_OBSERVED        | PASS   |
| FAILED_TRANSPORT_REMOTE_UNCHANGED | PASS   |
| REAL_REMOTE_MUTATED               | NO     |
| PRODUCT_PUBLISH_IMPLEMENTED       | NO     |

## Tool versions recorded

| Tool | Version                |
|------|------------------------|
| go   | 1.26.6 (darwin/arm64)  |
| jj   | 0.41.0                 |
| git  | 2.54.0                 |

These versions are recorded at runtime by `internal/lab.Run` into the
emitted `Evidence` record.

## Jujutsu CLI assumptions

The lab uses the following jujutsu 0.41.0 syntax (verified at
implementation time):

```text
jj git clone <url> <path>
jj git remote rename origin lab
jj config set --repo user.name <name>
jj config set --repo user.email <email>
jj new main -m <message>
jj bookmark create feature -r @
jj git push --remote lab --bookmark feature --allow-empty-description
```

Differences from older jj versions (e.g. `jj bookmark track`/`untrack`
syntax) were avoided in favor of the 0.41 idiom.

## Stop condition

This ACT closes when its acceptance contract is satisfied. The next
expected ACT is:

```text
ACT-BJJ-PLAN01 — Deterministic Publication Subject / PublishPlan
```

It will be designed using what this ACT demonstrates about the real
Jujutsu control surface, not assumptions made beforehand.

## ACT-BJJ-LAB01-CORRECTION01 — Closure integrity

Three corrections were applied to make deterministic evidence and the
reported verdict agree:

### C1 — central lab prerequisites fail closed

The original tests in `internal/lab/*_test.go` did:

```go
if _, err := exec.LookPath("jj"); err != nil {
    t.Skipf("jj not on PATH: %v", err)
}
```

That allowed a degraded environment to report `PASS` while the
central publication laboratory was never executed. ACT contracts
require the canonical verification path to FAIL when either required
executable is unavailable.

Fix: `internal/lab/lab.go` now exposes `RequireGitAndJJ(tb)` (and a
lower-level `requireToolTB`) that calls `t.Fatalf` (not `Skip`) when
either tool is missing. The canonical prerequisite gate is
`TestRequiredLabToolsAvailable` in `internal/lab/lab_test.go`. Every
central acceptance test calls `RequireGitAndJJ(t)` at the top.

Property:

```text
make all
  without git -> FAIL
  without jj  -> FAIL
```

Verified empirically by running `go test -count=1 ./internal/lab/...`
with `PATH` containing only `go` and `jj` (no `git`); the run failed
closed, not skipped.

### C2 — patch hygiene

Two trailing blank lines at EOF were removed:

- `internal/execx/execx.go`
- `internal/lab/exec.go`

The Makefile now invokes `git diff --check` as the first step of
`make all`:

```makefile
all: check-diff vet test build

check-diff:
	git diff --check
```

`git diff --check` returns exit 0 against the corrected working tree.

### C3 — closure evidence consistency

Closure changeset counts and line statistics are generated from the
frozen closure subject. They are evidence artifacts, not durable
constants in this ACT document.

The original closure narrative claimed `25 new files`. The
machine-derived digest reports `26 files changed`. The corrected
closure asserts only the durable, freeze-stable outcome:

```text
CLOSURE_CHANGESET_COUNT_MATCHES_DIGEST = PASS
```

The actual file count and insertion totals at any given moment must
be obtained by re-running the canonical evidence pipeline against the
frozen subject (`jj diff --stat`); they are intentionally NOT
copied into durable documentation, because copying mutable numbers
into documentation creates drift the moment the subject changes.

This is the embryonic doctrine that motivates `PublishPlan` and
`SubjectDigest`: durable contract + frozen subject ⇒ generated
evidence, never mutable statistics transcribed into prose.

## ACT-BJJ-LAB01-CORRECTION02 — Evidence Non-Drift

Two bounded hygiene corrections applied after the CORRECTION01
closure was reviewed.

### C1 — durable docs carry no mutable changeset statistics

CORRECTION01 inadvertently copied a mutable changeset statistic
(insertion count) into durable ACT documentation. By the time the
corrected closure was reviewed, the subject had grown because
CORRECTION01 itself changed it. The doc had already drifted.

Fix: the explicit `files_changed`, `added_files`, and `insertions`
counts have been removed from this ACT document. Only the durable,
freeze-stable assertion remains:

```text
CLOSURE_CHANGESET_COUNT_MATCHES_DIGEST = PASS
```

Re-running `jj diff --stat` against the frozen closure subject at any
moment continues to be the authoritative source for transient
statistics.

```text
DURABLE_DOC_HAS_NO_STALE_CHANGESET_STATS = PASS
```

### C2 — `testing` removed from production `internal/lab`

CORRECTION01 introduced `RequireGitAndJJ(testing.TB)` as a public
symbol of the production package `internal/lab`. Test assertion
machinery should not leak into production.

Fix: the runtime prerequisite logic (`requireTool` returning
ordinary errors) remains in production. The test-side helper
`requireToolTB` and `RequireGitAndJJ` are moved into a new
`_test.go` file `internal/lab/prereq_test.go`. The production
package `internal/lab` no longer imports `testing`.

```text
PRODUCTION_IMPORTS_TESTING = NO
CENTRAL_PREREQUISITES_FAIL_CLOSED = PASS
```

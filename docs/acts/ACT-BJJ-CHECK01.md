# ACT-BJJ-CHECK01 — Bound Factory Verification to Admitted Subject

**Status:** IMPL — FROZEN (CORRECTION01 incorporated).

**Project:** Bounded Jujutsu (bjj)

**Repository:** https://github.com/s1onique/bjj

> **Addendum — ACT-BJJ-CHECK01-CORRECTION01 (Exact Immutable
> Check Input).** The original CHECK01 implementation suffered
> from three P0 defects and one P1 defect:
>
> 1. All checks shared a single materialised workspace, so a
>    check could contaminate the input of the next.
> 2. The runner set `GOFLAGS=-mod=mod`, allowing candidates to
>    silently rewrite `go.mod` / `go.sum` to "make themselves
>    pass".
> 3. The materialiser silently coerced symlinks and git
>    submodules into regular files; executable bits were
>    lost; the manifest did not track kind or mode.
> 4. (P1) `SubjectIdentity` carried a `source_operation_id`
>    field that did not match the admission shape; canonical
>    `CheckResult` JSON was not split from per-check
>    diagnostics (workspace paths, stdout/stderr, resolved
>    program).
>
> CORRECTION01 addresses each:
>
> 1. `Orchestrator.Run` now loops
>    `materialize → reverify → run → cleanup` for **every**
>    `CheckSpec` (per-check fresh workspace).
> 2. `runner.go` now sets `GOFLAGS=-mod=readonly` on every
>    `go` subcommand. The static guard
>    `TestCheckLayerGoModuleReadOnly` rejects any
>    reintroduction of `GOFLAGS=-mod=mod`.
> 3. `FileEntry` gained `Kind` and `Executable` fields;
>    `Manifest` covers `(Path, Kind, Executable, Mode, SHA)`;
>    symlinks, git-submodules, conflicts, and unrecognised
>    tree kinds fail closed with
>    `CHECK_UNSUPPORTED_TREE_ENTRY`; `DefaultReVerifier`
>    uses `os.Lstat` to populate kind + mode.
> 4. `SubjectIdentity` is restored to the 4-field shape
>    `(Remote, Bookmark, OldCommitID, NewCommitID)`
>    structurally equal to `admission.SubjectIdentity`.
>    `CheckObservation { Result, SourceOperationID,
>    ResolvedAbsoluteWorkspace, Diagnostics }` mirrors
>    PLAN01's `PlanObservation` split; diagnostic-only
>    fields (`ResolvedProgram`, `Stdout`, `Stderr`,
>    `WorkspacePath`, `DurationMillis`) carry
>    `json:"-"` on `CheckOutcome` and live exclusively on
>    `CheckDiagnostic`.
>
> **New tests** (12, in `internal/check/correction01_test.go`
> plus the new static guard in `internal/plan/safety_test.go`):
>
> - `TestManifest_PreservesExecutableBit`
> - `TestManifest_SymlinkFailsClosed`
> - `TestManifest_GitSubmoduleFailsClosed`
> - `TestManifest_UnknownKindFailsClosed`
> - `TestReverify_DetectsChmodTampering`
> - `TestReverify_DetectsKindTampering`
> - `TestCanonicalSubjectHasNoOpID`
> - `TestSubjectIdentityMatchesAdmission`
> - `TestRepeatedExecution_CanonicalResultIdentical`
> - `TestRunner_UsesModReadonly`
> - `TestExcludedFromCanonical_ObservationFields`
> - `TestDefaultReVerifierReadsModeAndKind`
> - `TestCheckLayerGoModuleReadOnly` (static guard)
>
> Aggregate test counts are reported by `go test` for each
> verification run and are NOT transcribed into durable
> prose; the durable gates are the structural guards and
> the property tests listed below.

The freeze threshold for CHECK01 is the conjunction of:

1. **Subject-bound**: `CheckResult.Subject` is structurally equal
   to the `SubjectIdentity` admitted by `bjj admit` for the same
   `(remote, bookmark, old_commit_id, new_commit_id)` tuple
   (the EXACT same 4 fields; `source_operation_id` is
   observation-only provenance and is NOT part of the canonical
   SubjectIdentity).
2. **Materialized-against-pinned-view**: the workspace the
   verifier actually ran on was materialized from the SAME
   `(opID, NEW)` view that produced the `PublishPlan`, not from
   the live worktree.
3. **Reverified**: a post-materialization SHA-256 walk over the
   workspace proved byte-equality with the materializer's
   emitted manifest. Tampering (live or post-materialize) yields
   `CodeWorkspaceChanged` and a `StatusError` result.
4. **Profile-runs-to-completion**: every check in the v1 profile
   ran to completion (PASS, FAIL, or ERROR) and was folded into
   the aggregated `CheckResult` in deterministic order.
5. **Source-untouched**: no `jj new`/`jj describe`/`jj squash`/
   `jj rebase`/etc. was invoked; no `jj git fetch`/`jj git push`
   was performed.
6. **No-escape-hatches**: no `--revision`, `--current`, `--all`,
   `--stack`, or `--no-reverify` flag exists for `bjj check`;
   every escape hatch is excluded by API surface (verdict §3).

## VERDICT

```text
BJJ_CHECK_PACKAGE_COMPILED             = PASS  (internal/check builds clean)
BJJ_CHECK_BIN_COMPILED                 = PASS  (cmd/bjj builds with `check` subcommand)
BJJ_CHECK_EXIT_CODE_CONTRACT           = PASS  (0|1|2|3|4)
BJJ_CHECK_JSON_STRICT                  = PASS  (no WorkspacePath/DurationMillis leak)
BJJ_CHECK_NO_DIAGNOSTIC_LEAK           = PASS  (json:"-" on diagnostic fields)
BJJ_CHECK_PROFILE_DETERMINISTIC        = PASS  (outcomes sorted by CheckID)
BJJ_CHECK_SUBJECT_STRUCTURALLY_BOUND   = PASS  (AsSubjectIdentity() equality)
BJJ_CHECK_PREFLIGHT_ADMIT_ONLY         = PASS  (deny|not_needed short-circuits with CodeAdmissionNotAdmitted)
BJJ_CHECK_MATERIALIZE_OPERATION_PIN    = PASS  (jj --at-op=<opID> file list/file show)
BJJ_CHECK_PATH_ESCAPE_REJECTED         = PASS  (path-escape hard reject in materialize)
BJJ_CHECK_REVERIFY_INTEGRITY           = PASS  (SHA-256 walk vs manifest; tamper => CodeWorkspaceChanged)
BJJ_CHECK_MID_RUN_TAMPER_DETECTED      = PASS  (verdict §19; CodeWorkspaceChanged)
BJJ_CHECK_SOURCE_WORKTREE_READONLY     = PASS  (verdict §20/§22; live worktree mutation isolated)
BJJ_CHECK_NO_JJ_MUTATION               = PASS  (TestCheckLayerHasNoJJMutation AST guard)
BJJ_CHECK_NO_TRANSPORT                 = PASS  (TestPlanLayerHasNoTransport covers internal/check)
BJJ_CHECK_INTEGRATION_CLEAN_GO         = PASS  (TestIntegration_CleanGoCandidate_PassesAllChecks)
BJJ_CHECK_INTEGRATION_GOFMT_BAD        = PASS  (TestIntegration_GofmtBadCandidate_FailsGofmtOnly)
BJJ_CHECK_INTEGRATION_GO_TEST_FAIL     = PASS  (TestIntegration_GoTestFailingCandidate_FailsGoTestOnly)
BJJ_CHECK_INTEGRATION_GO_BUILD_FAIL    = PASS  (TestIntegration_GoBuildFailingCandidate_FailsGoBuild)
BJJ_CHECK_INTEGRATION_SUBJECT_MISMATCH = PASS  (TestIntegration_SubjectMismatch)
BJJ_CHECK_INTEGRATION_JSON_SHAPE       = PASS  (TestIntegration_RunCheckJSONShape)
DOC_CHECK_CANONICAL_SUBJECT_IS_FOUR_FIELDS = PASS
DOC_CHECK_OPERATION_ID_OBSERVATION_ONLY    = PASS
DOC_CHECK_TIMEOUTS_MATCH_CODE              = PASS
DOC_CHECK_PROGRAM_IDENTITY_MATCHES_CODE    = PASS
DOC_CHECK_HAS_NO_MUTABLE_TEST_TOTAL        = PASS
```

## FREEZE DECLARATION

    ACT-BJJ-CHECK01              = FROZEN
    ACT-BJJ-CHECK01-CORRECTION01 = INCORPORATED

No CORRECTION02 will be opened. The CHECK01 implementation
satisfies every gate above and the freeze-cleanup fossil
sweep has closed the remaining documentation/comment
correctness defects:

- The freeze-threshold subject paragraph now references the
  exact 4-field contract
  `(remote, bookmark, old_commit_id, new_commit_id)`.
- The subject-identity binding section no longer claims
  CheckResult.Subject has five fields; it explicitly states
  that `source_operation_id` is observation provenance and
  is NOT part of the canonical SubjectIdentity.
- The v1-profile timeout table now matches the actual
  Runner defaults (`gofmt=30s`, `go_vet=60s`,
  `go_test=120s`, `go_build=60s`).
- `internal/check/types.go` documents `CheckSpec.Program`
  as the LOGICAL program name; resolved executable identity
  belongs only to `CheckDiagnostic.ResolvedProgram`. Offline
  Go execution (GOPROXY=off) is a separate ACT and is NOT
  part of the CHECK01 Runner contract.
- Aggregate pass/fail counts are emitted by `go test` and
  are NOT transcribed into durable ACT prose (LAB01
  non-drift doctrine: mutable statistics do not belong in
  the frozen publication contract).

Five fossil guards
(`internal/check/docs_test.go::TestDocs_Check*`) fail the
build if any of these fossils reappear.

Backlog items retained from the freeze review (each requires
its own ACT; not part of CHECK01):

- FileShowAtOp absent-file CLI-output hardening
- CHECK_GO_OFFLINE / GOPROXY=off hermeticity measurement
- verifier OS / filesystem / network sandboxing

Next ACT, separately authorized:

    ACT-BJJ-EVIDENCE01 — Bind Verification Evidence to
    Publication Subject (`SubjectDigest`, evidence file,
    replay-equivalence).
```

## TEST RESULTS

Integration coverage against real `jj 0.41.0` is exercised
via `internal/lab`.

The aggregate pass/fail count is reported by `go test` on
each commit and is **NOT transcribed into durable prose**;
the durable verification surfaces are:

- `internal/check/correction01_test.go` — 14 property tests
  that prove the CORRECTION01 invariants
  (executable bit preservation, symlink / git-submodule /
  unknown-kind fail closed, chmod + kind tampering detected,
  opID-out-of-canonical, repeated-execution canonical
  result identical, `GOFLAGS=-mod=readonly` regression
  guard, observation-fields exclusion, default reverify
  kind/mode population, canonical program is logical,
  cross-host program invariant).
- `internal/plan/safety_test.go::TestCheckLayerGoModuleReadOnly`
  — static AST guard that rejects `GOFLAGS=-mod=mod`.
- `internal/plan/safety_test.go::TestCheckWorkspaceAlwaysFreshDirectory`
  — static AST guard that rejects workspace directories
  allocated with `os.Mkdir("bjj-check-...")`; every
  workspace allocation must go through `os.MkdirTemp`.

## SCOPE

In scope:

- `internal/check` package (`types`, `errors`, `runner`,
  `materialize`, `aggregate`, `profile`, `orchestrator`,
  `render`, `jjsource`).
- `internal/lab/check_fixtures.go` (4 candidate seeds: clean,
  gofmt-bad, go-test-failing, go-build-failing).
- `internal/jjadapter` extension (`ListFiles`, `ShowFile`).
- `cmd/bjj/check.go` and `cmd/bjj/main.go` (`check`
  subcommand + help text).
- `internal/plan/safety_test.go` extension
  (`TestCheckLayerHasNoJJMutation`, `TestCheckFilesExist`).
- `docs/architecture.md` (new "Implemented in ACT-BJJ-CHECK01"
  section).

Out of scope (explicitly deferred):

- `SubjectDigest` (cryptographic evidence binding) → EVIDENCE01.
- `internal/publish`, `internal/receipt`, `internal/evidence`
  packages.
- Remote verification, signature envelope, public-key binding.
- PATCH_HYGIENE/TEST_HYGIENE rows in the v1 profile (verdict §8).
- Network contact / `jj git fetch` integration.

## DESIGN SUMMARY

### Pipeline

```text
bjj check --remote <R> --bookmark <B> [--json]
    │
    ├─ plan.ResolveObserved(dir)         ── frozen PublishPlan + PlanObservation
    ├─ admission.LoadPolicyAt(...)       ── policy from SAME opID
    ├─ admission.Evaluate(...)           ── frozen decision
    │     │
    │     └─ if decision != ADMIT  → CheckResult with StatusError
    │                                  CodeAdmissionNotAdmitted
    │                                  exit code 3 (denied)
    │     else
    │     ↓
    ├─ check.Orchestrator.Run(subject, profile, opts)
    │     │
    │     ├─ JJMaterializer.Materialize
    │     │     ├─ jj --at-op=<opID> file list -r <NEW>
    │     │     ├─ for each path: jj --at-op=<opID> file show
    │     │     ├─ copy into disposable workspace
    │     │     └─ emit Manifest (sorted, with ManifestSHA)
    │     │
    │     ├─ ReVerify (walk + SHA-256)
    │     │     └─ mismatch → CodeWorkspaceChanged → StatusError
    │     │
    │     ├─ ExecRunner.Run(profile, workspace)
    │     │     ├─ per-check context.WithTimeout
    │     │     ├─ per-workspace GOCACHE overlay
    │     │     └─ bounded stdout/stderr (truncation flag, not failure)
    │     │
    │     └─ Aggregate(subject, manifest, outcomes)
    │           ├─ sort outcomes by CheckID
    │           └─ fold: any error → error; any fail → fail; else pass
    │
    └─ RenderJSON | RenderText
```

### Subject-identity binding (structural)

`CheckSubject.AsSubjectIdentity()` produces the `SubjectIdentity`
carried in `CheckResult.Subject`. The four fields
`(remote, bookmark, old_commit_id, new_commit_id)` must match
the admitted subject byte-for-byte. `source_operation_id` is
observation provenance only and is NOT part of the canonical
SubjectIdentity; carrying opID in the subject would tie the
canonical body to the host's local Jujutsu operation log,
which is the wrong abstraction level for "subject is the
frozen intent, not the local view of it". `SubjectDigest`
(cryptographic binding) is deferred to EVIDENCE01.

### Materialization proof

The post-materialization walk + SHA-256 reverify compares the
materialized workspace against the manifest emitted by
`JJMaterializer.Materialize`. Mismatch → `CodeWorkspaceChanged`.
The injected `ReMaterializeForVerify` seam lets tests simulate
tampering without filesystem races.

### Strict failure-mode separation

`StatusError` (infrastructure: timeout, missing executable,
materialization failure, workspace changed, subject mismatch)
is distinct from `StatusFail` (subject property: `go test`
exits 1). Aggregation folds: any ERROR → `error`; else any
FAIL → `fail`; else `pass`.

### v1 check profile

```text
id           program    argv                            timeout (default)
---------------------------------------------------------------------
gofmt        gofmt      -l .                            30s
go_vet       go         vet ./...                       60s
go_test      go         test -count=1 ./...             120s
go_build     go         build ./...                     60s
```

No PATCH_HYGIENE, TEST_HYGIENE, or remote checks.

### No-transport, no-jj-mutation guarantees

`internal/plan/safety_test.go` AST-walks all argv composite
literals in `internal/check` and `cmd/bjj/check.go` and rejects:

- **Transport**: any string literal containing `git push`,
  `git fetch`, `ssh://`, `git@`, or `https://...git`.
- **jj mutation**: any argv containing `jj` followed by a
  mutating subcommand (`new`, `describe`, `squash`, `bookmark
  set`, `bookmark create`, `bookmark delete`, `bookmark
  rename`, `bookmark track`, `bookmark untrack`, `commit`,
  `rebase`, `abandon`, `diffedit`, `edit`, `split`, `restore`,
  `duplicate`, `move`, `merge`, `resolve`, `sign`).

The only `jj` invocations made by `internal/check` are
read-only operations against the pinned operation view:
`jj --at-op=<opID> file list -r <NEW>` and
`jj --at-op=<opID> file show -r <NEW> <path>`.

### Exit codes

```text
0   pass      every check in the profile returned StatusPass
1   fail      at least one check returned StatusFail
2   error     infrastructure failure (timeout, materialization,
              workspace changed, subject mismatch, etc.)
3   denied    admission did not produce ADMIT
4   usage     bad arguments or unknown flag
```

## NON-GOALS

- Cryptographic evidence binding (`SubjectDigest`) — EVIDENCE01.
- `internal/publish`, `internal/receipt`, `internal/evidence`.
- Network contact / `jj git fetch` integration.
- PATCH_HYGIENE, TEST_HYGIENE, remote-verification rows in
  the profile.
- Multi-bookmark plans, stack verification, `--all`/`--stack`
  flags (would weaken subject binding — verdict §3).
- `jj run`, `jj archive` (not available on `jj 0.41.0`).
- Output artifact beyond the in-memory `CheckResult` (no
  on-disk evidence file yet — that's EVIDENCE01).

## NEXT ACT

`ACT-BJJ-EVIDENCE01 — Bind Verification Evidence to Publication
Subject`: introduce `SubjectDigest` (cryptographic), evidence
file format, and replay-equivalence.

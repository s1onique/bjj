# Architecture

BJJ is designed around a small number of explicit trust zones and a
transaction pipeline that crosses from one zone to another.

> The architecture document records **intended** structure. Only the
> parts marked **Implemented in ACT-BJJ-LAB01**,
> **Implemented in ACT-BJJ-PLAN01**, or **Implemented in
> ACT-BJJ-ADMISSION01** have been built. Everything after
> ADMISSION01 is **future architecture**.

## Trust zones

```text
LOCAL PRIVATE STATE
    │
    ▼
BOUNDED PUBLICATION TRANSACTION
    │
    ▼
REMOTE SHARED STATE
    │
    ▼
CANONICAL INTEGRATION AUTHORITY
```

- **LOCAL PRIVATE STATE** — the developer's working copy and
  Jujutsu store. Owned by `jj`. BJJ does not try to replace it.
- **BOUNDED PUBLICATION TRANSACTION** — the BJJ-mediated hand-off
  from private to shared state. This is the only place where remote
  publication is authorized.
- **REMOTE SHARED STATE** — the Git-compatible remote. BJJ treats it
  as infrastructure, not domain. The current public repository
  happens to be on GitHub; future adapters may target GitLab,
  Gitea, or any compatible service.
- **CANONICAL INTEGRATION AUTHORITY** — the rules under which remote
  shared state becomes the project's canonical history (e.g. branch
  protection, CI, signed merges). BJJ does not own this layer.

## Future transaction

The publication transaction is a finite state machine. Each step
narrows the set of acceptable next steps.

```text
RESOLVE            [implemented in ACT-BJJ-PLAN01]
  ↓
PLAN_FROZEN        [implemented in ACT-BJJ-PLAN01]
  ↓
ADMITTED           [implemented in ACT-BJJ-ADMISSION01]
  ↓
CHECKED
  ↓
EVIDENCE_BOUND
  ↓
PLAN_REVALIDATED
  ↓
CAPABILITY_ACQUIRED
  ↓
TRANSPORT
  ↓
REMOTE_VERIFIED
  ↓
RECEIPT
```

Status meanings:

| Status              | Meaning                                                   |
|---------------------|-----------------------------------------------------------|
| RESOLVE             | Determine the candidate subject from local state.         |
| PLAN_FROZEN         | Capture an immutable plan describing what will publish.   |
| ADMITTED            | The candidate has passed pre-flight gates.                |
| CHECKED             | Verifications (policies, signatures, hashes) have run.    |
| EVIDENCE_BOUND      | The plan is bound to verifiable evidence.                 |
| PLAN_REVALIDATED    | The bound plan is re-checked just before transport.       |
| CAPABILITY_ACQUIRED | A scoped, short-lived capability has been issued.         |
| TRANSPORT           | The transport actually executed.                          |
| REMOTE_VERIFIED     | The remote has been re-inspected independently.           |
| RECEIPT             | A signed receipt is recorded.                             |

### Fundamental invariant (future)

```text
RAW_GIT_PUSH_WITHOUT_BJJ    = AUTH_FAIL
RAW_JJ_GIT_PUSH_WITHOUT_BJJ = AUTH_FAIL
BJJ_PUBLISH                  = PASS
```

This invariant is **not** achieved by ACT-BJJ-LAB01. ACT-LAB01
establishes the reproducible laboratory in which it will later be
designed, falsified, and proven.

## Current ACT baseline (deliberately unbounded)

ACT-BJJ-LAB01 records the opposite baseline:

```text
unguarded raw git push       = PASS (CONTROL observation)
unguarded raw jj git push    = PASS (CONTROL observation)
```

Those passes are **CONTROL observations**, not product success. They
demonstrate the exact authority leak later ACTs must remove.

## Implemented in ACT-BJJ-LAB01

- `cmd/bjj` — minimal CLI exposing only `bjj version` /
  `bjj version --json`.
- `internal/execx` — bounded subprocess seam:
  - argv passed structurally (no shell);
  - absolute working directory required;
  - explicit environment overlay stripping credential-bearing
    variables (GITHUB_TOKEN, SSH_*, JJ_CONFIG, HOME by default);
  - bounded stdout/stderr capture with truncation marker;
  - typed errors that retain program, argv, exit, and bounded stderr.
- `internal/version` — build-time identity with `-ldflags`
  injection.
- `internal/lab` — publication laboratory:
  - constructs the entire universe under a temp dir:
    `remote.git`, `seed/`, `git-client/`, `jj-client/`, `home/`;
  - control experiment A: raw `git push` against a local bare remote;
  - control experiment B: raw `jj git push --remote lab` against the
    same local bare remote;
  - negative transport: push to a non-existent local remote;
  - isolation assertions: lab remote is local, real origin is never
    referenced, HOME credential is not needed.
- `Evidence` (typed, strict-JSON) — the lab produces a
  machine-checkable record of every observation.

## Explicit non-goals (deferred from ACT-LAB01)

- PublishPlan / subject digests
- Factory gate execution
- Evidence freshness guarantees
- Credential acquisition / SSH agent management / token handling
- GitHub / GitLab API integration
- Branch protection configuration
- `bjj check`, `bjj publish`, `bjj receipt`
- Stack verification
- `jj run` integration
- pre-push hooks as the security boundary
- ClineMM executor integration
- CI enforcement

These belong to subsequent ACTs. Each will be designed using what
ACT-LAB01 demonstrates about the real Jujutsu control surface.

## Implemented in ACT-BJJ-ADMISSION01

ADMISSION01 closes the **PLAN_FROZEN → ADMITTED** transition.
PLAN01 freezes the publication subject; ADMISSION01 evaluates
that subject against an explicit policy and emits a typed
decision (`admit` | `deny` | `not_needed`) before any expensive
verification runs.

### Implemented in ACT-BJJ-ADMISSION01-CORRECTION01

Three properties the original closure only claimed are now
implemented and tested:

1. **Subject-set exactness.** The fact gatherer reads
   `PublishPlan.Commits` verbatim from
   `planView.OutgoingCommits()` and restricts every predicate
   query to that exact set. Already-published ancestors can no
   longer widen admission.

2. **Fail-closed policy schema.** Unknown keys in
   `.bjj/policy.toml` produce a hard `POLICY_INVALID` error
   instead of being silently ignored. `schema_version` is the
   deliberate forward-compatibility boundary.

3. **Real package decoupling.** The concrete
   `*plan.PublishPlan -> admission.PlanView` adapter lives in
   `cmd/bjj/plan_adapter.go`. `internal/admission` does not
   import `internal/plan`; the boundary is enforced by
   `TestAdmissionHasNoDirectPlanImport`.

### Implemented in ACT-BJJ-ADMISSION01-CORRECTION02

Three remaining P1 defects closed after the CORRECTION01 review.
No architecture changes; this is a policy-parser hardening and a
documentation-truth pass.

1. **Duplicate policy keys fail closed.** `parsePolicyTOML`
   tracks every key in a `saw` map and returns
   `POLICY_INVALID` ("duplicate policy key %q") the second
   time the same key appears, regardless of whether the key is
   recognised or unknown. The bounded parser preserves the
   fail-closed duplicate-key rule that the TOML spec already
   mandates ("Defining a key multiple times is invalid"), so a
   generated/merged policy file cannot quietly weaken a rule
   by repeating the same key with a different value
   (e.g. `private_commits = "X"` followed by
   `private_commits = "none()"`).

2. **schema_version is required.** A present policy file MUST
   declare exactly one `schema_version = 1` line. The parser
   tracks `schemaSeen` and fails closed otherwise. The
   absent-file case still resolves to `DefaultPolicy()` with
   `PolicyOutcomeDefault`. The `schema_version` line is now an
   explicit schema boundary that future migrations can rely on.

3. **Documentation truth.** The post-CORRECTION01 review found
   three stale prose passages in the durable documents
   (malformed-policy description still saying "Unknown keys are
   ignored for forward-compatibility", the fact-gathering
   contract listing `subject | policy.PrivateCommits` (union)
   instead of the actual intersection, and the architecture
   section still describing a four-query gatherer with
   `::NEW ~ root()`). All replaced; the replacement text is
   enforced by `TestDocs_*` regression tests in
   `internal/admission/docs_test.go`.

### Implemented in ACT-BJJ-ADMISSION01-CORRECTION03

The post-CORRECTION02 review identified one P0 defect capable
of false admission or denial. `.bjj/policy.toml` was read
from the live working-tree filesystem via `os.ReadFile(<dir>/...)`
AFTER `plan.ResolveObserved` had pinned the operation view.
PLAN01 and admission facts were pinned to `OP_A`, but the
policy load ran against the live filesystem, which could be a
different operation view by the time it executed. CORRECTION03
binds the policy read to the same frozen Jujutsu view that
produced the PublishPlan.

What changed:

1. **Policy view binding (P0).** `.bjj/policy.toml` is now read
   via `jj --at-op=<OP_A> file show -r <NEW> .bjj/policy.toml`
   in `cmd/bjj/admit.go`. `internal/jjadapter.Adapter` gains
   `FileShowAtOp(ctx, dir, opID, revision, path) ([]byte,
   FilePresence, error)` with a typed PRESENT/ABSENT
   discriminator and explicit input validation (empty opID,
   revision, or path are rejected with `ErrJJFailed`).
2. **Candidate-local policy (P0 follow-on).** Policy bytes
   are read from `PublishPlan.BookmarkMoves[0].New.CommitID`
   as visible at `obs.SourceOperationID`. `internal/admission`
   gains a `PolicyReader` interface and a new entry point
   `LoadPolicyAt(ctx, reader, planView, dir, opID)` that the
   production CLI uses. The legacy `LoadPolicyFromRepo(dir)`
   helper is retained only for parser unit tests.
3. **DefaultPolicy semantics on absence (P0 follow-on).**
   `DefaultPolicy()` now applies only when the file is
   genuinely absent from the candidate tree at the pinned
   view. A working-copy mutation between plan resolution and
   policy load cannot change that outcome.
4. **Static guard (P0 follow-on).**
   `TestCmdAdmissionPathUsesPolicyReader` in
   `internal/plan/safety_test.go` walks `cmd/bjj/*.go`
   (non-test) and AST-rejects any caller that invokes
   `admission.LoadPolicyFromRepo`. The production admission
   decision path cannot regress to live-fs reads without this
   test failing.
5. **Documentation fossil (P2).** The durable prose repeatedly
   described the bounded parser as more restrictive than
   TOML itself with respect to duplicate keys. TOML already
   mandates that "Defining a key multiple times is invalid"
   (v1.1.0 spec). Corrected to "preserves the fail-closed
   duplicate-key rule that the TOML spec already mandates".

Invariants the P0 closure establishes:

```text
ADMISSION_PLAN_VIEW   == OP_A
ADMISSION_FACT_VIEW   == OP_A
ADMISSION_POLICY_VIEW == OP_A
```

i.e. the canonical publication subject, the candidate policy
bytes, and the admission facts all come from the same frozen
Jujutsu operation view. CHECK01 can now build on a single
admission decision.

`policy provenance != policy authority`: this closure binds
the provenance of repository-local policy bytes but does NOT
make that policy authoritative. Authoritative policy remains
deferred to a later ACT.

### New packages

- `internal/admission` — pure evaluator + typed fact gatherer:
  - `AdmissionInput`, `AdmissionFacts`, `AdmissionResult`,
    `SubjectIdentity`, `Reason`, `Policy`;
  - `MoveRelation` (`CREATE` | `FAST_FORWARD` |
    `NON_FAST_FORWARD` | `NO_CHANGE`);
  - `Decision` (`admit` | `deny` | `not_needed`);
  - typed `ReasonCode` (`NO_REMOTE_CHANGE` | `NON_FAST_FORWARD` |
    `NEW_BOOKMARK_NOT_ALLOWED` | `PRIVATE_COMMIT` |
    `CONFLICTED_COMMIT` | `EMPTY_DESCRIPTION` |
    `FACTS_INCONSISTENT`) in canonical total order;
  - typed `ErrorCode` (`JJ_QUERY_FAILED` | `JJ_PARSE_FAILED` |
    `POLICY_INVALID` | `INCONSISTENT_REPOSITORY_VIEW` |
    `NO_BOOKMARK_MOVE`);
  - `Policy` with `DefaultPolicy()` and `LoadPolicyFromRepo()`
    reading `.bjj/policy.toml` (failed-closed on malformed
    input; never silently falls back to defaults);
  - `PlanView` adapter — slim interface so the evaluator is
    decoupled from `internal/plan`. The concrete
    `*plan.PublishPlan -> admission.PlanView` bridge lives in
    `cmd/bjj/plan_adapter.go`; `internal/admission` does not
    import `internal/plan`.
- `internal/jjadapter` extensions:
  - new `CommitObs` template and `ListCommitObs` method
    (carrying `commit_id`, `change_id`, `conflict`, and
    `description.first_line()`);
  - `ParseCommitObsLines` fail-closed on any malformed row.
- `internal/lab` extensions:
  - `SeedConflictedCommit` fixture for `CONFLICTED_COMMIT`
    evidence tests.

### Process model

ADMISSION01 is read-only:

- no `git push`, `git fetch`, `jj git push`, `jj git fetch`;
- no `jj new`, `jj describe`, `jj bookmark set`, `jj squash`;
  these mutate the working-copy view and would invalidate the
  pinned operation id;
- no transport, no remote contact;
- no policy update; policy is loaded once and frozen per run.

### Fact-gathering contract

The `Gatherer` is the only place that talks to the adapter. It
issues at most three bounded `jj` queries, all pinned to
`--at-op=<OpID>`:

```text
1. ancestry probe      (OLD & ::NEW)              (classifyMove)
2. private predicate   (subject & policy.PrivateCommits)
3. per-commit obs      (subject)                  (ListCommitObs)
```

The canonical subject is taken VERBATIM from
`PublishPlan.Commits` via `planView.OutgoingCommits()`; the
gatherer never issues a fourth `::NEW ~ root()` query to
reconstruct the outgoing set. `subject & policy.PrivateCommits`
is an INTERSECTION, so an already-published private ancestor
that matches the policy expression does NOT participate in
admission.

The `OpID` is captured from the source `PlanObservation` and
threaded through to every query. Mid-gather mutation cannot mix
views because the production `Source` refuses to re-pin (an
empty `OpID` yields `INCONSISTENT_REPOSITORY_VIEW`).

```text
ADMISSION_FACTS_SAME_VIEW = PASS
ADMISSION_MID_GATHER_MUTATION_CANNOT_MIX_VIEWS = PASS
```

### Static safeguards

The `TestPlanLayerHasNoTransport` AST guard is extended to walk
`internal/admission` and reject any `[]string` literal
containing `git push`, `git fetch`, `jj git push`, or
`jj git fetch`. The `internal/admission` package has NO direct
import of `internal/plan`; the dependency goes through the
`PlanView` adapter. The `evaluate.go` file has NO imports of
`os`, `net`, `path/filepath`, or any I/O package.

### New CLI surface

- `bjj admit --remote <R> --bookmark <B>`
- `bjj admit --remote <R> --bookmark <B> --json`
- `bjj help` now lists `admit`.

### CLI exit codes

| Code | Meaning                                                   |
|------|-----------------------------------------------------------|
| 0    | admitted                                                  |
| 1    | invalid CLI arguments                                     |
| 2    | internal / infrastructure failure (typed `*Error`)        |
| 3    | valid decision but denied or not_needed                   |

### Default policy

When `.bjj/policy.toml` is absent, `DefaultPolicy()` is used:

```text
allow_new_bookmarks      = true
allow_non_fast_forward   = false
require_description      = true
allow_conflicted_commits = false
private_commits          = "none()"
```

### Properties

| Property                                                | Status |
|---------------------------------------------------------|--------|
| BJJ_ADMIT_COMMAND_IMPLEMENTED                           | PASS   |
| BJJ_ADMIT_DECISION_TYPED                                | PASS   |
| BJJ_ADMIT_EXIT_CODE_CONTRACT                            | PASS   |
| BJJ_ADMIT_JSON_STRICT                                   | PASS   |
| BJJ_ADMIT_DETERMINISTIC                                 | PASS   |
| BJJ_ADMIT_FACTS_SAME_VIEW                               | PASS   |
| BJJ_ADMIT_MID_GATHER_MUTATION_CANNOT_MIX_VIEWS          | PASS   |
| BJJ_ADMIT_DEFAULT_POLICY                                | PASS   |
| BJJ_ADMIT_MALFORMED_POLICY_FAILS_CLOSED                 | PASS   |
| BJJ_ADMIT_FACT_QUERY_FAIL_FAILS_CLOSED                  | PASS   |
| BJJ_ADMIT_FF                                            | PASS   |
| BJJ_ADMIT_NFF_DENY_DEFAULT                              | PASS   |
| BJJ_ADMIT_NFF_ALLOWED_BY_POLICY                         | PASS   |
| BJJ_ADMIT_PRIVATE_COMMIT                                | PASS   |
| BJJ_ADMIT_PRIVATE_ANCESTOR                              | PASS   |
| BJJ_ADMIT_CONFLICTED_COMMIT                             | PASS   |
| BJJ_ADMIT_EMPTY_DESCRIPTION                             | PASS   |
| BJJ_ADMIT_NO_REMOTE_CHANGE                              | PASS   |
| ADMISSION_LAYER_HAS_NO_TRANSPORT                        | PASS   |
| ADMISSION_NO_DIRECT_PLAN_IMPORT                         | PASS   |

CORRECTION02 properties:

| Property                                                | Status       |
|---------------------------------------------------------|--------------|
| POLICY_DUPLICATE_KEY_FAILS_CLOSED                       | FAIL_CLOSED  |
| POLICY_DUPLICATE_CANNOT_WEAKEN_POLICY                   | PASS         |
| POLICY_DUPLICATE_SCHEMA_VERSION                         | FAIL_CLOSED  |
| POLICY_SCHEMA_MISSING                                   | FAIL_CLOSED  |
| POLICY_SCHEMA_DUPLICATE                                 | FAIL_CLOSED  |
| POLICY_SCHEMA_UNSUPPORTED                               | FAIL_CLOSED  |
| POLICY_ABSENT_FILE_STILL_DEFAULT                        | PASS         |
| DOC_ADMISSION_UNKNOWN_KEYS_MATCH_CODE                   | PASS         |
| DOC_ADMISSION_QUERY_CONTRACT_MATCHES_CODE               | PASS         |
| DOC_ADMISSION_PLAN_BRIDGE_MATCHES_CODE                  | PASS         |
| DOC_ADMISSION_PRIVATE_PREDICATE_MATCHES_CODE            | PASS         |

See `docs/acts/ACT-BJJ-ADMISSION01.md` for the full acceptance
matrix and CLI samples.

## Implemented in ACT-BJJ-PLAN01

PLAN01 closes the **RESOLVE → PLAN_FROZEN** transition.

### New packages

- `internal/jjadapter` — bounded `jj` adapter:
  - typed records (`CommitRef`, `BookmarkRef`, `Snapshot`,
    `RemoteRef`, `Op`);
  - structured argv builders (no shell, no string concatenation);
  - the subprocess seam enforces the same credential-stripping
    overlay as `internal/execx`;
  - adapter never invokes `git push`, `git fetch`, `jj git push`,
    `jj git fetch`;
  - `ErrJJFailed` retains program, argv, exit code, and bounded
    stderr;
  - `ErrJJParseFailed` (post-CORRECTION02) — typed parse failure
    raised whenever `jj` output does not match the
    machine-oriented template. The parser fails closed: any
    malformed row produces an error rather than being silently
    skipped or partially-populated. The resolver maps this to
    `JJ_QUERY_FAILED`.
- `internal/plan` — domain model and resolver:
  - `PublishPlan`, `BookmarkMove`, `CommitRef`, `Remote`, `Status`
    (`planned` | `no_remote_change`);
  - typed errors with stable `ErrorCode` constants
    (`REMOTE_NOT_FOUND`, `LOCAL_BOOKMARK_NOT_FOUND`,
    `LOCAL_BOOKMARK_CONFLICTED`, `REMOTE_BOOKMARK_CONFLICTED`,
    `NO_REMOTE_CHANGE`, `JJ_QUERY_FAILED`,
    `INCONSISTENT_REPOSITORY_VIEW`);
  - `JJSource` / `Source` interface — one operation id is pinned
    exactly once at `Snapshot()` time; every subsequent
    `CommitsAt` is bound to that same operation id via
    `--at-op=<id>`. The source refuses an empty opID with the
    typed `INCONSISTENT_REPOSITORY_VIEW` error so re-pinning
    from inside a Resolve is structurally impossible
    (post-CORRECTION02);
  - resolver distinguishes NEW bookmark creation from OLD→NEW move
    using `heads(::NEW & remote_bookmarks())` for the effective
    `OLD`. A failed effective-old query propagates as
    `JJ_QUERY_FAILED` rather than silently widening the outgoing
    set (post-CORRECTION02);
  - `RenderJSON` / `RenderText` / `RenderErrorJSON` enforce strict
    JSON with full identifiers.

### New CLI surface

- `bjj plan --remote <R> --bookmark <B>`
- `bjj plan --remote <R> --bookmark <B> --json`
- `bjj help` now lists `plan`.

### New ACTs

- `docs/acts/ACT-BJJ-PLAN01.md` records the acceptance matrix,
  static safeguards, and documented limitations.

### Properties

| Property                                                | Status |
|---------------------------------------------------------|--------|
| PLAN_SINGLE_VIEW_CONSISTENCY                            | PASS   |
| PLAN_MID_RESOLVE_MUTATION_CANNOT_MIX_VIEWS              | PASS   |
| PLAN_EFFECTIVE_OLD_QUERY_FAILS_CLOSED                   | PASS   |
| JJ_BOOKMARK_OUTPUT_MALFORMED                            | FAIL_CLOSED |
| JJ_COMMIT_OUTPUT_MALFORMED                              | FAIL_CLOSED |
| PLAN_MALFORMED_JJ_OUTPUT                                | JJ_QUERY_FAILED |
| DOC_SINGLE_VIEW_CONTRACT_MATCHES_CODE                   | PASS   |
| CLI_COMMENT_MATCHES_OUTPUT                              | PASS   |
| BJJ_PLAN_DETERMINISTIC                                  | PASS   |
| BJJ_PLAN_JSON_STRICT                                    | PASS   |
| BJJ_PLAN_NETWORK_ACCESS                                 | NO     |
| BJJ_PLAN_REMOTE_MUTATION                                | NO     |
| BJJ_PLAN_BOOKMARK_MUTATION                              | NO     |
| PLAN_LAYER_HAS_NO_TRANSPORT                             | PASS   |
| PLAN_CANONICAL_BODY_HAS_NO_REMOTE_URL                   | PASS   |
| PLAN_SUBJECT_METADATA_BOUNDARY_EXPLICIT                 | PASS   |
| PLAN_COMMIT_ORDER_CONTRACT_MATCHES_IMPLEMENTATION       | PASS   |
| PLAN_REMOTE_BASELINE                                    | LOCAL_KNOWN_REMOTE_STATE |

### Single-view consistency (post-CORRECTION01)

PLAN01 captures the snapshot's operation id exactly once and
threads it through every subsequent `CommitsAt` call. The
production `JJSource` refuses an empty opID with the typed error
`INCONSISTENT_REPOSITORY_VIEW`; re-pinning from inside a single
resolve is no longer possible at the adapter boundary.

The canonical `PublishPlan` body contains NO environment-specific
or observation-specific fields. `source_operation_id` and
`repository_path` live in a separate `PlanObservation` envelope
returned by `plan.ResolveObserved`; they are diagnostic only and
do not participate in canonical subject comparison.

## Explicit non-goals (deferred from ACT-PLAN01)

- `SubjectDigest` and evidence binding
- `bjj check`, `bjj publish`, `bjj receipt`
- Factory gates, admission policy, transport, remote verification
- Multi-bookmark plans, automatic outgoing-bookmark discovery,
  stack verification
- `--all`, `--tracked`, `--changed`, `--stack`, `--remote-only`
- Remote network contact or implicit `jj git fetch`

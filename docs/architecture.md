# Architecture

BJJ is designed around a small number of explicit trust zones and a
transaction pipeline that crosses from one zone to another.

> The architecture document records **intended** structure. Only the
> parts marked **Implemented in ACT-BJJ-LAB01** or **Implemented in
> ACT-BJJ-PLAN01** have been built. Everything after PLAN01 is
> **future architecture**.

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
RESOLVE
  ↓
PLAN_FROZEN
  ↓
ADMITTED
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

Status meanings (future):

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

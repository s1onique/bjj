# ACT-BJJ-PLAN01 — Deterministic Publication Subject / PublishPlan

**Status:** IMPL — closed (CORRECTION01 + CORRECTION02 applied).

**Project:** Bounded Jujutsu (bjj)

**Repository:** https://github.com/s1onique/bjj

## Mission

Implement BJJ's first real publication-domain primitive:

```text
PublishPlan
```

and expose:

```text
bjj plan
bjj plan --json
```

The command must answer one question:

> **Exactly what shared remote state would a future publication transaction attempt to create or move?**

This ACT freezes and describes the **publication subject**. It
deliberately does NOT verify, approve, hash, bind evidence to, or
transport the plan.

## Scope of PLAN01

PLAN01 handles **explicit bookmark publication only**.

Required invocation:

```text
bjj plan --remote <remote> --bookmark <bookmark>
```

PLAN01 does NOT implement:

```text
--all
--tracked
--changed
--stack
--remote-only
```

## Layered architecture

```text
internal/execx       bounded subprocess seam
        ↓
internal/jjadapter   bounded jj adapter (typed records only)
        ↓
internal/plan        domain model + resolver
        ↓
cmd/bjj              CLI dispatch (runPlan)
```

Domain code never sees raw `jj` output. CLI code never parses
Jujutsu output.

## Domain model

PLAN01 introduces the typed `PublishPlan` (the **canonical subject**)
and the observation envelope (`PlanObservation`) that carries
diagnostic metadata separately.

```text
PublishPlan (CANONICAL SUBJECT — environment-independent)
 ├── SchemaVersion int
 ├── Status        Status      (planned | no_remote_change)
 ├── Remote        Remote      (Name only — no URL)
 ├── BookmarkMoves []BookmarkMove
 │    └── { Name, Old, New, Conflict }
 └── Commits       []CommitRef (CommitID, ChangeID)

PlanObservation (NON-CANONICAL envelope — diagnostic only)
 ├── Plan              *PublishPlan
 ├── SourceOperationID string
 └── RepositoryPath    string
```

Two repositories with identical semantic Jujutsu state (modulo
content-addressed commit ids) MUST produce canonical PublishPlan
bodies that share the same JSON shape and contain NO environment-
specific field. Observation metadata is intentionally excluded
from canonical comparison.

Typed errors:

```text
REMOTE_NOT_FOUND
LOCAL_BOOKMARK_NOT_FOUND
LOCAL_BOOKMARK_CONFLICTED
REMOTE_BOOKMARK_CONFLICTED
NO_REMOTE_CHANGE
JJ_QUERY_FAILED
INCONSISTENT_REPOSITORY_VIEW
```

## Single-view consistency (P0)

The resolver pins every `jj` invocation to **one** operation view
captured at Snapshot time and threads that EXACT opID through every
subsequent CommitsAt call. The Source interface contract is:

```go
type Source interface {
    Snapshot(ctx, dir) (Snapshot, error)
    CommitsAt(ctx, dir, opID, revset) ([]CommitRef, error)
}
```

The opID is captured exactly once. `CommitsAt` MUST NOT re-pin;
the production `JJSource` refuses an empty opID with code
`INCONSISTENT_REPOSITORY_VIEW` and the message
`"refusing to re-pin"`.

Every `jj` invocation passes `--at-op=<opID>` and
`--ignore-working-copy --no-integrate-operation`. This guarantees
that BJJ cannot accidentally assemble a plan from racing
observations while the repository mutates.

`PLAN_SINGLE_VIEW_CONSISTENCY = PASS` is the central property of
PLAN01.

`PLAN_MID_RESOLVE_MUTATION_CANNOT_MIX_VIEWS = PASS` is the
adversarial proof: a test wraps the production source, mutates the
repository between resolves, and asserts that within each resolve
every CommitsAt opID equals the corresponding snapshot's opID. The
test fails under the original re-pinning design.

### Why this matters

Jujutsu gives each individual command a consistent view of the
repository (one operation view per process). But BJJ runs several
`jj` subprocesses to construct a plan. If those subprocesses observe
different operations, the resulting plan could describe no real
state.

PLAN01 mitigates this by capturing the snapshot's opID and threading
it through every subsequent query. Re-pinning is no longer possible
because the production JJSource refuses an empty opID.

## Outgoing commit set

For OLD→NEW moves:

```text
OLD..NEW ~ root()
```

For NEW bookmark creation (OLD=absent):

```text
effective_old..NEW ~ root()
```

where `effective_old` is the nearest ancestor of NEW that is a
remote-tracked bookmark target, computed via the revset:

```text
heads(::NEW & remote_bookmarks())
```

If no effective OLD can be found (truly orphaned branch), the set
falls back to `::NEW ~ root()`.

The synthetic `root()` is always excluded because it carries no
publication-relevant identity.

### Canonical commit order

Commits are sorted by **lexicographic full commit_id** for canonical,
deterministic ordering. This is NOT ancestor-before-descendant
ordering. Future ACTs that need topological order will compute it
on top of this canonical set.

`PLAN_COMMIT_ORDER_CONTRACT_MATCHES_IMPLEMENTATION = PASS` is
asserted by `TestPlan_CanonicalOrder_IsLexicographicCommitID`.

## JSON contract (canonical subject)

```json
{
  "schema_version": 1,
  "status": "planned",
  "remote": { "name": "lab" },
  "bookmark_moves": [
    {
      "name": "feature",
      "old": null,
      "new": { "commit_id": "...", "change_id": "..." }
    }
  ],
  "commits": [
    { "commit_id": "...", "change_id": "..." }
  ]
}
```

Stable field names, full-length identifiers, lexicographic
commit_id canonical order. NO source_operation_id. NO remote URL.
NO repository path.

Errors:

```json
{
  "schema_version": 1,
  "code": "REMOTE_NOT_FOUND",
  "message": "remote \"wat\" is not known to the repository"
}
```

## Observation envelope (non-canonical)

For callers that need to record the operation id or repository
path alongside the plan, `plan.ResolveObserved` returns a
`PlanObservation` wrapping the canonical plan with diagnostic
metadata. This envelope is NOT part of the canonical subject.

```json
{
  "plan": { /* canonical PublishPlan */ },
  "source_operation_id": "...",
  "repository_path": "/path/to/repo"
}
```

`PLAN_SUBJECT_METADATA_BOUNDARY_EXPLICIT = PASS`.

## Static safeguards

`TestPlanLayerHasNoTransport` walks the AST of every production
file under `internal/plan` and `internal/jjadapter` and rejects any
argv slice containing the sequences:

```text
git push
git fetch
jj git push
jj git fetch
```

`PLAN_LAYER_HAS_NO_TRANSPORT = PASS`. This is a bounded regression
guard against statically-assembled transport argv; it cannot detect
dynamically-constructed argv, which is why runtime tests and the
adapter's typed records carry the real enforcement weight.

## Acceptance contract

```text
BJJ_PLAN_COMMAND                    = PASS
BJJ_PLAN_JSON_STRICT                = PASS
BJJ_PLAN_DETERMINISTIC              = PASS
PLAN_SINGLE_VIEW_CONSISTENCY        = PASS
PLAN_MID_RESOLVE_MUTATION_CANNOT_MIX_VIEWS = PASS

PLAN_NEW_BOOKMARK                   = PASS
PLAN_EXISTING_BOOKMARK_MOVE         = PASS
PLAN_NOOP_DETECTED                  = PASS

PLAN_REMOTE_NOT_FOUND               = TYPED_FAIL
PLAN_LOCAL_BOOKMARK_NOT_FOUND       = TYPED_FAIL
PLAN_LOCAL_BOOKMARK_CONFLICTED      = TYPED_FAIL (skipped on 0.41.0
                                              if conflict not surfaced)
PLAN_REMOTE_BOOKMARK_CONFLICTED     = TYPED_FAIL_OR_EXPLICITLY_UNREPRESENTABLE_IN_0_41

PLAN_COMMIT_IDS_FULL                = PASS
PLAN_CHANGE_IDS_FULL                = PASS
PLAN_REWRITE_IDENTITY_SEPARATED     = PASS
PLAN_COMMIT_ORDER_CONTRACT_MATCHES_IMPLEMENTATION = PASS
PLAN_CANONICAL_BODY_HAS_NO_REMOTE_URL = PASS
PLAN_SUBJECT_METADATA_BOUNDARY_EXPLICIT = PASS

PLAN_REMOTE_BASELINE                = LOCAL_KNOWN_REMOTE_STATE
BJJ_PLAN_NETWORK_ACCESS             = NO
BJJ_PLAN_REMOTE_MUTATION            = NO
BJJ_PLAN_BOOKMARK_MUTATION          = NO
PLAN_LAYER_HAS_NO_TRANSPORT         = PASS

GO_TEST_ALL                         = PASS
GO_VET_ALL                          = PASS
BJJ_BUILD                           = PASS
PATCH_HYGIENE                       = PASS

REAL_REMOTE_MUTATED                 = NO
PRODUCT_PUBLISH_IMPLEMENTED         = NO
SUBJECT_DIGEST_IMPLEMENTED          = NO
```

### Proof-of-correction acceptance items (CORRECTION01)

| Property                                                | Status |
|---------------------------------------------------------|--------|
| PLAN_SINGLE_VIEW_CONSISTENCY                            | PASS   |
| PLAN_MID_RESOLVE_MUTATION_CANNOT_MIX_VIEWS              | PASS   |
| PLAN_COMMIT_ORDER_CONTRACT_MATCHES_IMPLEMENTATION       | PASS   |
| PLAN_CANONICAL_BODY_HAS_NO_REMOTE_URL                   | PASS   |
| PLAN_SUBJECT_METADATA_BOUNDARY_EXPLICIT                 | PASS   |

## Documented limitations

### REMOTE_BOOKMARK_CONFLICTED on 0.41.0

The adapter surfaces `Conflict=true` for any bookmark whose
`added_targets` or `removed_targets` lists are non-empty. On
0.41.0, remote-tracking bookmarks can be in conflict, but
fabricating a deterministic fixture for them via the lab is fragile
across versions. PLAN01 detects the code path correctly; the
fixture is skipped on 0.41.0 when conflict is not surfaced.

### Stale-known-remote-state by design

PLAN01's planning baseline is the **local-known remote state**, not
the live remote state. This is intentional:

- `bjj plan` MUST NOT perform implicit `jj git fetch`.
- The plan is a snapshot of "what publication would do *right now*".
- Future ACTs (admission, transport, verification) re-check the
  remote immediately before the transport step.

This separates the planning subject from the live remote state.

### Static transport guard is bounded

`TestPlanLayerHasNoTransport` only inspects `[]string` composite
literals. Dynamically-assembled argv can evade it. The guard is a
bounded regression check, not a complete proof. Runtime enforcement
comes from:

- the absence of any transport-command builder in
  `internal/plan` and `internal/jjadapter`;
- typed records that never carry transport semantics;
- end-to-end tests that verify no remote mutation (`PLAN_REMOTE_MUTATION = NO`).

## ACT-BJJ-PLAN01-CORRECTION02 — Fail-Closed Observation

PLAN01 CORRECTION02 closes the remaining gaps flagged in the
expert review:

1. The "mid-resolve mutation" test was actually a
   between-resolves test. Replaced with a true mid-resolve
   adversarial proof that mutates the repository AFTER
   `Snapshot()` but BEFORE the first `CommitsAt()`.
2. `nearestRemoteAncestor` swallowed underlying query errors and
   fell back to `::NEW ~ root()`, silently widening the
   publication subject. Now propagates as `JJ_QUERY_FAILED`.
3. `jjadapter` parsers silently skipped malformed rows or
   invented partial values. Now they fail closed with a typed
   `*ErrJJParseFailed` that the resolver maps to
   `JJ_QUERY_FAILED`.
4. Documentation and CLI comments were corrected to match the
   actual code: one operation id is pinned once; the CLI emits
   only the canonical plan.

### Proof of correction

| Gate                                                 | Test                                                   | Status |
|------------------------------------------------------|--------------------------------------------------------|--------|
| PLAN_MID_RESOLVE_MUTATION_CANNOT_MIX_VIEWS (real)    | `TestPlan_SingleView_MidResolveMutation_PinnedToSnapshotOpID` | PASS |
| PLAN_EFFECTIVE_OLD_QUERY_FAILS_CLOSED                | `TestPlan_EffectiveOldQueryFailsClosed`                | PASS   |
| JJ_BOOKMARK_OUTPUT_MALFORMED                         | `TestPlan_BookmarkOutputMalformed_FailsClosed`         | PASS   |
| JJ_COMMIT_OUTPUT_MALFORMED                           | `TestPlan_CommitOutputMalformed_FailsClosed`           | PASS   |
| PLAN_MALFORMED_JJ_OUTPUT                             | `TestPlan_MalformedJJOutput_SurfacesAsJJQueryFailed`   | PASS   |
| DOC_SINGLE_VIEW_CONTRACT_MATCHES_CODE                | `TestPlan_DocSingleViewContractMatchesCode`            | PASS   |
| CLI_COMMENT_MATCHES_OUTPUT                           | `TestRunPlanCLI_JsonEmitsOnlyCanonicalPlan`            | PASS   |

### Updated acceptance matrix (CORRECTION02)

```text
PLAN_SINGLE_VIEW_CONSISTENCY                    = PASS
PLAN_MID_RESOLVE_MUTATION_CANNOT_MIX_VIEWS      = PASS
PLAN_EFFECTIVE_OLD_QUERY_FAILS_CLOSED            = PASS
JJ_BOOKMARK_OUTPUT_MALFORMED                     = FAIL_CLOSED
JJ_COMMIT_OUTPUT_MALFORMED                       = FAIL_CLOSED
PLAN_MALFORMED_JJ_OUTPUT                         = JJ_QUERY_FAILED
DOC_SINGLE_VIEW_CONTRACT_MATCHES_CODE            = PASS
CLI_COMMENT_MATCHES_OUTPUT                       = PASS
BJJ_PLAN_COMMAND                                 = PASS
BJJ_PLAN_JSON_STRICT                             = PASS
BJJ_PLAN_DETERMINISTIC                           = PASS
PLAN_NEW_BOOKMARK                                = PASS
PLAN_EXISTING_BOOKMARK_MOVE                      = PASS
PLAN_NOOP_DETECTED                               = PASS
PLAN_REMOTE_BASELINE                             = LOCAL_KNOWN_REMOTE_STATE
BJJ_PLAN_NETWORK_ACCESS                          = NO
BJJ_PLAN_REMOTE_MUTATION                         = NO
BJJ_PLAN_BOOKMARK_MUTATION                       = NO
PLAN_LAYER_HAS_NO_TRANSPORT                      = PASS
GO_TEST_ALL                                      = PASS
GO_VET_ALL                                       = PASS
BJJ_BUILD                                        = PASS
PATCH_HYGIENE                                    = PASS
REAL_REMOTE_MUTATED                              = NO
PRODUCT_PUBLISH_IMPLEMENTED                      = NO
SUBJECT_DIGEST_IMPLEMENTED                       = NO
```

PLAN01 is now frozen. Next ACT: `ACT-BJJ-ADMISSION01 —
Publication Subject Admission Policy`.

## Explicit non-goals (deferred from PLAN01)

- `SubjectDigest` (deferred to a later evidence-binding ACT).
- `bjj check`, `bjj publish`, `bjj receipt`.
- Factory gates, admission policy, evidence binding, transport.
- Multi-bookmark plans, automatic outgoing-bookmark discovery,
  stack verification.
- `--all`, `--tracked`, `--changed`, `--stack`, `--remote-only`.
- Remote network contact.

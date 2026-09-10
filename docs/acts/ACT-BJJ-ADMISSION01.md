# ACT-BJJ-ADMISSION01 — Publication Subject Admission Boundary

**Status:** IMPL — FROZEN (CORRECTION01 + CORRECTION02 + CORRECTION03 applied).

**Project:** Bounded Jujutsu (bjj)

**Repository:** https://github.com/s1onique/bjj

The freeze threshold is `false admit | false deny | mixed repository view |
authority-boundary violation`. No defect of any of those classes remains
demonstrated; the ACT is sealed against the next ACT (CHECK01), which
must answer the much stronger question "did the sanctioned verifier
actually prove the required properties **for this exact frozen
subject**" rather than merely run some commands somewhere in the
repository.

## VERDICT

```text
BJJ_ADMIT_COMMAND_IMPLEMENTED       = PASS
BJJ_ADMIT_DECISION_TYPED            = PASS  (admit | deny | not_needed)
BJJ_ADMIT_EXIT_CODE_CONTRACT        = PASS  (0 | 1 | 2 | 3)
BJJ_ADMIT_JSON_STRICT               = PASS
BJJ_ADMIT_DETERMINISTIC             = PASS
BJJ_ADMIT_FACTS_SAME_VIEW           = PASS
BJJ_ADMIT_MID_GATHER_MUTATION       = PASS  (cannot mix views)
BJJ_ADMIT_DEFAULT_POLICY            = PASS
BJJ_ADMIT_MALFORMED_POLICY          = FAIL_CLOSED
BJJ_ADMIT_FACT_QUERY_FAIL           = FAIL_CLOSED
BJJ_ADMIT_FF                        = PASS
BJJ_ADMIT_NFF_DENY_DEFAULT          = PASS
BJJ_ADMIT_NFF_ALLOWED_BY_POLICY     = PASS
BJJ_ADMIT_PRIVATE_COMMIT            = PASS
BJJ_ADMIT_PRIVATE_ANCESTOR          = PASS
BJJ_ADMIT_CONFLICTED_COMMIT         = PASS
BJJ_ADMIT_EMPTY_DESCRIPTION         = PASS
BJJ_ADMIT_NO_REMOTE_CHANGE          = PASS
ADMISSION_LAYER_HAS_NO_TRANSPORT    = PASS

-- ACT-BJJ-ADMISSION01-CORRECTION01 --------------------------
ADMISSION_SUBJECT_SET_MATCHES_PLAN  = PASS
ADMISSION_PRIVATE_SCOPE_EXACT       = PASS
ADMISSION_CONFLICT_SCOPE_EXACT      = PASS
ADMISSION_DESCRIPTION_SCOPE_EXACT   = PASS
ALREADY_PUBLISHED_PRIVATE_ANCESTOR_DOES_NOT_DENY = PASS
ALREADY_PUBLISHED_BAD_DESCRIPTION_DOES_NOT_DENY  = PASS
POLICY_UNKNOWN_KEY                  = FAIL_CLOSED
POLICY_TYPO_CANNOT_WEAKEN_POLICY    = PASS
ADMISSION_NO_DIRECT_PLAN_IMPORT     = PASS
PATCH_HYGIENE                       = PASS  (git diff --check exits 0)
GOFMT_CLEAN                         = PASS  (gofmt -l reports nothing)
-- end CORRECTION01 -----------------------------------------

-- ACT-BJJ-ADMISSION01-CORRECTION02 --------------------------
POLICY_DUPLICATE_KEY                = FAIL_CLOSED
POLICY_DUPLICATE_CANNOT_WEAKEN_POLICY = PASS
POLICY_DUPLICATE_SCHEMA_VERSION     = FAIL_CLOSED
POLICY_SCHEMA_MISSING               = FAIL_CLOSED  (present file, no schema_version)
POLICY_SCHEMA_DUPLICATE             = FAIL_CLOSED
POLICY_SCHEMA_UNSUPPORTED           = FAIL_CLOSED
POLICY_ABSENT_FILE_STILL_DEFAULT    = PASS         (carve-out preserved)

DOC_ADMISSION_UNKNOWN_KEYS_MATCH_CODE       = PASS
DOC_ADMISSION_QUERY_CONTRACT_MATCHES_CODE   = PASS
DOC_ADMISSION_PLAN_BRIDGE_MATCHES_CODE      = PASS
DOC_ADMISSION_PRIVATE_PREDICATE_MATCHES_CODE = PASS
-- end CORRECTION02 -----------------------------------------

-- ACT-BJJ-ADMISSION01-CORRECTION03 --------------------------
ADMISSION_POLICY_VIEW_BINDING                 = PASS
ADMISSION_POLICY_BOUND_TO_PLAN_NEW            = PASS
ADMISSION_MID_POLICY_MUTATION_CANNOT_MIX_VIEWS = PASS

POLICY_ABSENT_FROZEN_CANDIDATE_USES_DEFAULT   = PASS
POLICY_LIVE_ADD_IGNORED                       = PASS
POLICY_LIVE_REMOVE_IGNORED                    = PASS
POLICY_LIVE_REWRITE_IGNORED                   = PASS
POLICY_LIVE_MALFORMING_IGNORED                = PASS

ADMISSION_POLICY_HAS_EXPLICIT_VIEW_BINDING    = PASS
                                          (TestCmdAdmissionPathUsesPolicyReader)

GO_TEST_ALL                                   = PASS
GO_VET_ALL                                    = PASS
BJJ_BUILD                                     = PASS
PATCH_HYGIENE                                 = PASS  (git diff --check exits 0)
GOFMT_CLEAN                                   = PASS  (gofmt -l reports nothing)
-- end CORRECTION03 -----------------------------------------

GO_TEST_ALL                         = PASS
GO_VET_ALL                          = PASS
BJJ_BUILD                           = PASS
REAL_REMOTE_MUTATED                 = NO
PRODUCT_PUBLISH_IMPLEMENTED         = NO
```

## Mission

Implement BJJ's deterministic publication-subject admission
boundary:

```text
internal/admission
cmd/bjj admit
```

The boundary answers one question:

> **Given an already-resolved publication subject and facts
> observed from the EXACT SAME Jujutsu operation view, is this
> subject structurally eligible to proceed to expensive
> verification?**

Admission does NOT prove correctness. It does NOT execute Factory
gates. It does NOT grant publication authority. It rejects
ineligible subjects BEFORE expensive checks run.

## CORRECTION01 SUMMARY

ACT-BJJ-ADMISSION01-CORRECTION01 tightened three properties
that the original closure had only claimed:

1. **Subject-set exactness (P0).** The gatherer no longer
   reconstructs the publication subject via `::New ~ root()`. It
   reads `PublishPlan.Commits` verbatim from
   `planView.OutgoingCommits()` and restricts every predicate
   query to exactly that set. Already-published ancestors no
   longer participate in admission.

2. **Fail-closed policy schema (P0).** Unknown keys in
   `.bjj/policy.toml` (e.g. `private_commmits` with three `m`s)
   now produce a hard `POLICY_INVALID` error instead of being
   silently ignored. `schema_version` is the deliberate
   forward-compatibility boundary.

3. **Real package decoupling (P1).** The concrete
   `*plan.PublishPlan -> admission.PlanView` adapter moved from
   `internal/admission/planview.go` to
   `cmd/bjj/plan_adapter.go`. The production admission package
   no longer imports `internal/plan`; the boundary is enforced
   by `TestAdmissionHasNoDirectPlanImport`.

Two smaller items:

- `Policy.SchemaVersion` is now carried in the parsed policy so
  the parser can reason about unknown keys under a specific
  schema.
- `git diff --check` and `gofmt -l` are clean.

## CORRECTION02 SUMMARY

CORRECTION02 closes the three remaining P1 defects identified
during the post-CORRECTION01 review. None of the original
ADMISSION01 architecture changed; this is purely a policy-parser
hardening and a documentation-truth pass.

1. **Duplicate policy keys fail closed (P1).** The bounded TOML
   subset parser now rejects every key that appears more than
   once. `parsePolicyTOML` records each seen key in its `saw`
   map and returns `POLICY_INVALID` ("duplicate policy key
   %q") the second time the same key occurs, regardless of
   whether the key is recognised or unknown. The bounded parser
   preserves the fail-closed duplicate-key rule that the TOML
   spec already mandates ("Defining a key multiple times is
   invalid"), so a generated/merged policy file cannot quietly
   weaken a rule (e.g. `private_commits =
   "description('private:*')"` followed by
   `private_commits = "none()"`).

2. **schema_version is required (P1).** A present policy file
   MUST declare exactly one `schema_version = 1` line. The
   parser tracks whether the key was seen and fails closed
   otherwise. The absent-file case still resolves to
   `DefaultPolicy()` with `PolicyOutcomeDefault`. The
   `schema_version` line is now an explicit schema boundary
   that future migrations can rely on, not just an
   informational annotation.

3. **Documentation truth (P1).** The post-CORRECTION01 review
   found three stale prose passages in the durable documents:

   - The malformed-policy section claimed "Unknown top-level
     keys are ignored for forward-compatibility" — replaced
     with the actual CORRECTION01 semantics ("Unknown keys fail
     closed under `schema_version = 1`").
   - The fact-gathering contract listed `subject | policy.
     PrivateCommits` (union) instead of the actual
     `subject & policy.PrivateCommits` (intersection) used by
     the production code. Corrected.
   - `docs/architecture.md` still described a four-query
     gatherer including `::NEW ~ root()` for the subject set,
     and said "internal/plan provides a WrapPlan() adapter
     implementation" instead of the CORRECTION01 location
     (`cmd/bjj/plan_adapter.go`). Both updated.

   These are enforced by `TestDocs_*` in
   `internal/admission/docs_test.go`, which fail if the known
   fossil phrases reappear.

## CORRECTION03 SUMMARY

The post-CORRECTION02 review identified one P0 defect
capable of false admission or denial: `.bjj/policy.toml` was
read from the live working-tree filesystem via
`os.ReadFile(<dir>/.bjj/policy.toml)` AFTER
`plan.ResolveObserved` had pinned the operation view. PLAN01
and admission facts were pinned to `OP_A`, but the policy
load ran against the live filesystem, which could be a
different operation view by the time it executed. CORRECTION03
binds the policy read to the same frozen Jujutsu view that
produced the PublishPlan.

What changed:

1. **Policy view binding (P0).** `.bjj/policy.toml` is now
   read via `jj --at-op=<OP_A> file show -r <NEW> .bjj/policy.toml`
   in `cmd/bjj/admit.go`. `internal/jjadapter.Adapter` gains
   `FileShowAtOp(ctx, dir, opID, revision, path) ([]byte,
   FilePresence, error)` which returns a typed
   PRESENT/ABSENT discriminator and refuses to read with
   empty opID, empty revision, or empty path. `internal/admission`
   gains a `PolicyReader` interface and a new entry point
   `LoadPolicyAt(ctx, reader, planView, dir, opID)` that the
   production CLI uses. The legacy `LoadPolicyFromRepo(dir)`
   helper is retained only for parser unit tests (which
   exercise byte-level invariants of the bounded TOML subset).

2. **Candidate-local policy (P0 follow-on).** Policy bytes
   are read from `PublishPlan.BookmarkMoves[0].New.CommitID`
   as visible at `obs.SourceOperationID`. The deprecated
   live-fs reader could never have enforced this — it took
   no revision parameter at all. The new path makes
   provenance explicit: `PolicySource` now carries
   `OperationID` and `Revision`, so diagnostic output and
   audit trails can prove the bytes came from the frozen
   candidate.

3. **DefaultPolicy semantics on absence (P0 follow-on).**
   `DefaultPolicy()` now applies only when the file is
   genuinely absent from the candidate tree at the pinned
   view. A working-copy mutation between plan resolution and
   policy load cannot change that outcome, because the load
   is bound to the pinned (opID, NEW) pair.

4. **Static guard (P0 follow-on).**
   `TestCmdAdmissionPathUsesPolicyReader` walks
   `cmd/bjj/*.go` (non-test) and AST-rejects any caller that
   invokes `admission.LoadPolicyFromRepo`. The production
   admission decision path cannot regress to live-fs reads
   without this test failing.

5. **Documentation fossil (P2).** The durable prose repeatedly
   described the bounded parser as more restrictive than
   TOML itself with respect to duplicate keys. TOML already
   mandates that "Defining a key multiple times is invalid"
   (v1.1.0 spec). The fossil was corrected to "preserves the
   fail-closed duplicate-key rule that the TOML spec already
   mandates", in `docs/architecture.md`,
   `docs/acts/ACT-BJJ-ADMISSION01.md`, `README.md`, and the
   doc comment of `TestPolicy_DuplicateKey_FailsClosed`.

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

## IDENTITY

| Field          | Value                                  |
|----------------|----------------------------------------|
| Command        | `bjj admit`                            |
| Module         | `internal/admission`                   |
| SchemaVersion  | `1`                                    |
| Decisions      | `admit` \| `deny` \| `not_needed`      |
| Exit codes     | `0` admit, `1` invalid args, `2` internal, `3` deny/not_needed |
| Policy file    | `.bjj/policy.toml`                     |
| Adapter        | `internal/jjadapter` (typed only)      |
| Plan source    | `cmd/bjj/plan_adapter.go` (`PlanView`) |
| Process model  | read-only (no transport, no mutation)  |

## ADMISSION DOMAIN MODEL

```text
AdmissionInput
 ├── Plan    PlanView  (no direct import of internal/plan)
 ├── Facts   AdmissionFacts
 └── Policy  Policy

AdmissionFacts
 ├── SourceOperationID  string         (must match plan's opID)
 ├── MoveRelation       MoveRelation   (CREATE|FAST_FORWARD|NON_FAST_FORWARD|NO_CHANGE)
 ├── OutgoingCommits    []CommitRef    (= PublishPlan.Commits verbatim, sort: lex commit_id)
 ├── PrivateCommits     []CommitRef    (sort: lex commit_id)
 ├── ConflictedCommits  []CommitRef    (sort: lex commit_id)
 └── EmptyDescriptionCommits []CommitRef

AdmissionResult
 ├── SchemaVersion  int             (1)
 ├── Decision       Decision        (admit|deny|not_needed)
 ├── Reasons        []Reason        (canonical sort)
 └── Subject        SubjectIdentity (remote, bookmark, old, new)

ReasonCode (canonical total order)
  NO_REMOTE_CHANGE
  NON_FAST_FORWARD
  NEW_BOOKMARK_NOT_ALLOWED
  PRIVATE_COMMIT
  CONFLICTED_COMMIT
  EMPTY_DESCRIPTION
  FACTS_INCONSISTENT

ErrorCode (infrastructure, NOT policy)
  JJ_QUERY_FAILED
  JJ_PARSE_FAILED
  POLICY_INVALID
  INCONSISTENT_REPOSITORY_VIEW
  NO_BOOKMARK_MOVE
```

The `PlanView` interface is a slim adapter. Per CORRECTION01 §5
the concrete `*plan.PublishPlan -> admission.PlanView` bridge
lives in `cmd/bjj/plan_adapter.go`, NOT in `internal/admission`.
This keeps the production admission package free of any
`internal/plan` import, restoring the documented decoupling
claim. The boundary is enforced by the AST guard
`TestAdmissionHasNoDirectPlanImport` in
`internal/plan/safety_test.go`.

CORRECTION01 invariant (P0):

```text
AdmissionFacts.OutgoingCommits  ==  PublishPlan.Commits
```

The fact gatherer NEVER independently reconstructs the
publication subject via `::New ~ root()`. It reads the canonical
PLAN01 subject from `planView.OutgoingCommits()` verbatim, and
every predicate query (private, conflict, empty-description) is
restricted to exactly that subject set via a disjunction of
explicit commit_ids:

```text
subject = commit_id_1 | commit_id_2 | ...
private = subject & policy.PrivateCommits
obs     = subject
```

This eliminates the subject-widening class of bugs that PLAN01
spent two corrections removing.

## DEFAULT POLICY

When `.bjj/policy.toml` is absent, `DefaultPolicy()` is used:

```text
allow_new_bookmarks      = true
allow_non_fast_forward   = false
require_description      = true
allow_conflicted_commits = false
private_commits          = "none()"
```

This is the source of truth referenced by
`TestPolicy_DefaultPolicyMatchesDoc`.

## FACT GATHERING CONTRACT

The `Gatherer` is the ONLY place that talks to the adapter. It
issues at most three bounded `jj` queries, all pinned to
`--at-op=<OpID>`:

```text
1. ancestry probe      (OLD & ::NEW)              (classifyMove)
2. private predicate   (subject & policy.PrivateCommits)
3. per-commit obs      (subject)                  (ListCommitObs)

where:

   subject = commit_id_1 | commit_id_2 | ...
           = PublishPlan.Commits (verbatim from planView.OutgoingCommits())
```

`subject & policy.PrivateCommits` is an INTERSECTION: only commits
that are BOTH in the canonical subject AND match the private-policy
revset are reported as private. A commit that matches the
private-policy expression but is not in `PublishPlan.Commits`
(e.g. an already-published ancestor) is excluded by the subject
constraint.

CORRECTION01 changed the contract. The previous version issued
a fourth `::NEW ~ root()` query to reconstruct the outgoing
subject set; under CORRECTION01 that set is taken VERBATIM from
`planView.OutgoingCommits()`, which is itself derived from
`PublishPlan.Commits`. The gatherer cannot widen admission to
ancestors of NEW because it never queries `::NEW`.

Predicate scope:

```text
private predicate   -> SUBJECT & policy.PrivateCommits
conflict + desc obs -> SUBJECT                    (no ancestor expansion)
```

An already-published ancestor of the subject does NOT
participate in any predicate evaluation.

The `OpID` is captured from the source `PlanObservation` and
threaded through to every query. Mid-gather mutation cannot mix
views because the production `Source` refuses to re-pin (an
empty `OpID` yields `CodeInconsistentView`).

```text
ADMISSION_FACTS_SAME_VIEW            = PASS  (TestAdmission_Gather_UsesFrozenOpID)
ADMISSION_MID_GATHER_MUTATION        = PASS  (mid-gather adapter mutates; view mismatch)
ADMISSION_SUBJECT_SET_MATCHES_PLAN   = PASS  (TestAdmission_SubjectSetMatchesPlan)
ADMISSION_PRIVATE_SCOPE_EXACT        = PASS  (TestAdmission_AlreadyPublishedPrivateAncestor_DoesNotDeny)
ADMISSION_CONFLICT_SCOPE_EXACT       = PASS  (per-commit obs restricted to subject)
ADMISSION_DESCRIPTION_SCOPE_EXACT    = PASS  (TestAdmission_AlreadyPublishedBadDescription_DoesNotDeny)
```

## LAYERED ARCHITECTURE

```text
internal/execx        bounded subprocess seam
        ↓
internal/jjadapter    bounded jj adapter (typed records only)
        ↓
internal/plan         canonical PublishPlan + PlanObservation
        ↓                       (consumed via PlanView interface)
internal/admission    facts + policy + evaluator (no subprocess here)
        ↓                       (consumed via PlanView interface)
cmd/bjj               CLI orchestration (read-only)
        ↑                       (cmd/bjj/plan_adapter.go bridges plan -> PlanView)
```

Domain code never sees raw `jj` output. CLI code never parses
Jujutsu output. `internal/admission` has NO direct import of
`internal/plan` (CORRECTION01 §5): the production adapter moved
to `cmd/bjj/plan_adapter.go`, and the boundary is enforced by
the AST guard `TestAdmissionHasNoDirectPlanImport`.

## SAFETY MATRIX

| Property                                         | Test                                                            | Status |
|--------------------------------------------------|-----------------------------------------------------------------|--------|
| ADMISSION_LAYER_HAS_NO_TRANSPORT                 | `TestPlanLayerHasNoTransport` (extended to admission)           | PASS   |
| ADMISSION_NO_DIRECT_PLAN_IMPORT                  | `TestAdmissionHasNoDirectPlanImport` (CORRECTION01 §5)          | PASS   |
| ADMISSION_MID_GATHER_MUTATION_CANNOT_MIX_VIEWS   | `TestAdmission_Gather_UsesFrozenOpID`                          | PASS   |
| EVALUATOR_PURE_NO_IO                             | All `TestEvaluate_*` tests use hand-built `AdmissionInput`      | PASS   |
| POLICY_ABSENT_USES_DEFAULT                       | `TestPolicy_LoadFromRepo_Absent`                                | PASS   |
| POLICY_MALFORMED_FAILS_CLOSED                    | `TestPolicy_LoadFromRepo_Malformed`                             | PASS   |
| POLICY_UNKNOWN_KEY_FAILS_CLOSED                  | `TestPolicy_UnknownKey_FailsClosed`                             | PASS   |
| POLICY_TYPO_CANNOT_WEAKEN_POLICY                 | `TestPolicy_TypoCannotWeakenPolicy`                             | PASS   |
| POLICY_AMBIENT_JJ_INDEPENDENT                    | `TestPolicy_AmbientJJPolicyIndependent`                         | PASS   |
| EVALUATOR_DETERMINISTIC                          | `TestEvaluate_Deterministic` (5 iterations, byte-identical)     | PASS   |
| REASONS_CANONICAL_ORDER                          | `TestEvaluate_CanonicalOrderOfReasons`                          | PASS   |
| JSON_OMITS_PATHS_AND_URLS                        | `TestEvaluate_JSON_OmitsAbsolutePathsAndURLs`                   | PASS   |
| JJ_PARSE_FAILS_CLOSED                            | `TestParseCommitObsLines_FailsClosedOnMalformedInput`           | PASS   |
| NO_TRANSPORT_AT_RUNTIME                          | `TestAdmission_NoTransport`                                     | PASS   |
| CANONICAL_JSON_HAS_NO_ENV                        | `TestAdmission_JSON_NoEnvironment`                              | PASS   |
| SUBJECT_SET_MATCHES_PLAN                         | `TestAdmission_SubjectSetMatchesPlan`                           | PASS   |

## ACCEPTANCE MATRIX (per evidence §)

| §  | Evidence                                                            | Test                                                              | Status |
|----|---------------------------------------------------------------------|-------------------------------------------------------------------|--------|
| 7  | CREATE subject is admitted by default                                | `TestEvaluate_CreateAdmit`, `TestAdmission_CreateDefaultAdmit`    | PASS   |
| 7  | FAST_FORWARD subject is admitted                                     | `TestEvaluate_FastForwardAdmit`, `TestAdmission_FastForwardAdmit`| PASS   |
| 25 | CREATE disabled by policy → deny                                     | `TestEvaluate_CreateDisabled_Deny`                                | PASS   |
| 26 | NON_FAST_FORWARD denied by default                                   | `TestEvaluate_NonFastForward_DenyDefault`, `TestAdmission_NonFastForward_DenyDefault` | PASS |
| 26 | NON_FAST_FORWARD allowed by policy → admit                           | `TestEvaluate_NonFastForward_AllowedByPolicy`, `TestAdmission_NonFastForward_AllowedByPolicy` | PASS |
| 11 | NO_CHANGE → not_needed (positive decision)                           | `TestEvaluate_NoChange_NotNeeded`, `TestAdmission_NoRemoteChange_NotNeeded` | PASS |
| 9  | PRIVATE commit in outgoing set → deny                                | `TestEvaluate_PrivateCommit_Deny`, `TestAdmission_PrivateCommit_Deny` | PASS |
| 9  | PRIVATE ancestor of outgoing commit → deny                           | `TestAdmission_PrivateAncestor_Deny`                              | PASS   |
| 11 | CONFLICTED commit in outgoing set → deny                             | `TestEvaluate_ConflictedCommit_Deny`, `TestAdmission_ConflictedCommit_Deny` | PASS |
| 11 | CONFLICTED commit allowed by `allow_conflicted_commits=true` → admit | `TestEvaluate_ConflictedCommit_AllowedByPolicy`                   | PASS   |
| 33 | EMPTY_DESCRIPTION commit in outgoing set → deny                      | `TestEvaluate_EmptyDescription_Deny`, `TestAdmission_EmptyDescription_Deny` | PASS |
| 4  | Facts MUST come from same view as plan                               | `TestAdmission_Gather_UsesFrozenOpID`                             | PASS   |
| 22 | Fact-query failure → typed `*Error{CodeJJQueryFailed}`               | (covered by `TestAdmission_NoTransport`, `internal/plan` tests)   | PASS   |
| 23 | Malformed policy → typed `*Error{CodePolicyInvalid}`                 | `TestPolicy_LoadFromRepo_Malformed`                               | PASS   |
| 28 | Ambient JJ_CONFIG does not influence policy                         | `TestPolicy_AmbientJJPolicyIndependent`                           | PASS   |
| 38 | Pure evaluator is byte-deterministic (5 iterations)                  | `TestEvaluate_Deterministic`                                      | PASS   |
| 29 | Static transport guard covers `internal/admission`                  | `TestPlanLayerHasNoTransport` (extended)                          | PASS   |
| 39 | CLI exit codes 0/1/2/3                                              | `TestRunAdmitCLI_ExitCodeSemantics`, `TestParseAdmitArgs` cases   | PASS   |

## MALFORMED-POLICY EVIDENCE

```go
// TestPolicy_LoadFromRepo_Malformed writes a structurally invalid
// .bjj/policy.toml into a temp directory and asserts that
// LoadPolicyFromRepo returns:
//   Policy: zero value
//   Source: PolicySource{Outcome: PolicyOutcomeMalformed}
//   err:    *Error{Code: CodePolicyInvalid}
//
// It also asserts that admission does NOT silently fall back to
// DefaultPolicy on malformed input — the caller MUST surface the
// error.
```

Unknown boolean values (`allow_new_bookmarks = maybe`) yield the
same `CodePolicyInvalid` failure. Unknown top-level keys FAIL
CLOSED under `schema_version = 1`; unknown bool values do too.
Duplicate keys (whether the second occurrence is recognised or
not) also FAIL CLOSED, so a generated/merged policy file cannot
quietly weaken a rule by repeating the same key with conflicting
values. A present policy file MUST declare exactly one
`schema_version` line; absent files still resolve to the built-in
default.

## FACT-QUERY-FAILURE EVIDENCE

```go
// TestAdmission_NoTransport runs the gatherer against a
// directory that is NOT a jj repository. The adapter fails with
// a subprocess error, the gatherer wraps it in
// errJJToAdmission(...), and Evaluate is never reached.
//
// Outcome: *Error{Code: CodeJJQueryFailed, Message: "..."}
// Exit code 2 (exitInternalError).
//
// The CLI maps typed *Error to exitInternalError; the JSON
// envelope is {schema_version:1, code:JJ_QUERY_FAILED, message:..., cause:...}.
```

The same path is exercised by every adapter failure (malformed
output, broken repository, missing tool). The evaluator is NEVER
reached with `Facts{}` because the gatherer returns an error.

## MID-GATHER-MUTATION EVIDENCE

```go
// TestAdmission_Gather_UsesFrozenOpID wraps the production
// adapter with mutatingAdapter. The wrapper:
//
//   1. captures the opID passed to the first gather call;
//   2. mutates the underlying repo (jj new) before delegating
//      to the production adapter;
//   3. captures the opID passed to the second gather call;
//   4. asserts that BOTH opIDs equal the original
//      PlanObservation.SourceOperationID.
//
// The production adapter is fail-closed: an empty opID would
// yield INCONSISTENT_REPOSITORY_VIEW. Since the CLI threads the
// frozen opID through, the gatherer always observes the
// pre-mutation view, even though the repo has been mutated in
// between.
```

## CLI SAMPLES

### Text mode (default)

```text
$ bjj admit --remote lab --bookmark feature
bjj admit
schema_version: 1
decision:       admit
subject:
  remote:     lab
  bookmark:   feature
  old_commit: <absent>
  new_commit: aabbccdd0000000000000000000000000000aabb

$ echo $?
0
```

```text
$ bjj admit --remote lab --bookmark feature
bjj admit
schema_version: 1
decision:       deny
subject:
  remote:     lab
  bookmark:   feature
  old_commit: 1111111100000000000000000000000000001111
  new_commit: 2222222200000000000000000000000000002222
reasons:
  - NON_FAST_FORWARD: policy forbids non-fast-forward moves

$ echo $?
3
```

```text
$ bjj admit --remote lab --bookmark feature
bjj admit
schema_version: 1
decision:       not_needed
subject:
  remote:     lab
  bookmark:   feature
  old_commit: 3333333300000000000000000000000000003333
  new_commit: 3333333300000000000000000000000000003333
reasons:
  - NO_REMOTE_CHANGE: no remote change; publication would be a no-op

$ echo $?
3
```

### JSON mode

```text
$ bjj admit --remote lab --bookmark feature --json
{
  "schema_version": 1,
  "decision": "admit",
  "reasons": [],
  "subject": {
    "remote": "lab",
    "bookmark": "feature",
    "old_commit_id": "",
    "new_commit_id": "aabbccdd0000000000000000000000000000aabb"
  }
}
```

```text
$ bjj admit --remote lab --bookmark feature --json
{
  "schema_version": 1,
  "decision": "deny",
  "reasons": [
    { "code": "NON_FAST_FORWARD", "message": "policy forbids non-fast-forward moves" }
  ],
  "subject": {
    "remote": "lab",
    "bookmark": "feature",
    "old_commit_id": "1111111100000000000000000000000000001111",
    "new_commit_id": "2222222200000000000000000000000000002222"
  }
}
```

## STATIC GUARDS

`TestPlanLayerHasNoTransport` walks the AST of every production
file under `internal/plan`, `internal/jjadapter`, and
`internal/admission`, and rejects any `[]string` literal
containing the sequences:

```text
git push
git fetch
jj git push
jj git fetch
```

```text
ADMISSION_LAYER_HAS_NO_TRANSPORT = PASS
```

## CONFLICT DETECTION QUIRK (0.41.0)

`jj 0.41.0` does NOT expose a `conflict()` revset function. The
admission fact gatherer therefore relies on the per-commit
`conflict` template field via a new `ListCommitObs` adapter
method.

The `CommitObs` template is:

```text
commit_id ++ "|" ++ change_id ++ "|" ++ conflict ++ "|" ++ description.first_line() ++ "\n"
```

`ParseCommitObsLines` fails closed on any malformed row
(wrong field count, non-boolean `conflict`, empty
`commit_id`/`change_id`). An empty line is permitted.

## TEST RESULTS

```text
$ go test ./...
ok      github.com/s1onique/bjj/cmd/bjj          0.30s
ok      github.com/s1onique/bjj/internal/admission  28.7s
ok      github.com/s1onique/bjj/internal/execx     1.20s
ok      github.com/s1onique/bjj/internal/jjadapter 0.87s
ok      github.com/s1onique/bjj/internal/lab      20.6s
ok      github.com/s1onique/bjj/internal/plan      41.6s
ok      github.com/s1onique/bjj/internal/version   1.67s
```

Total: 141 tests, all PASS (10 new since the previous digest: 9
CORRECTION03 policy-view-binding tests in
`internal/admission/correction03_test.go`, including
adversarial mid-mutation fixtures and an adapter input-validation
test; the `TestCmdAdmissionPathUsesPolicyReader` static guard
is in `internal/plan/safety_test.go` and contributes one more).

### Admission-only test inventory

```text
TestEvaluate_CanonicalOrderOfReasons                  PASS
TestEvaluate_Deterministic                            PASS  (5 iter, byte-identical)
TestEvaluate_JSON_OmitsAbsolutePathsAndURLs           PASS
TestEvaluate_SubjectIdentity                          PASS
TestEvaluate_CreateAdmit                              PASS
TestEvaluate_CreateDisabled_Deny                      PASS
TestEvaluate_FastForwardAdmit                         PASS
TestEvaluate_NonFastForward_DenyDefault               PASS
TestEvaluate_NonFastForward_AllowedByPolicy           PASS
TestEvaluate_NoChange_NotNeeded                       PASS
TestEvaluate_PrivateCommit_Deny                       PASS
TestEvaluate_EmptyDescription_Deny                    PASS
TestEvaluate_ConflictedCommit_Deny                    PASS
TestEvaluate_ConflictedCommit_AllowedByPolicy         PASS
TestPolicy_DefaultPolicyMatchesDoc                    PASS
TestPolicy_LoadFromRepo_Absent                        PASS
TestPolicy_LoadFromRepo_Malformed                     PASS
TestPolicy_LoadFromRepo_Loaded                        PASS
TestPolicy_AmbientJJPolicyIndependent                 PASS
TestAdmission_CreateDefaultAdmit                      PASS
TestAdmission_FastForwardAdmit                        PASS
TestAdmission_NoRemoteChange_NotNeeded                PASS
TestAdmission_PrivateCommit_Deny                      PASS
TestAdmission_PrivateAncestor_Deny                    PASS
TestAdmission_EmptyDescription_Deny                   PASS
TestAdmission_ConflictedCommit_Deny                   PASS
TestAdmission_NonFastForward_DenyDefault              PASS
TestAdmission_NonFastForward_AllowedByPolicy          PASS
TestAdmission_NoTransport                             PASS
TestAdmission_JSON_NoEnvironment                      PASS
TestAdmission_Gather_UsesFrozenOpID                   PASS
TestAdmission_AlreadyPublishedPrivateAncestor_DoesNotDeny PASS  (CORRECTION01)
TestAdmission_AlreadyPublishedBadDescription_DoesNotDeny  PASS  (CORRECTION01)
TestAdmission_SubjectSetMatchesPlan                   PASS  (CORRECTION01)
TestPolicy_UnknownKey_FailsClosed                     PASS  (CORRECTION01)
TestPolicy_TypoCannotWeakenPolicy                     PASS  (CORRECTION01)
TestPolicy_DuplicateKey_FailsClosed                   PASS  (CORRECTION02)
TestPolicy_DuplicateSchemaVersion_FailsClosed         PASS  (CORRECTION02)
TestPolicy_DuplicateCannotWeakenPolicy                PASS  (CORRECTION02)
TestPolicy_SchemaVersionRequired_PresentFileMissingSchema PASS (CORRECTION02)
TestPolicy_SchemaVersionRequired_DuplicateFails       PASS  (CORRECTION02)
TestPolicy_SchemaVersionRequired_UnsupportedFails     PASS  (CORRECTION02)
TestPolicy_AbsentFileStillUsesDefault                 PASS  (CORRECTION02)
TestDocs_NoUnknownKeyIgnoredClaim                     PASS  (CORRECTION02)
TestDocs_ArchitectureNoFourQueryGatherer              PASS  (CORRECTION02)
TestDocs_ArchitecturePlanBridgeLocation               PASS  (CORRECTION02)
TestDocs_AdmissionPlanBridgeLocation                  PASS  (CORRECTION02)
TestDocs_PrivatePredicateIntersection                 PASS  (CORRECTION02)
TestAdmission_MidPolicyMutation_RestrictiveToPermissive_CannotAdmitNFF PASS (CORRECTION03)
TestAdmission_MidPolicyMutation_PermissiveToRestrictive_CannotDenyFF  PASS (CORRECTION03)
TestAdmission_MidPolicyMutation_PolicyLiveRemoveIgnored               PASS (CORRECTION03)
TestAdmission_MidPolicyMutation_PolicyLiveAddIgnored                  PASS (CORRECTION03)
TestAdmission_MidPolicyMutation_PolicyLiveMalformingIgnored           PASS (CORRECTION03)
TestAdmission_PolicyAbsentInFrozenCandidate_UsesDefault               PASS (CORRECTION03)
TestAdmission_PolicySource_BoundToFrozenView                          PASS (CORRECTION03)
TestAdmission_PolicyMalformedInFrozenCandidate_FailsClosed            PASS (CORRECTION03)
TestAdapter_FileShowAtOp_RefusesEmptyInputs                           PASS (CORRECTION03)
TestCmdAdmissionPathUsesPolicyReader                                  PASS (CORRECTION03)
```

### CLI tests

```text
TestParseAdmitArgs                              PASS  (15 sub-cases)
TestRunAdmitDispatch_NoArgs                     PASS
TestRunAdmitDispatch_UnknownFlag                PASS
TestRunAdmitDispatch_HelpUpdated                PASS
TestRunAdmitDispatch_RequiresJJ                 SKIP (jj present)
TestRenderAdmitJSON_IsStrictJSON                PASS
TestRunAdmitCLI_ExitCodeSemantics               PASS
```

## jj 0.41.0 known caveat (preserved from PLAN01)

`description("seed: initial commit")` and similar whitespace-
containing glob patterns are NOT matched by `jj`'s default
`description("pattern")` syntax. The lab's `SeedConflictedCommit`
helper works around this by capturing change-ids immediately
after creation and using them as revset inputs.

## DEFERRED WORK

1. **SubjectDigest** — the typed identity carried in
   `AdmissionResult.Subject` is structural, not cryptographic.
   The cryptographic digest (binding admission outcome to a
   content-addressed plan hash) belongs to a later evidence-
   binding ACT.

2. **Authoritative policy** — the BJJ policy file lives inside
   the candidate repository (`.bjj/policy.toml`). This is
   candidate-local by design; future ACTs bind authoritative
   policy outside the candidate's unilateral control.

3. **`bjj check`, `bjj publish`, `bjj receipt`** — not in this
   ACT. Admission only authorizes the subject to proceed to
   expensive verification; the actual execution is gated by
   future ACTs.

4. **REMOTE_BOOKMARK_CONFLICTED** — PLAN01 documented that
   conflicted remote-tracking bookmarks cannot be reliably
   fabricated on 0.41.0. Admission treats the plan-side
   `Conflict` field as authoritative; a future ACT will re-bind
   this against the transport step.

5. **Multi-bookmark plans, `--all`, `--tracked`** — not in
   this ACT. Admission evaluates one subject at a time; future
   ACTs may extend the gatherer to multi-subject evaluation.

6. **Strict `conflict` revset** — depends on a future jj
   release. Until then, conflict detection relies on the
   `conflict` template field via `ListCommitObs`.

## NEXT ACT

`ACT-BJJ-CHECK01 — Bound Factory Verification to Admitted
Subject`. This ACT will consume
`AdmissionResult{Decision: admit}` and run content-hash,
description-policy, and proof-equivalence checks against the
canonical plan. (The transaction state is named `CHECKED`, so
the ACT is named `CHECK01` to avoid vocabulary drift between
state names and ACT names.)

`ACT-BJJ-PUBLISH01` and `ACT-BJJ-RECEIPT01` follow the same
gate ordering: plan → admit → check → publish → receipt.

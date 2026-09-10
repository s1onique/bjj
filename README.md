# Bounded Jujutsu (bjj)

> **Experimental.** The publication boundary is not implemented yet.
> This project is currently the reproducible laboratory in which that
> boundary will later be designed, falsified, and proven.

Bounded Jujutsu is a bounded publication interface layered above
Jujutsu (`jj`) and Git-compatible remotes. It owns the transition from
private/local candidate state to shared/remote state.

## What BJJ is

```text
LLM / engineer
      │
      ├──────────────> jj
      │                 local/private VCS operations
      │
      └─ publication intent
             │
             ▼
            bjj
      ┌────────────────────┐
      │ plan               │
      │ policy             │
      │ verification       │
      │ evidence binding   │
      │ capability gate    │
      │ controlled publish │
      │ remote verification│
      └─────────┬──────────┘
                │
                ▼
        Git-compatible remote
```

## What it is not

BJJ is not:

- another DVCS,
- a complete wrapper around `jj`,
- a collection of Git hooks,
- a GitHub-specific client.

## Intended model

```text
jj  = expressive local change management
bjj = bounded authority transitions
remote hosting = final shared authority
```

## Current ACT

This repository is at **ACT-BJJ-ADMISSION01 — Publication Subject
Admission Boundary** (with CORRECTION01, CORRECTION02, and
CORRECTION03 incorporated; builds on ACT-BJJ-PLAN01). The act adds:

- a typed `PublishPlan` domain model (`internal/plan`),
- a bounded `jj` adapter (`internal/jjadapter`) that never invokes
  transport commands,
- a `bjj plan` command that answers *"what shared remote state would
  publication attempt to create or move?"* for a given
  `(remote, bookmark)` pair.
- a `bjj admit` command that answers *"is this publication subject
  structurally eligible to proceed to expensive verification?"*
  with a typed decision (`admit` | `deny` | `not_needed`).

PLAN01 freezes the publication subject. ADMISSION01 evaluates that
subject against an explicit policy and emits a typed decision.
Both ACTs deliberately do **not** implement `bjj publish`. Evidence
binding, transport, and remote verification belong to subsequent
ACTs.

### ADMISSION01-CORRECTION01

The original ADMISSION01 closure claimed three properties that
were not actually enforced in code. CORRECTION01 implements them
and adds tests for each:

1. **Subject-set exactness.** The fact gatherer reads
   `PublishPlan.Commits` verbatim from
   `planView.OutgoingCommits()` and restricts every predicate
   query to that exact set. An already-published ancestor no
   longer widens admission.
2. **Fail-closed policy schema.** Unknown keys in
   `.bjj/policy.toml` produce a hard `POLICY_INVALID` error
   instead of being silently ignored.
3. **Real package decoupling.** `internal/admission` does not
   import `internal/plan`; the concrete
   `*plan.PublishPlan -> admission.PlanView` bridge lives in
   `cmd/bjj/plan_adapter.go`.

### ADMISSION01-CORRECTION02

Three remaining P1 defects closed after the CORRECTION01 review.
No architecture changes; this is a policy-parser hardening and
a documentation-truth pass.

1. **Duplicate policy keys fail closed.** Every key in
   `.bjj/policy.toml` (recognised or unknown) may appear at
   most once; a second occurrence is `POLICY_INVALID`. The
   bounded parser preserves the fail-closed duplicate-key
   rule that the TOML spec already mandates ("Defining a key
   multiple times is invalid"), so a generated or merged
   policy file cannot quietly weaken a rule by repeating the
   same key with a conflicting value.
2. **`schema_version` is required.** A present policy file
   must declare exactly one `schema_version = 1` line; absent
   files still resolve to `DefaultPolicy()`. The
   `schema_version` line is now an explicit schema boundary
   that future migrations can rely on.
3. **Documentation truth.** The post-CORRECTION01 review
   found three stale prose passages in the durable documents
   (the malformed-policy description still saying "Unknown
   keys are ignored for forward-compatibility", the
   fact-gathering contract listing `subject | policy.
   PrivateCommits` (union) instead of the actual
   intersection, and the architecture section still
   describing a four-query gatherer with `::NEW ~ root()`).
   All replaced and now enforced by `TestDocs_*` regression
   tests in `internal/admission/docs_test.go`.

### ADMISSION01-CORRECTION03

One P0 defect capable of false admission or denial: the
repository-local policy was read from the live working-tree
filesystem (`os.ReadFile(<dir>/.bjj/policy.toml)`) AFTER
PLAN01 had pinned the operation view, so a concurrent
working-copy mutation could mix views. CORRECTION03 binds
the policy read to the same frozen Jujutsu view that
produced the PublishPlan.

1. **Policy view binding (P0).** `.bjj/policy.toml` is now
   read via `jj --at-op=<OP_A> file show -r <NEW> .bjj/policy.toml`.
   `internal/jjadapter.Adapter.FileShowAtOp` carries the typed
   (opID, revision) seam; `internal/admission.PolicyReader` +
   `LoadPolicyAt` is the production load path. The legacy
   `LoadPolicyFromRepo(dir)` helper is retained only for
   parser unit tests.
2. **Single-view invariant.** Plan, facts, and policy bytes
   all come from the same frozen operation view:
   `ADMISSION_PLAN_VIEW == ADMISSION_FACT_VIEW ==
   ADMISSION_POLICY_VIEW == OP_A`. CHECK01 can build on this
   single decision.
3. **Adversarial mid-admission mutation tests.** Six
   regression tests prove that policy bytes from OP_A/NEW
   survive every form of working-copy mutation between plan
   resolution and policy load: rewrite to permissive,
   rewrite to restrictive, remove, add, malform. One
   additional test asserts `PolicySource` carries the
   (opID, NEW) pair. One asserts `FileShowAtOp` refuses
   empty opID/revision/path.
4. **Static guard.** `TestCmdAdmissionPathUsesPolicyReader`
   in `internal/plan/safety_test.go` AST-rejects any
   production caller that invokes
   `admission.LoadPolicyFromRepo`. The live-fs reader
   cannot reappear in the production path without this test
   failing.
5. **"stricter than TOML" fossil.** The CORRECTION02 docs
   claimed the bounded parser is "stricter than TOML
   itself" with respect to duplicate keys, but TOML already
   bans duplicate keys (v1.1.0). Corrected to "preserves the
   fail-closed duplicate-key rule that the TOML spec
   already mandates"; the fossil is enforced by
   `TestDocs_NoStricterThanTOMLForDuplicates`.

`policy provenance != policy authority`: this closure binds
the provenance of repository-local policy bytes but does NOT
make that policy authoritative. Authoritative policy remains
deferred to a later ACT.

See:

- `docs/architecture.md` for trust zones and the transaction
  pipeline.
- `docs/acts/ACT-BJJ-LAB01.md` for the laboratory baseline.
- `docs/acts/ACT-BJJ-PLAN01.md` for the plan primitive contract.
- `docs/acts/ACT-BJJ-ADMISSION01.md` for the admission boundary
  (now including CORRECTION01, CORRECTION02, and CORRECTION03).

## Command surface

```sh
bjj version [--json]
bjj plan --remote <remote> --bookmark <bookmark> [--json]
bjj admit --remote <remote> --bookmark <bookmark> [--json]
```

## Build and test

```sh
go vet ./...
go test ./...
go build ./cmd/bjj
```

## License

Apache License 2.0. See `LICENSE`.

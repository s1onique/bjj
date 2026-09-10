# Architecture

BJJ is designed around a small number of explicit trust zones and a
transaction pipeline that crosses from one zone to another.

> The architecture document records **intended** structure. Only the
> parts marked **Implemented in ACT-BJJ-LAB01** have been built.
> Everything after the laboratory is **future architecture**.

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

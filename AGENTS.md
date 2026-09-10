# AGENTS.md — Operating Rules for BJJ Agents

This file describes the conventions every agent (human or automated) is
expected to follow while working in this repository.

## VCS doctrine

This repository is **Jujutsu-first**.

Agents should use `jj` for ordinary VCS inspection and local history
manipulation:

```text
jj status
jj log
jj diff
jj describe
jj new
jj squash
jj rebase
jj bookmark list --all
jj op log
```

Do not normalize a workflow around raw Git porcelain merely because a
`.git` backend exists.

Raw Git may be used where explicitly required for interoperability
experiments (for example, seeding the BJJ publication laboratory).

## Publication doctrine

**Until BJJ publication authority exists, NO remote publication is
authorized by this ACT.**

Specifically, while implementing ACT-BJJ-LAB01 against the real
repository:

```text
git push        FORBIDDEN
jj git push     FORBIDDEN
gh ... mutation FORBIDDEN
```

The ACT's push experiments must operate **only against disposable
local remotes created by the test/lab harness** (see
`internal/lab`).

## Safety

Never run destructive commands against the real remote during tests.

Lab fixtures must use temporary directories created by
`os.MkdirTemp("bjj-lab-...")`. Tests must not depend on the developer's:

```text
GitHub credentials
SSH agent
Git credential helper
global Git identity
global Jujutsu identity
real repository remote
```

where isolation is feasible. Tests should explicitly configure
per-repository identities and `JJ_CONFIG` overrides.

The subprocess seam in `internal/execx` strips credential-bearing
variables from child environments by default; do not bypass it.

## Output

Machine-facing BJJ output introduced in this repository must
eventually support strict structured output. Do not emit pseudo-JSON
or human-only formats where machine parsability is expected.

## Publication boundary

The BJJ publication boundary is **partially implemented**:

- `internal/plan` (`bjj plan`) — ACT-BJJ-PLAN01 closed: freezes
  the canonical publication subject.
- `internal/admission` (`bjj admit`) — ACT-BJJ-ADMISSION01
  closed: evaluates the frozen subject against an explicit
  policy and emits a typed decision (`admit` | `deny` |
  `not_needed`).
- `internal/verify`, `internal/publish`, `internal/receipt` —
  **not yet implemented**.

Do not attempt to push this repository automatically. The user
will decide the publication step separately because the very
mechanism by which this repository should eventually publish is
what the project is constructing.

The `bjj admit` command is read-only: it never invokes
`git push`, `git fetch`, `jj git push`, or `jj git fetch`. The
`internal/admission` layer has no direct import of
`internal/plan` (the dependency is mediated by the `PlanView`
adapter) and the evaluator (`evaluate.go`) imports no I/O
package. Any future change that violates these invariants MUST
be caught by the AST guard in `internal/plan/safety_test.go`.

The bounded `.bjj/policy.toml` parser is fail-closed in three
directions and these invariants are enforced by tests:

- unknown keys → `POLICY_INVALID` (CORRECTION01 §4);
- duplicate keys → `POLICY_INVALID` (CORRECTION02 §1);
- present file without an explicit `schema_version = 1` →
  `POLICY_INVALID` (CORRECTION02 §2).

Absent files still resolve to the built-in default; the
schema_version requirement applies only to present files.

The policy load is bound to the SAME frozen Jujutsu operation
view that produced the PublishPlan. The production CLI
(`cmd/bjj/admit.go`) MUST NOT call
`admission.LoadPolicyFromRepo(dir)`; it MUST go through
`admission.LoadPolicyAt(ctx, reader, planView, dir, opID)`,
which reads the file via
`jj --at-op=<OP_A> file show -r <NEW> <path>`. The legacy
live-fs reader is retained only for parser unit tests, and
the static guard `TestCmdAdmissionPathUsesPolicyReader` in
`internal/plan/safety_test.go` AST-rejects any production
caller that tries to use it.

Documentation durability is enforced by `TestDocs_*` in
`internal/admission/docs_test.go`, which fail if any of the
known CORRECTION01/CORRECTION02/CORRECTION03 fossil phrases
reappear in `docs/architecture.md` or
`docs/acts/ACT-BJJ-ADMISSION01.md` ("Unknown top-level keys
are ignored for forward-compatibility", "outgoing set ::NEW ~
root()", "internal/plan provides a WrapPlan() adapter
implementation", `subject | policy.PrivateCommits` inside a
code-fenced block, "stricter than TOML itself" applied to
duplicate keys).

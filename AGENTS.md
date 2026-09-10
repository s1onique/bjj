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

The BJJ publication boundary is **not yet implemented**. Do not
attempt to push this repository automatically. The user will decide
the publication step separately because the very mechanism by which
this repository should eventually publish is what the project is
constructing.

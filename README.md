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

This repository is at **ACT-BJJ-PLAN01 — Deterministic Publication
Subject / PublishPlan**. The act adds:

- a typed `PublishPlan` domain model (`internal/plan`),
- a bounded `jj` adapter (`internal/jjadapter`) that never invokes
  transport commands,
- a `bjj plan` command that answers *"what shared remote state would
  publication attempt to create or move?"* for a given
  `(remote, bookmark)` pair.

PLAN01 deliberately does **not** implement `bjj publish`. It freezes
the publication subject only; admission, evidence binding, transport
and remote verification belong to subsequent ACTs.

See:

- `docs/architecture.md` for trust zones and the transaction
  pipeline.
- `docs/acts/ACT-BJJ-LAB01.md` for the laboratory baseline.
- `docs/acts/ACT-BJJ-PLAN01.md` for the plan primitive contract.

## Command surface

```sh
bjj version [--json]
bjj plan --remote <remote> --bookmark <bookmark> [--json]
```

## Build and test

```sh
go vet ./...
go test ./...
go build ./cmd/bjj
```

## License

Apache License 2.0. See `LICENSE`.

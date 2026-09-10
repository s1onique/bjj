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

This repository is at **ACT-BJJ-LAB01 — Publication Authority
Laboratory**. The act establishes:

- a tiny Go CLI (`cmd/bjj`),
- a bounded subprocess seam (`internal/execx`),
- a publication laboratory (`internal/lab`) that constructs disposable
  universes under `os.MkdirTemp` and exercises two CONTROL experiments:
  raw `git push` and raw `jj git push`,
- typed evidence suitable for machine auditing.

The ACT does **not** implement `bjj publish`. It establishes the
unbounded baseline that subsequent ACTs must remove.

See:

- `docs/architecture.md` for trust zones and the future transaction
  pipeline.
- `docs/acts/ACT-BJJ-LAB01.md` for the formal act and acceptance
  contract.

## Build and test

```sh
go vet ./...
go test ./...
go build ./cmd/bjj
```

## License

Apache License 2.0. See `LICENSE`.

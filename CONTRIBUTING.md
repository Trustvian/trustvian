# Contributing to Trustvian

Issues and pull requests are welcome. This document covers what CI
enforces and how to run the same checks locally.

For what the code should look like, see [CLAUDE.md](CLAUDE.md) and
[.claude/rules/](.claude/rules/) — they document the conventions this
codebase actually follows (package shape, error handling, test style,
dependency confinement), not generic Go advice.

## Before opening a pull request

```bash
make check
```

That runs `gofmt -l`, `go vet`, a build, and the race tests for the root
module — the same gates CI runs. `make help` lists every target.

New behavior needs a test. Security-relevant behavior needs a test that
would fail if the behavior regressed; several of this repository's
guarantees were verified by deliberately breaking the implementation and
confirming the test caught it.

## The three modules

Trustvian is three Go modules, and they are separate on purpose:

| Module | What it is |
|---|---|
| `.` (root) | The engine, CLI, and public API |
| `processor/` | The OpenTelemetry Collector processor |
| `examples/` | Runnable examples — and proof the public API works from outside |

`processor/` and `examples/` resolve the core module through a `replace`
directive, so **always verify them with the workspace disabled**:

```bash
cd processor && GOWORK=off go build ./... && GOWORK=off go test -race ./...
cd examples  && GOWORK=off go build ./... && GOWORK=off go test ./...
```

A Go workspace makes these modules resolve the local source automatically,
which hides a broken module boundary until someone tries to consume the
published module. CI runs both with `GOWORK=off` for exactly this reason.

`examples/` must never import `internal/*`. It exists to demonstrate that
the public API is sufficient; an internal import would compile here and
break for every real consumer. CI checks this.

## PostgreSQL tests

Storage integration and stress tests are opt-in. They skip when
`TRUSTVIAN_TEST_POSTGRES_DSN` is unset, so `go test ./...` never requires a
database:

```bash
make integration-postgres
```

That starts the reference deployment's PostgreSQL service and runs the full
suite against it. See [docs/storage-guide.md](docs/storage-guide.md) for
the manual setup and for the database-restart durability test, which needs
its own opt-in because restarting a server disrupts everything else
connected to it.

## Test tiers

Not every test runs on every commit:

| Tier | Runs | What it covers |
|---|---|---|
| **Pull request / push** | `main` and `develop` | Format, vet, build, tests, race — all three modules. PostgreSQL integration with `-short`. Compose config validation. |
| **Nightly** | Scheduled, or on demand | The full PostgreSQL stress tier (high-contention writes, concurrent first-writes, bounded row counts) and the reference deployment's end-to-end smoke test. |

The split is by cost, not by importance. Correctness gates belong on pull
requests, where a failure means the change is wrong. The stress tier takes
minutes and measures contention behavior, so it runs on a schedule where
it is useful as a trend rather than a tax on every push.

Run the stress tier locally by omitting `-short`:

```bash
TRUSTVIAN_TEST_POSTGRES_DSN='...' go test -race ./...          # everything
TRUSTVIAN_TEST_POSTGRES_DSN='...' go test -race -short ./...   # integration only
```

## Reference deployment

```bash
cd deployments/docker-compose
docker compose config    # what CI validates on every push
./smoke-test.sh          # the full end-to-end proof, nightly in CI
```

## CI

Two workflows, both read-only and neither requiring any secret:

| Workflow | Trigger | Purpose |
|---|---|---|
| `.github/workflows/ci.yml` | push / PR on `main` and `develop` | Quality gates |
| `.github/workflows/nightly.yml` | schedule, manual | Expensive tiers |

Neither publishes anything. Because they need no credentials, pull
requests from forks run the full gate set with nothing to leak.

If CI fails on formatting, run `make fmt` — CI reports violations but never
rewrites your code.

## Commits and releases

Work lands on `develop` and reaches `main` by pull request; release tags
are created on the resulting merge commit. Release history lives in
[CHANGELOG.md](CHANGELOG.md), and milestone status in
[docs/ROADMAP.md](docs/ROADMAP.md) — please don't add project-status prose
to the README, which is deliberately evergreen.

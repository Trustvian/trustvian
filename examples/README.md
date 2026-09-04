# Examples

Six small, runnable `package main` programs demonstrating the full
`Event -> Features -> Fingerprint -> Baseline -> Anomaly -> Trust ->
Policy -> Decision` pipeline against the real
`github.com/Trustvian/trustvian` Go SDK. Each is a genuinely external
consumer of the module — `examples/go.mod` is a separate Go module with
a `replace github.com/Trustvian/trustvian => ../` directive back to this
repository root, so these programs only ever see what an outside caller
actually sees (no `internal/` imports anywhere under this directory).

Five of the six scenarios port event sequences already verified in
[docs/use-cases.md](../docs/use-cases.md) and
[docs/sdk-guide.md § watching trust mature](../docs/sdk-guide.md#watching-trust-mature)
almost verbatim; the sixth (`frequency-abuse`) is new. Every README below
has real, `go run`-captured output, not hand-written output.

| Example | What it demonstrates | README |
|---|---|---|
| [basic](basic/) | Construct an `Engine`, analyze one well-formed event, print it via `Result.Explain()` — the "hello world" of the SDK | [basic/README.md](basic/README.md) |
| [credential-misuse](credential-misuse/) | A trusted, high-confidence service account performs a novel bulk-export action; full novelty from a trusted identity does not by itself mean block | [credential-misuse/README.md](credential-misuse/README.md) |
| [unexpected-dependency](unexpected-dependency/) | A payment gateway's baseline matures against its normal dependency, then the same route reaches an unexpected internal service at lower identity confidence | [unexpected-dependency/README.md](unexpected-dependency/README.md) |
| [external-destination](external-destination/) | An order service's baseline matures against a normal internal RPC call, then it suddenly reaches an external secrets manager | [external-destination/README.md](external-destination/README.md) |
| [frequency-abuse](frequency-abuse/) | A fully mature, familiar fingerprint bursts far outside its learned request cadence — `frequency_deviation` fires even with zero categorical novelty | [frequency-abuse/README.md](frequency-abuse/README.md) |
| [ai-agent](ai-agent/) | An AI agent's baseline matures against a benign CRM-lookup tool call, then the same agent reaches for a credentials store | [ai-agent/README.md](ai-agent/README.md) |

## Running them

```bash
cd examples/basic && go run .
# ...or all six at once, from the repository root:
make examples
```

`make examples` (see the root [`Makefile`](../Makefile)) runs every
example in turn and fails the build if any exits non-zero — this is the
mechanism that keeps these examples from silently going stale as the
`Engine`'s public API evolves.

No example requires network access, external services, or any setup
beyond `go run`.

## A note on `Decision`

Every example in this directory uses `trustvian.NewEngine()` with no
options, so every printed `Decision` is `observe_only` — `NewEngine()`'s
default `Policy` has no rules and always falls through to its
`observe_only` default (see
[docs/sdk-guide.md § Constructing an Engine](../docs/sdk-guide.md#constructing-an-engine)).

This is deliberate, not an oversight: getting `ALLOW`/`BLOCK`
differentiation like [docs/use-cases.md](../docs/use-cases.md) requires
passing a custom `Policy` via `trustvian.WithPolicy`, and `policy.Policy`
is a type that currently lives under `internal/` — per
[ADR 0002](../docs/adr/0002-public-api-boundary.md), a true external
module (which is exactly what `examples/` is set up to be) cannot
construct one yet. `Anomaly` and `Trust` — the interesting, differentiated
numbers each example is actually demonstrating — are unaffected by this;
only the final policy decision is flattened to `observe_only` here. See
[docs/sdk-guide.md § The public/internal boundary, today](../docs/sdk-guide.md#the-publicinternal-boundary-today)
for the full reasoning, and [`cmd/trustvian`](../cmd/trustvian) for how
this repository's own CLI configures a differentiated policy from code
living inside the module.

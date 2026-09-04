# basic

The "hello world" of the Trustvian Go SDK. Construct an `Engine` with no
options, analyze one well-formed HTTP event, print the `Result` via
`Result.Explain()`, then `Observe` it so it would count toward a future
baseline.

This corresponds to Example 1 in [docs/tasks/010-examples.md](../../docs/tasks/010-examples.md)
and mirrors the exact program shown in
[docs/sdk-guide.md § Analyze](../../docs/sdk-guide.md#analyze).

Run it:

```bash
cd examples/basic && go run .
```

## Real output

```
Decision: observe_only
trust 1.00 (low): identity confidence 1.00, anomaly 1.00 at 0% confidence, context risk 0.00
Anomaly score: 1.00 (confidence 0.00)
Detected:
  - categorical_novelty: 1.00 (fingerprint never observed for this actor)
Policy: default action (no policy rules configured; observing by default)
```

Anomaly score is `1.00` because this actor/operation/target combination
has never been observed before (cold start) — but `Confidence` is `0.00`,
so `Trust` doesn't collapse: the engine correctly treats "this is
unfamiliar" and "how much should I trust that reading" as two separate
questions. See [Architecture § cold start](../../docs/ARCHITECTURE.md#cold-start-two-numbers-not-one).

`Decision` is `observe_only` for every example in this directory —
`NewEngine()`'s default `Policy` has no rules and always falls through to
its `observe_only` default. Getting `ALLOW`/`BLOCK` differentiation like
[docs/use-cases.md](../../docs/use-cases.md) requires a custom `Policy`,
which today can only be constructed by code living inside this module
(see [the examples index](../README.md#a-note-on-decision) for why).

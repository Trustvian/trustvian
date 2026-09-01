# Testing Conventions

## Baseline gate

Before any slice is done: `go build ./...`, `go vet ./...`, `gofmt -l`
(empty output), and `go test ./...` all pass. Anything touching
`internal/store` or `internal/baseline` also runs under `go test
-race`, since concurrent `Observe`/`Get` correctness is the actual
point of that package's sharded-lock design.

## Shape

- Table-driven tests are the default (`tests := []struct{...}{...}`
  with `t.Run(tt.name, ...)`), not a sequence of one-off `TestX`
  functions for closely related cases.
- Test the documented formula, not just its direction. When a
  computation is specified as a formula (`internal/anomaly`'s
  noisy-OR combination, `internal/trust`'s multiplicative score), at
  least one test reproduces the exact arithmetic
  (`TestScoreMatchesDocumentedNoisyOrFormula`,
  `TestComputeMultiplicativeFormula`) — "the score went up" is not
  enough for a security-relevant number.
- Prove immutability, don't just document it. Where a type is
  immutable-by-convention (`Baseline.Observe`), a test constructs a
  value, derives a second one, and asserts the first is unaffected
  (`TestBaselineObserveIsImmutable`).

## Benchmarks

- `b.Loop()` + `b.ReportAllocs()`, not the classic `for i := 0; i <
  b.N; i++` loop.
- Split the common/cheap path from the worst case rather than
  benchmarking one blended scenario — `BenchmarkScoreKnownFamiliar` vs
  `BenchmarkScoreNovelWithAllSignals`, `BenchmarkInMemoryObserveSameKey`
  vs `...DistinctKeys`. This is what actually caught the wasted
  `fmt.Sprintf` call on the anomaly-scoring hot path in Slice 5 — a
  single averaged benchmark would have hidden it.
- When a benchmark reveals an avoidable allocation on a path that
  should be cheap (the "nothing is wrong" case), fix it before moving
  on. Don't defer a hot-path regression just because the acceptance
  criteria didn't explicitly name it.

## Things that cannot be faked

- `sdktrace.ReadOnlySpan` (OTel) has a deliberately unexported method —
  only the real SDK can produce one. `internal/otel`'s tests build
  actual spans through a `TracerProvider` with a capturing
  `SpanExporter`; don't attempt a hand-rolled implementation of that
  interface.
- Don't assume `nil` means "no resource" when constructing a test
  `TracerProvider` — the SDK attaches its own default `Resource`
  (including a fallback `service.name`) unless you pass
  `resource.Empty()` explicitly. This tripped up an early version of
  the OTel adapter tests; the fix was in the test, not the adapter.

## CLI tests

`cmd/trustvian`'s tests call `run(args)` in-process with
`os.Stdout`/`os.Stderr` temporarily redirected through `os.Pipe`
(`captureOutput` in `main_test.go`), not `exec.Command` on a built
binary. This is faster and still exercises the real code path — file
I/O, JSON parsing, the full engine pipeline — without the
`go build`-per-test-run overhead a subprocess approach would add.

## End-to-end tests are load-bearing, not decorative

The isolated per-package tests (Slices 1–7) all pass in isolation
using hand-seeded baselines. They did not — and structurally could not
— catch two real bugs that only surfaced once the pipeline was wired
together in `engine_test.go` (Slice 8):

1. A learning deadlock where `Observe` excluded `ALERT` decisions from
   eligibility, so a fingerprint whose transient partial-maturity risk
   crossed into `ALERT` could never accumulate the observations needed
   to mature past it.
2. A test fixture that assumed a `SensitiveTargetFloor`-gated
   fingerprint could reach full maturity through the *gated* learning
   loop — impossible by construction, since `BLOCK` is correctly
   ineligible for learning.

Any new cross-stage behavior (anything involving `Engine.Analyze` +
`Engine.Observe` together, not just one stage in isolation) needs at
least one test that runs the actual gated loop end-to-end, not a
synthetic baseline built by calling internal `Observe` methods
directly. Reserve direct baseline-seeding for tests that are
specifically checking behavior the gated loop cannot itself construct
(see `TestAnalyzeSensitiveTargetFloorEndToEnd`'s comment for the
distinction).

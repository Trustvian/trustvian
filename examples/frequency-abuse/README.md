# frequency-abuse

The one scenario in this directory with no corresponding
[docs/use-cases.md](../../docs/use-cases.md) section to port — the
`frequency_deviation` anomaly signal
([docs/tasks/004-anomaly.md](../../docs/tasks/004-anomaly.md)) did not
exist when that document was written.

A service (`svc-poller`) polls the same status endpoint
(`StatusService.GetStatus` on `status-service`) on a consistent
10-second cadence, 25 times. This matures the baseline's `IntervalMean`
(an EWMA of inter-observation intervals — see
[`internal/baseline.FingerprintStats`](../../internal/baseline/baseline.go)).
All `Timestamp`s are constructed explicitly (`base.Add((i-1) *
10*time.Second)`), not produced by real `time.Sleep` calls, so the
example runs instantly and deterministically. One final event arrives
only 50ms after the previous one — a burst far outside the learned
cadence — and `frequency_deviation` appears in the result's
`Anomaly.Contributors`, even though the fingerprint itself is now fully
familiar (`categorical_novelty` has decayed to zero and drops out of
`Contributors` entirely, since only non-zero-value signals are
reported).

The program asserts `frequency_deviation` is actually present before
exiting (`log.Fatal` otherwise), so a regression in
`internal/anomaly.Score`'s frequency signal would fail this example, not
just silently produce different-looking output.

Run it:

```bash
cd examples/frequency-abuse && go run .
```

## Real output

```
poll  1: confidence=0.00 trust=0.95 decision=observe_only learned=true
poll 10: confidence=0.45 trust=0.71 decision=observe_only learned=true
poll 20: confidence=0.95 trust=0.90 decision=observe_only learned=true
poll 25: confidence=1.00 trust=0.95 decision=observe_only learned=true

Decision: observe_only
trust 0.38 (high): identity confidence 0.95, anomaly 0.60 at full confidence, context risk 0.00
Anomaly score: 0.60 (confidence 1.00)
Detected:
  - frequency_deviation: 1.00 (interval 50ms deviates from a stable baseline of 10s (stddev ~0))
Policy: default action (no policy rules configured; observing by default)

frequency_deviation signal present in Contributors, as expected.
```

Unlike the other five examples, this final event's `Anomaly.Confidence`
is `1.00` (the fingerprint is fully mature) and `categorical_novelty`
does not appear in `Contributors` at all — the entire `Anomaly.Score` of
`0.60` comes from `frequency_deviation` alone (`1.00` signal value ×
`0.6` `FrequencyWeight`, per `internal/anomaly.combine`'s noisy-OR
formula). This is what distinguishes "abnormal request frequency" from
plain cold-start novelty: a familiar actor doing a familiar thing, just
far too often. See
[the examples index](../README.md#a-note-on-decision) for why `Decision`
here is `observe_only` rather than a differentiated `ALERT`/`BLOCK`.

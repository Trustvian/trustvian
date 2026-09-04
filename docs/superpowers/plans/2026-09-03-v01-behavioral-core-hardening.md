# v0.1 — Behavioral Core Hardening & First Public Release — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take Trustvian's already-implemented core pipeline (`Event → Features →
Fingerprint → Baseline → Anomaly → Trust → Policy → Decision`) from "works and is
tested" to "hardened, explainable, benchmarked, demonstrated, and released as a
stable OSS artifact" — no new pipeline stages, depth not breadth.

**Architecture:** Ten independently-scoped roadmap tasks (001, 002, 004, 005, 006,
007, 010, 011, 012, 013 — 003 is already done), executed in dependency order. Each
task is its own vertical slice through one or two packages, each ends with its own
`go test`/`go vet`/`gofmt` gate and (where relevant) a benchmark and a doc update,
per CLAUDE.md's Implementation Strategy.

**Tech Stack:** Go 1.27, no new dependencies (root module stays free of anything
beyond stdlib; `internal/otel` keeps its existing OTel SDK dependency, confined to
that package).

**Spec:** [docs/ROADMAP.md § v0.1](../../ROADMAP.md#v01--behavioral-core-hardening--first-public-release)
and the ten task files under [docs/tasks/](../../tasks/) (001, 002, 004, 005, 006,
007, 010, 011, 012, 013). Each task file already carries its own full Objective,
Scope, Non-Goals, Technical Requirements, Tests, Benchmarks, Documentation, and
Acceptance Criteria sections — this plan does not restate that content. Executors
**must read both** this plan (for exact current-code anchors, concrete type/function
signatures, and execution order) and the referenced task file (for the complete
acceptance bar) before starting a task.

## Global Constraints

- Go 1.27 idioms: `range N`, `b.Loop()`, `min`/`max` builtins, `maps.Copy`,
  `omitzero` (not `omitempty`) on new JSON struct tags.
- `gofmt -l` empty, `go vet ./...` clean, `go test ./...` passing (with `-race` for
  anything touching `internal/store` or `internal/baseline`) before any task is
  considered done — this is the baseline gate from `.claude/rules/testing.md`, not
  a one-time end-of-milestone check.
- No new third-party dependency anywhere except what already exists in
  `internal/otel`.
- Domain values stay immutable — no method mutates its receiver; `Baseline.Observe`-
  style copy-on-write for anything that evolves.
- Every scoring/threshold constant needs a config field with a doc comment
  justifying it (no "arbitrary formulas" — `.claude/rules/security.md`).
- Dependency direction only ever points earlier in the pipeline
  (`event → features → fingerprint → baseline/store → anomaly → trust → policy →
  root`). Never add an import pointing the other way.
- Do not create git commits unless the user explicitly asks (per CLAUDE.md's Git
  section) — this plan's steps include `git add`/`git commit` as the skill's
  standard task-closing step; skip actually invoking them unless told to, or ask
  before the first one.
- Update `docs/DOMAIN.md`, `docs/PERFORMANCE.md`, `docs/SECURITY.md`, and other
  `docs/` files listed per-task — CLAUDE.md requires `docs/` to stay in sync with
  the code, not deferred to the end.

**Execution order (dependency-respecting):** 001 → 002 → 004 → 005 → 006 → 007 →
010 → 011 → 012 → 013. This differs from the numeric order only in that 003 (done)
is skipped and 004 is pulled forward of 002/... no — order is exactly ascending
numeric order among the ten tasks in scope; it happens to already satisfy every
task file's stated `Depends on:` (002 depends on 001; 005 depends on 004; 010/011
benefit from 004 landing first; 013 depends on everything).

---

### Task 1: 001 — Feature Model Hardening (`TargetCategory`)

**Files:**
- Modify: `event/event.go` (add `TargetCategory` type + `Target.Category` field)
- Modify: `internal/features/features.go` (add `StableFeatures.TargetCategory`,
  populate in `Extract`)
- Test: `event/event_test.go` (new type validity table)
- Test: `internal/features/features_test.go` (extraction, including unset case)
- Test: new regression test proving `Context.TraceID`/`SpanID`/`Event.ID` never
  reach `Fingerprint.ID` — add to `internal/fingerprint/fingerprint_test.go` since
  it must go through `features.Extract` → `fingerprint.Compute` (add a small local
  helper wiring `event.Event` → `features.Extract` → `fingerprint.Compute` inline
  in the test; `internal/fingerprint` already imports `internal/features`, and this
  test package is a `_test` package that may import `event` too — check the
  existing test file's package clause before adding the import).
- Benchmark: re-run existing `BenchmarkExtract` in
  `internal/features/features_test.go` (no new benchmark file needed).

**Interfaces:**
- Consumes: `event.Event.Target` (currently `struct{ Name string }`).
- Produces: `event.TargetCategory` (new exported string type + constants),
  `features.StableFeatures.TargetCategory` (new field consumed by Task 2's
  `fingerprint.Compute`).

- [ ] **Step 1: Write the failing test for `TargetCategory` validity**

In `event/event_test.go`, add a table-driven test mirroring the existing
`ActorType`/`OperationCategory` pattern (read the existing test file first to match
its exact table shape and helper names). New test:

```go
func TestTargetCategoryValid(t *testing.T) {
	tests := []struct {
		name string
		cat  TargetCategory
		want bool
	}{
		{"unspecified is valid", TargetCategoryUnspecified, true},
		{"internal", TargetCategoryInternal, true},
		{"external", TargetCategoryExternal, true},
		{"database", TargetCategoryDatabase, true},
		{"unknown value invalid", TargetCategory("bogus"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cat.valid(); got != tt.want {
				t.Errorf("TargetCategory(%q).valid() = %v, want %v", tt.cat, got, tt.want)
			}
		})
	}
}

func TestEventValidateIgnoresTargetCategory(t *testing.T) {
	e := validEvent() // reuse whatever helper event_test.go already has for a
	// minimal valid Event; if none exists, build one inline matching Validate's
	// required fields (ID, Timestamp, Actor with valid Type and 0<=IdentityConfidence<=1,
	// Operation with valid Category and non-empty Name).
	e.Target.Category = TargetCategory("not-a-real-category")
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil — TargetCategory must not be checked", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./event/... -run TestTargetCategoryValid -v`
Expected: FAIL — `TargetCategory` undefined.

- [ ] **Step 3: Add `TargetCategory` type and `Target.Category` field**

In `event/event.go`, add after the `OperationDirection` block (around line 116),
following the exact pattern of `OperationDirection`/`ActorType`:

```go
// TargetCategory classifies what kind of destination a Target is. It is
// optional; the zero value means "unclassified" — a producer may set it or
// leave it unset, matching how Direction is already optional today.
type TargetCategory string

const (
	TargetCategoryUnspecified TargetCategory = ""
	TargetCategoryInternal    TargetCategory = "internal"
	TargetCategoryExternal    TargetCategory = "external"
	TargetCategoryDatabase    TargetCategory = "database"
)

func (c TargetCategory) valid() bool {
	switch c {
	case TargetCategoryUnspecified, TargetCategoryInternal, TargetCategoryExternal, TargetCategoryDatabase:
		return true
	default:
		return false
	}
}
```

Then update `Target`:

```go
// Target is the destination of the Operation: a service name, a database,
// or an external host. It is optional — not every Operation has a distinct
// destination. Category, when set, classifies what kind of destination
// this is (see TargetCategory); it is not required for Validate to pass.
type Target struct {
	Name     string         `json:"name"`
	Category TargetCategory `json:"category,omitzero"`
}
```

Do **not** call `Category.valid()` from `Event.Validate()` — per the task's
Technical Requirements, an invalid/unclassified value must never fail validation.
`valid()` exists only so a future caller (or a test) can check well-formedness
explicitly; it is unused by `Validate` by design (a linter may flag it as unused —
either give it an explicit test call, which Step 1 already does, or accept it's
exercised only from tests).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./event/... -v`
Expected: PASS, including all pre-existing `event` package tests.

- [ ] **Step 5: Extend `features.StableFeatures` and `Extract`**

In `internal/features/features.go`:

```go
type StableFeatures struct {
	ActorType         event.ActorType
	OperationCategory event.OperationCategory
	OperationName     string
	TargetName        string
	TargetCategory    event.TargetCategory
	Environment       string
}
```

(insert `TargetCategory` after `TargetName` — pick a consistent field order since
Task 2 will hash fields in struct-declaration-adjacent order for readability, though
the hash order is whatever `fingerprint.Compute` explicitly writes, not reflection).

In `Extract`:

```go
Stable: StableFeatures{
	ActorType:         e.Actor.Type,
	OperationCategory: e.Operation.Category,
	OperationName:     e.Operation.Name,
	TargetName:        e.Target.Name,
	TargetCategory:    e.Target.Category,
	Environment:       e.Context.Environment,
},
```

- [ ] **Step 6: Add features extraction test**

In `internal/features/features_test.go`, extend (or add alongside) the existing
`TestExtract`-style table with a case setting `Target.Category` and asserting it
flows through, plus a case leaving it unset asserting the zero value
(`event.TargetCategoryUnspecified`) comes through.

- [ ] **Step 7: Run `go test ./event/... ./internal/features/...` — verify green**

Run: `go test ./event/... ./internal/features/... -v`
Expected: PASS.

- [ ] **Step 8: Re-run `BenchmarkExtract`, confirm zero-allocation common case**

Run: `go test ./internal/features/... -bench BenchmarkExtract -benchmem -run ^$`
Expected: the "no `TargetCategory` set" case still reports `0 allocs/op` — a string
field copy adds no allocation, but verify, don't assume (per CLAUDE.md's benchmark
discipline). If a benchmark case doesn't already exist for the "nothing set"
scenario, confirm the existing `BenchmarkExtract` uses such an event; if it doesn't
split common/worst case, that's pre-existing — do not restructure it as part of
this task unless the zero-alloc claim can't otherwise be verified.

- [ ] **Step 9: Add the fingerprint-independence regression test**

Open `internal/fingerprint/fingerprint_test.go` first to see its package clause and
existing helper style. Add (adjusting names to match the file's conventions):

```go
func TestFingerprintIDIndependentOfEventIdentifiers(t *testing.T) {
	base := event.Event{
		ID:        "event-1",
		Timestamp: time.Now(),
		Actor:     event.Actor{ID: "actor-1", Type: event.ActorTypeService, IdentityConfidence: 1},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /x"},
		Target:    event.Target{Name: "svc-a"},
		Context:   event.Context{Environment: "prod", TraceID: "trace-1", SpanID: "span-1"},
	}
	varied := base
	varied.ID = "event-2"
	varied.Context.TraceID = "trace-2"
	varied.Context.SpanID = "span-2"

	fp1 := fingerprint.Compute(features.Extract(base).Stable)
	fp2 := fingerprint.Compute(features.Extract(varied).Stable)

	if fp1.ID != fp2.ID {
		t.Errorf("Fingerprint.ID changed when only Event.ID/TraceID/SpanID changed: %q vs %q", fp1.ID, fp2.ID)
	}
}
```

Add `"time"`, `"github.com/Trustvian/trustvian/event"`, and
`"github.com/Trustvian/trustvian/internal/features"` imports if not already
present (check first — `internal/fingerprint`'s own package already imports
`internal/features`, but the `_test` file may need `event` added).

- [ ] **Step 10: Run full task test suite**

Run: `go test ./event/... ./internal/features/... ./internal/fingerprint/... -v -race`
Expected: PASS.

- [ ] **Step 11: Update `docs/DOMAIN.md`**

Add `TargetCategory` to the stable-feature list in `docs/DOMAIN.md § Feature`
(read the existing section first to match its table/list format exactly). Note in
`docs/ARCHITECTURE.md` (per the task file) that no structural/dependency-direction
change occurred — a one-line confirmation, not a new section.

- [ ] **Step 12: `gofmt -l .` and `go vet ./...`**

Run: `gofmt -l . && go vet ./...`
Expected: no output from `gofmt -l`, no errors from `go vet`.

- [ ] **Step 13: Commit**

```bash
git add event/event.go event/event_test.go internal/features/features.go \
  internal/features/features_test.go internal/fingerprint/fingerprint_test.go \
  docs/DOMAIN.md docs/ARCHITECTURE.md
git commit -m "Add optional Target.Category stable dimension (task 001)"
```

---

### Task 2: 002 — Fingerprint Versioning & Design Doc

**Depends on:** Task 1 landed (TargetCategory exists so it can be folded into the
same version bump — spec: "one field-set change, one version bump, not two").

**Files:**
- Modify: `internal/fingerprint/fingerprint.go` (version marker + `TargetCategory`
  in the hash)
- Test: `internal/fingerprint/fingerprint_test.go` (version-changes-ID test;
  existing determinism/collision tests continue passing unmodified)
- Benchmark: re-run `BenchmarkCompute`

**Interfaces:**
- Consumes: `features.StableFeatures.TargetCategory` (from Task 1).
- Produces: no change to `Fingerprint{ID, Stable}`'s public shape — versioning is
  internal to `Compute`'s hash input only (per the task's own recommended
  resolution: no separate `Version` field, since no current consumer needs to read
  it — this keeps `.claude/rules/architecture.md`'s "add an interface/field only
  when needed" discipline).

- [ ] **Step 1: Write the failing version-changes-ID test**

`internal/fingerprint.Compute` has no seam to swap versions at runtime (by design —
one algorithm, one constant, per the task's Non-Goals). Test the property directly
by asserting the version constant is actually mixed into the hash: compute a
`Fingerprint` today, then (in the same test, before the constant is bumped) assert
against a hand-computed FNV-1a hash that includes the version string, proving the
version genuinely participates in the digest rather than being a no-op. Add to
`internal/fingerprint/fingerprint_test.go`:

```go
func TestComputeIncludesVersionInHash(t *testing.T) {
	stable := features.StableFeatures{
		ActorType:         event.ActorTypeService,
		OperationCategory: event.OperationCategoryHTTP,
		OperationName:     "GET /x",
		TargetName:        "svc-a",
		TargetCategory:    event.TargetCategoryInternal,
		Environment:       "prod",
	}

	h := fnv.New64a()
	writeFieldForTest(h, fingerprintVersion) // exported test-only alias, see Step 3
	writeFieldForTest(h, string(stable.ActorType))
	writeFieldForTest(h, string(stable.OperationCategory))
	writeFieldForTest(h, stable.OperationName)
	writeFieldForTest(h, stable.TargetName)
	writeFieldForTest(h, string(stable.TargetCategory))
	writeFieldForTest(h, stable.Environment)
	want := strconv.FormatUint(h.Sum64(), 16)

	got := fingerprint.Compute(stable)
	if got.ID != want {
		t.Errorf("Compute(...).ID = %q, want %q (hand-computed with version+6 fields)", got.ID, want)
	}
}
```

Since `writeField` and `fingerprintVersion` are unexported, this test must live in
package `fingerprint` (white-box), not `fingerprint_test` — check which the
existing test file uses. If the file is already `package fingerprint_test`
(black-box), add this one test to a new same-directory `package fingerprint` file
(e.g. `internal/fingerprint/version_internal_test.go`) instead of converting the
whole suite, and call `writeField`/reference `fingerprintVersion` directly (no
alias needed in that case — drop the `ForTest` suffix, use the real unexported
names).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/fingerprint/... -run TestComputeIncludesVersionInHash -v`
Expected: FAIL — `fingerprintVersion` undefined, or IDs don't match (version not
yet in the hash).

- [ ] **Step 3: Add the version marker and `TargetCategory` field to `Compute`**

In `internal/fingerprint/fingerprint.go`:

```go
// fingerprintVersion is written into the hash before every stable field.
// Bump it whenever the stable field set or hash algorithm changes (e.g.
// task 001's TargetCategory addition is what version "1" already
// includes) — this guarantees a composition change produces a disjoint ID
// space instead of silently reinterpreting old IDs under new semantics.
const fingerprintVersion = "1"

// Compute derives a Fingerprint from stable. It is a pure function of
// stable: identical input always produces an identical ID, and stable is
// never modified.
func Compute(stable features.StableFeatures) Fingerprint {
	h := fnv.New64a()
	writeField(h, fingerprintVersion)
	writeField(h, string(stable.ActorType))
	writeField(h, string(stable.OperationCategory))
	writeField(h, stable.OperationName)
	writeField(h, stable.TargetName)
	writeField(h, string(stable.TargetCategory))
	writeField(h, stable.Environment)

	return Fingerprint{
		ID:     strconv.FormatUint(h.Sum64(), 16),
		Stable: stable,
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/fingerprint/... -v`
Expected: PASS, including all pre-existing determinism/collision/volatile-
independence tests (their assertions are behavioral — "same input same ID",
"different input different ID" — not pinned to exact ID strings, so they pass
unmodified per the task file's own note).

- [ ] **Step 5: Re-run `BenchmarkCompute`, record before/after numbers**

Run: `go test ./internal/fingerprint/... -bench BenchmarkCompute -benchmem -run ^$`
Expected: negligible change (one more `writeField` call — roughly +1 allocation if
`writeField`'s `io.WriteString`/`Write` calls allocate per call, or +0 if not;
report the actual measured numbers, do not guess). Record Go version, OS/arch, CPU
model alongside the numbers exactly as `docs/PERFORMANCE.md`'s existing entries do.

- [ ] **Step 6: Write the fingerprint design-doc content in `docs/DOMAIN.md`**

Under `docs/DOMAIN.md § Fingerprint`, add: what feeds the hash (the six stable
fields including `TargetCategory`, in the order `Compute` writes them), why FNV-1a
(fast, non-cryptographic, sufficient for a content-addressed identity key, not a
security boundary), the NUL-separator field-boundary-collision protection
(`writeField`), and the new versioning behavior (the `fingerprintVersion` constant,
what it protects against, when to bump it).

- [ ] **Step 7: Update `docs/PERFORMANCE.md`**

Add the re-measured `BenchmarkCompute` numbers from Step 5 to the measured-results
table, replacing the old (unversioned) figures.

- [ ] **Step 8: `gofmt -l .`, `go vet ./...`, full package test**

Run: `gofmt -l . && go vet ./... && go test ./internal/fingerprint/... -v -race`
Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add internal/fingerprint/fingerprint.go internal/fingerprint/fingerprint_test.go \
  docs/DOMAIN.md docs/PERFORMANCE.md
git commit -m "Add fingerprint hash version marker (task 002)"
```

---

### Task 3: 004 — Anomaly v2: Frequency Deviation

**Files:**
- Modify: `internal/baseline/baseline.go` (interval-EWMA fields on
  `FingerprintStats`, updated in `observe`)
- Modify: `internal/anomaly/anomaly.go` (new `Config` fields, `frequencySignal`,
  wired into `Score`)
- Test: `internal/baseline/baseline_test.go` (interval EWMA convergence)
- Test: `internal/anomaly/anomaly_test.go` (normal/spike/cold-start frequency cases,
  extend the exact-formula test)
- Benchmark: re-run `BenchmarkScoreKnownFamiliar`/`BenchmarkScoreNovelWithAllSignals`
  (`internal/anomaly`), new/updated `BenchmarkObserve` numbers if needed
  (`internal/baseline`)

**Interfaces:**
- Consumes: `features.Features.Volatile.Timestamp` (already exists — this is the
  "now" the interval is measured against; `anomaly.Score` does not currently take a
  separate time parameter and must not gain one — use the event's own timestamp,
  consistent with how `Engine.Observe` already threads `result.Event.Timestamp`
  through to `store.Observe`).
- Produces: `baseline.FingerprintStats.IntervalObservations uint64`,
  `.IntervalMean float64` (ns), `.IntervalVariance float64` (ns²);
  `anomaly.Config.FrequencyZThreshold float64`, `.FrequencyWeight float64`; a new
  `"frequency_deviation"` entry in `Anomaly.Contributors` when it fires.

- [ ] **Step 1: Write the failing baseline interval-EWMA test**

In `internal/baseline/baseline_test.go`, add (matching the file's existing table/
helper conventions — check `TestFingerprintStatsLatencyConvergesToStableValue`
first and mirror its shape exactly):

```go
func TestFingerprintStatsIntervalConvergesToStableValue(t *testing.T) {
	stable := features.StableFeatures{OperationName: "op"}
	vol := features.VolatileFeatures{}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var stats baseline.FingerprintStats
	interval := 10 * time.Second
	now := start
	for range 50 {
		stats = stats.Observe(stable, vol, now) // adjust to actual exported/unexported
		// call shape used by the package — FingerprintStats.observe is unexported;
		// drive this through baseline.Baseline.Observe with a fixed fingerprint.ID
		// instead if `observe` itself isn't test-accessible from this package.
		now = now.Add(interval)
	}

	if stats.IntervalObservations == 0 {
		t.Fatal("IntervalObservations = 0 after 50 observations, want > 0")
	}
	got := time.Duration(stats.IntervalMean)
	if diff := got - interval; diff < -500*time.Millisecond || diff > 500*time.Millisecond {
		t.Errorf("IntervalMean = %s after convergence, want ~%s", got, interval)
	}
}
```

Note: `FingerprintStats.observe` is unexported and only reachable via
`Baseline.Observe`, which is where package `baseline_test` (black-box, if that's
the existing convention) must drive this from — via a fixed `fingerprint.Fingerprint{ID: "fp-1", Stable: stable}`
and repeated `bl = bl.Observe(fp, vol, now)` calls, then reading
`bl.Fingerprints["fp-1"]`. Adjust the sketch above to go through `Baseline.Observe`
if the test file is black-box; write it through the unexported path only if the
existing test file is white-box (`package baseline`). Check the file first — do not
guess.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/baseline/... -run TestFingerprintStatsIntervalConvergesToStableValue -v`
Expected: FAIL — `IntervalObservations`/`IntervalMean` undefined.

- [ ] **Step 3: Add interval-EWMA fields and update `observe`**

In `internal/baseline/baseline.go`, extend `FingerprintStats`:

```go
type FingerprintStats struct {
	Count uint64

	FirstObserved time.Time
	LastObserved  time.Time

	LatencyObservations uint64
	LatencyMean         float64
	LatencyVariance     float64

	// IntervalObservations is the number of times an inter-observation
	// interval has been recorded for this Fingerprint (Count-1 once
	// Count>0, since the first observation has no prior LastObserved to
	// measure from). IntervalMean/IntervalVariance are meaningless when
	// this is zero.
	IntervalObservations uint64
	IntervalMean         float64 // EWMA mean inter-observation interval, in nanoseconds
	IntervalVariance     float64 // EWMA variance, in nanoseconds^2

	ErrorRate float64

	Stable features.StableFeatures
}
```

Update `observe` — the interval must be computed from the *previous* `LastObserved`
before it's overwritten:

```go
func (s FingerprintStats) observe(stable features.StableFeatures, vol features.VolatileFeatures, now time.Time) FingerprintStats {
	if s.Count == 0 {
		s.FirstObserved = now
	} else {
		intervalNS := float64(now.Sub(s.LastObserved))
		if s.IntervalObservations == 0 {
			s.IntervalMean = intervalNS
			s.IntervalVariance = 0
		} else {
			delta := intervalNS - s.IntervalMean
			s.IntervalMean += emaAlpha * delta
			s.IntervalVariance = (1 - emaAlpha) * (s.IntervalVariance + emaAlpha*delta*delta)
		}
		s.IntervalObservations++
	}
	s.Count++
	s.LastObserved = now
	s.Stable = stable

	// ... rest of the existing method body (latency, error rate) unchanged
```

Keep the rest of the method (latency EWMA, error rate) exactly as-is below this
insertion — do not reorder those blocks.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/baseline/... -v -race`
Expected: PASS, including every pre-existing `internal/baseline` test.

- [ ] **Step 5: Write the failing anomaly frequency-signal tests**

In `internal/anomaly/anomaly_test.go`, mirror the existing latency table exactly
(check `TestScoreLatencyDeviation`-style test names first). Add three cases:

```go
func TestScoreFrequencyDeviation(t *testing.T) {
	cfg := anomaly.DefaultConfig()
	stable := features.StableFeatures{OperationName: "op"}
	fp := fingerprint.Compute(stable)

	t.Run("normal rate does not fire", func(t *testing.T) {
		bl := baselineWithStableInterval(t, fp, stable, 10*time.Second, 50) // test helper: builds a Baseline whose FingerprintStats has IntervalMean=10s, IntervalVariance~0, Count=50
		feat := features.Features{Stable: stable, Volatile: features.VolatileFeatures{Timestamp: bl.LastObserved.Add(10 * time.Second)}}
		an := anomaly.Score(feat, fp, bl, cfg)
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				t.Errorf("frequency_deviation fired on a normal-rate event: %+v", s)
			}
		}
	})

	t.Run("spike fires strongly", func(t *testing.T) {
		bl := baselineWithStableInterval(t, fp, stable, 10*time.Second, 50)
		feat := features.Features{Stable: stable, Volatile: features.VolatileFeatures{Timestamp: bl.LastObserved.Add(100 * time.Millisecond)}}
		an := anomaly.Score(feat, fp, bl, cfg)
		found := false
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				found = true
				if s.Value < 0.9 {
					t.Errorf("frequency_deviation.Value = %v, want near 1 for a 100x rate spike", s.Value)
				}
			}
		}
		if !found {
			t.Error("frequency_deviation did not fire on a 100x rate spike")
		}
	})

	t.Run("cold start does not fire", func(t *testing.T) {
		bl := baseline.New(baseline.Key{})
		feat := features.Features{Stable: stable, Volatile: features.VolatileFeatures{Timestamp: time.Now()}}
		an := anomaly.Score(feat, fp, bl, cfg)
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				t.Errorf("frequency_deviation fired on an unknown fingerprint: %+v", s)
			}
		}
	})
}
```

Write `baselineWithStableInterval` as a small package-local test helper in
`anomaly_test.go` that drives `baseline.Baseline.Observe` in a loop with a fixed
interval, exactly as Task 3 Step 1's helper does — do not duplicate logic, factor
it once and reuse if both test files need it (they're different packages, so a
literal helper function must exist independently in each, but keep both minimal
and identical in shape).

Also extend the existing `TestScoreMatchesDocumentedNoisyOrFormula`-style test with
one new table entry that includes a firing `frequency_deviation` signal and asserts
the exact `combine()` arithmetic with it included (per `.claude/rules/testing.md`'s
"test the documented formula, not just direction" bar).

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./internal/anomaly/... -run TestScoreFrequencyDeviation -v`
Expected: FAIL — `Config.FrequencyZThreshold`/`FrequencyWeight` undefined, or no
`frequency_deviation` signal ever appears.

- [ ] **Step 7: Add `Config` fields and `frequencySignal`**

In `internal/anomaly/anomaly.go`, extend `Config`:

```go
type Config struct {
	MinObservations uint64

	LatencyZThreshold float64
	// FrequencyZThreshold is the |z-score| at which the frequency-deviation
	// signal (deviation of the current inter-observation interval from the
	// baseline's typical interval) reaches its full strength (1.0). Must be > 0.
	FrequencyZThreshold float64

	NoveltyWeight   float64
	LatencyWeight   float64
	ErrorWeight     float64
	FrequencyWeight float64

	SensitiveTargetFloor map[string]float64
}
```

Extend `DefaultConfig`:

```go
func DefaultConfig() Config {
	return Config{
		MinObservations:      20,
		LatencyZThreshold:    3.0,
		FrequencyZThreshold:  3.0,
		NoveltyWeight:        1.0,
		LatencyWeight:        0.6,
		ErrorWeight:          0.8,
		FrequencyWeight:      0.6,
		SensitiveTargetFloor: map[string]float64{},
	}
}
```

Add `frequencySignal`, mirroring `latencySignal` exactly (same nearZeroStdDev
handling — a baseline with an essentially-constant interval must not divide by
~zero):

```go
// frequencySignal only formats its Detail string once it knows the signal
// actually contributes (Value > 0) — mirrors latencySignal's cost discipline.
func frequencySignal(currentInterval time.Duration, stats baseline.FingerprintStats, cfg Config) Signal {
	stddev := math.Sqrt(stats.IntervalVariance)
	mean := stats.IntervalMean
	currentNS := float64(currentInterval)
	nearZeroStdDev := stddev < float64(time.Millisecond)

	var value, z float64
	if nearZeroStdDev {
		if currentNS != mean {
			value = 1
		}
	} else {
		z = (currentNS - mean) / stddev
		if z < 0 {
			z = -z
		}
		value = min(z/cfg.FrequencyZThreshold, 1)
	}

	if value == 0 {
		return Signal{Name: "frequency_deviation", Weight: cfg.FrequencyWeight}
	}

	detail := fmt.Sprintf("inter-event interval z-score %.2f (mean %s, stddev %s)", z, time.Duration(mean), time.Duration(stddev))
	if nearZeroStdDev {
		detail = fmt.Sprintf("interval %s deviates from a stable baseline of %s (stddev ~0)", currentInterval, time.Duration(mean))
	}
	return Signal{Name: "frequency_deviation", Value: value, Weight: cfg.FrequencyWeight, Detail: detail}
}
```

Add `"math"` to the import block. Wire it into `Score`, guarded exactly like
`latencySignal`'s cold-start guard (`known && stats.IntervalObservations > 0`):

```go
if known && stats.IntervalObservations > 0 {
	interval := feat.Volatile.Timestamp.Sub(stats.LastObserved)
	if s := frequencySignal(interval, stats, cfg); s.Value > 0 {
		signals = append(signals, s)
	}
}
```

Insert this block after the existing latency-signal block in `Score`, before the
error-signal block (order in `Contributors` should follow the order named in the
roadmap's example: novelty, latency, error, frequency is fine either way since
`combine` doesn't care about order — but keep it adjacent to the latency block
since both compare against a baseline stat via z-score, for readability).

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/anomaly/... -v -race`
Expected: PASS, including all pre-existing anomaly tests and the updated
exact-formula test.

- [ ] **Step 9: Re-run benchmarks**

Run: `go test ./internal/anomaly/... -bench . -benchmem -run ^$`
Run: `go test ./internal/baseline/... -bench BenchmarkObserve -benchmem -run ^$`

Expected: `BenchmarkScoreKnownFamiliar` (the "nothing fires" common case) stays
zero-allocation — verify explicitly; if the new `known && IntervalObservations>0`
branch itself doesn't allocate when `frequencySignal` returns a zero-Value signal
(it shouldn't — same pattern as `latencySignal`), this should already hold. Record
whatever `BenchmarkObserve`'s numbers show (a few more float64 field writes per
call — expect no allocation change, but measure).

- [ ] **Step 10: Update docs**

`docs/DOMAIN.md § Anomaly`: add `frequency_deviation` to the signal table (name,
what it measures, cold-start behavior, weight/threshold config fields).
`docs/PERFORMANCE.md`: updated `anomaly.Score`/`baseline.Observe` numbers from
Step 9.

- [ ] **Step 11: `gofmt -l .`, `go vet ./...`**

Run: `gofmt -l . && go vet ./...`

- [ ] **Step 12: Commit**

```bash
git add internal/baseline/baseline.go internal/baseline/baseline_test.go \
  internal/anomaly/anomaly.go internal/anomaly/anomaly_test.go \
  docs/DOMAIN.md docs/PERFORMANCE.md
git commit -m "Add frequency_deviation anomaly signal (task 004)"
```

---

### Task 4: 005 — Trust & Risk Calibration

**Depends on:** Task 3 (004) landed, so the new frequency signal is included in the
calibration matrix's mental model (the matrix itself sweeps `Trust`'s inputs
directly, not `Anomaly`'s, so no code dependency — just do this after 004 per the
stated order).

**Files:**
- Modify: `internal/trust/trust.go` (add `Explain()`)
- Test: `internal/trust/trust_test.go` (scenario matrix + `Explain()` tests)
- Documentation: `docs/DOMAIN.md § Trust and Risk`, `docs/sdk-guide.md`

**Interfaces:**
- Consumes: `Trust{Score, Risk, IdentityConfidence, AnomalyScore, AnomalyConfidence, ContextRisk}`
  (all fields already exist).
- Produces: `func (t Trust) Explain() string` — reused by Task 6 (007)'s
  `Result.Explain()`.

- [ ] **Step 1: Write the failing `Explain()` test**

In `internal/trust/trust_test.go`:

```go
func TestExplain(t *testing.T) {
	tr := trust.Trust{
		Score:              0.35,
		Risk:               trust.RiskHigh,
		IdentityConfidence: 0.97,
		AnomalyScore:       0.91,
		AnomalyConfidence:  1.0,
		ContextRisk:        0.10,
	}
	got := tr.Explain()
	want := "trust 0.35 (high): identity confidence 0.97, anomaly 0.91 at full confidence, context risk 0.10"
	if got != want {
		t.Errorf("Explain() = %q, want %q", got, want)
	}
}

func TestExplainPartialAnomalyConfidence(t *testing.T) {
	tr := trust.Trust{Score: 0.9, Risk: trust.RiskLow, IdentityConfidence: 1, AnomalyScore: 0.8, AnomalyConfidence: 0.4, ContextRisk: 0}
	got := tr.Explain()
	if !strings.Contains(got, "40% confidence") {
		t.Errorf("Explain() = %q, want it to mention partial confidence as a percentage", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/trust/... -run TestExplain -v`
Expected: FAIL — `Explain` undefined.

- [ ] **Step 3: Implement `Explain()`**

In `internal/trust/trust.go`:

```go
// Explain renders t as a short, human-readable sentence summarizing its
// score, risk bucket, and the components that produced it. It is pure
// formatting over t's existing fields — no new computation.
func (t Trust) Explain() string {
	confidence := "full confidence"
	if t.AnomalyConfidence < 1 {
		confidence = fmt.Sprintf("%.0f%% confidence", t.AnomalyConfidence*100)
	}
	return fmt.Sprintf(
		"trust %.2f (%s): identity confidence %.2f, anomaly %.2f at %s, context risk %.2f",
		t.Score, t.Risk, t.IdentityConfidence, t.AnomalyScore, confidence, t.ContextRisk,
	)
}
```

Add `"fmt"` to the import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/trust/... -v`
Expected: PASS.

- [ ] **Step 5: Write the failing scenario-matrix test**

In `internal/trust/trust_test.go`:

```go
func TestComputeScenarioMatrixBoundsAndMonotonicity(t *testing.T) {
	levels := []float64{0, 0.25, 0.5, 0.75, 1}
	cfg := trust.DefaultConfig()

	for _, ident := range levels {
		for _, anomScore := range levels {
			for _, anomConf := range levels {
				for _, ctxRisk := range levels {
					an := anomaly.Anomaly{Score: anomScore, Confidence: anomConf}
					got := trust.Compute(an, ident, ctxRisk, cfg)
					if got.Score < 0 || got.Score > 1 {
						t.Fatalf("Score out of [0,1]: %v for ident=%v anomScore=%v anomConf=%v ctxRisk=%v", got.Score, ident, anomScore, anomConf, ctxRisk)
					}
				}
			}
		}
	}

	// Monotonicity: increasing any risk input never increases TrustScore.
	for i := 0; i < len(levels)-1; i++ {
		lo, hi := levels[i], levels[i+1]
		base := trust.Compute(anomaly.Anomaly{Score: lo, Confidence: 1}, 1, 0, cfg)
		bumped := trust.Compute(anomaly.Anomaly{Score: hi, Confidence: 1}, 1, 0, cfg)
		if bumped.Score > base.Score {
			t.Errorf("increasing Anomaly.Score from %v to %v increased TrustScore: %v -> %v", lo, hi, base.Score, bumped.Score)
		}

		baseCtx := trust.Compute(anomaly.Anomaly{Score: 0.5, Confidence: 1}, 1, lo, cfg)
		bumpedCtx := trust.Compute(anomaly.Anomaly{Score: 0.5, Confidence: 1}, 1, hi, cfg)
		if bumpedCtx.Score > baseCtx.Score {
			t.Errorf("increasing ContextRisk from %v to %v increased TrustScore: %v -> %v", lo, hi, baseCtx.Score, bumpedCtx.Score)
		}

		baseIdent := trust.Compute(anomaly.Anomaly{Score: 0.5, Confidence: 1}, lo, 0, cfg)
		bumpedIdent := trust.Compute(anomaly.Anomaly{Score: 0.5, Confidence: 1}, hi, 0, cfg)
		if bumpedIdent.Score < baseIdent.Score {
			t.Errorf("increasing IdentityConfidence from %v to %v decreased TrustScore: %v -> %v", lo, hi, baseIdent.Score, bumpedIdent.Score)
		}
	}
}
```

Add `"github.com/Trustvian/trustvian/internal/anomaly"` to the test file's imports
if not already present.

- [ ] **Step 6: Run test to verify it fails or passes**

Run: `go test ./internal/trust/... -run TestComputeScenarioMatrixBoundsAndMonotonicity -v`
Expected: this should already PASS against the existing `Compute` formula (the task
is validating existing behavior, not changing `Compute`) — if it fails, that is a
real correctness bug in `Compute` to investigate and fix (report findings before
patching; per Non-Goals, this task must not change `Compute`'s formula/signature —
if the formula itself is broken, stop and flag it to the user rather than silently
"fixing" a documented, previously-tested formula mid-task).

- [ ] **Step 7: `gofmt -l .`, `go vet ./...`, full package test**

Run: `gofmt -l . && go vet ./... && go test ./internal/trust/... -v -race`

- [ ] **Step 8: Update docs**

`docs/DOMAIN.md § Trust and Risk`: note the now-explicitly-tested monotonicity/
bounds guarantees. `docs/sdk-guide.md`: mention `Trust.Explain()` in the `Result`
walkthrough section.

- [ ] **Step 9: Commit**

```bash
git add internal/trust/trust.go internal/trust/trust_test.go docs/DOMAIN.md docs/sdk-guide.md
git commit -m "Add Trust.Explain() and a scenario-matrix bounds/monotonicity test (task 005)"
```

---

### Task 5: 006 — Policy Engine Hardening (Attribute Matching)

**Files:**
- Modify: `internal/policy/policy.go` (`Condition.Attributes`, `Input.Attributes`,
  `Matches`)
- Modify: `engine.go` (`Engine.Analyze` — pass `ev.Attributes` into `policy.Input`)
- Test: `internal/policy/policy_test.go`
- Test: `engine_test.go` (end-to-end rule using the new matcher, if not already
  covered by the policy-package test alone — check whether `engine_test.go` builds
  custom `Policy` values already; if so add one case there too per this repo's
  "end-to-end tests are load-bearing" convention in
  `.claude/rules/testing.md`)
- Documentation: `docs/policy-guide.md`, `docs/DOMAIN.md § Policy and Decision`

**Interfaces:**
- Consumes: `event.Event.Attributes map[string]any` (already exists).
- Produces: `policy.Condition.Attributes map[string]string` (new field),
  `policy.Input.Attributes map[string]any` (new field, populated by
  `Engine.Analyze` from `ev.Attributes`).

- [ ] **Step 1: Write the failing attribute-matcher tests**

In `internal/policy/policy_test.go`, extend the existing `TestConditionMatchesPerField`
table (read it first to match its exact struct shape) with attribute cases:

```go
func TestConditionMatchesAttributes(t *testing.T) {
	tests := []struct {
		name  string
		cond  policy.Condition
		attrs map[string]any
		want  bool
	}{
		{
			name:  "matching attribute",
			cond:  policy.Condition{Attributes: map[string]string{"tool.category": "secrets"}},
			attrs: map[string]any{"tool.category": "secrets"},
			want:  true,
		},
		{
			name:  "mismatched value",
			cond:  policy.Condition{Attributes: map[string]string{"tool.category": "secrets"}},
			attrs: map[string]any{"tool.category": "files"},
			want:  false,
		},
		{
			name:  "absent key",
			cond:  policy.Condition{Attributes: map[string]string{"tool.category": "secrets"}},
			attrs: map[string]any{},
			want:  false,
		},
		{
			name:  "nil matcher matches everything",
			cond:  policy.Condition{},
			attrs: map[string]any{"anything": "goes"},
			want:  true,
		},
		{
			name:  "bool attribute value stringified",
			cond:  policy.Condition{Attributes: map[string]string{"admin": "true"}},
			attrs: map[string]any{"admin": true},
			want:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := policy.Input{Attributes: tt.attrs}
			if got := tt.cond.Matches(in); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateToolCategorySecretsExample(t *testing.T) {
	p := policy.Policy{
		Rules: []policy.Rule{
			{
				Name: "block-ai-agent-secrets-access",
				When: policy.Condition{
					ActorType:  event.ActorTypeAIAgent,
					Attributes: map[string]string{"tool.category": "secrets"},
				},
				Action: policy.DecisionBlock,
				Reason: "AI agents may not access secrets-category tools",
			},
		},
		DefaultAction: policy.DecisionObserveOnly,
		DefaultReason: "no matching rule",
	}
	in := policy.Input{
		Stable:     features.StableFeatures{ActorType: event.ActorTypeAIAgent},
		Attributes: map[string]any{"tool.category": "secrets"},
	}
	result := p.Evaluate(in)
	if result.Decision != policy.DecisionBlock {
		t.Errorf("Decision = %v, want %v", result.Decision, policy.DecisionBlock)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/policy/... -run 'TestConditionMatchesAttributes|TestEvaluateToolCategorySecretsExample' -v`
Expected: FAIL — `Condition.Attributes`/`Input.Attributes` undefined.

- [ ] **Step 3: Add `Attributes` to `Condition` and `Input`, extend `Matches`**

In `internal/policy/policy.go`:

```go
type Input struct {
	Stable     features.StableFeatures
	Trust      trust.Trust
	Attributes map[string]any
}
```

```go
type Condition struct {
	ActorType         event.ActorType
	OperationCategory event.OperationCategory
	TargetName        string
	Environment       string
	MinRiskLevel      trust.RiskLevel
	// Attributes, if non-empty, requires every key to be present in
	// Input.Attributes with a value whose string representation (via
	// fmt.Sprint) equals the configured value. A nil or empty map means
	// "don't care", consistent with every other field.
	Attributes map[string]string
}
```

```go
func (c Condition) Matches(in Input) bool {
	if c.ActorType != "" && c.ActorType != in.Stable.ActorType {
		return false
	}
	if c.OperationCategory != "" && c.OperationCategory != in.Stable.OperationCategory {
		return false
	}
	if c.TargetName != "" && c.TargetName != in.Stable.TargetName {
		return false
	}
	if c.Environment != "" && c.Environment != in.Stable.Environment {
		return false
	}
	if c.MinRiskLevel != "" && !in.Trust.Risk.AtLeast(c.MinRiskLevel) {
		return false
	}
	for key, want := range c.Attributes {
		got, ok := in.Attributes[key]
		if !ok || fmt.Sprint(got) != want {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/policy/... -v`
Expected: PASS, including every pre-existing `internal/policy` test (rule
ordering, `Unless`, all three fail-closed scenarios) unmodified.

- [ ] **Step 5: Wire `Engine.Analyze` to populate `Input.Attributes`**

In `engine.go`, change the `policy.Evaluate` call:

```go
pr := e.policy.Evaluate(policy.Input{Stable: feat.Stable, Trust: tr, Attributes: ev.Attributes})
```

- [ ] **Step 6: Add/extend an end-to-end engine test**

In `engine_test.go`, add a case constructing an `Engine` `WithPolicy` using the
same `tool.category: secrets` rule from Step 1, calling `Analyze` with an
`event.Event` whose `Attributes` include `"tool.category": "secrets"`, and
asserting `Result.Decision == policy.DecisionBlock`. Check `engine_test.go`'s
existing `WithPolicy`-using tests first and match their construction style exactly.

- [ ] **Step 7: Run full test suite for this task**

Run: `go test ./internal/policy/... . -v -race`
Expected: PASS (root package `.` covers `engine_test.go`).

- [ ] **Step 8: Re-run policy benchmarks**

Run: `go test ./internal/policy/... -bench 'BenchmarkEvaluateMatch|BenchmarkEvaluateDefault' -benchmem -run ^$`
Expected: a `Condition` with no `Attributes` set stays zero-allocation (the `for
key, want := range c.Attributes` loop over a nil map is a zero-iteration no-op —
verify this holds, don't assume) — numbers should match the existing 21–37 ns/op,
0 allocs/op baseline from `docs/PERFORMANCE.md`.

- [ ] **Step 9: Update docs**

`docs/policy-guide.md`: update the `Condition` field reference table; replace the
"attribute-based conditions need a future policy loader" caveat with the real
usage example from Step 1's `TestEvaluateToolCategorySecretsExample`.
`docs/DOMAIN.md § Policy and Decision`: note `Input`'s new `Attributes` field.

- [ ] **Step 10: `gofmt -l .`, `go vet ./...`**

Run: `gofmt -l . && go vet ./...`

- [ ] **Step 11: Commit**

```bash
git add internal/policy/policy.go internal/policy/policy_test.go engine.go engine_test.go \
  docs/policy-guide.md docs/DOMAIN.md
git commit -m "Add attribute matching to policy Condition (task 006)"
```

---

### Task 6: 007 — Decision & Explainability (`Result.Explain()`)

**Depends on:** Task 4 (005)'s `Trust.Explain()` landed — reuse it.

**Files:**
- Modify: `result.go` (add `Explain()`)
- Test: new `result_test.go` (or extend an existing root-package test file — check
  what exists first)
- Documentation: `docs/DOMAIN.md`, `docs/sdk-guide.md § Result`

**Interfaces:**
- Consumes: `Result{Decision, Trust, Anomaly, Explanation}` (all exist),
  `trust.Trust.Explain()` (Task 4).
- Produces: `func (r Result) Explain() string`.

- [ ] **Step 1: Write the failing checklist test**

Create/extend `result_test.go`:

```go
func TestResultExplainContainsAllDecisionFields(t *testing.T) {
	ctx := context.Background()
	e := NewEngine(WithAnomalyConfig(anomaly.Config{
		MinObservations: 1, NoveltyWeight: 1, LatencyWeight: 0.6, ErrorWeight: 0.8, FrequencyWeight: 0.6,
		LatencyZThreshold: 3, FrequencyZThreshold: 3, SensitiveTargetFloor: map[string]float64{},
	}))
	ev := event.Event{
		ID: "e1", Timestamp: time.Now(),
		Actor:     event.Actor{ID: "a1", Type: event.ActorTypeService, IdentityConfidence: 1},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /x"},
		Target:    event.Target{Name: "svc"},
	}
	result, err := e.Analyze(ctx, ev)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	got := result.Explain()

	for _, want := range []string{
		string(result.Decision),
		fmt.Sprintf("%.2f", result.Trust.Score),
		fmt.Sprintf("%.2f", result.Trust.Risk == result.Trust.Risk && true), // placeholder removed below
	} {
		_ = want
	}
	if !strings.Contains(got, string(result.Decision)) {
		t.Errorf("Explain() missing Decision %q:\n%s", result.Decision, got)
	}
	if !strings.Contains(got, result.Explanation.Reason) {
		t.Errorf("Explain() missing policy reason %q:\n%s", result.Explanation.Reason, got)
	}
	// A brand-new fingerprint fires categorical_novelty — assert at least
	// one contributing signal's name appears.
	if len(result.Anomaly.Contributors) == 0 {
		t.Fatal("test setup: expected at least one anomaly contributor on a cold-start event")
	}
	if !strings.Contains(got, result.Anomaly.Contributors[0].Name) {
		t.Errorf("Explain() missing contributor name %q:\n%s", result.Anomaly.Contributors[0].Name, got)
	}
}

func TestResultExplainNoContributorsDoesNotPanic(t *testing.T) {
	r := Result{
		Decision:    policy.DecisionAllow,
		Explanation: policy.Explanation{Reason: "default allow"},
		Trust:       trust.Trust{Score: 1, Risk: trust.RiskLow, IdentityConfidence: 1},
		Anomaly:     anomaly.Anomaly{Contributors: nil},
	}
	got := r.Explain() // must not panic on an empty Contributors slice
	if strings.Contains(got, "Detected:") && len(r.Anomaly.Contributors) == 0 {
		t.Error("Explain() rendered an empty Detected section for zero contributors")
	}
}
```

(Delete the placeholder no-op loop above the first assertion — it was left in as a
reminder that `%.2f`-formatted scores must match `Explain()`'s own formatting
exactly; write the real assertions using whatever format Step 3 actually produces,
checked after Step 3 is implemented, not guessed in advance.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestResultExplain -v`
Expected: FAIL — `Result.Explain` undefined.

- [ ] **Step 3: Implement `Result.Explain()`**

In `result.go`:

```go
// Explain renders r as a multi-line, human-readable summary of the full
// decision: what was decided, the trust/risk/anomaly scores, which
// signals contributed, and which policy rule (or default) produced it.
// It is pure formatting over r's existing fields — no new computation.
func (r Result) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Decision: %s\n", r.Decision)
	fmt.Fprintf(&b, "%s\n", r.Trust.Explain())
	fmt.Fprintf(&b, "Anomaly score: %.2f (confidence %.2f)\n", r.Anomaly.Score, r.Anomaly.Confidence)
	if len(r.Anomaly.Contributors) > 0 {
		b.WriteString("Detected:\n")
		for _, c := range r.Anomaly.Contributors {
			fmt.Fprintf(&b, "  - %s: %.2f", c.Name, c.Value)
			if c.Detail != "" {
				fmt.Fprintf(&b, " (%s)", c.Detail)
			}
			b.WriteString("\n")
		}
	}
	if r.Explanation.MatchedDefault {
		fmt.Fprintf(&b, "Policy: default action (%s)\n", r.Explanation.Reason)
	} else {
		fmt.Fprintf(&b, "Policy: rule %q (%s)\n", r.Explanation.RuleName, r.Explanation.Reason)
	}
	return b.String()
}
```

Add `"fmt"` and `"strings"` to `result.go`'s import block.

- [ ] **Step 4: Fix up the test's placeholder assertions and run**

Replace the placeholder block from Step 1 with real assertions matching the actual
`Explain()` output shape from Step 3 (e.g. `fmt.Sprintf("%.2f", result.Trust.Score)`
should indeed appear via `Trust.Explain()`'s own `%.2f` formatting).

Run: `go test . -v`
Expected: PASS.

- [ ] **Step 5: `gofmt -l .`, `go vet ./...`**

Run: `gofmt -l . && go vet ./...`

- [ ] **Step 6: Update docs**

`docs/DOMAIN.md`: one-line update to the closing paragraph pointing at `Explain()`.
`docs/sdk-guide.md § Result`: add `Explain()` to the field/method reference.

- [ ] **Step 7: Commit**

```bash
git add result.go result_test.go docs/DOMAIN.md docs/sdk-guide.md
git commit -m "Add Result.Explain() convenience method (task 007)"
```

---

### Task 7: 010 — Real-World Examples

**Depends on:** Task 3 (004)'s `frequency_deviation` signal, for Example 5.

**Files:**
- Create: `examples/basic/main.go`, `examples/basic/README.md`
- Create: `examples/credential-misuse/main.go`, `.../README.md`
- Create: `examples/unexpected-dependency/main.go`, `.../README.md`
- Create: `examples/external-destination/main.go`, `.../README.md`
- Create: `examples/frequency-abuse/main.go`, `.../README.md`
- Create: `examples/ai-agent/main.go`, `.../README.md`
- Create: `examples/README.md`
- Create: `examples/go.mod` (a genuinely separate module using `go mod replace` back
  to this repo during development — mirror `docs/sdk-guide.md § watching trust
  mature`'s exact pattern; read that doc section first before writing any example
  code, since it already shows verified, working external-consumer code against
  this SDK)
- Modify: `README.md` (root) — add an `examples/` pointer
- Modify: `docs/ROADMAP.md` — mark examples implemented
- Test/Makefile: a way to run all six and check exit codes (see Step 6)

**Interfaces:**
- Consumes: the full public API (`trustvian.NewEngine`, `.Analyze`, `.Observe`,
  `event.Event` and nested types) exactly as an external module sees it — no
  `internal/` imports anywhere under `examples/`.

- [ ] **Step 1: Read the reference material before writing any example**

Read `docs/sdk-guide.md` in full (particularly the "watching trust mature" section)
and `docs/use-cases.md` in full (the five already-verified scenarios this task
reuses almost verbatim: valid identity/abnormal behavior, API behavioral anomaly,
service-to-service security, AI-agent security). Do not invent new scenario details
— copy the exact event shapes/sequences those docs already show as verified
input/output pairs.

- [ ] **Step 2: Set up `examples/go.mod` as a real external module**

```bash
cd examples
go mod init trustvian-examples
go mod edit -replace github.com/Trustvian/trustvian=../
go get github.com/Trustvian/trustvian@v0.0.0
go mod tidy
```

(adjust the exact `go get` version pin as needed if `go mod tidy` complains — the
`replace` directive is what actually matters; the version string is a formality).

- [ ] **Step 3: Write `examples/basic/main.go`**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
)

func main() {
	engine := trustvian.NewEngine()

	ev := event.Event{
		ID:        "evt-001",
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "svc-checkout",
			Type:                event.ActorTypeService,
			IdentityConfidence: 1.0,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryHTTP,
			Name:     "GET /api/orders",
		},
		Target: event.Target{Name: "orders-api"},
	}

	result, err := engine.Analyze(context.Background(), ev)
	if err != nil {
		log.Fatalf("analyze: %v", err)
	}

	fmt.Println(result.Explain())

	if _, err := engine.Observe(context.Background(), result); err != nil {
		log.Fatalf("observe: %v", err)
	}
}
```

- [ ] **Step 4: Run it, capture real output**

```bash
cd examples/basic && go run .
```
Paste the actual captured stdout into `examples/basic/README.md` (a short
paragraph explaining the scenario, plus the verbatim output in a code fence) —
per the task's explicit "real, run output, not hand-written" requirement.

- [ ] **Step 5: Write the remaining five examples**

For each of `credential-misuse`, `unexpected-dependency`, `external-destination`,
`ai-agent`: port the exact event sequence from the corresponding `docs/use-cases.md`
section (read the matching subsection referenced in the task file, e.g. "§ valid
identity, abnormal behavior") into a `package main` following Step 3's structure —
multiple `Analyze`+`Observe` calls in a loop to build up baseline maturity where the
use-case shows that, then the anomalous final call. Each gets its own `go run`,
captured real output, and a `README.md`.

For `frequency-abuse` (the new scenario, no existing use-case doc to port): loop
`Analyze`+`Observe` ~20+ times with a consistent ~10-second-equivalent interval
(use `event.Event.Timestamp` values you construct explicitly, not real wall-clock
sleeps, so the example runs instantly) to mature the baseline's `IntervalMean`, then
send one final event with a `Timestamp` far closer to the previous one (e.g. 50ms
later instead of 10s) and confirm `frequency_deviation` appears in
`result.Anomaly.Contributors` — print it explicitly via `result.Explain()`.

- [ ] **Step 6: Add a way to run and verify all six examples**

Add a `Makefile` target (check if a root `Makefile` already exists — task file 010
mentions "decide during implementation" between a Go test and a Makefile target;
given this repo already has a `Makefile` per recent commit history, prefer adding
an `examples` target there):

```makefile
.PHONY: examples
examples:
	@for d in examples/*/; do \
		if [ -f "$$d/main.go" ]; then \
			echo "==> $$d"; \
			(cd "$$d" && go run .) || exit 1; \
		fi; \
	done
```

Run: `make examples`
Expected: all six run to completion with exit code 0.

- [ ] **Step 7: Write `examples/README.md`**

An index mirroring `docs/README.md`'s style: one line per example, what it
demonstrates, a link to its own `README.md`.

- [ ] **Step 8: Update root `README.md` and `docs/ROADMAP.md`**

Add an `examples/` pointer to the root `README.md`. In `docs/ROADMAP.md`, mark the
010 examples line item implemented (do not restructure the whole "Current status"
section yet — that's Task 10 (013)'s job at the very end).

- [ ] **Step 9: `gofmt -l ./examples/...`, `go vet ./examples/...` (from within `examples/`)**

```bash
cd examples && gofmt -l . && go vet ./...
```

- [ ] **Step 10: Commit**

```bash
git add examples/ README.md docs/ROADMAP.md Makefile
git commit -m "Add runnable examples/ directory (task 010)"
```

---

### Task 8: 011 — Performance Baseline Completion

**Files:**
- Test/Benchmark: `internal/otel/otel_test.go` (add `BenchmarkEventFromSpan`)
- Test/Benchmark: new `internal/store/store_bench_test.go` addition or new file
  (add `BenchmarkInMemoryMemoryGrowth`) — check `internal/store/store_bench_test.go`
  first; it likely already exists per the file listing (`store_bench_test.go`,
  `file_bench_test.go` both already exist) — add to the existing file rather than
  creating a new one.
- Documentation: `docs/PERFORMANCE.md`

**Interfaces:**
- Consumes: `internal/otel.EventFromSpan` (existing, read above), `store.InMemory`
  (existing, read above — `Get`/`Observe`).

- [ ] **Step 1: Read `internal/otel/otel_test.go`'s existing span-construction helper**

It already builds real spans via a `TracerProvider` + capturing `SpanExporter`
(per `.claude/rules/testing.md`'s "things that cannot be faked" note) — reuse that
exact helper for the benchmark rather than writing a second one.

- [ ] **Step 2: Write `BenchmarkEventFromSpan`**

In `internal/otel/otel_test.go` (or a new `otel_bench_test.go` in the same
package, matching whichever convention the existing benchmarks in this repo use —
check `internal/anomaly`/`internal/fingerprint` for the pattern):

```go
func BenchmarkEventFromSpan(b *testing.B) {
	span := buildTestSpan(b) // reuse the existing test helper that constructs a
	// representative ReadOnlySpan via a real TracerProvider; adjust the name to
	// whatever otel_test.go's actual helper is called.
	b.ReportAllocs()
	for b.Loop() {
		_ = EventFromSpan(span)
	}
}
```

- [ ] **Step 3: Run it, record numbers**

Run: `go test ./internal/otel/... -bench BenchmarkEventFromSpan -benchmem -run ^$`
Record ns/op and allocs/op with Go version, OS/arch, CPU model.

- [ ] **Step 4: Write `BenchmarkInMemoryMemoryGrowth`**

In `internal/store/store_bench_test.go`:

```go
func BenchmarkInMemoryMemoryGrowth(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			ctx := context.Background()
			s := store.NewInMemory()
			b.ReportAllocs()
			for i := range b.N {
				key := baseline.Key{ActorID: fmt.Sprintf("actor-%d", i%n), Environment: "prod"}
				fp := fingerprint.Fingerprint{ID: fmt.Sprintf("fp-%d", i%n)}
				_, _ = s.Observe(ctx, key, fp, features.VolatileFeatures{}, time.Now())
			}
		})
	}
}
```

Adjust imports/exact helper names to whatever `store_bench_test.go` already
imports — check the file first, do not assume `context`/`fmt`/`time` aren't
already aliased differently.

- [ ] **Step 5: Run it, record numbers, note the growth shape**

Run: `go test ./internal/store/... -bench BenchmarkInMemoryMemoryGrowth -benchmem -run ^$`
Record `B/op`/allocs at each of the three key counts. If growth is clearly
non-linear or otherwise concerning, write that down as a fact with numbers in
`docs/PERFORMANCE.md` (per the task's Non-Goals: measuring only, not fixing, in
this task).

- [ ] **Step 6: Re-run the full benchmark suite now that 004/006 have landed**

Run: `go test ./... -bench . -benchmem -run ^$`
Update every affected number in `docs/PERFORMANCE.md`'s table (not just the two new
ones) — `anomaly.Score`, `internal/baseline.Observe`, `internal/policy.Evaluate`
all changed in Tasks 3/5.

- [ ] **Step 7: Update `docs/PERFORMANCE.md`**

Add both new benchmark results with the same environment-disclosure rigor as every
existing entry. Remove both items from the "what's not benchmarked yet" section;
add whatever the next real gap turns out to be, if any (likely none, per the task's
framing that this closes the only two known gaps).

- [ ] **Step 8: `gofmt -l .`, `go vet ./...`**

Run: `gofmt -l . && go vet ./...`

- [ ] **Step 9: Commit**

```bash
git add internal/otel/ internal/store/ docs/PERFORMANCE.md
git commit -m "Add OTel-adapter and store memory-growth benchmarks (task 011)"
```

---

### Task 9: 012 — Dedicated Security Test Suite

**Files:**
- Modify: `event/event.go` (reject `NaN`/`±Inf` `IdentityConfidence` in `Validate`
  — a real validation gap, not new runtime logic per the task's Non-Goals allowance)
- Test: `event/event_test.go` (malformed/extreme input cases)
- Test: `engine_test.go` (cross-actor isolation, resource-exhaustion smoke tests)
- Documentation: `docs/SECURITY.md` (two new threat entries + index table)

**Interfaces:**
- Consumes: everything already built; no new exported API.

- [ ] **Step 1: Write the failing `NaN`/`Inf` validation test**

In `event/event_test.go`:

```go
func TestValidateRejectsNonFiniteIdentityConfidence(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validEvent() // reuse the same minimal-valid-event helper Task 1 used
			e.Actor.IdentityConfidence = tt.value
			if err := e.Validate(); !errors.Is(err, ErrInvalidIdentityConfidence) {
				t.Errorf("Validate() error = %v, want ErrInvalidIdentityConfidence", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./event/... -run TestValidateRejectsNonFiniteIdentityConfidence -v`
Expected: FAIL — today's `a.IdentityConfidence < 0 || a.IdentityConfidence > 1` is
false for `NaN` (both comparisons involving NaN are false in Go), so `NaN` passes
`validate()` silently today. `+Inf`/`-Inf` are actually already caught by the
existing range check (`+Inf > 1` is true, `-Inf < 0` is true) — confirm which
sub-cases genuinely fail before assuming all three do.

- [ ] **Step 3: Fix `Actor.validate()`**

In `event/event.go`:

```go
func (a Actor) validate() error {
	if a.ID == "" {
		return ErrMissingActorID
	}
	if !a.Type.valid() {
		return fmt.Errorf("%w: %q", ErrInvalidActorType, a.Type)
	}
	if math.IsNaN(a.IdentityConfidence) || a.IdentityConfidence < 0 || a.IdentityConfidence > 1 {
		return fmt.Errorf("%w: %v", ErrInvalidIdentityConfidence, a.IdentityConfidence)
	}
	return nil
}
```

Add `"math"` to `event/event.go`'s import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./event/... -v`
Expected: PASS, including every pre-existing `event` test.

- [ ] **Step 5: Write remaining malformed/extreme-input tests**

In `event/event_test.go`, add table cases (extending existing `Validate` tests or a
new `TestValidateMalformedInput` table) covering: empty `Actor.ID`/`Operation.Name`
(already rejected — add explicit regression cases if none exist), a very long
string (e.g. 100,000 characters) in `Actor.ID` (assert it's *accepted* — `Validate`
has no length limit today, and adding one is out of scope per Non-Goals unless a
concrete DoS vector is found; assert current behavior explicitly instead of
silently assuming it), a deeply nested/very large `Event.Attributes` map (assert
`Engine.Analyze` doesn't panic — this belongs in `engine_test.go`, see Step 6), and
negative values arriving via the `duration_ms` attribute (assert `features.Extract`
handles a negative float64 without producing a negative `time.Duration` that later
corrupts a variance calculation — trace through `internal/anomaly`'s
`latencySignal`/now `frequencySignal` math with a negative duration and confirm no
NaN propagates into `Trust.Score`; if one does, that is the validation gap this
task exists to close — add the guard at the point that's actually wrong, most
likely rejecting a negative `duration_ms` in `features.durationMillisAttr` or
flagging it as `HasLatency: false`).

- [ ] **Step 6: Write cross-actor isolation and resource-exhaustion tests**

In `engine_test.go`:

```go
func TestAnalyzeCrossActorIsolation(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()

	shape := func(actorID string) event.Event {
		return event.Event{
			ID: actorID + "-evt", Timestamp: time.Now(),
			Actor:     event.Actor{ID: actorID, Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /shared"},
			Target:    event.Target{Name: "shared-target"},
			Context:   event.Context{Environment: "prod"},
		}
	}

	for range 30 {
		r, err := e.Analyze(ctx, shape("actor-a"))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if _, err := e.Observe(ctx, r); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}

	// actor-b has never been observed for this identical shape — it must
	// still register full categorical novelty, proving actor-a's 30
	// observations never leaked into actor-b's Baseline.
	rB, err := e.Analyze(ctx, shape("actor-b"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rB.Anomaly.Confidence != 0 {
		t.Errorf("actor-b Confidence = %v, want 0 (no baseline should exist yet — cross-actor leak?)", rB.Anomaly.Confidence)
	}
}

func TestAnalyzeLargeAttributesMapDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()
	attrs := make(map[string]any, 100000)
	for i := range 100000 {
		attrs[fmt.Sprintf("key-%d", i)] = i
	}
	ev := event.Event{
		ID: "evt", Timestamp: time.Now(),
		Actor:     event.Actor{ID: "a", Type: event.ActorTypeService, IdentityConfidence: 1},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /x"},
		Attributes: attrs,
	}
	if _, err := e.Analyze(ctx, ev); err != nil {
		t.Fatalf("Analyze() error = %v, want nil (large Attributes must not error or panic)", err)
	}
}

func TestObserveUnboundedFingerprintsDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()
	for i := range 5000 {
		ev := event.Event{
			ID: fmt.Sprintf("evt-%d", i), Timestamp: time.Now(),
			Actor:     event.Actor{ID: "actor-flood", Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: fmt.Sprintf("GET /x/%d", i)},
			Context:   event.Context{Environment: "prod"},
		}
		r, err := e.Analyze(ctx, ev)
		if err != nil {
			t.Fatalf("Analyze() at i=%d: %v", i, err)
		}
		if _, err := e.Observe(ctx, r); err != nil {
			t.Fatalf("Observe() at i=%d: %v", i, err)
		}
	}
}
```

- [ ] **Step 7: Run tests to verify they pass (or reveal real gaps)**

Run: `go test . -run 'TestAnalyzeCrossActorIsolation|TestAnalyzeLargeAttributesMapDoesNotPanic|TestObserveUnboundedFingerprintsDoesNotPanic' -v -race`
Expected: PASS. If any panics or fails, that is itself the finding this task exists
to surface — fix the specific gap found (per the task's Non-Goals: fix only what a
failing test reveals, don't invent new protections speculatively).

- [ ] **Step 8: Update `docs/SECURITY.md`**

Add two new threat entries — "malformed/extreme input values" and "resource
exhaustion" — with the same implemented/deferred rigor as every existing entry,
each pointing at the specific new test(s) from Steps 1–6. Add a short index table
(or inline references) pointing at the *existing* tests that already cover the
other named threats (baseline poisoning →
`TestObserveLearnsOnlyFromEligibleDecisions`/`TestAnalyzeSensitiveTargetFloorEndToEnd`
in `engine_test.go`; policy bypass → the three fail-closed tests in
`internal/policy/policy_test.go`) — reference them, do not move or rewrite them.

- [ ] **Step 9: `go test ./... -race -v`, `gofmt -l .`, `go vet ./...`**

Run: `go test ./... -race -v && gofmt -l . && go vet ./...`
Expected: everything green.

- [ ] **Step 10: Commit**

```bash
git add event/event.go event/event_test.go engine_test.go docs/SECURITY.md
git commit -m "Add dedicated security test suite: malformed input, resource exhaustion, cross-actor isolation (task 012)"
```

---

### Task 10: 013 — OSS v0.1 Release Gate

**Depends on:** Tasks 1–9 (roadmap tasks 001, 002, 004, 005, 006, 007, 010, 011,
012) all merged.

**Files:**
- No new implementation files — verification, `docs/ROADMAP.md`, a new
  `CHANGELOG.md`, and a git tag.

- [ ] **Step 1: Verify each dependency task's acceptance criteria individually**

Re-open each of `docs/tasks/001-feature-model.md` through `012-security-tests.md`'s
"Acceptance Criteria" section and check every bullet against what actually landed
in Tasks 1–9 above. Do not re-derive new criteria — only confirm what's already
written.

- [ ] **Step 2: Run the full baseline gate fresh**

```bash
go build ./... && go vet ./... && gofmt -l . && go test -race -count=1 ./... && go mod tidy
```
Expected: all clean; `go mod tidy` produces no diff (`git diff go.mod go.sum`
empty) — no accidental new dependency across the whole milestone.

- [ ] **Step 3: Re-run the documentation consistency check**

Grep every `.md` file under `docs/` and the root for stale package paths, old
function signatures, and "core depends on X" phrasing that Tasks 1–9 may have
invalidated (e.g. any doc still describing `Target{Name string}` without
`Category`, any doc still describing `Condition` without `Attributes`, any
still-broken internal doc links). Check every cross-reference link/anchor
mentioned across `docs/*.md` and `docs/adr/*.md` actually resolves.

- [ ] **Step 4: Confirm `examples/` runs against the exact commit being tagged**

```bash
cd examples && go mod tidy && cd .. && make examples
```
Expected: all six examples still build and run cleanly against the final state of
the module (not an earlier snapshot from when Task 7 was written).

- [ ] **Step 5: Run the full benchmark suite, record final numbers**

```bash
go test ./... -bench . -benchmem -run ^$
```
Record these in `docs/PERFORMANCE.md` explicitly as the "as of v0.1" baseline.

- [ ] **Step 6: Decide and document the version-compatibility promise**

Write a short section (in `CHANGELOG.md` or a `docs/` file — decide during this
step) stating precisely what `v0.1` commits to keeping stable: at minimum,
`event.Event`'s and `Result`'s public field shapes, `Engine`'s public method
signatures (`Analyze`, `Observe`), and the `Option` functions. Everything under
`internal/` carries no compatibility promise.

- [ ] **Step 7: Update `docs/ROADMAP.md`'s "Current status" section**

Move its content to reflect `v0.1` as shipped; make `v0.2` the new "next up" — edit
the prose, don't just append.

- [ ] **Step 8: Write `CHANGELOG.md`**

Summarize what `v0.1` actually contains, task by task (001–007, 010–013), in
plain release-notes language — this is the first point such a document earns its
keep.

- [ ] **Step 9: Tag the release**

Only after explicit user confirmation (tagging and any push are exactly the kind of
irreversible, externally-visible action CLAUDE.md and the environment's execution-
care guidance require confirming first):

```bash
git tag -a v0.1.0 -m "v0.1.0 — Behavioral core hardening & first public release"
```
Do not `git push` the tag unless separately asked.

- [ ] **Step 10: Commit the release-gate documentation changes**

```bash
git add docs/ROADMAP.md docs/PERFORMANCE.md CHANGELOG.md
git commit -m "Release v0.1.0: behavioral core hardening (task 013)"
```

---

## Self-Review Notes

- **Spec coverage:** every one of the ten in-scope task files (001, 002, 004, 005,
  006, 007, 010, 011, 012, 013) maps to exactly one Task above, in the same
  dependency order each file states. 003 and 008/009/014/015/016 are correctly out
  of scope (003 already done; the rest belong to v0.2+ per `docs/ROADMAP.md`).
- **Placeholder scan:** every code step above shows real, compileable-shape Go
  code, not a description of what to write. The two spots with an explicit
  "check the existing file first" caveat (Task 1 Step 9's test-package choice,
  Task 2 Step 1's black-box-vs-white-box choice, Task 3 Step 1's same issue) are
  genuine implementation-time decisions gated on reading a file this plan's author
  did not have open line-by-line — not vagueness about what to build, only about
  which of two mechanically-equivalent Go test-package conventions the file
  already uses.
- **Type consistency:** `TargetCategory` (Task 1) flows unchanged into
  `StableFeatures.TargetCategory` (Task 1) into `fingerprint.Compute` (Task 2);
  `FingerprintStats.IntervalMean/IntervalVariance/IntervalObservations` (Task 3)
  are the exact names `frequencySignal` (Task 3) reads; `Trust.Explain()` (Task 4)
  is the exact method `Result.Explain()` (Task 6) calls; `Condition.Attributes`/
  `Input.Attributes` (Task 5) are the exact names `Engine.Analyze` (Task 5) and the
  012 tests (Task 9) would use if they touched policy (they don't, by design).

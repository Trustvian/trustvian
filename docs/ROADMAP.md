# Roadmap

Milestone-based (`v0.1` through a `v1.0.0` production-readiness gate),
reconciled against what's actually in this repository today — verified
by reading source, tests, and benchmarks, not assumed from the original
vision document. Each milestone maps to one or more detailed task files
in [`docs/tasks/`](tasks/); each task file is independently
understandable and carries its own objective, scope, non-goals,
technical requirements, tests, benchmarks, documentation, and
acceptance criteria. Milestones without a fully-scoped task sequence
yet (`v0.8`–`v0.9` below) are deliberately not pre-scoped in detail —
this roadmap's own "small vertical slices" principle, applied to
itself. Every earlier milestone through `v0.7` has each of its slices
either scoped and done, or explicitly named as the next slice, not
left as a vague placeholder: `v0.5` has all five of its tasks scoped
and done — [019](tasks/019-policy-config-model.md),
[020](tasks/020-policy-config-loader.md),
[021](tasks/021-cli-config-integration.md),
[022](tasks/022-collector-config-integration.md), and
[023](tasks/023-declarative-alert-configuration.md); `v0.6` has all
four of its feature slices scoped and done, plus its stabilization
gate — [025](tasks/025-sequence-analysis-foundation.md),
[026](tasks/026-transition-rarity.md),
[027](tasks/027-bounded-ngram-detection.md),
[028](tasks/028-markov-transition-scoring.md), and
[029](tasks/029-v06-stabilization-release-gate.md); `v0.7` has all
five of its slices scoped and done, including its own stabilization
gate — [014](tasks/014-ai-agent.md),
[030](tasks/030-approval-aware-policy-semantics.md),
[031](tasks/031-delegation-behavioral-semantics.md),
[032](tasks/032-agent-security-scenario-validation.md), and
[033](tasks/033-v07-stabilization-release-gate.md).

Cross-references: [ARCHITECTURE.md](ARCHITECTURE.md) (system shape),
[DOMAIN.md](DOMAIN.md) (what exists today), [SECURITY.md](SECURITY.md)
(threat model), [PERFORMANCE.md](PERFORMANCE.md) (measured numbers),
[`adr/`](adr/) (why past decisions were made).

## The OSS / Enterprise product boundary

Stated explicitly, since it governs every milestone below: **Trustvian
OSS is a complete, standalone, production-usable behavioral security
product.** Trustvian Control/Enterprise is never the place core
detection capability lives — nothing an operator needs to *detect,
score, decide, alert on, integrate, or run* Trustvian in production is
gated behind a paid product. Concretely, OSS owns the full vertical:

```text
Detect        — Event, Features, Fingerprint, Baseline, Anomaly
Score         — Trust, Risk
Decide        — Policy, Decision, Explainability
Alert         — Alert Evaluation, Notification, WebhookSink (v0.4+)
Integrate     — Go SDK, CLI, OTel adapter, OTel Collector processor, MCP
Run           — persistence, deployment packaging, self-observability, security hardening
```

Enterprise/Trustvian Control's job is organizational scale and
governance **on top of** a fully-capable OSS core, never a substitute
for missing OSS capability:

```text
Centralized management       Investigation workflows
Governance                   Case management / audit / compliance
Multi-tenancy                Reporting
SSO / SAML / OIDC / RBAC     Fleet management
Central policy management    HA / scaling operations
Advanced alert governance    Managed SaaS, support/SLA
```

The dividing question is never "is this feature valuable enough to
withhold from OSS" — it is "does this feature only make sense at
organizational scale" (managing *many* deployments, *many* tenants,
*many* teams' policies centrally) or "is this an operations/compliance
concern orthogonal to detection itself" (audit trails, RBAC, SLAs).
Detecting, scoring, deciding, and alerting on *one deployment's*
behavior is squarely OSS, regardless of how sophisticated the detection
gets. **Control consumes the OSS core as a normal external dependency
and never forks or reimplements it** — the same constraint [task
016](tasks/016-control.md) already recorded before any Control work
started, now stated as this roadmap's standing product principle, not
just that task's own scope note.

This section supersedes any earlier phrasing in this document, the
[project spec](../trustvian-project-spec.md), or task files that could
be read as "Enterprise provides X because OSS doesn't" for anything in
the `Detect`/`Score`/`Decide`/`Alert`/`Integrate`/`Run` list above — see
[§ Alert & Notification phase](#alert--notification-phase) and
[§ Control / Enterprise phase](#control--enterprise-phase) below for
where this principle was applied to correct exactly that kind of
phrasing.

## Current status

**`v0.1.0` is shipped.** The core pipeline — `Event → Features →
Fingerprint → Baseline → Anomaly → Trust → Policy → Decision` — is
implemented, tested, benchmarked end to end, and race-clean, and every
task in the `v0.1` milestone below (001–007, 010–013) is complete and
individually verified against its own acceptance criteria as part of
[task 013](tasks/013-oss-v01.md)'s release gate. See
[CHANGELOG.md](../CHANGELOG.md) for what `v0.1.0` actually contains and
the public API compatibility promise that starts at this tag.

Concretely, as of `v0.1.0`: a Go SDK (`Engine`, functional options), a
CLI (`analyze`, `baseline build`), and an inbound OpenTelemetry adapter
(`internal/otel.EventFromSpan`) all exist and are exercised by
end-to-end tests. Architecture hardening (package boundaries,
dependency direction, a real hot-path fix) is recorded in [ADRs
0001–0006](adr/). A persistent `store.FileStore` (survives a process
restart — [ADR 0006](adr/0006-file-backed-persistent-store.md)),
fingerprint staleness, and per-actor learning freeze are implemented
and benchmarked. `Target.Category` (a new stable dimension), a
versioned fingerprint hash, a `frequency_deviation` anomaly signal
(shipped opt-in — `FrequencyWeight` defaults to `0` pending real-traffic
calibration), `Trust.Explain()`/`Result.Explain()`, policy attribute
matching, a runnable `examples/` directory, a complete performance
baseline (see
[PERFORMANCE.md § Measured results](PERFORMANCE.md#measured-results)),
and a dedicated, threat-organized security test suite (see
[SECURITY.md](SECURITY.md)) all shipped as part of this milestone.

**`v0.2` — OpenTelemetry maturation is also shipped.** Both of its
tasks are done: [008](tasks/008-otel.md) added the five well-defined
outbound `trustvian.*` attributes (`internal/otel.AttributesFromResult`;
`trustvian.behavior.id` deliberately omitted — see
[OPENTELEMETRY.md](OPENTELEMETRY.md#trustvian-output-attributes)), and
[009](tasks/009-otel-collector.md) added a real, working OTel Collector
processor as a separate module ([`processor/`](../processor/) — see its
own [README](../processor/README.md)), verified end-to-end against a
real OTel-SDK span sent over real OTLP/gRPC to a real running Collector
binary.

**`v0.3.0` — Baseline & anomaly depth is shipped.** Its one task,
[017](tasks/017-baseline-time-patterns.md), added an hour-of-day time
pattern signal (`baseline.FingerprintStats.HourActivity`, `anomaly`'s
`time_pattern_deviation`, shipped opt-in like `frequency_deviation`
before it) — see the milestone section below for the full writeup,
including the day-of-week scope decision and the empirically-discovered
`hourActivityAlpha` design correction.

**`v0.4.0` — Alert & Notification Foundation is shipped.**
[Task 018](tasks/018-alert-notification-foundation.md) added
a new public package, `alert` (sibling to `event` — see
[ADR 0007](adr/0007-alert-package-is-public.md) for why it isn't
`internal/`): `Alert`, a `Severity` concept, a
`policy.Condition`-shaped Alert Evaluation matcher (`Condition`/`Rule`/
`Evaluate`), the `Sink` interface, and `WebhookSink` — a generic,
HMAC-signed HTTPS webhook, the one delivery mechanism this stage ships
— without changing `Decision` semantics or touching the core pipeline
at all. See the milestone section below for the full writeup.

**`v0.5.0` — Policy & Configuration is shipped.** All five of its
tasks — [019](tasks/019-policy-config-model.md),
[020](tasks/020-policy-config-loader.md),
[021](tasks/021-cli-config-integration.md),
[022](tasks/022-collector-config-integration.md), and
[023](tasks/023-declarative-alert-configuration.md) — are done in code,
tests, and documentation, and the `v0.5.0` tag itself is published on
`origin`. A new public package, `config`, lets a caller outside this
module compile a `PolicyConfig` into a real `policy.Policy` and hand it
to `trustvian.WithPolicy` — without `internal/policy` becoming public
(see [ADR 0008](adr/0008-policy-config-boundary.md)) — and load that
same `PolicyConfig` from a real YAML file (`config.LoadFile`/`Load`),
strictly, with unknown fields and duplicate keys both rejected.
`trustvian analyze`/`trustvian baseline build` now accept `--config
<path>` and consume that exact loader/compiler path, failing closed
(non-zero exit, no analysis output) if the file can't be safely loaded
and compiled — see [task 021](tasks/021-cli-config-integration.md).
The standalone OTel Collector processor
([`processor/`](../processor/README.md)) now has an equivalent
`policy:` block in its own configuration, decoded and compiled through
the same `config` package — see [task
022](tasks/022-collector-config-integration.md) — and `processor/go.mod`
depends on this exact `v0.5.0` release, verified with a clean
`GOWORK=off` build/test against it (no local workspace involved).
Declarative Alert configuration now exists too — a structurally
**separate** `AlertConfig`/`CompileAlerts` model (see [task
023](tasks/023-declarative-alert-configuration.md) and [ADR
0009](adr/0009-alert-config-is-a-separate-document.md)), not merged
into `PolicyConfig` and not yet wired into the CLI or the Collector
processor (neither has an alert-delivery flow for it to plug into yet
— a deliberate scope boundary, not a gap).

**`v0.6.0` — Behavioral Detection Depth is shipped.** All five of its
tasks — [025](tasks/025-sequence-analysis-foundation.md),
[026](tasks/026-transition-rarity.md),
[027](tasks/027-bounded-ngram-detection.md),
[028](tasks/028-markov-transition-scoring.md), and
[029](tasks/029-v06-stabilization-release-gate.md) — are done in code,
tests, and documentation, and the `v0.6.0` tag itself is published on
`origin` (`gh release list` confirms the GitHub release, marked
`Latest`). Four new, opt-in anomaly signals reuse the existing
`Baseline`/`Store` architecture rather than a parallel one: pairwise
`transition_deviation`/`transition_rarity` (has this exact predecessor
ever led here before, and if so how commonly), bounded 3-gram
`ngram_deviation`/`ngram_rarity` (the same question one step further
back, over a fixed `(grandparent, predecessor)` pair — proven to add
genuine information beyond the pairwise signals, not merely restate
it), and `markov_surprisal` (an alternative, information-theoretic
severity curve over the *identical* evidence `transition_rarity`
already reads — proven mathematically to be a monotonic
reparameterization, not independent evidence, and structurally
prevented from double-counting alongside it — see [ADR
0013](adr/0013-first-order-markov-surprisal-without-duplicate-evidence.md)).
Every new signal defaults to a zero weight; existing `v0.5` callers see
byte-for-byte unchanged `Score` output and `BenchmarkEngineAnalyze`
allocation profile. `processor/go.mod` does not depend on `v0.6.0` —
`v0.6` added zero public API surface (verified via `git diff
v0.5.0..HEAD -- '*.go'`), so there is no functional reason to bump it,
unlike `v0.3.0 -> v0.5.0`'s own bump (which was needed because `config`
didn't exist in `v0.3.0`).

What's **not** yet true, concretely — the gaps this roadmap's remaining
milestones exist to close:

- Day-of-week seasonality — evaluated and kept out of `v0.3`'s
  hour-of-day slice; classified below as useful-after-`v1.0`, not
  required for it (see [Future research](#future-research)).
- Alert delivery retry, deduplication, cooldown, escalation, and alert
  history — the Reliability stage, an OSS capability not yet built
  (see [Alert & Notification phase](#alert--notification-phase)
  below); no `Slack`/`Teams`/`PagerDuty` sink exists yet either, though
  nothing prevents one from being a future OSS `Sink` implementation
  (same section). Declarative Alert *configuration* (which rules
  produce which severity) is now implemented — see `v0.5` below — but
  that is a different concern from *delivery* reliability.
- A custom `Policy` can now be constructed from outside this module in
  Go ([task 019](tasks/019-policy-config-model.md)), loaded from
  a real YAML file ([task 020](tasks/020-policy-config-loader.md),
  `config.LoadFile`), consumed directly by the CLI via `--config`
  ([task 021](tasks/021-cli-config-integration.md)), and consumed by
  `processor/`'s own code ([task
  022](tasks/022-collector-config-integration.md), now depending on the
  real `v0.5.0` release). Declarative Alert configuration
  (`config.AlertConfig`/`CompileAlerts`, compiling into `[]alert.Rule`
  — [task 023](tasks/023-declarative-alert-configuration.md)) now
  exists as a Go-SDK-usable capability, but is not wired into the CLI
  or `processor/` — deliberately, per task 023's own Non-Goals, not a
  gap analogous to the `processor/go.mod` release blocker above.
- Sequence-aware detection (order, not just individual-event anomaly)
  is now implemented — see `v0.6` below, shipped. Arbitrary-length
  n-grams and a full/higher-order Markov treatment (a transition
  matrix, smoothing) remain unbuilt future work (see [ADR
  0012](adr/0012-bounded-trigram-behavioral-context.md)'s and [ADR
  0011](adr/0011-transition-rarity-statistic-and-orientation.md)'s own
  "why X still waits" sections).
- AI-agent event types work today only through the generic `Event`
  model (`ActorTypeAIAgent` + `OperationCategoryTool`) — no session,
  delegation, or tool-sequence concepts exist yet (`v0.7`, below, now
  in progress).
- No production-grade persistent `Store` beyond `FileStore`, no Docker
  deployment path (`v0.8`); no CI/CD, no release automation, no
  container image (`v0.9`).
- No MCP interface (an optional integration, not a `v1.0` blocker — see
  [Trustvian MCP](#trustvian-mcp) below), no Trustvian Control (an
  organizational-governance layer that consumes OSS, not a
  prerequisite for OSS to be complete — see
  [§ The OSS / Enterprise product boundary](#the-oss--enterprise-product-boundary)
  above).

**Next up:** with `v0.1`–`v0.6.0` shipped (see [Release readiness —
v0.5.0](#release-readiness--v050) for the tag/dependency verification
pattern `v0.6.0` repeated), `v0.7` — AI Agent Behavioral Security is now
in progress — see the `v0.7` milestone section below. The rest of this roadmap's
remaining work (the rest of `v0.7` through `v1.0` Production-Ready OSS,
defined below) charts the
path to a complete, standalone, production-usable OSS product — see
each milestone section for
dependencies; none of `v0.6`–`v0.9` are strictly ordered relative to
the still-unscoped Alert Reliability stage or AI-agent work, only
relative to each other where a real dependency exists (stated per
milestone).

This roadmap's job is to close the remaining gaps in the order that
respects the roadmap principles (deterministic before ML, security
before advanced features, real-world validation before enterprise,
small vertical slices).

---

## v0.1 — Behavioral core hardening & first public release

**Objective.** Take the already-implemented core pipeline from
"works and is tested" to "hardened, explainable, benchmarked,
demonstrated, and released as a stable OSS artifact." No new pipeline
stages — this milestone is depth, not breadth.

**Scope** (task files [001](tasks/001-feature-model.md)–[007](tasks/007-decision.md),
[010](tasks/010-examples.md)–[013](tasks/013-oss-v01.md)). Every task
below shipped; see [CHANGELOG.md](../CHANGELOG.md) for the release-note
version of the same list.

- **001 Feature model — done.** The stable/volatile split is formally
  documented, and one genuinely justified new stable dimension —
  `Target.Category` (`internal`/`external`/`database`, optional, zero
  value accepted) — flows through `features.Extract` into the
  `Fingerprint`. No volatile telemetry leaked into behavioral identity.
- **002 Fingerprint — done.** `fingerprint.Compute` writes an explicit
  version marker into its hash ahead of the stable fields, so a future
  change to which dimensions feed it produces a disjoint ID space
  rather than silently reinterpreting persisted IDs. The design (what
  feeds the hash, in what order, and why FNV-1a) is written up in
  [DOMAIN.md § Fingerprint](DOMAIN.md#fingerprint).
- **003 Baseline v2 — done.** Persistent `store.FileStore`, explicit
  staleness handling (`FingerprintStats.IsStale`, using the
  already-tracked `LastObserved`), and a baseline-freeze mechanism
  (`store.Freezer` — stop learning without discarding history, e.g.
  during an active investigation) are all implemented. See
  [ADR 0006](adr/0006-file-backed-persistent-store.md).
- **004 Anomaly v2 — done.** The one concretely missing signal,
  `frequency_deviation`, is implemented on top of a new inter-observation
  interval EWMA in `internal/baseline`. It ships opt-in
  (`Config.FrequencyWeight` defaults to `0`): the signal is computed and
  reported in `Anomaly.Contributors`, but contributes to `Score` only
  once an operator has calibrated `FrequencyZThreshold` against their own
  traffic's jitter — see [DOMAIN.md § Anomaly](DOMAIN.md#anomaly).
- **005 Trust/Risk calibration — done.** A scenario-matrix test sweeps
  identity confidence, anomaly score/confidence, and context risk and
  asserts the multiplicative formula stays in `[0,1]` and monotonic in
  every input; `Trust.Explain()` renders a value as a readable
  sentence. The formula itself is unchanged.
- **006 Policy hardening — done.** `policy.Condition` gained an
  `Attributes map[string]string` field, ANDed with every other condition
  field, closing the spec's own `tool.category: secrets` example gap.
  Flat key/value equality only — no combinators, no operators, no
  dynamic policy loading, no policy language.
- **007 Decision/Explainability — done.** `Result.Explain() string`
  renders the full decision summary (decision, trust/risk/anomaly
  scores, every contributing signal with its detail, the matched rule or
  default reason), and a test formalizes that every field the spec's
  Decision checklist names is present on `Result`.
- **010 Real-world examples — done.** A runnable `examples/` directory
  covering the six scenarios named in the roadmap brief, each a genuine
  external module (via `go mod replace`) with real, `go run`-captured
  output; wired into the root `Makefile`'s `examples` target.
- **011 Performance baseline — done.** `BenchmarkEventFromSpan`
  (`internal/otel`) and `BenchmarkInMemoryMemoryGrowth`
  (`internal/store`, at 100/1,000/10,000 distinct keys) close the two
  gaps [PERFORMANCE.md](PERFORMANCE.md) had named as unmeasured; every
  pipeline stage is now individually benchmarked, with numbers
  reproduced fresh for the release.
- **012 Security tests — done.** A dedicated, threat-organized suite
  covers malformed/extreme input (`NaN`/`±Inf` identity confidence, very
  long strings, negative `duration_ms`), resource-exhaustion safety, and
  an explicit end-to-end cross-actor isolation proof.
  [SECURITY.md](SECURITY.md) now documents every threat named in the
  roadmap brief, each with a test reference or an explicit
  deferred-mitigation label.
- **013 OSS v0.1 release gate — done.** The checklist tying 001–012
  (plus what already existed) together into a tagged release, including
  [CHANGELOG.md](../CHANGELOG.md) and the public API compatibility
  promise that starts at `v0.1.0`.

**Non-goals.** No OTel Collector processor, no outbound OTel
attributes, no AI-agent-specific event fields, no MCP, no Control, no
ML, no distributed/multi-instance anything.

**Dependencies.** None outside this repository. Everything in v0.1
builds on packages that already exist.

**Acceptance criteria.** See [013-oss-v01.md](tasks/013-oss-v01.md) in
full; summarized: `go build`/`go vet`/`go test -race`/`gofmt -l` clean;
every task 001–012 individually meets its own acceptance criteria;
`examples/` runs against a tagged release; `docs/` contains no
contradiction with the tagged code (re-run the Phase 9 consistency
check from the architecture-hardening pass).

---

## v0.2 — OpenTelemetry maturation

**Objective.** Complete the OTel integration story in the outbound
direction, and take the first step toward a production Collector
deployment — without pulling OTel, or the Collector toolchain, into
the core.

**Scope** (task files [008](tasks/008-otel.md), [009](tasks/009-otel-collector.md)):

- **008 OTel integration v2 — done.** `internal/otel.AttributesFromResult`
  derives five of the six outbound `trustvian.*` attributes
  (`anomaly.score`, `trust.score`, `risk.level`, `decision`,
  `fingerprint.id`) from a `Result`. `trustvian.behavior.id` — the
  sixth, never defined beyond its name in the original spec — is
  deliberately not implemented: every plausible meaning collapses into
  `Fingerprint.ID`, so implementing it as a duplicate, or inventing a
  new domain concept to justify a distinct one, would be exactly the
  kind of undocumented telemetry attribute CLAUDE.md's OpenTelemetry
  section warns against. See [OPENTELEMETRY.md § Trustvian output
  attributes](OPENTELEMETRY.md#trustvian-output-attributes) for the
  full rationale and mapping table. Verified via `go list -deps` that
  this introduces no import cycle and that `internal/otel` remains the
  sole package in this module depending on OpenTelemetry.
- **009 OTel Collector processor — done.** [`processor/`](../processor/)
  (module `trustvian-processor`) is a minimal, working Collector
  processor consuming this module's public API exactly like any other
  embedder — verified via `go list -deps` in *this* module's root that
  `go.mod`/`go.sum` here are completely unaffected by its existence.
  Its own `ptrace.Span → event.Event` mapping and `Result → attributes`
  writer are necessarily parallel implementations of task 008's, not
  reuses of it: `ptrace.Span` (the Collector's pdata model) shares no
  relationship with `sdktrace.ReadOnlySpan` (the SDK model
  `internal/otel.EventFromSpan` requires), and `internal/otel` is
  unreachable from a separate module regardless. [ADR
  0002](adr/0002-public-api-boundary.md)'s public API boundary was
  considered and explicitly *not* revisited — building this processor
  isn't the "real external consumer" trigger that ADR named, so it
  runs Trustvian's default `Policy` (every span resolves to
  `observe_only` today), documented as a known limitation rather than
  worked around. See [`processor/README.md`](../processor/README.md)
  for the full design and a real, captured end-to-end run (a genuine
  OTel-SDK span sent over real OTLP/gRPC to a real running Collector
  binary built with this processor, correctly enriched with all five
  `trustvian.*` attributes).

**Non-goals.** No distributed Trustvian server, no processor-side
persistence beyond what `v0.1`'s `Store` already provides, no
Kubernetes/Helm packaging (that's Phase 5 territory in the original
spec, not scoped into any milestone here yet — add it only when a
concrete deployment need exists).

**Dependencies.** v0.1 complete (`Result`'s shape should be stable
before writing an exporter for it; a persistent `Store` makes a
long-running Collector deployment actually useful).

**Acceptance criteria.** See task files 008–009. Summarized: outbound
attributes round-trip-tested (`Result` → attributes → re-parsed);
Collector processor builds as an independent module with no `go.mod`
coupling back into this one; a documented example Collector config
pipeline exists.

---

## v0.3 — Baseline & anomaly depth

**Objective.** Everything in Phase 3/4 of the original brief that
*wasn't* already scoped into v0.1's minimal hardening — the genuinely
"v2-and-beyond" statistical depth, kept deterministic and explainable,
not ML.

**Scope** (task file [017](tasks/017-baseline-time-patterns.md)).

- **017 Baseline & anomaly depth: hour-of-day time pattern — done.**
  Resolved the open question this section previously carried
  ("does day-of-week/hour-of-day seasonality need anything beyond a
  straightforward per-hour-bucket EWMA, or does it move to Future
  research?") by scoping the hour-of-day half narrowly and building it:
  `baseline.FingerprintStats` gained `HourActivity [24]float64` (a
  per-UTC-hour EWMA of traffic share) and `TimePatternObservations`
  (a dedicated maturity counter, gating the signal independently of
  `Count` so a pre-existing persisted `FileStore` record — where
  `HourActivity` unmarshals to its zero array — is correctly treated as
  immature rather than falsely mature-with-empty-data). `anomaly.Score`
  gained a sixth signal, `time_pattern_deviation`, shipped opt-in
  (`Config.TimePatternWeight` defaults to `0`, the same precedent as
  `FrequencyWeight`) both for calibration and because
  `MinObservations` maturity alone doesn't guarantee a fingerprint has
  actually been observed across a representative spread of hours. Day-
  of-week seasonality was **not** built — it remains
  [Future research](#future-research), since scoping proved the
  hour-of-day case alone was already substantial enough to warrant its
  own task, and a second, independent EWMA dimension is a separate
  vertical slice, not a small addition to this one. See
  [DOMAIN.md § Baseline / § Anomaly](DOMAIN.md), [SECURITY.md §
  Baseline poisoning](SECURITY.md), and
  [PERFORMANCE.md § v0.3 task 017 re-measurement](PERFORMANCE.md) for
  the full design, security, and performance writeup. A genuinely
  non-obvious, empirically-discovered result: reusing the existing
  `emaAlpha = 0.2` for `HourActivity` was tried first and rejected —
  `TestFingerprintStatsHourActivityUniformTraffic` proved it makes
  uniform (patternless) hour-of-day traffic look sharply time-anomalous
  purely as a measurement-phase artifact, which is why a new, slower,
  separately-justified `hourActivityAlpha = 0.02` constant exists
  instead of reusing the shared one.

**Non-goals.** Day-of-week seasonality (see above — moved to
[Future research](#future-research), not merely deferred to a later
v0.3 task). Sequence/n-gram anomaly detection (still deliberately
deferred — see [ADR 0001](adr/0001-hexagonal-core-and-pipeline-shape.md)
and [Future research](#future-research)), ML of any kind.

**Dependencies.** v0.1 (needed the persistent `Store` and
frequency-tracking groundwork from 003/004) — satisfied.

**Acceptance criteria.** See
[task 017](tasks/017-baseline-time-patterns.md)'s own Acceptance
Criteria section — all met, verified by
`go test ./... -race -count=1` and `go test -bench=. -benchmem ./...`.

---

## v0.4 — Alert & Notification Foundation

**Objective.** Turn a Trustvian `Result`/`Decision` into a minimal,
explainable, externally deliverable `Alert`, without changing
`Decision` semantics or the existing detection pipeline. This is the
"Foundation" stage of the broader Alert & Notification phase (see
below for the stages sequenced after it) — the only stage that must
ship before the rest are useful. See
[`trustvian-project-spec.md` § 18](../trustvian-project-spec.md#18-alert--notification-system)
for the full architecture this milestone implements against.

**Scope** (task file [018](tasks/018-alert-notification-foundation.md)).

- **018 Alert & Notification Foundation — done.** New public package
  `alert` (a sibling to `event`, not `internal/` — see
  [ADR 0007](adr/0007-alert-package-is-public.md)): the `Alert` domain
  concept (reusing `Trust`/`Anomaly`/`Decision`/`Event.Actor`/
  `Event.Target`/`Result.Explain()`'s material — no parallel model),
  `Severity` (`INFO`/`LOW`/`MEDIUM`/`HIGH`/`CRITICAL`, explicitly
  distinct from risk/anomaly score/trust score/decision, always exactly
  what the matched `Rule` configured), a minimal Alert Evaluation
  matcher (`Condition`/`Rule`/`Evaluate` — flat AND-of-optional-fields
  on decision/risk/actor type/target category/anomaly score/trust
  score, no combinators, mirroring `internal/policy.Condition`'s
  existing discipline, structurally independent from `internal/policy`
  itself), the `Sink` interface, and `WebhookSink` — a generic,
  HMAC-SHA256-signed HTTPS webhook (the one delivery mechanism this
  stage ships) with a versioned payload contract
  (`Envelope{Version, Alert}`, `PayloadVersion = "1"`). Verified
  end-to-end via [`examples/alert-webhook`](../examples/alert-webhook/README.md) — a
  genuinely external module (`examples/go.mod`) building an `Alert`
  from a live `Engine.Analyze` call and delivering it to a signed
  webhook, no OTel involvement anywhere. Resolved the genuine open
  architecture question this milestone previously carried here (should
  `Alert`/`Sink` be public, unlike `policy.Policy`/`anomaly.Config`?) by
  choosing public, recorded in [ADR
  0007](adr/0007-alert-package-is-public.md): `Sink`'s entire purpose is
  for third-party code to implement it against a stable `Alert` type,
  which Go's `internal/` visibility rule would make impossible
  otherwise.

**Non-goals.** No boolean combinators or expression language for alert
rules; no `Policy` reuse or coupling (Alert Evaluation is a
structurally independent, read-only consumer of `Result`, not a second
decision stage); no provider SDKs (Slack/Teams/PagerDuty/Kafka/SMTP/
Redis/database-backed queue) — the generic webhook is the only
transport; no delivery reliability subsystem (retry, backoff,
delivery-state, idempotency, deduplication, cooldown, suppression,
escalation, dead-letter handling — see the Reliability stage below); no
incident-management domain (an `Alert` is one notification-worthy
event, not a grouped investigation); no multi-tenancy, RBAC, or Control
integration. See [task 018's own Non-Goals
section](tasks/018-alert-notification-foundation.md#non-goals) for the
full, precise list.

**Dependencies.** `v0.1`'s stable `Result` shape (Alert Evaluation
reads `Result`; a still-moving `Result` shape would mean redesigning
the `Alert` view underneath it) — satisfied. Not blocked on `v0.2` or
`v0.3` — Alert Evaluation depends on `Decision`/`Trust`/`Anomaly`, not
on OTel outbound attributes or baseline/anomaly depth — but sequenced
here, after those, because neither this roadmap nor the spec commits to
shipping it before them.

**Acceptance criteria.** See [task
018](tasks/018-alert-notification-foundation.md)'s own Acceptance
Criteria section — all met, verified by `go test ./... -race -count=1`,
`go test -bench=. -benchmem ./alert/...`, `go list -deps` confirming no
new HTTP/provider-SDK dependency reached the core engine, and
`examples/alert-webhook`'s genuinely-external end-to-end run.

---

## Alert & Notification phase

**This section now covers only the stages after `v0.4`.** The
Foundation stage above is `v0.4`, with its own numbered task file
(018); this section is what comes after it. The remaining two stages
named in
[`trustvian-project-spec.md` §
18](../trustvian-project-spec.md#18-alert--notification-system) depend
on it and are not yet scoped as task files — each gets its own task
file (following the existing template) once the prior stage is done and
its real shape is known, per this roadmap's own "small vertical slices"
principle. No milestone number or task ID is reserved for either yet:

- **Reliability** — OSS scope (per [§ The OSS / Enterprise product
  boundary](#the-oss--enterprise-product-boundary) above: "Alert" is
  squarely in OSS's `Detect...Alert` vertical, not an Enterprise
  add-on). Delivery retry with exponential backoff, delivery status,
  idempotency, deduplication, and a cooldown window — closing the "one
  incident, one alert" and "a flaky endpoint doesn't lose alerts" gaps
  Foundation deliberately leaves open (spec § 18.11–18.12). Depends on
  `v0.4`.
- **Provider-specific sinks** (Slack, Microsoft Teams, PagerDuty, and
  anything past those) — **also OSS scope, if architecturally clean**,
  correcting this section's own earlier framing (which lumped them in
  with governance below): a `Sink` implementation for a specific
  provider is no different in kind from `WebhookSink` itself, and
  nothing in [ADR 0007](adr/0007-alert-package-is-public.md)'s
  reasoning is provider-specific. They are simply **not required**
  for the OSS core to be useful, since the generic webhook already
  covers most of what these providers offer via automation platforms
  (n8n, SOAR, custom relays) — "not yet built because not urgent," not
  "reserved for a paid product." Depends on `v0.4`.
- **Notification governance** — genuinely Trustvian Control/Enterprise
  territory, per the boundary above (organizational-scale concerns, not
  detection/alerting capability): centralized notification management
  across many deployments, advanced/combinator alert rules, multi-tenant
  notification configuration, escalation policy, alert history at
  scale, delivery observability dashboards, RBAC over alert
  configuration, and audit of who changed what rule. Depends on `v0.4`
  and, practically, on Control existing at all.

**Non-goals (all three items above).** No incident-management domain
(an `Alert` is one notification-worthy event, not a grouped
investigation — see spec § 18.13); no boolean combinators or a general
expression language for alert rules; no Kafka, Redis, or PostgreSQL
dependency added to the core detection engine merely to support
alerting — every provider integration lives behind `Sink`, the same
way `internal/otel` is the only package depending on OpenTelemetry
today. A provider-specific `Sink` package may depend on that
provider's own SDK/HTTP client — the constraint is that the *core
detection engine* (`event` through `internal/policy`, `Engine`) never
does, exactly as it doesn't today for `net/http` via `alert`.

**Acceptance criteria.** Defined when each item's own task file is
written — not before, per "small vertical slices" and this roadmap's
standing policy of not pre-committing to unscoped work. No task file
number is reserved for any of the three yet.

## v0.5 — Policy & Configuration

**Objective.** Make Trustvian usable without embedding custom Go code.
Today, a meaningful custom `Policy` (task 006) or Alert `Rule` set
(task 018) can only be constructed by code living inside this module —
[ADR 0002](adr/0002-public-api-boundary.md) named this precisely for
`Policy`, and it applies equally to `alert.Rule` even though `alert`
itself is public, since `Rule`'s `When Condition` fields are
constructible from Go literals but not from a config file without a
loader. This milestone closes that gap with a narrow, stable,
declarative configuration boundary usable identically by the Go SDK,
the CLI, the OTel Collector processor, and a standalone deployment.

**Scope** (task files [019](tasks/019-policy-config-model.md),
[020](tasks/020-policy-config-loader.md),
[021](tasks/021-cli-config-integration.md),
[022](tasks/022-collector-config-integration.md),
[023](tasks/023-declarative-alert-configuration.md), and
[024](tasks/024-v05-release-gate.md) — all five feature slices plus
the release gate, closing this milestone's full scope):

- **019 Public Policy configuration model + compiler — done.** New
  public package `config` (a sibling to `event`/`alert`, not
  `internal/` — see [ADR 0008](adr/0008-policy-config-boundary.md)):
  `PolicyConfig`/`PolicyRule`/`PolicyCondition` (primitive-typed
  structs mirroring `policy.Policy`/`Rule`/`Condition` exactly — no new
  matchable dimension `policy.Condition` doesn't already support),
  `(PolicyConfig).Validate() error` (fails closed on typo'd enum
  values, missing/duplicate rule names, an incomplete default, and
  more — see [SECURITY.md § Configuration-input
  validation](SECURITY.md#configuration-input-validation)), and
  `CompilePolicy(PolicyConfig) (policy.Policy, error)`. Resolved the
  open question this section previously carried ("promote the minimum
  types an external loader needs to construct a `Policy`") with a
  narrower answer than expected: `internal/policy` is **not** promoted
  at all. A Go language property — verified empirically in [ADR
  0008](adr/0008-policy-config-boundary.md), not assumed — lets an
  external caller receive a `policy.Policy` from `CompilePolicy` and
  pass it straight into `trustvian.WithPolicy` via type inference,
  without ever importing `internal/policy`. Proven end-to-end (not
  just unit-tested): `TestEndToEndConfiguredPolicyProducesConfiguredDecision`
  builds a `PolicyConfig`, compiles it, constructs a real `Engine`, and
  confirms both a matching rule's `Decision` and the configured
  default fire correctly.
- **020 Config file loader & schema v1 parsing — done.** `yaml:"..."`
  struct tags added to task 019's existing `PolicyConfig`/`PolicyRule`/
  `PolicyCondition` (purely additive; no field renamed or retyped), plus
  `Load([]byte) (PolicyConfig, error)` and
  `LoadFile(path string) (PolicyConfig, error)`. No new file-specific
  model — the YAML document decodes directly into the same public
  types task 019 defined. One new dependency,
  `go.yaml.in/yaml/v3` — its `Decoder.KnownFields(true)` rejects any
  unrecognized field unconditionally (a config-time typo must fail
  loudly, never silently fall through to a different security
  behavior), and its default `Decoder` rejects duplicate mapping keys,
  both verified by test against the real library rather than assumed.
  `LoadFile` bounds its read to 1 MiB. Proven end-to-end:
  `TestEndToEndYAMLFileChangesEngineDecision` — a real file on disk,
  loaded, compiled, and confirmed to change what a real `Engine`
  actually decides for a `RiskCritical` event, not just that it
  decodes into a struct. The real schema-v1 file shape (no `policy:`
  wrapper — the document *is* a `PolicyConfig` directly, since Alert
  configuration isn't part of this schema version):

  ```yaml
  version: v1
  default_decision: observe_only
  default_reason: no policy rules configured; observing by default
  rules:
    - name: suspicious-secret-access
      when:
        min_risk_level: critical
      decision: require_approval
      reason: critical risk requires manual approval
  ```

- **021 CLI configuration integration — done.** `trustvian analyze`
  and `trustvian baseline build` both accept an optional `--config
  <path>`, wired through the same shared engine-construction helper
  (`newEngine`, `cmd/trustvian/policy.go`): `config.LoadFile` →
  `config.CompilePolicy` → `trustvian.WithPolicy`, exactly the path any
  other external caller uses — the CLI does not import
  `internal/policy` for this path and does not reimplement any parsing
  or validation task 020 already owns. Without `--config`, behavior is
  unchanged: the CLI's pre-existing built-in `defaultPolicy()` (which
  does import `internal/policy`, legitimately — `cmd/trustvian` is
  in-module code, per [ARCHITECTURE.md](ARCHITECTURE.md)) is untouched.
  An invalid, missing, or unparseable `--config` fails the whole
  command closed — non-zero exit, no analysis report printed, no
  fallback to `defaultPolicy()` — proven by
  `TestRunAnalyzeInvalidConfigFailsClosed` and
  `TestRunAnalyzeMissingConfigFailsClosed`. A real override of the
  built-in default is proven by
  `TestRunAnalyzeConfigOverridesDefaultDecision` (a `--config`d
  catch-all-block policy turns `TestRunAnalyzeNormalEventIsAllowed`'s
  same `ALLOW` event into `BLOCK`), and `baseline build`'s own
  acceptance by `TestRunBaselineBuildAcceptsConfigFlag`. See [task
  021](tasks/021-cli-config-integration.md).
- **022 OTel Collector policy configuration integration — done.**
  `processor.Config` gains a `Policy map[string]any` field
  (`mapstructure:"policy,omitempty"`); a new `decodePolicy` function
  converts that generic map into a real `config.PolicyConfig` using
  `go-viper/mapstructure/v2` pointed at `PolicyConfig`'s existing
  `yaml:"..."` tags (task 020's), rather than embedding
  `config.PolicyConfig` directly — verified empirically that
  Collector's own confmap decoder only reads `mapstructure` tags,
  matched case-sensitively, so it would silently fail to populate any
  of `PolicyConfig`'s snake_case fields otherwise. No
  `processor.PolicyConfig`/`PolicyRule`/`PolicyCondition` type exists;
  `config.CompilePolicy` remains the sole validation/compilation
  authority. `newTrustvianProcessor`/`createTracesProcessor` now
  compile the configured Policy once, at processor-creation time, and
  fail the whole Collector's startup on an invalid explicit `policy:`
  block — proven by `TestNewFactoryFailsOnInvalidPolicy`,
  `TestConsumeTracesConfiguredPolicyChangesDecision`,
  `TestConsumeTracesDefaultPolicyUnchangedWithoutConfig`, and
  `TestConsumeTracesPolicyRuleOrderingFirstMatchWins`. Initially
  implemented and tested only against a local, unreleased root module
  (via `go.work`), since `processor/go.mod` still pointed at `v0.3.0`
  (predating `config` entirely); once `v0.5.0` was tagged and pushed to
  `origin`, `processor/go.mod` was updated to depend on it for real —
  verified with `GOWORK=off go build ./... && GOWORK=off go test ./...
  -race`, no workspace involved. See [task
  022](tasks/022-collector-config-integration.md)'s own "Release /
  Module Compatibility" section for the original verification of the
  gap this closed.
- **023 Declarative Alert configuration — done.** A new, independent
  public model in the same `config` package:
  `AlertSchemaVersionV1`, `AlertConfig{Version, Rules}`,
  `AlertRuleConfig{Name, When, Severity}`,
  `AlertConditionConfig{Decision, MinRiskLevel, ActorType,
  TargetCategory, MinAnomalyScore, MaxTrustScore}` — primitive-typed
  structs mirroring `alert.Rule`/`alert.Condition` exactly — plus
  `(AlertConfig).Validate() error`,
  `CompileAlerts(AlertConfig) ([]alert.Rule, error)`, and
  `LoadAlerts`/`LoadAlertsFile`. **Deliberately a separate document, a
  separate type, and a separate compilation path from
  `PolicyConfig`/`CompilePolicy`** — not a new field bolted onto the
  existing schema v1 document, and not a combined "schema v2" wrapping
  `policy:`/`alerts:` as siblings — see [ADR
  0009](adr/0009-alert-config-is-a-separate-document.md) for the full
  weighing of that alternative and why it was rejected for this task.
  `PolicyConfig`/`Load`/`LoadFile`/schema v1 are byte-for-byte
  unmodified: every pre-existing `config` test passes unchanged.
  Proven end-to-end:
  `TestEndToEndConfiguredAlertProducesExpectedAlert` — a real
  `AlertConfig`, compiled via `CompileAlerts`, evaluated via the real,
  unmodified `alert.Evaluate` against a real `Result` from a real
  `Engine.Analyze` call, produces the exact `Alert` (matching severity
  and fingerprint) for a matching `Result`, and no `Alert` at all for a
  non-matching one. Not wired into the CLI or the Collector processor
  — deliberately: neither has an alert-delivery flow today for a
  compiled `[]alert.Rule` to plug into (see task 023's own Non-Goals).
  See [task 023](tasks/023-declarative-alert-configuration.md).
- **024 v0.5.0 release gate — done.** The `v0.1`-style release gate
  ([task 013](tasks/013-oss-v01.md)) applied to `v0.5`: every task
  019–023 independently verified against its own acceptance criteria,
  the full root and `processor/` quality gates re-run together (not
  just per task), the documentation consistency check re-run across the
  whole `v0.5` diff, the `v0.5.0` tag published on `origin`, and
  `processor/go.mod`'s dependency bumped to it in a follow-up commit,
  verified with a clean `GOWORK=off` build/test. See [Release readiness
  — v0.5.0](#release-readiness--v050) below and [task
  024](tasks/024-v05-release-gate.md) for the exact checklist.

**Non-goals.** No general expression language, no scripting, no
boolean-combinator DSL for `when:` blocks beyond what
`policy.Condition`/`alert.Condition` already support natively — the
config format is a serialization of the existing flat matcher shape,
not a new, more powerful one. No numeric `anomaly_score`/`trust_score`
matcher on `PolicyCondition` — `policy.Condition` has no such field
today; adding one is a separate, future decision about
`internal/policy` itself (`AlertConditionConfig` *does* have
`min_anomaly_score`/`max_trust_score`, because `alert.Condition`
already supports them — this is translation, not scope expansion). No
JSON support (YAML only), no environment-variable interpolation, no
live config reload — see [task 019's](tasks/019-policy-config-model.md#non-goals),
[task 020's](tasks/020-policy-config-loader.md#non-goals), and [task
023's](tasks/023-declarative-alert-configuration.md#non-goals) own
Non-Goals sections for the complete, precise lists.

**Dependencies.** `v0.1` (stable `Policy`/`Decision`) — satisfied.
Task 020 depends on task 019 (decodes into its existing types). Tasks
021 and 022 each depend on both 019 and 020 directly (they call
`config.LoadFile`/`CompilePolicy`/`PolicyConfig` unmodified) and not on
each other. Task 023 depends on `v0.4.0`/`alert.Rule` being stable
(it's the one task in this milestone that does) and is otherwise
independent of 019–022 — it shares no document, type, or compilation
path with `PolicyConfig` (see ADR 0009). Task 022 additionally depended
on a real Trustvian core release newer than `v0.4.0` existing on this
project's remote before its own code could be wired into
`processor/go.mod`'s committed dependency; task 024 (the release gate)
produced that release and resolved this dependency.

**Acceptance criteria.** See [task
019](tasks/019-policy-config-model.md)'s, [task
020](tasks/020-policy-config-loader.md)'s, [task
021](tasks/021-cli-config-integration.md)'s, [task
022](tasks/022-collector-config-integration.md)'s, and [task
023](tasks/023-declarative-alert-configuration.md)'s own Acceptance
Criteria sections — all fully met, verified by `go test ./... -race
-count=1`, `go test -bench=. -benchmem ./config/...`, `go list -deps`
confirming no core-engine import of `config` or
`go.yaml.in/yaml/v3`/`alert`, and (for task 022 specifically)
`processor/`'s own `GOWORK=off go build ./... && GOWORK=off go test
./... -race` against the real, published `v0.5.0` dependency. **The
milestone is shipped** — see [Release readiness —
v0.5.0](#release-readiness--v050) immediately below for the exact
verification.

### Release readiness — v0.5.0

```text
v0.5.0 implementation release-ready: YES
v0.5.0 shipped: YES
```

All code, tests, and documentation for tasks 019–023 are complete, and
the release sequence [task 024](tasks/024-v05-release-gate.md) laid out
has run:

1. ~~Resolve the pre-existing local-only `v0.5.0` tag.~~ **Done.** The
   stale local tag (pointing at an earlier commit containing only
   tasks 019–020) was moved to `main`'s tip (containing 019–023) and
   pushed.
2. ~~Push the tag to `origin`.~~ **Done** —
   `git ls-remote --tags origin` lists `v0.5.0`, and
   `go get github.com/Trustvian/trustvian@v0.5.0` (from outside this
   repository) succeeds.
3. ~~Update `processor/go.mod`.~~ **Done**, in a small, separate
   follow-up commit after the tag was live (this genuinely could not
   happen before the tag existed: `go.sum` entries are content hashes
   of the published module, verified empirically that nothing can
   compute them for an unpublished version).
4. ~~Verify that follow-up cleanly.~~ **Done** —
   `GOWORK=off go mod download && GOWORK=off go list -m
   github.com/Trustvian/trustvian` reports `v0.5.0`; `processor/`'s
   full quality gate (`go build`/`go vet`/`go test -race ./...`) passes
   with no `go.work` involved.
5. ~~Publish GitHub release notes from the `CHANGELOG.md § v0.5.0`
   entry.~~ **Done** — `gh release list` shows `v0.5.0` ("Policy &
   Configuration") published and marked `Latest` on GitHub.

Nothing remains outstanding for this release.

## v0.6 — Behavioral Detection Depth

**`v0.6.0` is shipped.** [Task 025](tasks/025-sequence-analysis-foundation.md)
(Sequence Analysis Foundation), [task 026](tasks/026-transition-rarity.md)
(Transition Rarity), [task 027](tasks/027-bounded-ngram-detection.md)
(Bounded n-gram Detection), [task
028](tasks/028-markov-transition-scoring.md) (Markov Transition
Scoring), and [task 029](tasks/029-v06-stabilization-release-gate.md)
(Stabilization & Release Gate) are all done. Task 029's own audit found
no release blocker — see its own Findings section for the full
checklist (double-counting protection re-verified, state bounds
confirmed, public API and dependencies confirmed unchanged since
`v0.5.0`, one genuine documentation-drift item found and fixed). The
`v0.6.0` tag is pushed to `origin` and its GitHub release ("Behavioral
Detection Depth") is published and marked `Latest` — verified directly
via `gh release list`, not assumed. The rest of
the milestone below remains unscoped, per this roadmap's own "small
vertical slices" principle.

**Objective.** OSS detection should not stop at individual-event
anomaly signals. Promote sequence-aware detection from
[Future research](#future-research) into an intentional, deterministic
OSS capability — the one item this document's prior "Future research"
section named that a production-usable OSS product genuinely needs
before `v1.0`, per this roadmap's now-explicit product boundary.

**Scope** (task files [025](tasks/025-sequence-analysis-foundation.md),
[026](tasks/026-transition-rarity.md),
[027](tasks/027-bounded-ngram-detection.md),
[028](tasks/028-markov-transition-scoring.md), and
[029](tasks/029-v06-stabilization-release-gate.md) — every task this
milestone currently scopes is done):

- **025 Sequence Analysis Foundation — done.** `internal/baseline.Baseline`
  gains `LastFingerprintID`/`LastFingerprintTime` (an actor's most
  recent Fingerprint and its ordering-guarded timestamp);
  `FingerprintStats` gains `PredecessorCounts` (bounded at 64 distinct
  entries — see [ADR 0010](adr/0010-bounded-process-local-sequence-state.md)).
  `internal/anomaly` gains one new signal, `transition_deviation`
  (opt-in via `Config.TransitionWeight`, defaulting to `0` — the exact
  precedent `FrequencyWeight`/`TimePatternWeight` already set):
  has this exact predecessor `Fingerprint.ID` ever led to this
  destination before, for this actor? No `SequenceStore`, no
  `SequenceKey`, no `SequenceAnalyzer` — the existing
  `Baseline`/`Store` infrastructure carries the new state, under the
  same per-`Key` concurrency/persistence guarantees, with zero changes
  to `engine.go`, `internal/store`, `internal/policy`, `internal/trust`,
  `alert`, `config`, the CLI, or `processor/`. Proven end-to-end
  through the real, gated `Analyze`+`Observe` loop:
  `TestAnalyzeTransitionDeviationEndToEnd` (see
  [engine_test.go](../engine_test.go)) shows a never-observed
  `read -> delete` transition firing `transition_deviation` without
  `categorical_novelty` also firing (destination independently
  familiar via a different predecessor), and the actor's actual normal
  path (`read -> update`) carrying no such signal. See [Sequence
  Analysis](../sequence-analysis.md) for the full design and [task
  025](tasks/025-sequence-analysis-foundation.md) for benchmarks and
  the complete test list.
- **026 Transition Rarity — done.** Evolves task 025's binary
  `transition_deviation` (seen vs. never seen) into a graded measure for
  transitions that *have* been seen: a new signal, `transition_rarity`
  (opt-in via `Config.TransitionRarityWeight`, defaulting to `0`),
  computes `1 - (PredecessorCounts_B[A] / OutgoingTransitionTotal_A)` —
  an empirical relative frequency, deliberately never called a
  "probability." `FingerprintStats` gains exactly one new scalar,
  `OutgoingTransitionTotal` (no new map, no new cardinality dimension):
  `PredecessorCounts` alone (task 025) is destination-oriented and
  answers `P(predecessor|destination)`, not the
  `P(destination|predecessor)` a transition detector actually needs —
  resolving that orientation question correctly, before writing any
  code, is this task's central contribution. See [ADR
  0011](adr/0011-transition-rarity-statistic-and-orientation.md) for
  the full proof. Gated on a minimum-support threshold
  (`Config.MinTransitionObservations`, default `20`) below which the
  signal does not fire — a tiny sample is evidence of insufficient data,
  not evidence of rarity. `transition_deviation` and `transition_rarity`
  remain mutually exclusive by construction (never both fire for the
  same transition). Zero changes to `engine.go`, `internal/store`,
  `internal/policy`, `internal/trust`, `alert`, `config`, the CLI, or
  `processor/` — identical reuse discipline to task 025. Proven
  end-to-end through the real, gated `Analyze`+`Observe` loop:
  `TestAnalyzeTransitionRarityEndToEnd` (see
  [engine_test.go](../engine_test.go)). See [Sequence
  Analysis § Transition rarity](../sequence-analysis.md#transition-rarity-v06-task-026)
  and [task 026](tasks/026-transition-rarity.md) for benchmarks and the
  complete test list.
- **027 Bounded n-gram Detection — done.** Extends order-awareness one
  step further back: given a 3-gram `A -> B -> C`, has this exact
  (grandparent, predecessor) pair ever led to this destination before,
  and if so, how commonly? Two new signals, `ngram_deviation`/
  `ngram_rarity` (opt-in via `Config.NGramWeight`/
  `Config.NGramRarityWeight`, both defaulting to `0`), mirroring tasks
  025/026's own binary-then-graded pattern one level up. A fixed
  3-gram only — no configurable `n`, no Markov model (per the task's
  own explicit non-goals). `Baseline` gains exactly one more scalar,
  `PreviousFingerprintID`; `FingerprintStats` gains two new,
  *independently* bounded maps, `TrigramCounts` (destination-keyed by
  a `(grandparent, predecessor)` pair) and `TrigramContinuationTotal`
  (predecessor-keyed by grandparent) — a scalar (as
  `OutgoingTransitionTotal` is for the 2-gram case) cannot answer a
  3-gram's denominator, since a single fingerprint can be the
  "predecessor" half of many distinct pairs; see [ADR
  0012](adr/0012-bounded-trigram-behavioral-context.md) for the full
  orientation proof and for a genuine correctness subtlety this task's
  own review caught: `TrigramContinuationTotal`'s cardinality is *not*
  automatically bounded by `PredecessorCounts`'s existing cap, and
  needed its own explicit, independent bound. Zero changes to
  `engine.go`, `internal/store`, `internal/policy`, `internal/trust`,
  `alert`, `config`, the CLI, or `processor/` — identical reuse
  discipline to tasks 025/026. Proven end-to-end through the real,
  gated `Analyze`+`Observe` loop:
  `TestAnalyzeNGramEndToEnd` (see [engine_test.go](../engine_test.go)),
  and — the task's own mandatory proof that this detector adds genuine
  information beyond tasks 025/026 —
  `TestScoreNGramDeviationDetectsNovelTrigramDespiteFamiliarPairwiseTransitions`
  (see `internal/anomaly/anomaly_test.go`): both individual pairwise
  hops familiar, complete 3-gram never observed,
  `ngram_deviation` still fires. See [Sequence Analysis § Bounded
  3-gram detection](../sequence-analysis.md#bounded-3-gram-detection-v06-task-027)
  and [task 027](tasks/027-bounded-ngram-detection.md) for benchmarks
  and the complete test list.
- **028 Markov Transition Scoring — done.** Before writing any code,
  this task answered a mandatory question: what would Markov scoring
  provide that task 026's `transition_rarity` does not already? The
  answer, proven not just asserted: **nothing new, as evidence.**
  `transition_rarity = 1 - P(B|A)` and the textbook Markov "surprisal"
  statistic `-log(P(B|A))` are both strictly monotonic functions of the
  identical `count/total` frequency — proven directly by
  `TestMarkovSurprisalIsMonotonicReparameterizationOfRarity` (see
  [ADR 0013](adr/0013-first-order-markov-surprisal-without-duplicate-evidence.md)
  for the full analysis). What genuinely differs is curve shape, not
  evidence: `-log2(frequency)`, bounded into `[0,1)`, preserves more
  resolution across the rare tail than the linear `1-frequency` mapping
  does. The new signal, `markov_surprisal` (opt-in via
  `Config.MarkovWeight`, defaulting to `0`), is therefore built as an
  **alternative, mutually exclusive scoring curve** over task 025/026's
  own existing state (`PredecessorCounts`/`OutgoingTransitionTotal` —
  no new `Baseline`/`FingerprintStats` field, no new bound), not a
  second, independently-weighted signal: `anomaly.Score` forces
  `transition_rarity`'s own contribution to zero whenever
  `MarkovWeight > 0`, enforced in code — proven by
  `TestScoreMarkovAndTransitionRarityAreMutuallyExclusiveInScoring` —
  so a caller cannot double-count this evidence by any combination of
  the two weights. Reuses `Config.MinTransitionObservations` exactly
  (no separate Markov-specific threshold); never evaluates a `count ==
  0` transition (that remains `transition_deviation`'s domain), so
  `-log2(0)` is never computed and `Inf`/`NaN` are impossible by
  construction. Zero changes to `engine.go`, `internal/baseline`,
  `internal/store`, `internal/policy`, `internal/trust`, `alert`,
  `config`, the CLI, or `processor/`. See [ADR
  0013](adr/0013-first-order-markov-surprisal-without-duplicate-evidence.md)
  and [task 028](tasks/028-markov-transition-scoring.md) for the full
  mathematical definition, benchmarks, and the complete test list.
- **029 Stabilization & Release Gate — done.** Not new functionality —
  a verification pass tying tasks 025–028 together, mirroring [task
  024](tasks/024-v05-release-gate.md)'s shape for `v0.5`. Its own
  highest-priority item: re-verifying, from source and by a fresh
  full-combination benchmark
  (`BenchmarkEngineAnalyzeFullBehavioral`), that
  `transition_rarity`/`markov_surprisal`'s mutual-exclusion mechanism
  (task 028/ADR 0013) holds under every realistic weight combination,
  not just the pairwise scenarios task 028's own tests constructed —
  no defect found. Also audited and resolved: one genuine
  documentation-drift finding (this document's own stale "first task
  done" wording, corrected), a confirmed-intentional (not a blocker)
  decision that `v0.6` behavioral configuration stays programmatic-only
  for now (consistent with `FrequencyWeight`/`TimePatternWeight`'s own
  pre-`v0.6` precedent), and a confirmed-intentional (not a defect)
  finding that every v0.6 signal computes regardless of its own weight
  — the same explainability-driven convention every prior opt-in signal
  in this package already follows, measured and documented rather than
  "optimized" away. Zero new dependencies and zero public API changes
  since `v0.5.0`, verified directly. See [task
  029](tasks/029-v06-stabilization-release-gate.md) for the complete
  findings list and release checklist.
- **Already covered by existing signals**, named here only to close
  the gap between this document's language and the original spec's
  ([`trustvian-project-spec.md` §
  6](../trustvian-project-spec.md#6-sequence-analysis)) — no new work
  needed: "target deviation" and "destination deviation"
  (`categorical_novelty` on `Target`/`sensitive_target`), "dependency
  deviation" (`categorical_novelty` on an unexpected internal/external
  call — see
  [`examples/unexpected-dependency`](../examples/unexpected-dependency/README.md)),
  "operation deviation" (`categorical_novelty` on `Operation`), and
  "frequency"/"time-of-day deviation" (`frequency_deviation`,
  `time_pattern_deviation`, both already shipped).
- **Not yet done — nothing currently scoped.** Tasks 025–029 have
  delivered pairwise transition detection (binary and graded), bounded
  higher-order (3-gram) detection, a first-order Markov severity curve
  over the same evidence, and a stabilization pass confirming no
  release blocker remains — a complete build-out of this milestone's
  "sequence-aware detection" objective. `v0.6.0` itself (the tag and
  GitHub release) has not been created — that is a human action outside
  this milestone's own task scope, not a further feature slice. A
  genuinely new detection capability beyond what tasks 025–028 already
  built would need its own concrete, evidence-driven justification and
  would belong to a later milestone (see `v0.7` below), not a
  speculative addition to this list.

**Preferred architecture** (explicit, to close off scope creep before
it starts):

```text
Deterministic/statistical engine   (existing signals + sequence deviation)
        ↓
Sequence-aware detection            (this milestone)
        ↓
Optional ML plugins/research        (never a core dependency — see Non-goals)
```

**Non-goals.** ML is not required anywhere in this milestone or the
core detection path generally — n-gram/Markov transition modeling are
deterministic/statistical, not ML. Graph-based behavioral deviation and
ML-based sequence models remain [Future research](#future-research),
revisited only if a concrete design and consumer need emerge — this
milestone does not build them speculatively. Day-of-week seasonality is
a separate, independent EWMA dimension
([task 017](tasks/017-baseline-time-patterns.md)'s own Non-Goals
already established this) and stays in
[Future research](#future-research) rather than being folded in here.
No CLI/Collector/config-schema integration for `transition_deviation`,
`transition_rarity`, `ngram_deviation`, `ngram_rarity`, or
`markov_surprisal` yet — see
[task 025's](tasks/025-sequence-analysis-foundation.md#non-goals),
[task 026's](tasks/026-transition-rarity.md#non-goals), [task
027's](tasks/027-bounded-ngram-detection.md#non-goals), and [task
028's](tasks/028-markov-transition-scoring.md#non-goals) own Non-Goals
sections. No configurable n-gram length either — task 027 ships a
fixed 3-gram only (see [ADR 0012](adr/0012-bounded-trigram-behavioral-context.md#why-3-grams-first-not-arbitrary-n)).

**Dependencies.** `v0.1` (stable `Baseline`/`Anomaly` — a sequence
signal reads a fingerprint's recent history the same way
`frequency_deviation` reads its interval history). Independent of
`v0.5`/`v0.7`.

**Acceptance criteria.** See [task
025](tasks/025-sequence-analysis-foundation.md)'s, [task
026](tasks/026-transition-rarity.md)'s, [task
027](tasks/027-bounded-ngram-detection.md)'s, [task
028](tasks/028-markov-transition-scoring.md)'s, and [task
029](tasks/029-v06-stabilization-release-gate.md)'s own Acceptance
Criteria sections — all fully met, verified by `go
test ./... -race -count=1` and `go test -bench=. -benchmem ./...`
showing `BenchmarkEngineAnalyze`'s allocation profile unchanged across
all five tasks, and by task 029's own release-readiness audit finding
no blocker. **The milestone is shipped** — `v0.6.0` is a real, tagged,
published release (`gh release list` shows it marked `Latest`),
matching the same tag/GitHub-release step `v0.5.0`'s own gate ([task
024](tasks/024-v05-release-gate.md)) required before that milestone
could be called shipped.

## v0.7 — AI Agent Behavioral Security

**Objective.** Extend the *existing* event model for richer AI-agent
behavioral context, without building a separate security engine for
agents — per the original brief's own explicit instruction, agents
remain "another behavioral source" through the same pipeline:

```text
Event → Features → Fingerprint → Baseline → Anomaly → Trust → Policy → Decision
```

**Task 014 — AI Agent Event/Context Foundation — is done.** [Task
file 014](tasks/014-ai-agent.md) added three optional `Context`
fields — `SessionID` (session grouping), `DelegatedFrom` (single-hop
agent-to-agent delegation), and `ApprovalStatus` (a recorded
human-approval fact, typed the same way `OperationDirection` already
was: optional, unvalidated, not yet read by `features.Extract`) — and
proved, rather than merely designed, the milestone's two central
claims:

- **Session identity never becomes behavioral identity.**
  `TestAnalyzeAgentSessionIDDoesNotExplodeBaseline` runs 1000 events
  with 1000 distinct `SessionID`s and identical behavior through the
  real, gated `Engine.Analyze`/`Observe` loop and confirms exactly one
  `Fingerprint` accumulates all 1000 observations — `SessionID` never
  enters `baseline.Key` or `Fingerprint.Compute`.
- **Tool-sequence analysis needs no new algorithm.**
  `TestAnalyzeAgentToolSequenceNoveltyDetectedByExistingEngine` trains
  `search → secret.read` and `secret.read → external.post` as
  independently familiar via separate contexts, then shows `v0.6`'s
  existing bounded 3-gram signal (`ngram_deviation`, task
  [027](tasks/027-bounded-ngram-detection.md)) still flags
  `search → secret.read → external.post` as anomalous — proving the
  higher-order novelty is caught by the same signal any other actor's
  operation sequence would use, with zero agent-specific code.

Agent-to-agent calls, tool calls, external destinations, file access,
database access, and secret access were all already representable
through the generic `Event`/`Actor`/`Target` model
(`ActorTypeAIAgent`, `OperationCategoryTool`,
`TargetCategoryExternal`/`Database`) before this task — it added only
the session/delegation/approval *correlation* fields, not new
representational capability the pipeline lacked. See [ADR
0014](adr/0014-ai-agents-as-first-class-behavioral-actors.md) for the
full domain-model reasoning.

**Non-goals (held).** No agent-specific `Fingerprint`/`Baseline`/
`Anomaly` implementation was introduced — the same `internal/*`
packages kept working unchanged for agent-sourced events, proven by
reusing existing signals against the extended `Event` shape rather
than writing parallel ones. No second security engine, no dedicated
agent package beyond the three optional `Context` fields.

**Dependencies.** `v0.1` (stable `Event`/public API — extending
`Event` after `v0.1` shipped meant doing it in a backward-compatible
way, adding optional fields only). Benefited from `v0.6`'s
sequence-deviation signal, which task 014 reused rather than
duplicated.

**Acceptance criteria.** See [014-ai-agent.md](tasks/014-ai-agent.md).

**Task 030 — Approval-Aware Policy Semantics — is done.** [Task file
030](tasks/030-approval-aware-policy-semantics.md) gave
`ApprovalStatus` its first real consumer, entirely inside
`internal/policy` — zero new anomaly signal, zero new pipeline stage.
`policy.Input`/`Condition` and `config.PolicyCondition` each gained one
new `ApprovalStatus` field, letting the *existing* `Rule.Unless`
mechanism express "this operation requires approval"
(`Unless: &Condition{ApprovalStatus: ApprovalApproved}`) with no new
Rule-level primitive. This task's own brief demanded an answer to two
design questions before any code was written, both now proven by test,
not just documented:

- **Approval belongs to `policy`, not `anomaly`.** `shell.execute` can
  be completely familiar to a `Baseline` and still lack a required
  approval — `TestAnalyzeAgentApprovalPolicyAllowsApprovedDeniesUnapproved`
  trains it familiar first, then shows the identical, low-risk
  behavior still gets blocked without `Approved` evidence.
  `TestAnalyzeApprovalPolicyBehavioralScoreIndependence` proves
  `Anomaly.Score`/`Trust.Score`/`Fingerprint.ID` are byte-for-byte
  identical regardless of `ApprovalStatus` — only `Decision` differs.
- **Policy owns the requirement; the event supplies only evidence.**
  `TestEvaluateApprovalPolicyAuthorityEventCannotOverridePolicy` proves
  an event self-declaring `ApprovalNotRequired` cannot exempt itself
  from a `Policy` rule that requires approval, and
  `TestEvaluateApprovalFailSafeOnMissingEvidence` proves missing
  evidence (`ApprovalUnspecified`) fails closed to `BLOCK` rather than
  silently passing.

`ApprovalStatus` remains untrusted, self-reported evidence — this task
adds no cryptographic verification, no OAuth/IAM integration, and no
coupling to `Actor.IdentityConfidence`; see [ADR
0015](adr/0015-approval-as-policy-evidence-not-behavioral-anomaly.md).
The mechanism is domain-generic, not coupled to `ActorTypeAIAgent`:
`TestAnalyzeApprovalPolicyGenericNotHardCodedToAIAgent` proves the
identical rule gates a `service` actor's matching operation the same
way. A `Policy` with no approval-aware rule at all is byte-for-byte
unaffected (`TestEvaluateNoApprovalRuleConfiguredIsUnaffectedByApprovalStatus`)
— this task is purely additive, and the CLI/OTel Collector processor
require zero code changes, since both already delegate to
`config.CompilePolicy`/`internal/policy`.

**Acceptance criteria.** See
[030-approval-aware-policy-semantics.md](tasks/030-approval-aware-policy-semantics.md).

**Task 031 — Delegation Behavioral Semantics — is done.** [Task file
031](tasks/031-delegation-behavioral-semantics.md) answers this
milestone's own design question: *can Trustvian identify unusual
delegation relationships using bounded behavioral learning without
treating self-reported delegation metadata as authenticated
provenance?* Yes — and it required no new pipeline stage, no
delegation graph, and no new port method. `features.VolatileFeatures`
gained one field (`DelegatedFrom`, read from `Context.DelegatedFrom`);
`baseline.Baseline` gained one bounded map (`DelegatorCounts`, capped
at 64 distinct delegators, mirroring `PredecessorCounts`'s exact
shape one level up — actor-scoped, not per-operation, since "who
normally delegates to this actor" is a property of the actor, not of
any one thing it does); `internal/anomaly` gained one opt-in signal,
`delegation_deviation` (binary seen/unseen, mirroring
`transition_deviation`'s exact shape — deliberately not a rarity
signal). Two design questions this task's own brief demanded be kept
separate, both proven by test, not just documented:

- **Behavioral familiarity is not authorization.**
  `TestAnalyzeDelegationApprovalIndependence` proves delegation
  evidence and approval-policy evidence never entangle: a familiar
  delegator does not exempt an operation from an approval requirement,
  and a novel delegator does not itself block an operation approval
  already satisfies.
- **`DelegatedFrom` stays unauthenticated, self-reported input.**
  `delegation_deviation` answers "is this unusual for this actor?",
  never "is this delegation authentic?" — no cryptographic
  verification, no provenance check, exactly the boundary
  `docs/SECURITY.md § AI Agent behavioral security`'s "Delegation
  abuse" entry already documented before this task existed.

The existing learning-eligibility gate protects delegation state for
free: `TestAnalyzeDelegationPoisoningIneligibleEventsDoNotTrain`
proves repeated BLOCKed delegation from a never-seen delegator never
becomes "familiar" through repetition alone.
`TestAnalyzeDelegationActorIsolation` and
`TestAnalyzeDelegationFingerprintStability` confirm the identical
actor-isolation and Fingerprint-independence guarantees every other
`v0.6`/`v0.7` signal already has. See [ADR
0016](adr/0016-delegation-as-behavioral-evidence-not-provenance.md).

**Acceptance criteria.** See
[031-delegation-behavioral-semantics.md](tasks/031-delegation-behavioral-semantics.md).

**Task 032 — Agent Security Scenario Validation — is done.** [Task
file 032](tasks/032-agent-security-scenario-validation.md) validated
the combined system (030 + 031 + existing `v0.6`/`v0.1` signals)
against five realistic scenarios plus a combined case, adding
**zero** new detectors: unexpected privileged tool use
(`categorical_novelty`/`transition_deviation`), a sensitive
read-then-external-post sequence (`ngram_deviation`), an approval
violation (task 030's `Policy` mechanism), an unexpected delegator
(task 031's `delegation_deviation`), external-destination drift
(`categorical_novelty` via `Target.Category`), and one combined
scenario exercising all three evidence types (delegation, sequence,
approval) together — proven orthogonal, explainable, and bounded, not
merely asserted, by
`TestScenarioCombinedDelegationSequenceApproval` in
[`scenario_test.go`](../scenario_test.go). A dedicated poisoning
regression, a session-cardinality re-run, a non-agent regression, and
an identity-confidence-independence check round out the eleven new
tests.

**One genuine gap surfaced by task 032, closed by task 033:**
`anomaly.Config` (unlike `policy.Policy`, which got a public
`config.CompilePolicy` path in `v0.5`) had no public-API equivalent —
an OSS consumer outside this module could not raise
`DelegationWeight`/`NGramWeight`/any `v0.6`/`v0.7` signal weight above
its default-`0` opt-in value, by any means. Task 032 reported this
rather than patching it speculatively mid-scenario-audit; task 033
below is where it was actually fixed.

**Task 033 — v0.7 Stabilization & Release Gate — is done.** [Task file
033](tasks/033-v07-stabilization-release-gate.md) closed the gap task
032 found: `config.AnomalyConfig` (new), compiled by
`config.CompileAnomaly` into the exact `anomaly.Config` value
`trustvian.WithAnomalyConfig` already accepted — a third independent
document alongside `PolicyConfig`/`AlertConfig`, mirroring
`CompilePolicy`'s exact shape (see [ADR
0017](adr/0017-public-anomaly-configuration-boundary.md)). Every
existing `anomaly.Config` field is now publicly configurable; none was
newly invented. `CompileAnomaly(AnomalyConfig{})` reproduces
`anomaly.DefaultConfig()` byte-for-byte, proven by
`TestCompileAnomalyZeroValueMatchesDefaultConfig` — existing callers
who configure nothing see unchanged behavior. The CLI gained a matching
`--anomaly-config <path>` flag on both `analyze` and `baseline build`.
[`examples/ai-agent-security`](../examples/ai-agent-security/) was
rewritten to demonstrate the full combined scenario (delegation +
sequence + approval) through public config alone, with a
`main_test.go` proving it from within the genuinely separate `examples`
Go module — the mandatory external-consumer proof, run as real
`go test`, not just `go build`. A full `v0.7` regression re-run (all of
tasks 014/030/031/032's own tests, plus every `v0.6` sequence test)
passed under `go test ./... -race -count=1` with zero changes to
`internal/anomaly`, `internal/baseline`, `internal/policy`, or
`engine.go` itself.

**Acceptance criteria.** See
[033-v07-stabilization-release-gate.md](tasks/033-v07-stabilization-release-gate.md).

**`v0.7` is release-ready.** All five tasks (014, 030, 031, 032, 033)
are done; the one blocker task 032 found is resolved, not deferred.
`v0.7.0` has not been tagged — tagging and publishing the release
remain a separate, explicit human action, matching every prior
milestone's own gate (`v0.5.0`'s [task 024](tasks/024-v05-release-gate.md),
`v0.6.0`'s [task 029](tasks/029-v06-stabilization-release-gate.md)).

**015** and **016** remain reserved for MCP and Control respectively —
neither was touched or reused by any `v0.7` task.

## v0.8 — Production Runtime & Storage

**Objective.** OSS should be deployable as a real production system,
not only a library and a CLI against a local file.

**Scope** (no task file yet):

- **A production-grade persistent `Store` candidate.** Today:
  `store.InMemory`, `store.FileStore` (JSON, synchronous `fsync`,
  documented as the MVP's *only* persistent implementation — see
  [ADR 0006](adr/0006-file-backed-persistent-store.md)). This
  milestone documents — does not yet implement — the preferred next
  `Store` implementation:

  ```text
  Store interface (internal/store)
   ├─ InMemory      (existing)
   ├─ FileStore     (existing)
   └─ PostgreSQLStore   (documented candidate, this milestone)
  ```

  PostgreSQL is the preferred candidate specifically for: durability
  and transactional guarantees `FileStore`'s single-file-plus-rename
  approach can't offer at higher write volume; broad operational
  familiarity (most teams already run and back up Postgres); real
  queryability (ad hoc inspection of learned baselines without writing
  a custom tool against `FileStore`'s JSON blob); and OSS-friendliness
  (no proprietary licensing, a mature Go driver ecosystem). This is a
  documented preference, not a commitment made blindly — the actual
  schema/transaction design is this milestone's own task file's job,
  not this roadmap edit's.
- **A production-like deployment path**: `Docker` + `Docker Compose`
  wiring together the OTel Collector, Trustvian (as a library inside a
  consuming service, or via the Collector processor), and the
  persistent store above — a documented, runnable reference deployment,
  not a Helm chart.

**Non-goals.** No Redis, Kafka, ClickHouse, or OpenSearch — none of
these have a concrete milestone justification today, and introducing
one "because security products use it" is exactly the infrastructure
creep this roadmap's principles reject (see [§ Architecture
constraints](#the-oss--enterprise-product-boundary) and
[CLAUDE.md](../CLAUDE.md)'s "avoid... premature microservices").
Kubernetes/Helm support explicitly follows *only* once a real
deployment need justifies it — not scoped here, not a `v1.0` blocker.

**Dependencies.** `v0.1` (the `Store` interface already exists and is
narrow — [ADR 0004](adr/0004-narrow-store-port-in-memory-only.md)).
Independent of `v0.5`–`v0.7`.

**Acceptance criteria.** Defined when this milestone's own task file is
written — not before.

## v0.9 — Operational Readiness

**Objective.** Production engineering hygiene, so `v1.0` is a real
release, not just a version number bump.

**Scope** (no task file yet; items are capabilities to have, not a
prescription of specific tooling beyond what's already established in
this repository):

```text
CI, build gates (go vet / gofmt / go test -race — already run manually
  every task, this milestone automates them)
Release automation
Multi-arch binaries/images
Docker image (packaging the reference deployment v0.8 designed)
SBOM
Dependency / vulnerability scanning
Signed artifacts/images where practical
SemVer discipline (already documented — see CHANGELOG.md § Public API
  compatibility promise; this milestone is enforcing it in CI, not
  inventing it)
Module version consistency (root/processor/examples go.mod alignment —
  see Version and Module Consistency Review notes below)
Health checks, readiness checks
Self-observability (Trustvian observing its own runtime health, not to
  be confused with the OTel *input* adapter)
Resource limits, graceful shutdown
Backup/restore documentation (for the v0.8 persistent store)
Upgrade/migration documentation
Security disclosure process
CONTRIBUTING.md, issue templates
Production examples (beyond examples/'s current SDK-usage demos)
```

**Non-goals.** No specific tool mandated here beyond what this
repository already uses (`go vet`, `gofmt`, `go test -race`, the
existing `Makefile` targets) unless a concrete need is identified when
this milestone starts.

**Dependencies.** `v0.8` (the Docker image packages that milestone's
deployment path). Otherwise independent of `v0.5`–`v0.7`.

**Acceptance criteria.** Defined when this milestone's own task file is
written — not before.

## v1.0 — Production-Ready OSS

**Objective.** `v1.0.0` is a real production-readiness milestone, not
merely the next version number. It is the point at which Trustvian OSS
is a complete, standalone, production-usable behavioral security
product per [§ The OSS / Enterprise product
boundary](#the-oss--enterprise-product-boundary) — everything an
operator needs to detect, score, decide, alert, integrate, and run
Trustvian in production, without Trustvian Control.

**Release gate**, organized by theme (each verified against the real
repository state when this milestone is evaluated, the same "verified
by reading source, tests, and benchmarks, not assumed" discipline this
whole document already follows):

- **Correctness** — unit/integration/end-to-end tests across every
  package named in `v0.1`–`v0.9`; `go test -race ./...` clean;
  deterministic behavior proven by test wherever a formula or algorithm
  is involved (this document's existing "test the documented formula"
  standard, applied to every new signal added through `v0.6`).
- **Performance** — every hot path benchmarked (extending
  [PERFORMANCE.md](PERFORMANCE.md)'s existing table through `v0.6`'s
  sequence signal and `v0.5`'s config loading path); bounded memory
  documented for the `v0.8` persistent store; a load test against the
  `v0.8` reference deployment.
- **Security** — every threat in [SECURITY.md](SECURITY.md)'s test
  index still passing, extended to cover: baseline poisoning against
  the new sequence signal (`v0.6`), untrusted telemetry at the config
  boundary (`v0.5`'s loader must fail closed on malformed config, the
  same discipline `policy.Policy.Evaluate` already has), cardinality/
  resource exhaustion for sequence state (bounded per-fingerprint
  memory, the same discipline `HourActivity`'s fixed `[24]float64`
  already established), webhook security (already implemented —
  [SECURITY.md § Alert/notification delivery
  integrity](SECURITY.md#alertnotification-delivery-integrity)),
  secrets handling (`v0.5`'s config format and `v0.8`'s persistent
  store connection credentials), and dependency vulnerability scanning
  (`v0.9`).
- **Operations** — `v0.8`'s deployment path, health/readiness checks,
  persistence, and self-observability all real and documented, not
  aspirational.
- **Documentation** — install, configure, deploy, integrate,
  troubleshoot, and upgrade all have a real, verified document (most
  already exist for the current feature set — [Getting
  Started](getting-started.md), [SDK Guide](sdk-guide.md), [CLI
  Guide](cli-guide.md), [Policy Guide](policy-guide.md); `v0.5`–`v0.9`
  each add their own as they land, per each milestone's own
  Documentation practice).

**Dependencies.** `v0.1`–`v0.9`, or the applicable subset — this
milestone is a gate over what preceded it, not a place to invent new
scope. Anything discovered missing at gate time becomes a new task
under the milestone it actually belongs to, not a `v1.0`-specific
exception.

**Non-goals.** Everything explicitly Enterprise/Control per [§ The OSS
/ Enterprise product boundary](#the-oss--enterprise-product-boundary)
above — `v1.0` is not gated on Trustvian Control existing, on MCP
existing (see [Trustvian MCP](#trustvian-mcp) below), or on any
Kafka/Redis/Kubernetes infrastructure without its own concrete
milestone justification.

**Acceptance criteria.** All release-gate themes above verified
against the real repository state; defined precisely (specific test
names, specific benchmark numbers) when this milestone's own task file
is written, mirroring how every prior milestone's acceptance criteria
were only fixed once its task file existed.

## Trustvian MCP

**Objective.** Expose Trustvian's read/query surface
(`get_behavior`, `get_trust_score`, `explain_decision`, etc.) to AI
agents and developer tooling via MCP. This is an OSS integration
surface — it fits the architecture the same way `cmd/trustvian` and
`internal/otel` already do (a thin adapter depending on the core, the
core never depending on it) — but it is explicitly **not the
Trustvian security engine**, and it does not gate OSS `v1.0`.

**Direction, stated explicitly because it's easy to get backwards:**

```text
MCP → Trustvian Engine       (correct: MCP wraps/queries/evaluates through the core)
Trustvian Engine → MCP       (wrong: the core must never depend on or know about MCP)
```

**Scope** (task file [015](tasks/015-trustvian-mcp.md)): a new adapter
(a new `cmd/`-style binary or a separate module, mirroring how
`internal/otel` and `cmd/trustvian` already depend on the core without
the core knowing they exist) implementing an MCP server backed by
`Engine`.

**Non-goals.** No new decision-making logic in the MCP layer itself —
it is a read/query and evaluate-via-existing-`Engine` adapter, not a
second policy engine. **Not required for `v1.0`**: MCP is a valuable
integration surface for AI-tooling ecosystems, not a load-bearing part
of "detect, score, decide, alert, integrate, run" — [§ v1.0
above](#v10--production-ready-oss) does not list it as a release-gate
item, and nothing about production-usability depends on it existing.
Revisit this only if a concrete, strong architectural or product
reason emerges to promote it — not scheduled ahead of that signal.

**Dependencies.** v0.1 (a stable public API and `Result` shape to
expose), ideally after `v0.7` (richer AI-agent context to query).

**Acceptance criteria.** See [015-trustvian-mcp.md](tasks/015-trustvian-mcp.md).

---

## Control / Enterprise phase

**Objective.** Only after the OSS core has demonstrated real-world
value (external adoption, not just internal completeness) — an
organizational-scale management and governance layer, per
[§ The OSS / Enterprise product
boundary](#the-oss--enterprise-product-boundary) above. Restated
because it's the single most important constraint this phase carries:
**Control never becomes the place core detection capability lives.**
Everything in `Detect`/`Score`/`Decide`/`Alert`/`Integrate`/`Run` is
OSS, full stop, regardless of how far `v0.5`–`v1.0` above advance that
capability. Control's job starts where "one deployment's behavior" ends
and "many deployments/tenants/teams, governed centrally" begins:

```text
Centralized management        Investigation UI / case management
Governance                    Historical analytics
Multi-tenancy                 Audit / compliance
SSO / SAML / OIDC             Enterprise reporting
Enterprise RBAC               Fleet management
Central policy management     HA / scaling management
Advanced alert governance     Enterprise integrations
                               Managed cloud, support / SLA
```

**Scope** (task file [016](tasks/016-control.md)): explicitly a
placeholder today. Defines what Trustvian Control *would* need
(consumes the OSS core as a dependency; never forks it) without
speculatively designing dashboards, RBAC, or multi-tenancy now.

**Non-goals.** Everything in the list above and the original spec's
Phase 6/7: web dashboard, central API, historical analytics, RBAC,
SSO, multi-tenancy, audit, SIEM/Kafka integration, HA/horizontal
scaling. None of this is scoped, designed, or implemented as part of
any milestone above (`v0.1`–`v1.0` are all OSS). Also non-goals,
correcting language this document and the project spec previously
carried: Control does **not** provide alerting, notification delivery,
sequence detection, AI-agent behavioral context, or any other item
already listed as OSS scope above — those are shipped or roadmapped in
`v0.4`–`v0.7`, not withheld for this phase.

**Dependencies.** v0.1 shipped and adopted. Not otherwise defined yet.

**Acceptance criteria.** Not defined — this phase doesn't start until
its own planning pass, explicitly gated on real-world OSS usage
existing to design against.

---

## Future research

Re-evaluated against the `v1.0` product goal, per [§ The OSS /
Enterprise product boundary](#the-oss--enterprise-product-boundary)
above. Each item below is classified as **required before `v1.0`**
(and therefore now has a real milestone, not just a research note),
**useful after `v1.0`**, **research only** (no concrete design/consumer
yet), or an **Enterprise operational concern** (organizational-scale,
not detection capability). Explicitly not blindly promoted wholesale —
most items stay exactly where they were, with the reasoning restated
against the new goal rather than just carried over.

- **Sequence/n-gram/Markov anomaly detection** — **reclassified:
  required before `v1.0`.** Moved out of this section into
  [v0.6 — Behavioral Detection Depth](#v06--behavioral-detection-depth):
  a production-usable OSS behavioral security product needs
  order-aware detection, and n-gram/Markov transition modeling are
  deterministic/statistical, not ML, so they don't conflict with "no ML
  required." Graph-based sequence analysis and ML-based sequence models
  remain here (below), since neither has a concrete design or consumer
  yet.
- **ML-based anomaly detection (including graph-based sequence
  models)** — **research only.** Explicitly deferred behind
  deterministic/statistical methods per the roadmap principle "no ML
  before deterministic detection," and explicitly **never required** —
  [v0.6](#v06--behavioral-detection-depth)'s own architecture keeps ML
  as an optional plugin layer beneath the deterministic engine, never a
  dependency of the core detection path. No timeline.
- **Automatic sensitive-target classification** — **research only,
  unchanged.** Today `anomaly.Config.SensitiveTargetFloor` requires an
  operator to name sensitive destinations explicitly (see
  [SECURITY.md § malicious agents](SECURITY.md#malicious-agents--privilege-escalation)).
  Automatic classification would need either heuristics or ML — the
  latter is out of scope per the above, so this stays research until a
  concrete heuristic design exists. Not required for `v1.0`: explicit
  operator configuration is a complete, production-usable answer to
  this threat on its own.
- **Splitting `internal/otel` into its own Go module** — **useful after
  `v1.0`.** See [ADR 0003](adr/0003-opentelemetry-adapter-single-module.md);
  revisit if an external consumer reports OTel appearing in their build
  for a core-only import. Purely a packaging/dependency-footprint
  concern, not a capability gap — doesn't block `v1.0`.
- **Promoting `Policy`/`Config`/`Store` to a public package** —
  **reclassified: required before `v1.0`, addressed by
  [v0.5 — Policy & Configuration](#v05--policy--configuration).** This
  was the single most direct blocker to "OSS is a complete, standalone,
  production-usable product": without it, `processor/` and any future
  standalone deployment cannot express a meaningful custom `Policy` at
  all. `v0.5` resolves it the same deliberate way
  [ADR 0007](adr/0007-alert-package-is-public.md) resolved the
  equivalent question for `alert` — a narrow public contract, not
  promoting `internal/policy` wholesale.
- **Multi-instance / distributed baseline sharing** — **Enterprise
  operational concern.** Explicitly not a goal per the roadmap
  principles ("no distributed architecture unless justified by a
  concrete milestone"); sharing baselines across many instances is
  exactly the "operate organizationally, at scale" territory
  [§ Control / Enterprise phase](#control--enterprise-phase) above
  reserves for Control, not a gap in OSS's single-deployment
  capability. No milestone above justifies it, and none should until a
  concrete need does.
- **Day-of-week seasonality** — **useful after `v1.0`, not required.**
  Moved here during [v0.3](#v03--baseline--anomaly-depth)'s scoping
  pass ([task 017](tasks/017-baseline-time-patterns.md)). A second EWMA
  dimension (7 day-of-week buckets, or a 7×24 joint distribution) is a
  separate vertical slice from the hour-of-day signal task 017 shipped,
  not a small addition to it, and needs its own maturity/calibration
  analysis (a fingerprint needs weeks of traffic to mature a
  day-of-week distribution the way it needs hours to mature an
  hour-of-day one). `v1.0`'s "time-aware detection" release-gate item
  is already satisfied by the shipped hour-of-day signal — revisit
  day-of-week only if real operator feedback on it justifies the added
  complexity.

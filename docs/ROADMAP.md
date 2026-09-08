# Roadmap

Milestone-based (`v0.1`/`v0.2`/`v0.3`/...), reconciled against what's
actually in this repository today — verified by reading source, tests,
and benchmarks, not assumed from the original vision document. Each
milestone maps to one or more detailed task files in
[`docs/tasks/`](tasks/); each task file is independently
understandable and carries its own objective, scope, non-goals,
technical requirements, tests, benchmarks, documentation, and
acceptance criteria.

Cross-references: [ARCHITECTURE.md](ARCHITECTURE.md) (system shape),
[DOMAIN.md](DOMAIN.md) (what exists today), [SECURITY.md](SECURITY.md)
(threat model), [PERFORMANCE.md](PERFORMANCE.md) (measured numbers),
[`adr/`](adr/) (why past decisions were made).

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

**`v0.4` — Alert & Notification Foundation is implemented, not yet
tagged.** [Task 018](tasks/018-alert-notification-foundation.md) added
a new public package, `alert` (sibling to `event` — see
[ADR 0007](adr/0007-alert-package-is-public.md) for why it isn't
`internal/`): `Alert`, a `Severity` concept, a
`policy.Condition`-shaped Alert Evaluation matcher (`Condition`/`Rule`/
`Evaluate`), the `Sink` interface, and `WebhookSink` — a generic,
HMAC-signed HTTPS webhook, the one delivery mechanism this stage ships
— without changing `Decision` semantics or touching the core pipeline
at all. See the milestone section below for the full writeup.

What's **not** yet true, concretely — the gaps this roadmap's remaining
milestones exist to close:

- No CI/CD, no container image.
- `processor/`'s processor runs Trustvian's default `Policy` only —
  every span it scores resolves to `observe_only`, since a genuinely
  separate module cannot construct a custom `Policy` today (see
  [ADR 0002](adr/0002-public-api-boundary.md), deliberately not
  revisited by task 009 — see
  [`processor/README.md` § Configuration](../processor/README.md#configuration)).
- Day-of-week seasonality — moved to [Future research](#future-research)
  during `v0.3` scoping, not built (see the `v0.3` section below).
- Alert delivery retry, deduplication, cooldown, escalation, alert
  history, and every provider-specific sink (Slack, Teams, PagerDuty) —
  the Reliability and Additional-sinks-and-governance stages, both
  explicitly deferred past `v0.4` (see [Alert & Notification
  phase](#alert--notification-phase) below).
- AI-agent event types work today only through the generic `Event`
  model (`ActorTypeAIAgent` + `OperationCategoryTool`) — no session,
  delegation, or tool-sequence concepts exist.
- No MCP interface, no Trustvian Control.

**Next up:** with `v0.1`–`v0.4` all implemented, the Alert &
Notification phase's Reliability stage and AI-agent session/delegation
concepts are both unblocked candidates for the next body of work,
neither predetermined as "first" by this document — see [Alert &
Notification phase](#alert--notification-phase) and [AI Agent
phase](#ai-agent-phase) below.

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

- **Reliability** (depends on `v0.4`): delivery retry with exponential
  backoff, delivery status, idempotency, deduplication, and a cooldown
  window — closing the "one incident, one alert" and "a flaky endpoint
  doesn't lose alerts" gaps Foundation deliberately leaves open (spec §
  18.11–18.12).
- **Additional sinks and governance** (depends on `v0.4`; mostly
  Trustvian Control/Enterprise territory per the OSS/Enterprise
  boundary in the spec's § 18.16): Slack, Microsoft Teams, PagerDuty,
  and anything past those, plus centralized notification management,
  advanced alert rules, multi-tenant configuration, escalation, alert
  history, delivery observability, RBAC, and audit. The generic webhook
  from Foundation already covers most of what these providers offer via
  automation platforms (n8n, SOAR, custom relays), so this stage is
  explicitly not required for the OSS core to be useful.

**Non-goals (both stages).** No incident-management domain (an `Alert`
is one notification-worthy event, not a grouped investigation — see
spec § 18.13); no boolean combinators or a general expression language
for alert rules; no Slack/Teams/PagerDuty SDK, HTTP client, Kafka,
Redis, or PostgreSQL dependency added to the core detection engine
merely to support alerting — every provider integration lives behind
`AlertSink`, the same way `internal/otel` is the only package depending
on OpenTelemetry today.

**Acceptance criteria.** Defined when each stage's own task file is
written — not before, per "small vertical slices" and this roadmap's
standing policy of not pre-committing to unscoped work.

## AI Agent phase

**Objective.** Extend the *existing* event model for richer AI-agent
behavioral context, without building a separate security engine for
agents — per the brief's own explicit instruction, agents remain
"another behavioral source" through the same pipeline.

**Scope** (task file [014](tasks/014-ai-agent.md)):

- Optional new dimensions on `Event`/`Context` for session grouping and
  agent-to-agent delegation.
- Human-approval *workflow* semantics layered onto the existing
  `REQUIRE_APPROVAL` decision (which already exists) rather than a new
  decision type.
- Tool-sequence analysis explicitly stays out of this phase — it's a
  new anomaly *algorithm* (sequence-aware, not just new event fields),
  which is `v0.3`+/[Future research](#future-research) territory per
  [ADR 0001](adr/0001-hexagonal-core-and-pipeline-shape.md)'s "add a
  second algorithm only when it has a concrete design" stance.

**Non-goals.** No agent-specific `Fingerprint`/`Baseline`/`Anomaly`
implementation — the same `internal/*` packages must keep working
unchanged for agent-sourced events, proven by reusing existing tests
against the extended `Event` shape, not writing parallel ones.

**Dependencies.** v0.1 (stable `Event`/public API — extending `Event`
after `v0.1` ships means doing it in a backward-compatible way, adding
optional fields only).

**Acceptance criteria.** See [014-ai-agent.md](tasks/014-ai-agent.md).

## Trustvian MCP

**Objective.** Expose Trustvian's read/query surface
(`get_behavior`, `get_trust_score`, `explain_decision`, etc.) to AI
agents and developer tooling via MCP.

**Scope** (task file [015](tasks/015-trustvian-mcp.md)): a new adapter
(a new `cmd/`-style binary or a separate module, mirroring how
`internal/otel` and `cmd/trustvian` already depend on the core without
the core knowing they exist) implementing an MCP server backed by
`Engine`.

**Non-goals.** No new decision-making logic in the MCP layer itself —
it is a read/query and evaluate-via-existing-`Engine` adapter, not a
second policy engine.

**Dependencies.** v0.1 (a stable public API and `Result` shape to
expose), ideally after the AI Agent phase (richer context to query).

**Acceptance criteria.** See [015-trustvian-mcp.md](tasks/015-trustvian-mcp.md).

---

## Control / Enterprise phase

**Objective.** Only after the OSS core has demonstrated real-world
value (external adoption, not just internal completeness) — a
commercial management layer.

**Scope** (task file [016](tasks/016-control.md)): explicitly a
placeholder today. Defines what Trustvian Control *would* need
(consumes the OSS core as a dependency; never forks it) without
speculatively designing dashboards, RBAC, or multi-tenancy now.

**Non-goals.** Everything in the original spec's Phase 6/7: web
dashboard, central API, historical analytics, RBAC, SSO, multi-tenancy,
audit, SIEM/Kafka integration, HA/horizontal scaling. None of this is
scoped, designed, or implemented as part of any milestone above.

**Dependencies.** v0.1 shipped and adopted. Not otherwise defined yet.

**Acceptance criteria.** Not defined — this phase doesn't start until
its own planning pass, explicitly gated on real-world OSS usage
existing to design against.

---

## Future research

Explicitly not committed to any milestone above; revisit only when a
concrete need (not speculation) justifies it:

- **Sequence/n-gram/Markov anomaly detection** over operation order —
  the original spec's own "potential future algorithms" list. Needs a
  concrete design before it earns a package, per
  [ADR 0001](adr/0001-hexagonal-core-and-pipeline-shape.md).
- **ML-based anomaly detection** — explicitly deferred behind
  deterministic/statistical methods per the roadmap principle "no ML
  before deterministic detection." No timeline.
- **Automatic sensitive-target classification** — today
  `anomaly.Config.SensitiveTargetFloor` requires an operator to name
  sensitive destinations explicitly (see
  [SECURITY.md § malicious agents](SECURITY.md#malicious-agents--privilege-escalation)).
  Automatic classification would need either heuristics or ML — the
  latter is out of scope per the above, so this stays research until a
  concrete heuristic design exists.
- **Splitting `internal/otel` into its own Go module** — see
  [ADR 0003](adr/0003-opentelemetry-adapter-single-module.md); revisit
  if an external consumer reports OTel appearing in their build for a
  core-only import.
- **Promoting `Policy`/`Config`/`Store` to a public package** — see
  [ADR 0002](adr/0002-public-api-boundary.md); revisit when a real
  external consumer needs to configure an `Engine` from outside this
  module.
- **Multi-instance / distributed baseline sharing** — explicitly not a
  goal per the roadmap principles ("no distributed architecture unless
  justified by a concrete milestone"); no milestone above justifies it
  yet.
- **Day-of-week seasonality** — moved here during
  [v0.3](#v03--baseline--anomaly-depth)'s scoping pass ([task
  017](tasks/017-baseline-time-patterns.md)). A second EWMA dimension
  (7 day-of-week buckets, or a 7×24 joint distribution) is a separate
  vertical slice from the hour-of-day signal task 017 shipped, not a
  small addition to it, and needs its own maturity/calibration analysis
  (a fingerprint needs weeks of traffic to mature a day-of-week
  distribution the way it needs hours to mature an hour-of-day one) —
  revisit only if real operator feedback on the hour-of-day signal
  justifies the added complexity.

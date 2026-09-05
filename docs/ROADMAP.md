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

What's **not** yet true, concretely — the `v0.2`-and-later gaps this
roadmap's remaining milestones exist to close:

- No CI/CD, no container image.
- The six *outbound* `trustvian.*` OTel enrichment attributes (from
  the original spec) are undocumented-as-implemented because they
  aren't — only the four *inbound* override attributes exist.
- No OTel Collector processor.
- AI-agent event types work today only through the generic `Event`
  model (`ActorTypeAIAgent` + `OperationCategoryTool`) — no session,
  delegation, or tool-sequence concepts exist.
- No MCP interface, no Trustvian Control.

**Next up: `v0.2` — OpenTelemetry maturation** (see below), completing
the outbound OTel integration story now that `v0.1`'s `Result` shape is
stable enough to write an exporter against.

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

- **008 OTel integration v2** — implement the six outbound
  `trustvian.*` attributes (write a `Result` back onto a span/attribute
  set); review semantic-convention mapping completeness now that real
  usage exists.
- **009 OTel Collector processor** — design, and build a minimal
  version of, a Collector processor as a **separate Go module**,
  consuming this module's public API exactly like any other embedder.

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

**Scope.** Time-based pattern awareness (day-of-week/hour-of-day
seasonality) evaluated as a possible `Baseline` extension — this is
deliberately not pre-committed to v0.1 or even guaranteed for v0.3,
because it's the first place in this roadmap where "keep it simple and
statistical" (CLAUDE.md) is genuinely in tension with "time-based
patterns" (the brief). If it turns out to need anything beyond a
straightforward per-hour-bucket EWMA, it moves to
[Future research](#future-research) instead. No dedicated task file
number is reserved for this yet — it will be scoped as a proper task
(following the same template) once v0.2 is done and it's clear whether
the simple version is sufficient.

**Non-goals.** Sequence/n-gram anomaly detection (still deliberately
deferred — see [ADR 0001](adr/0001-hexagonal-core-and-pipeline-shape.md)
and [Future research](#future-research)), ML of any kind.

**Dependencies.** v0.1 (needs the persistent `Store` and
frequency-tracking groundwork from 003/004).

**Acceptance criteria.** Defined when this milestone's task file is
written — not before, per "small vertical slices" and not
pre-committing to unscoped work.

---

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

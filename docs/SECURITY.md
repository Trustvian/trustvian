# Security Model

This document describes the threats Trustvian's design accounts for,
what's actually implemented today versus deliberately deferred, and
where to find each protection in code. See
[`.claude/rules/security.md`](../.claude/rules/security.md) for the
same principles phrased as engineering rules for future changes.

Trustvian is not itself an authentication system, a database, or an
enforcement point in the network path — it is a scoring and decision
engine. Its security model is about the integrity of *its own
reasoning* (can its scores/decisions be manipulated or bypassed), not
about securing the transport or storage layers around it (those are
explicitly out of core scope — see
[ARCHITECTURE.md](ARCHITECTURE.md)).

## Threats considered

### Telemetry spoofing

**Threat:** a producer sends fabricated or misleading event data to
manipulate a decision.

**Status: partially addressed, by design boundary.** Trustvian treats
`Actor.IdentityConfidence` as an external input it trusts, never
something it computes (`internal/trust`) — it is explicitly *not* an
authenticator. If upstream telemetry is spoofed with a high
`IdentityConfidence` and no compensating signal, Trustvian will trust
it accordingly; this is a stated boundary, not a gap being hidden.
Verifying the telemetry pipeline itself (mTLS, signed spans, a trusted
collector) is the deploying application's responsibility, upstream of
this engine. **Future work:** none planned in-core; this is inherently
a transport/collection-layer concern.

### Identity confusion

**Threat:** two different actors (or the same actor in different
environments/tenants) collide on the same behavioral history, letting
one actor's baseline apply to another.

**Status: implemented.** `baseline.Key{ActorID, Environment}` is a
composite key from the start, not a bare actor-ID string — even though
the OSS engine is single-tenant today. This is deliberately cheap now
and expensive to retrofit later (see
[ADR 0004](adr/0004-narrow-store-port-in-memory-only.md)). A future
multi-tenant `TenantID` dimension is an additive extension of the same
pattern, not a redesign.

### Replay

**Threat:** a captured, legitimate event is resubmitted to
artificially reinforce a baseline or repeat a decision.

**Status: not implemented; explicitly deferred.** `Event.ID` and
`Timestamp` exist and could support a bounded-window dedup cache, but
this is an adapter/collection-layer concern (where events first enter
the system), not something `Engine.Analyze`/`Observe` currently do —
the core engine is stateless per call except for the `Baseline` it's
explicitly asked to update. **Future work:** dedup at the OTel adapter
or Collector processor layer.

### Baseline poisoning

**Threat:** an attacker (or a persistently misbehaving process)
gradually "trains" the baseline into treating malicious behavior as
normal by repeating it.

**Status: implemented, and verified end-to-end.** `Engine.Observe`
only learns from `Decision`s where the action *proceeded*
(`ALLOW`/`OBSERVE_ONLY`/`ALERT`); anything held or stopped
(`CHALLENGE`/`REQUIRE_APPROVAL`/`BLOCK`) is never folded into the
baseline. This gating lives inside `Observe` itself, not in caller
discipline — it's safe to call unconditionally after every `Analyze`.
See `TestObserveLearnsOnlyFromEligibleDecisions` and
`TestAnalyzeSensitiveTargetFloorEndToEnd` in
[`engine_test.go`](../engine_test.go), which specifically proves a
`BLOCK`ed, sensitive-destination pattern cannot mature its way to
trust no matter how many times it's repeated.

A related, non-obvious nuance: the eligible-decision set includes
`ALERT`, not only `ALLOW`/`OBSERVE_ONLY`. Excluding `ALERT` created a
real deadlock during development — a brand-new, entirely benign
fingerprint can transiently cross into `ALERT`-level risk purely from
partial maturity, and if that state were ineligible for learning, it
could never mature past it. See `eligibleForLearning`'s doc comment in
[`engine.go`](../engine.go).

**Persistence-adjacent note:** `store.FileStore`
([ADR 0006](adr/0006-file-backed-persistent-store.md)) flushes to disk
synchronously after every `Observe`, so an unclean shutdown loses at
most the single in-flight observation, never a corrupted or
partially-written file (writes are atomic via temp-file-plus-rename).
This does not introduce a new poisoning vector: the gating above
applies identically regardless of which `Store` implementation is
configured — `FileStore` persists exactly what `Observe` already
decided was eligible to learn, nothing more. Restarting a process using
`FileStore` resumes from the same (gated) baseline it had before the
restart, rather than the empty one `InMemory` would present — this is
the intended fix for the "every restart quietly forgets an attacker's
prior flagged behavior" gap an in-memory-only store would otherwise
leave.

### Malicious agents / privilege escalation

**Threat:** an AI agent or service account uses legitimate credentials
to access something outside its normal scope (secrets, admin
interfaces), and — because access to a sensitive destination is itself
learnable behavior — gradually normalizes that access.

**Status: implemented.** `anomaly.Config.SensitiveTargetFloor` sets a
fixed minimum anomaly contribution for specific destinations that
persists *regardless of baseline familiarity* — full maturity does not
erase it (see [DOMAIN.md § Anomaly](DOMAIN.md#anomaly) and the same
`TestAnalyzeSensitiveTargetFloorEndToEnd` test above). This is what
directly prevents the "train the baseline into trusting me" attack
path for destinations an operator has explicitly flagged as sensitive.
**Caveat:** this requires the operator to configure
`SensitiveTargetFloor` for the destinations that matter — it is not
automatic classification. See [ROADMAP.md](ROADMAP.md) for automatic
sensitivity detection as a possible future direction.

### Policy bypass

**Threat:** a missing, empty, or misconfigured policy silently
resolves to `ALLOW`, defeating enforcement without anyone noticing.

**Status: implemented.** `policy.Policy.Evaluate` fails closed to
`BLOCK` — not `ALLOW` — when `DefaultAction` is unset or invalid, or
when `DefaultAction` is set but `DefaultReason` is empty. There is no
code path from "the config is wrong" to a silent allow; this is
verified directly by
`TestEvaluateFailsClosedOnZeroValuePolicy`,
`TestEvaluateFailsClosedOnInvalidDefaultAction`, and
`TestEvaluateFailsClosedOnEmptyDefaultReason` in
[`internal/policy/policy_test.go`](../internal/policy/policy_test.go).

### Malformed events / extreme input values

**Threat:** a producer sends a structurally valid but adversarial
`Event` — `NaN`/`Inf` `IdentityConfidence`, empty or extremely long
identifiers, negative durations, deeply nested or very large
`Attributes` — attempting to crash the engine, corrupt a score, or
smuggle bad data past validation.

**Status: partially addressed; not yet a dedicated, exhaustive test
suite.** `Event.Validate()` already rejects several classes of bad
input (empty required fields, out-of-range `IdentityConfidence`,
invalid enum values — see `event/event_test.go`), and `trust.Compute`
defensively clamps its numeric inputs to `[0,1]` regardless of what's
passed (`internal/trust/trust.go`). What's missing is a systematic,
threat-labeled sweep across the full space of adversarial input
(`NaN`/`Inf` specifically, oversized `Attributes`, extreme string
lengths) rather than the incidental coverage that exists today. See
[`docs/tasks/012-security-tests.md`](tasks/012-security-tests.md) —
this is that task's explicit scope.

### Resource exhaustion

**Threat:** an attacker (or a misbehaving legitimate producer) sends
input designed to consume disproportionate CPU or memory relative to
its size — an unbounded `Attributes` map, or an actor generating an
unbounded number of distinct fingerprints to grow `Baseline` without
limit.

**Status: not yet tested; behavior is currently "whatever Go's map and
slice growth does," not a deliberately engineered bound.** There is no
per-event size limit on `Attributes` today, and `store.InMemory` has
no eviction policy — see
[PERFORMANCE.md § what's not benchmarked](PERFORMANCE.md#whats-not-benchmarked-yet)
for the related (unmeasured) memory-growth question. **Future
mitigation:** [`docs/tasks/012-security-tests.md`](tasks/012-security-tests.md)
scopes the safety-property tests (no panic, no deadlock, cost
proportional to input); [`docs/tasks/011-performance.md`](tasks/011-performance.md)
scopes characterizing the growth curve itself. Neither task commits to
adding an enforced limit — that's a decision to make only if the
characterization shows it's actually needed, not preemptively.

### Future multi-tenant isolation

**Threat:** in a multi-tenant deployment, one tenant's behavioral data
or policy decisions leak into or influence another's.

**Status: not implemented; the data model is prepared for it.**
Trustvian's OSS core is explicitly single-tenant (multi-tenancy, RBAC,
and centralized management are Trustvian Control/Cloud concerns — see
[ARCHITECTURE.md § relationship to Control/Cloud](ARCHITECTURE.md#relationship-to-trustvian-controlcloud)).
However, `baseline.Key`'s composite `(ActorID, Environment)` shape
means the data is already scoped in a way a future `TenantID` addition
extends rather than restructures — a deliberate choice to make that
future work an access-control addition, not a data migration.

## Explainability as a security property

Every `Anomaly` retains its `Contributors`; every `policy.Result`
carries a non-empty `Explanation`. This isn't incidental — an
un-explainable decision is itself a kind of risk (an operator can't
audit or contest what they can't see the reasoning for). These
properties are checked by tests
(`TestEvaluateAlwaysProducesNonEmptyExplanationReason`), not just
documented as an aspiration.

## What Trustvian does not protect against

- Compromise of the process it runs in (memory tampering, a malicious
  binary). Out of scope for an in-process library.
- Weaknesses in the identity/authentication system upstream of it — it
  consumes `IdentityConfidence` as an input, it does not produce it.
- Network-level attacks against however events are transported to it —
  that's the transport/collector's responsibility.

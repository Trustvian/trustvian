# 016 — Trustvian Control (Placeholder)

**Milestone:** Control / Enterprise phase · **Depends on:** v0.1
shipped and adopted by real external users (not just internally
complete) · **Blocks:** none

## Objective

This is deliberately **not** an implementation task. It exists so the
constraint "Trustvian Control consumes the OSS core as a dependency;
never forks it" is written down before any Control work starts, not
decided under the pressure of already having code to reconcile.

## Why

Every other task in this roadmap can be scoped concretely because it
extends something that already exists. Trustvian Control cannot be —
per the roadmap brief's own ordering, it starts "only after the OSS
core proves its value," which is an external-adoption signal this
repository cannot manufacture for itself. Writing a detailed technical
task file now, before that signal exists, would be exactly the kind of
premature, speculative design [ADR 0001](../adr/0001-hexagonal-core-and-pipeline-shape.md)
through [0004](../adr/0004-narrow-store-port-in-memory-only.md)
consistently avoided elsewhere in this codebase.

## Scope (of *this* task, not of Control itself)

- Record the one architectural constraint that must hold regardless of
  when Control work starts: it imports this module's public API
  (`trustvian`, `event`, and whatever [ADR 0002](../adr/0002-public-api-boundary.md)
  has made public by then) as a normal external dependency. It does
  not fork this repository, does not get a privileged internal-package
  import exemption, and does not cause the core to gain any
  Control-awareness (no `if runningUnderControl` branches, no
  Control-specific hooks added to `Engine`).
- Note the concrete capabilities named in the original spec (dashboard,
  central API, service/agent inventory, policies, investigations,
  historical analysis, audit, RBAC, SSO, multi-tenancy, alerts, SIEM
  integrations, HA) as the eventual scope, without designing any of
  them now.

## Non-Goals

- No dashboard, API, database schema, RBAC model, or multi-tenancy
  design — all of this is explicitly deferred to a future planning
  pass with its own dedicated task breakdown, gated on real adoption
  evidence.
- No multi-tenant `TenantID` implementation in `baseline.Key` — the
  data model is already *shaped* for this addition (composite key,
  see [SECURITY.md § future multi-tenant isolation](../SECURITY.md#future-multi-tenant-isolation)),
  but adding the actual field/enforcement is Control-phase work, not
  this task's.
- No Kafka/SIEM/HA infrastructure of any kind in this repository.

## Technical Requirements

N/A — no implementation in this task.

## Tests

N/A.

## Benchmarks

N/A.

## Documentation

- [ARCHITECTURE.md § relationship to Trustvian Control/Cloud](../ARCHITECTURE.md#relationship-to-trustvian-controlcloud)
  already states this constraint; this task's only concrete output (if
  any, beyond this file existing) is confirming that section stays
  accurate as `v0.1`/`v0.2`/`v0.3` land, since each of those could in
  principle introduce a Control-awareness regression if not watched
  for.

## Acceptance Criteria

- This file exists and accurately reflects the constraint.
- At the point real Control work is scoped, it starts as its own
  planning pass (its own roadmap milestone, its own task breakdown)
  referencing this file — not by expanding this file in place after
  the fact into something it wasn't designed to be.

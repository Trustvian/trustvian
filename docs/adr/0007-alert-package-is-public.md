# 0007 — `alert` is a public package, not `internal/alert`

## Context

[Task 018](../tasks/018-alert-notification-foundation.md) (v0.4,
Alert & Notification Foundation) named a genuine open question rather
than deferring it: where should `Alert` and its `Sink` interface live?

[ADR 0002](0002-public-api-boundary.md) established the rule this
module has followed ever since: a type stays under `internal/` unless
an external caller genuinely needs to *construct* one, not merely read
one back off a `Result`. `policy.Policy`, `anomaly.Config`,
`trust.Config`, and `store.Store` all stay internal today under exactly
that rule — no external consumer needs to construct them yet.

`Sink` breaks that precedent on its own terms, not by exception. Its
entire purpose (spec § 18.6: "a new provider can be added by
implementing one method against a stable Alert shape, without the
Notification Dispatcher... changing") is for code this module does not
control to implement `Send(ctx context.Context, a Alert) error`. A Go
method signature naming a type requires importing the package that
defines it. If `Alert` lived under `internal/alert`, no code outside
this module could ever write that method — the one thing this stage
exists to enable would be structurally impossible from day one, not
merely inconvenient.

This is a real, current need, not a speculative one: the OSS/Enterprise
boundary this stage documents (spec § 18.16) explicitly places "the
`Alert` domain model," "basic alert evaluation," and "a generic webhook
sink" in OSS-core scope, all meant to be usable directly via the Go SDK
with no OTel adapter and no Collector processor involved at all
(task 018's own Acceptance Criteria: "Direct Go SDK usage ... can
produce and deliver a real `Alert` through the webhook sink,
end-to-end"). That bar cannot be met by an internal type.

## Decision

`Alert`, `Severity`, `Condition`, `Rule`, `Evaluate`, `Sink`, and
`WebhookSink` all live in a new top-level public package, `alert`
(`github.com/Trustvian/trustvian/alert`) — a sibling to `event`, not a
subpackage of `internal/`, and not folded into the root `trustvian`
package.

Sibling to `event`, not merged into root, because:

- `event` is already this module's precedent for "the one type an
  external caller must touch to use a feature at all" living outside
  `internal/`. `Alert`/`Sink` are the same shape of problem `Event` and
  `internal/event` originally hit (ADR 0002's own context section) —
  reusing the established pattern rather than inventing a second one.
- The root `trustvian` package is the pipeline's composition root
  (`Engine`, `Option`, `Result`). Alert Evaluation is deliberately
  **not** a pipeline stage — it is a downstream, optional consumer of
  `Result` that the core engine has no awareness of (see
  [ARCHITECTURE.md § Relationship to a future Alert & Notification
  layer](../ARCHITECTURE.md#relationship-to-a-future-alert--notification-layer)).
  Folding `Alert`'s several new types (severity, matcher, sink,
  webhook signing) directly into the root package's existing files
  would blur that boundary for no benefit — `.claude/rules/go.md`'s
  "one package, one responsibility" applies here exactly as it does to
  every pipeline-stage package.

**Import direction:** `alert` imports the root `trustvian` package (for
`trustvian.Result`) and `internal/policy`/`internal/trust` (for
`policy.Decision`/`trust.RiskLevel`, so `Alert`'s fields carry the same
strong types `Result` itself uses, exactly as the root package already
does for the same reason). This is legal and cycle-free: `alert` lives
inside this module's tree (same as the root package), so Go's
`internal/` restriction does not block the import; and the root package
has no need to import `alert` back, since nothing in `Engine.Analyze`/
`Observe` calls into alert evaluation. This mirrors `internal/otel`'s
existing relationship to the root package (`AttributesFromResult(result
trustvian.Result)` imports root without root importing back) — the
only difference is that `alert` sits outside `internal/`, because
(unlike `internal/otel`) it has a real third-party-construction
requirement `internal/otel` does not.

**What stays inside `alert`, not split into a further subpackage:**
`WebhookSink` — the one transport this Foundation stage ships — lives
directly in `alert`, not `alert/webhook`. There is exactly one sink
implementation today; splitting a package for a single implementation
is the premature abstraction `.claude/rules/architecture.md` warns
against for anomaly-algorithm plugins ("add the interface when a second
one exists, not before"), applied to packages instead of interfaces.
`alert` importing `net/http`/`crypto/hmac` is architecturally fine here
specifically because `alert` is not part of the core detection engine
boundary (task 018's own Architecture guarantees apply to `event`,
`internal/features` through `internal/policy`, and `Engine` — not to
this new, explicitly downstream package).

## Alternatives considered

- **Keep `Alert`/`Sink` under `internal/alert`, matching
  `policy.Policy`/`anomaly.Config`'s precedent.** Rejected: this is the
  one case where that precedent's own stated condition ("no external
  consumer needs to construct this yet") is false by the feature's own
  design — a custom `Sink` implementation is the explicit reason this
  abstraction exists (spec § 18.6). Following the precedent
  mechanically here would make the resulting package satisfy the
  wrong requirement.
- **Put `Alert`/`Sink`/`Evaluate` directly in the root `trustvian`
  package**, next to `Result`/`Engine`. Rejected: conflates the
  pipeline's composition root with a downstream, optional consumer of
  its output, and would pull `net/http`/HMAC-signing types into the
  same file set as `Engine`/`Option`, which a reader has no reason to
  associate with alerting. Also makes a future promotion of `alert`'s
  webhook logic to its own subpackage (if a second sink type
  eventually needs it) a root-package refactor instead of a
  same-package file move.
- **A separate Go module for `alert`**, mirroring `processor/`.
  Rejected: `processor/` is a separate module specifically because it
  needs the OTel Collector's heavyweight `otelcol-builder` toolchain,
  which must never enter this module's dependency graph (ADR 0003).
  `alert` adds zero third-party dependencies beyond the standard
  library — there is no dependency-isolation reason to pay a
  separate-module's operational cost (its own `go.mod`, its own
  versioning, a `replace` directive for local development) for no
  benefit.

## Consequences

- A third-party `Sink` implementation (a future Slack/Teams/PagerDuty
  relay, or any custom integration) can `import
  "github.com/Trustvian/trustvian/alert"` and implement `alert.Sink`
  today, from a genuinely separate module, with no code changes to this
  repository — the exact capability this decision exists to preserve.
- `alert` is now the third package (after root `trustvian` and `event`)
  an external module can import, and inherits the same compatibility
  discipline [CHANGELOG.md § Public API compatibility
  promise](../../CHANGELOG.md#public-api-compatibility-promise)
  documents for those two: its exported shapes are a promise once
  released, not a place for casual breaking changes.
- If a second `Sink` implementation ever needs enough of its own
  dependencies or complexity to justify a package split (e.g. a Slack
  adapter pulling in a Slack-specific request/response shape), moving
  `WebhookSink` out to `alert/webhook` at that point is a same-module,
  backward-compatible addition — nothing about this decision forecloses
  it.

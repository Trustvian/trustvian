# ai-agent-security

Ports [docs/tasks/032-agent-security-scenario-validation.md](../../docs/tasks/032-agent-security-scenario-validation.md)'s
"familiar != authorized" scenario — and, unlike every other example in
this directory, gets a genuinely differentiated `Decision` (not just
`observe_only`), by using the real, declarative configuration path an
OSS user would actually reach for: a `config.PolicyConfig` value (the
same schema a `trustvian.yaml` file decodes into — see
[docs/policy-guide.md § Loading a Policy from a YAML file](../../docs/policy-guide.md#loading-a-policy-from-a-yaml-file))
requires approval for `shell.execute`, compiled with
`config.CompilePolicy` into the `policy.Policy` value
`trustvian.WithPolicy` accepts. This file never imports
`internal/policy` — see [the examples index](../README.md#a-note-on-decision)
for why every other example here is stuck at `observe_only`, and why
this one isn't.

An AI agent's `shell.execute` tool call matures into fully familiar
behavior (25 `Analyze`+`Observe` calls, all `Approved` — mirroring
[docs/sdk-guide.md § watching trust mature](../../docs/sdk-guide.md#watching-trust-mature)),
then the identical, now-familiar call is evaluated twice more at the
same steady-cadence timestamp: once with `ApprovalStatus: approved`
(allowed), once with `ApprovalStatus: denied` (blocked) — proving that
behavioral familiarity and policy authorization are different
questions. `Anomaly.Score`/`Trust.Score` are identical between the two
cases; only `Decision` differs.

Run it:

```bash
cd examples/ai-agent-security && go run .
```

## Real output

```
warm-up  1: confidence=0.00 trust=0.95 decision=allow learned=true
warm-up 10: confidence=0.45 trust=0.71 decision=allow learned=true
warm-up 20: confidence=0.95 trust=0.90 decision=allow learned=true
warm-up 25: confidence=1.00 trust=0.95 decision=allow learned=true

shell.execute is now fully familiar behavior for this agent.

Case A — ApprovalStatus: approved
Decision: allow
trust 0.95 (low): identity confidence 0.95, anomaly 0.00 at full confidence, context risk 0.00
Anomaly score: 0.00 (confidence 1.00)
Policy: default action (no approval requirement configured for this operation)

Decision: allow (rule "": no approval requirement configured for this operation)

Case B — ApprovalStatus: denied (same familiar behavior)
Decision: block
trust 0.95 (low): identity confidence 0.95, anomaly 0.00 at full confidence, context risk 0.00
Anomaly score: 0.00 (confidence 1.00)
Policy: rule "shell-execute-requires-approval" (shell.execute requires approval; approval evidence was not Approved)

Decision: block (rule "shell-execute-requires-approval": shell.execute requires approval; approval evidence was not Approved)

Alert: alt_1953b9013fb4ef13a4f4e8f4104d747e severity=high reasons=[policy rule "shell-execute-requires-approval": shell.execute requires approval; approval evidence was not Approved]
```

(`Alert.ID` is randomly generated per run — see `alert.New`.)

## What this shows

- **Policy, not the event, is authoritative.** The approval
  requirement is entirely config data (`config.PolicyRule`'s `Unless`),
  never something the event itself declares. Nothing in `Case B`'s
  event claims `ApprovalStatus: not_required` — even if it did, the
  rule would still fire, because `Unless` only exempts `approved`.
- **Behaviorally familiar is not the same as authorized.** Both cases
  score identical `Anomaly`/`Trust` — a fully mature, unremarkable
  fingerprint — yet produce opposite `Decision`s, entirely on
  `ApprovalStatus`'s value.
- **`alert.Evaluate` needs no approval-specific code.** The existing
  `alert.Rule`/`Condition` (matching on `Decision`, received from a
  public `trustvian.Result` field and passed through without ever
  naming `policy.Decision`) represents the approval violation exactly
  like any other `BLOCK`.

## What this example does not show

Task 032's own scenario validation also exercises `delegation_deviation`
(task 031) and the bounded n-gram sequence signal (task 027/`v0.6`) in
combination with approval — see
[`scenario_test.go`](../../scenario_test.go) at the repository root for
that full combined proof. This example, deliberately, does not: those
signals are configured via `anomaly.Config`
(`trustvian.WithAnomalyConfig`), a type that lives under `internal/`
with no `config`-package equivalent yet — an OSS user outside this
module has no way to raise `DelegationWeight`/`NGramWeight` above their
default-`0`, opt-in value today. This is a genuine, documented gap (see
[docs/tasks/032-agent-security-scenario-validation.md § Findings](../../docs/tasks/032-agent-security-scenario-validation.md)),
not something this example papers over — `Policy` configuration got a
public path in `v0.5` ([ADR 0008](../../docs/adr/0008-policy-config-boundary.md));
`anomaly.Config` has not, yet.

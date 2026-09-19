# 0020 — A repository-wide compatibility contract, not an API-only one

**Status:** Accepted

## Context

`CHANGELOG.md` has carried a "Public API compatibility promise" since
`v0.1.0`, covering `event.Event`, `Result`, and `Engine`. It is precise
and it has been honoured — but it covers the Go API and nothing else.

Everything an operator actually depends on sat outside it: configuration
schemas, CLI flags and exit codes, environment variables, the Collector
processor's configuration keys, the PostgreSQL schema, the file-store
snapshot format, metric names and label keys, health endpoint paths, the
webhook payload envelope, container entrypoint and ports, release
artifact names. A dashboard breaks as thoroughly from a renamed metric
label as a build does from a removed exported symbol.

`v1.0` converts every one of those from a reversible choice into a
commitment. The v1.0 production-readiness audit recorded the gap as
GAP-003, and the `v1.0` release gate requires the policy to exist before
the tag.

There is also a question type checking cannot answer. A change that
keeps every signature intact but alters what the engine *decides* —
enabling a signal by default, changing which decisions train the
baseline, relaxing fail-closed behavior — is felt by users as breakage
regardless of what the compiler says.

## Decision

Publish a single compatibility contract covering **every surface a
consumer or operator can observe**, not only the Go API:
[docs/compatibility.md](../compatibility.md).

Four choices define it:

**Classify surfaces rather than promise uniformly.** Six classes, from
STABLE through OBSERVATIONAL to INTERNAL. A uniform promise would either
overcommit — nobody should be bound to the wording of CLI output — or
undercommit on the surfaces that matter. Each surface is classified from
what the code actually does, so the contract can be checked against the
repository rather than believed.

**Treat behavioral change as a compatibility surface.** The contract
states which semantic changes are bug fixes, which are compatible
tuning, and which are breaking, with the deciding question stated
plainly: would an operator's existing policy start making different
decisions on unchanged traffic? Trustvian is a security product; a
silent change to what it blocks is not a patch.

**Version-based deprecation, not time-based.** Mark, document the
replacement, keep it for at least one subsequent minor, then remove at a
permitted boundary. An OSS project cannot honestly promise a calendar,
and a deprecation window expressed in releases is one a maintainer can
actually enforce.

**A narrow security exception.** A severe vulnerability may require a
change that cannot wait for a deprecation cycle, and pretending
otherwise would produce a contract that gets broken quietly the first
time it is inconvenient. The exception requires an explicit security
rationale, a CHANGELOG entry, migration guidance where one exists, and a
`security:` commit — so using it leaves a trail.

The contract is documentation. It changes no code, no schema, and no
public surface; whether any current surface should change before the
freeze is recorded as input to the public API review, not decided here.

## Alternatives considered

- **Extend the CHANGELOG promise in place.** Rejected: the promise is
  embedded in release history, which is append-only by convention. A
  living contract that is consulted on every pull request needs a
  document that can be revised, and `CHANGELOG.md` is the wrong shape
  for a matrix.
- **Put it in `docs/governance/`.** Rejected: governance describes who
  may do what to the repository. This describes what the software
  promises its users — a product contract, and one a consumer
  evaluating Trustvian should find without reading maintainer process.
- **Promise only the Go API and declare everything else unsupported.**
  Rejected as false. The reference deployment, the container, the
  metrics, and the Collector configuration are all documented and
  intended for production use; calling them unsupported to avoid the
  obligation would contradict what the rest of the documentation tells
  operators to do.
- **Promise numerically stable scores.** Rejected as impossible.
  Baselines are learned state and scores move as they learn. The
  contract promises stable *rules*, not stable numbers.

## Consequences

- "Is this change breaking?" has one place to look, and the answer does
  not depend on which maintainer is asked.
- Some changes that previously looked routine are now explicitly major:
  renaming a metric label key, changing an exit code's meaning, enabling
  a detection signal by default.
- The contract asserts things about the code, so it can drift from it.
  Confirming each classification still matches the implementation is
  part of the `v1.0` release gate rather than a one-time exercise.
- One limit is documented rather than fixed: the configuration loader
  rejects unknown fields, so an older binary cannot read a newer
  configuration. Compatibility runs forward only, which constrains
  rollback and is now stated where operators will find it.

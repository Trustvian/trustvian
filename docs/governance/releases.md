# Release Governance

Who may release Trustvian, what a release tag means once it exists, and which
steps require a human rather than automation.

This document is about **authority**. For the procedure — how to cut a
release, what each artifact contains, and how to verify a downloaded one —
see the [Release Guide](../release-guide.md). The split is deliberate: the
guide answers "how do I do it", this answers "who may, and what can never be
undone".

## Release authority

```text
Human Organization Admin
        |
        v
creates a protected v* tag        <- the only way a release begins
        |
        v
release automation executes the pipeline
        |
        v
draft GitHub Release + signed container image
        |
        v
Human Organization Admin publishes the release
```

A release begins and ends with a human decision. Automation does the work in
between and decides nothing.

| Action | Who |
|---|---|
| Decide that a release happens | Human Organization Admin |
| Create a `v*` tag | Human Organization Admin — nobody else can |
| Run the release pipeline | Automation, triggered by that tag |
| Publish the drafted GitHub Release | Human Organization Admin |
| Promote a candidate to stable | Human Organization Admin, by creating the stable tag |

## Protected tag semantics

Tags matching `v*` are covered by a repository ruleset that restricts
**creation, update, deletion, and force-move**. A human Organization Admin is
the authorized bypass actor; every other identity — contributors, agents, CI,
`GITHUB_TOKEN` — is refused.

Two consequences worth stating plainly:

- **No workflow can mint a release.** The release job holds `contents: write`,
  which would otherwise be enough to push a tag. It is not an Organization
  Admin, so the ruleset refuses it.
- **A published tag is permanent.** Not by convention — by server-side rule.

The live values are readable from the ruleset itself; see
[Repository Governance § Reading the live configuration](repository.md#reading-the-live-configuration)
rather than trusting this paragraph if the two ever disagree.

## Candidate immutability

A release candidate is a tag on `main`, and once pushed it is evidence rather
than a work surface.

```text
vX.Y.Z-rc.1  →  verification fails
      |
      v
fix branch  →  PR  →  CI  →  human review  →  main gets a NEW commit
      |
      v
vX.Y.Z-rc.2 at the new commit
```

The failed tag stays where it is. It records what was attempted and what the
pipeline caught, which is the most useful artifact a failed release produces —
`v0.9.0` needed three candidates, and the first two remain in the history for
exactly this reason.

Candidate numbers only ever increase. Never:

```text
move an existing RC tag
reuse an RC tag
rewrite a stable release tag
silently promote a different commit
```

## Same-SHA promotion

When a candidate verifies and needs no correction, the stable tag is created
from **that same commit**:

```text
main@abc123  →  v1.2.0-rc.1  →  verified  →  v1.2.0 at abc123
```

If anything changed — source, workflow, dependency, even documentation — the
result is a new candidate, not a promotion:

```text
main@abc123  →  v1.2.0-rc.1  →  failed
main@def456  →  v1.2.0-rc.2  →  verified  →  v1.2.0 at def456
```

The rule exists because the artifacts a candidate verified were built from one
specific tree. Promoting a different commit would ship something nobody
verified while claiming the candidate's evidence.

## What automation may do

The release workflow is *triggered by* a tag push (`on: push: tags: ["v*"]`)
and never creates one. Given a tag, it:

- validates the tag is SemVer and that the checkout matches it;
- re-runs the full gate set against the tagged source — format, vet, tests,
  race, PostgreSQL integration, the processor module, `govulncheck` — rather
  than trusting an earlier run on a different commit;
- builds the release artifact matrix and verifies its checksums;
- creates the GitHub Release **as a draft**, with `--verify-tag`, marked as a
  prerelease for candidate tags;
- builds the container image, scans it, and gates on the scan result;
- pushes the immutable image tag with SBOM and provenance attestations;
- signs the pushed digest keylessly with Cosign;
- moves the floating container tags only after signing succeeds, and only for
  a stable release.

What it may **not** do: create, move, or delete a tag; publish the drafted
release; decide that a release should happen.

Drafting rather than publishing is the deliberate seam. A human compares the
generated notes against `CHANGELOG.md` and publishes, so no pipeline run can
announce a release on its own.

## Agent authority

An AI agent may prepare and verify a release:

- run gates, inspect CI, verify published artifacts and signatures;
- draft release notes and open a pull request for them;
- report that a candidate is ready for a human decision.

An agent may **not**, whatever credential it happens to hold:

- create, move, delete, or reuse a `v*` tag;
- publish or edit a GitHub Release;
- promote a candidate to stable;
- repair a failed release by mutating published history;
- use the Organization Admin bypass to do any of the above.

A failed release is fixed by moving forward — fix branch, pull request, review,
next candidate — never by rewriting what was published. See
[Agent Governance](agents.md).

## Human checklist

Before creating a stable tag:

```text
[ ] the candidate's pipeline succeeded end to end
[ ] published artifacts and the image signature verify
[ ] main is unchanged since the candidate's commit
[ ] CHANGELOG.md describes this release
[ ] nightly tiers are green
[ ] I am creating the stable tag at the candidate's exact commit
```

The third line is the one that matters most: if `main` has moved, the answer
is a new candidate, not a promotion.

## Related

- [Release Guide](../release-guide.md) — the release procedure and verification
- [Repository Governance](repository.md) — rulesets, review, and merge authority
- [Branching Strategy](branching.md) — where release commits come from
- [Agent Governance](agents.md) — agent authority and prohibitions

# Historical Documentation

This directory preserves historical engineering material for traceability —
what was decided, specified, and built, at the time it happened.

**It is not current product documentation.** Nothing here is maintained, and
statements in these files were true when written rather than now. Archiving a
document does not deprecate whatever it describes; most of it shipped and is
documented in the current [`docs/`](../README.md) set.

Current sources of truth:

| Question | Document |
|---|---|
| What is Trustvian? | [README.md](../../README.md) |
| How does it work? | [ARCHITECTURE.md](../ARCHITECTURE.md) · [DOMAIN.md](../DOMAIN.md) |
| Where is it going? | [ROADMAP.md](../ROADMAP.md) |
| What shipped? | [CHANGELOG.md](../../CHANGELOG.md) |
| Why was it decided? | [adr/](../adr/README.md) |

## What is here

| Path | Contents |
|---|---|
| [`project-spec.md`](project-spec.md) | The original project specification, curated. It lived at the repository root through `v0.9` and mixed product vision, architecture sketches, and planning — the durable parts are kept, the rest is listed in its own header |
| [`tasks/`](tasks/README.md) | Completed task specifications, grouped by the release milestone that shipped them |
| [`plans/`](plans/) | Historical implementation plans — execution checklists for work that has since landed |

## Reading these

Use them for *why something was done a particular way at the time*: the
constraints, the alternatives considered, and the acceptance criteria a change
was built against. For decisions that still govern the architecture, read the
[decision records](../adr/README.md) instead — those are maintained, and each
one states whether it is still authoritative.

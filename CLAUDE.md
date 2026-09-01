# Trustvian Development Guide

## Project

Trustvian is an open-source behavioral security and trust engine
for applications, services, and AI agents.

## Language

Go.

Use the latest stable Go version supported by the project.

## Architecture

Prefer:

- Clean Architecture
- Hexagonal Architecture
- Small interfaces
- Dependency inversion
- Explicit domain models
- Testable components

Avoid:

- Global state
- Unnecessary abstractions
- Reflection unless justified
- Framework-heavy design
- Premature microservices

## Core Pipeline

Event
 -> Features
 -> Fingerprint
 -> Baseline
 -> Anomaly
 -> Trust
 -> Policy
 -> Decision

## Security

Security decisions must be:

- Explainable
- Deterministic where possible
- Auditable
- Testable

Never silently weaken a security policy.

## Performance

Trustvian is a runtime security engine.

Pay attention to:

- Allocations
- CPU overhead
- Lock contention
- Goroutine leaks
- Memory growth
- Hot paths

Use benchmarks for performance-sensitive code.

## OpenTelemetry

Follow OpenTelemetry semantic conventions.

Do not invent telemetry attributes without documenting them.

## Testing

Every new feature must include tests.

Prefer:

- Unit tests
- Table-driven tests
- Integration tests where necessary
- Benchmarks for hot paths

Run:

go test ./...

before considering a task complete.

## Git

Do not create commits unless explicitly requested.

Do not push to GitHub unless explicitly requested.

## Implementation Strategy

Do not implement large features in one step.

First:

1. Inspect the existing code.
2. Explain the proposed design.
3. Identify affected files.
4. Implement the smallest vertical slice.
5. Run tests.
6. Review the implementation.
7. Benchmark when relevant.
8. Update documentation.

Never rewrite working code unnecessarily.
# ADR-0001: Use a one-way JSON Lines subprocess boundary

## Status

Accepted

## Date

2026-08-20

## Context

ProcessPilot needs genuine native telemetry collection and a local application layer while preserving an unusually narrow trust boundary. Machine telemetry is sensitive, collector output can be malformed, and V1 must be architecturally unable to control processes.

## Decision

Run one unprivileged Rust collector as a fixed Go child process. Rust emits versioned, sanitized JSON snapshots to stdout. Go reads bounded lines, strictly decodes protocol V1, and owns every downstream behavior. The protocol is one-way: no command channel exists.

Rust owns native metric acquisition, normalization, and first-line redaction. Go owns defensive validation, persistence, deterministic analysis, HTTP, CLI, UI, and optional explanation providers.

## Alternatives Considered

### Embed Rust through C FFI

This could reduce process overhead but would increase build complexity and collapse fault isolation. A malformed native result could affect the application process directly.

### Run the collector as a local network service

This would add an unnecessary listener, authentication questions, and another path by which telemetry could become remotely reachable.

### Use shell commands such as `ps`

Shelling out would complicate argument safety, parsing, portability, and resource measurement. It would also weaken the explicit prohibition on arbitrary command execution.

### Implement everything in one language

This would fail the product requirement and erase the deliberate responsibility split: Rust provides a small memory-safe native collector; Go provides a simple concurrent local service and SQLite application layer.

## Consequences

- The collector can crash or emit malformed data without corrupting the Go process.
- The protocol must be versioned, bounded, documented, and contract-tested in both languages.
- Packaging must include both binaries and resolve the collector path without browser influence.
- The subprocess has no input channel for actions, making the V1 observe-only boundary easier to audit.

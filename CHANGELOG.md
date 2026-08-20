# Changelog

All notable changes are documented here. This project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-08-20

### Added

- Unprivileged Rust telemetry collector with protocol-v1 JSON Lines output, path normalization, and secret redaction.
- Strict Go supervision and defensive protocol validation.
- Deterministic application grouping, ownership, purpose, stopping risk, system pressure, and anomaly rules.
- Bounded SQLite history with one-minute top-application sampling and configurable one-hour-to-30-day retention.
- Localhost-only read API, server-rendered responsive dashboard, and sanitized SSE updates.
- Read-only `status`, `top`, `inspect`, and `explain` CLI commands.
- Default deterministic explanations and optional loopback-only Ollama explanations with fallback.
- Clearly labeled deterministic demo mode.
- Explicit architectural security regression tests and desktop/mobile browser tests.
- Checksummed macOS arm64 and amd64 packaging workflow.

### Security

- Raw process snapshots remain memory-only; no command line, environment, file content, or network content is collected or persisted.
- No process-control or arbitrary-command interface exists.
- The dashboard host is fixed to `127.0.0.1`.

[1.0.0]: https://github.com/mateoosoriodelhonte/processpilot/releases/tag/v1.0.0

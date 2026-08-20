# Implementation Plan: ProcessPilot V1

## Goal

Deliver a portfolio-grade, zero-cost macOS monitor that observes ordinary unprivileged telemetry, classifies it deterministically, explains it clearly, and is architecturally unable to control the machine.

## Constraints

- Work only in this repository, its GitHub project, and isolated build/cache locations.
- Bind to `127.0.0.1` by default and never upload telemetry.
- Never request root, invasive entitlements, arbitrary paths, raw process memory, environment variables, file contents, network contents, or command execution.
- Never expose process-control actions or allow AI output to affect classification, anomaly rules, subprocesses, files, settings, or processes.
- Persist only sanitized, bounded telemetry; default retention is seven days.
- Keep Rust and Go responsibilities real, independently tested, and joined by a versioned JSON Lines contract.

## Architecture Decisions

- Rust owns one long-running collector process. It reuses a `sysinfo::System`, refreshes only required metrics, sanitizes metadata before serialization, and writes protocol-v1 JSON Lines to stdout.
- Go owns fixed-executable supervision, defensive protocol validation, deterministic grouping/classification/pressure/anomalies, SQLite history, read-only API/CLI, server-rendered UI, SSE, and explanation providers.
- The collector/server boundary is a subprocess pipe, not a network service. Go treats every collector line as untrusted and enforces size, schema, range, timestamp, and protocol-version limits.
- The UI uses semantic Go templates, CSS, and small progressive-enhancement JavaScript only. All server routes are explicit allowlists.
- `NoAIProvider` is the default. Optional Ollama receives an allowlisted DTO and may connect only to loopback by default.
- Demo mode uses deterministic in-memory fixtures and is visibly labeled `Demo data`.

## Ordered Work

### Phase 1: Governance and foundation

1. Bootstrap the Rust/Go layout, local developer commands, pinned minimum versions, license, ignore rules, and CI skeleton. Verify both toolchains and builds. GitHub issue: #1.
2. Define protocol v1 and shared fixtures before implementing either side. Verify serialization, rejection, and version mismatch. Issues: #2, #21.
3. Establish privacy normalization/redaction invariants with failing tests first. Issue: #4.

### Checkpoint: foundation

- Both projects format, build, and run their initial tests.
- The same protocol fixture is accepted by Rust and Go.
- Raw command-line data is absent from every persistent/wire type.

### Phase 2: Trusted telemetry slice

4. Implement the unprivileged collector for process, hierarchy, CPU, memory, start time, status, machine memory/swap/CPU/load, and safe executable identity. Issue: #3.
5. Implement the fixed-command Go supervisor with graceful shutdown and bounded line ingestion. Issue: #5.
6. Run a real collector-to-Go smoke path on macOS and reject malformed/oversized/version-mismatched lines.

### Checkpoint: telemetry

- A normal user can emit and ingest a real sanitized snapshot.
- No shell, sudo, process signals, file crawling, environment access, or remote listener exists.

### Phase 3: Analysis and history slices

7. Add SQLite migrations, parameterized persistence, indexes, retention validation, and cleanup. Issues: #6, #7.
8. Add deterministic grouping and ownership trees with malformed-graph handling. Issues: #8, #10.
9. Add deterministic classification, conservative stopping risk, and system pressure. Issues: #9, #11.
10. Add deterministic anomaly detection against bounded history. Issue: #12.

### Checkpoint: local intelligence

- Group totals reconcile with process totals.
- Unknown/system identities fail conservative.
- Anomalies include their rule and measured evidence.
- SQLite schema and queries cannot contain raw commands or unbounded history.

### Phase 4: Product slices

11. Add read-only API resources and architectural route-denial tests. Issue: #13.
12. Add the default NoAI provider and optional loopback-only Ollama provider with an allowlisted payload. Issues: #18, #19.
13. Add the launcher and read-only `status`, `top`, `inspect`, and `explain` CLI commands. Issue: #17.
14. Add Overview, Applications, Processes, Process Detail, Privacy, Settings, History, and Anomalies pages. Issues: #14, #16.
15. Add bounded SSE updates and deterministic, visibly labeled demo mode. Issue: #15.

### Checkpoint: usable product

- One command starts the trusted local architecture.
- Every important workflow works without AI.
- Browser smoke tests cover overview, process list/detail, history, anomalies, and privacy.
- No control button, process-control route, command route, or arbitrary path route is present.

### Phase 5: Trust, performance, and shipping

16. Complete the explicit security regression matrix. Issue: #20.
17. Measure collector/server idle CPU and RSS plus representative SQLite growth; record commands, interval, duration, OS, and hardware. Issue: #22.
18. Complete README, architecture, contribution, security, privacy, protocol, classification, development, performance, and threat-model docs. Issue: #23.
19. Add macOS packaging and release asset workflow without privileged helpers or entitlements. Issue: #24.
20. Run full Rust, Go, integration, security, browser, dependency, privacy, performance, and hiring-manager audits; fix genuine findings.
21. Push the feature branch, open one reviewable PR, require green CI, merge, tag `v1.0.0`, publish checksummed assets, and close only issues whose criteria are met. Issue: #25.

## Validation Matrix

| Surface | Proof |
| --- | --- |
| Rust | `cargo fmt --check`, `cargo clippy --all-targets --all-features -- -D warnings`, `cargo test` |
| Go | `gofmt` verification, `go vet ./...`, `go test ./...` |
| Contract | Compile Rust; emit fixture and real snapshot; ingest with Go; reject invalid variants |
| Database | Migration, persistence, parameterization, retention, and growth checks |
| API/security | Route allowlist, localhost default, malformed input, traversal denial, missing process-control/exec paths |
| AI | Payload allowlist, redaction, loopback-only default, timeout/failure behavior, no action path |
| Browser | Runtime checks for critical pages, demo labeling, accessibility basics, console/network cleanliness |
| Packaging | Build both macOS architectures in CI where supported; single-command smoke run |
| Release | Green required checks, reviewed diff, tagged final main SHA, checksummed assets, curated changelog |

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Process metadata leaks secrets | High | Do not collect full command lines; redact at Rust edge and validate again in Go |
| Collector crate exposes control methods | High | Keep control methods out of owned interfaces and regression-scan routes/source/dependencies |
| CPU sampling is inaccurate on first refresh | Medium | Reuse one collector instance and honor the crate's minimum refresh interval |
| SQLite grows without bound | High | Bounded interval, default seven-day retention, indexed deletion, and measured growth |
| AI gains authority | High | Allowlisted immutable explanation DTO; provider returns text only; deterministic results remain authoritative |
| Local telemetry becomes remotely accessible | High | Literal loopback default, explicit opt-in rejection for V1, bind tests, no CORS broadening |
| Toolchains are absent on host | Medium | Install isolated official toolchains in an allowed temporary location and record exact versions |

## Stop Conditions

Stop before any action that would require payment, credentials not already authorized, root/admin access, invasive permissions, destructive history rewriting, public-network exposure, or weakening a privacy boundary. A blocked optional feature must not block independent no-AI/local work.

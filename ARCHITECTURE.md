# Architecture

## Product boundary

ProcessPilot is a local observability and explanation tool for macOS. Its architecture deliberately cannot turn browser or AI input into process control. Reduced visibility is preferred to elevated privilege.

## Components

### Rust collector

`processpilot-collector` is the only component that reads operating-system process telemetry. It reuses one `sysinfo::System` instance and refreshes only CPU, resident memory, executable identity, process relationships, start time, state, physical memory, swap, load, and logical CPU count.

Command lines, environment variables, working directories, open files, process memory, file contents, and network contents are not requested. Names and executable identities are normalized and redacted before serialization.

The collector accepts only `--once` or a bounded `--interval-ms`. It has no stdin command protocol, network listener, process-control interface, or shell execution.

### Go supervisor and protocol validator

Go resolves exactly one regular executable named `processpilot-collector` beside its own resolved binary. It launches that path directly with fixed arguments—never through a shell—and discards collector stderr to avoid accidental telemetry logging.

Stdout is treated as untrusted. The decoder enforces a 4 MiB line limit, valid UTF-8, unique JSON keys, required strict fields, protocol version 1, plausible timestamps, monotonic sequence, finite/ranged metrics, canonical sanitized identities, bounded process counts, unique PIDs, valid hierarchy basics, and SQLite-safe integer/aggregate ranges. A malformed or stale stream stops ingestion rather than widening access or retrying with another collector.

### Deterministic application service

Every accepted snapshot is classified and published as an immutable state copy. Go owns:

- ordered classification signatures and conservative stopping risk;
- bounded parent traversal, ownership confidence, and application grouping;
- deterministic pressure and anomaly rules with measured evidence;
- subscription fan-out with 32 bounded, latest-value channels carrying only compact aggregate updates;
- explanation input construction from an explicit metadata allowlist.

Unknown identities remain unknown and are not merged merely because their names match.

### Storage

SQLite uses the CGO-free `modernc.org/sqlite` driver, WAL mode, parameterized statements, one connection, explicit migrations, indexed history lookups, and foreign-key cascades.

Current raw process evidence remains memory-only. Once per minute, ProcessPilot persists the system sample and at most the 100 highest-memory application aggregates; partial process snapshots persist no application aggregate. Anomaly reads are cached for one minute and globally bounded to the latest ten samples for each of at most the current top 100 application identities, supported by an identity/timestamp index. Cleanup is throttled and deletes samples older than the configured one-hour-to-30-day retention; seven days is the default. Database directories use mode `0700` and the database uses `0600`. Runtime persistence failures degrade history explicitly without discarding the current in-memory snapshot, and a binary refuses a database schema newer than it understands.

### Local HTTP, UI, SSE, and CLI

The Go server constructs only `127.0.0.1:<port>` addresses; V1 exposes no host flag. Non-loopback Host headers and cross-origin browser requests are rejected. Routes are explicit GET allowlists. Query names, overflow-safe pagination, timestamps, limits, and PIDs are validated. Static assets are embedded, local, and dependency-free at runtime.

Security headers include a self-only Content Security Policy, `nosniff`, no-referrer, frame denial, and no-store behavior for telemetry. SSE sends system pressure, at most 100 application aggregates without PID lists, bounded anomalies, truncation status, timestamp, and demo state—not raw process fields. Browser JavaScript reports when a newer snapshot is available instead of mutating only part of a server-rendered state.

The CLI has four fixed read-only commands and a redirect-denying, size-bounded localhost HTTP client.

### Explanation providers

`NoAIProvider` is the default and needs no model or network. Optional Ollama accepts only an HTTP loopback base URL and an allowlisted model name, uses a timeout and response-size cap, forbids redirects, and receives only application/category/risk/CPU/memory plus deterministic reason and impact text.

Provider output is inert escaped text. It cannot change state, classification, anomaly rules, files, commands, settings, or processes. An unavailable Ollama falls back to `NoAIProvider`.

## Runtime sequence

1. Go resolves its packaged sibling collector and opens its private SQLite database.
2. Go obtains and validates one initial Rust snapshot before serving a live dashboard.
3. Go binds a TCP listener to literal loopback and starts the fixed collector stream.
4. Each accepted snapshot replaces in-memory state, optionally records a downsample, and publishes a bounded SSE summary.
5. Control-C or SIGTERM cancels the context. Go shuts down HTTP and its owned collector child. There is no API that can trigger this lifecycle.

Demo mode skips the collector and SQLite file. It uses deterministic values and an in-memory history provider, and marks every page/API state as demo data.

## Why these choices

- **Why Rust for telemetry?** Native telemetry benefits from memory safety, predictable overhead, and a small responsibility surface.
- **Why Go for the application layer?** Go provides straightforward concurrency, strict composition, SQLite orchestration, HTTP, and packaging.
- **Why two languages?** Each owns a genuine boundary; neither is decorative.
- **Why no root?** An invasive monitor would undermine the product's purpose. Missing facts remain unavailable.
- **Why localhost only?** Machine telemetry can be sensitive even after sanitization.
- **Why no automatic control?** Observation and explanation do not establish enough context to modify a machine safely.
- **Why deterministic rules?** Purpose, pressure, risk, and anomaly claims must be reproducible and reviewable.

## External foundations

- [`sysinfo` 0.39.6 documentation](https://docs.rs/sysinfo/0.39.6/sysinfo/)
- [`modernc.org/sqlite` package documentation](https://pkg.go.dev/modernc.org/sqlite)
- [Ollama local API documentation](https://docs.ollama.com/api/introduction)

The accepted cross-language decision is recorded in [ADR-0001](docs/decisions/0001-rust-go-jsonl-boundary.md).

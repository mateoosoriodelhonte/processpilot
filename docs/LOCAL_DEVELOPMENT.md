# Local development

## Requirements

- macOS on Apple Silicon or Intel
- Rust 1.98 with `rustfmt` and `clippy`
- Go 1.27
- `make`
- Node.js 22 and npm only for Playwright tests

The Rust crate declares MSRV 1.95, while V1 CI and `rust-toolchain.toml` pin 1.98. Go uses the version declared in `go.mod`. No root install, privileged service, external database, cloud account, or AI model is required.

## Build and run

```sh
make build
./bin/processpilot
```

The Go binary expects `processpilot-collector` as a regular executable beside it. `make build` creates this exact layout. The dashboard is [http://127.0.0.1:7345](http://127.0.0.1:7345).

Use deterministic demo data when working on UI or documentation:

```sh
make demo
```

Demo mode does not invoke the collector or create a SQLite file.

## Quality gates

```sh
make fmt          # Rust and Go formatting
make lint         # clippy -D warnings and go vet
make test         # both unit/integration-level language suites
make integration  # compile Rust and ingest a real snapshot in Go
make security     # privacy and architectural regression suites
make check        # all of the above
```

Critical browser tests:

```sh
npm ci
npx playwright install chromium
npm test
```

The browser suite starts a labeled demo server on `127.0.0.1:7346`, runs desktop and mobile flows, fails on unexpected remote requests, and applies axe-core WCAG 2.1 AA rules to every primary/detail page.

Release dependency gates:

```sh
cargo audit
govulncheck ./...
npm audit --audit-level=high
```

CI installs pinned audit-tool versions before running these commands.

## Repository layout

```text
collector/             Rust telemetry collector
cmd/processpilot/      Go composition root
internal/protocol/     strict Go decoder
internal/collector/    fixed child supervisor
internal/analysis/     deterministic rules
internal/store/        SQLite migrations/history
internal/app/          immutable service state
internal/web/          API, SSE, templates, static assets
internal/ai/           NoAI and loopback Ollama
internal/security/     architectural regression suite
integration/           real Rust-to-Go contract test
tests/browser/         Playwright critical flows
tools/performance/     aggregate-only storage measurement
docs/                  public design and trust documentation
```

## Protocol changes

Protocol V1 decoders reject unknown fields. Update the Rust type, Go type, `testdata/protocol-v1.json`, both language tests, and [COLLECTOR_PROTOCOL.md](COLLECTOR_PROTOCOL.md) together. An additive field is breaking under strict V1 and normally requires protocol version 2.

## Data and logs

Live runs write only to ProcessPilot's per-user configuration directory. Tests use temporary directories. Runtime messages intentionally avoid process names, paths, payloads, and model prompts. When reporting bugs, use demo or synthetic data.

## Packaging

```sh
make package
```

The packaging script validates an exact `MAJOR.MINOR.PATCH` version and architecture, builds both sibling binaries, creates `dist/processpilot-<version>-darwin-<arch>.tar.gz`, and writes a portable SHA-256 file containing only the archive filename. Archive timestamps default to the current Git commit time (or explicit `SOURCE_DATE_EPOCH`), ownership and ordering are normalized, and gzip timestamps are disabled. CI builds both `arm64` and `amd64` targets, asserts each Mach-O architecture, verifies the archive checksum, and smoke-runs the native packaged pair against its loopback API. It never installs a launch daemon or writes to system locations.

## Upgrade, reinstall, and rollback

Release archives are self-contained directories containing a matched `processpilot` / `processpilot-collector` binary pair. Stop ProcessPilot, verify and extract the new archive into a new directory, and launch it from there. Do not mix binaries from different archives. Reinstalling or replacing the binary directory does not delete the application-owned SQLite history under the per-user ProcessPilot configuration directory.

Before a future schema-changing upgrade, stop ProcessPilot and copy its application-owned database if you need a rollback backup. To roll back, stop the newer binary pair and restart the previously verified pair. A ProcessPilot binary refuses a database schema newer than it supports instead of guessing at compatibility; restore the matching pre-upgrade database backup if that check fails. V1 release rollback is otherwise a binary-directory switch because no daemon, privileged helper, or system installation exists.

## Removing a development build

Stop the server, remove repository build outputs if desired, and separately remove the application-owned `ProcessPilot` configuration directory only if you intend to delete its retained history. There is no background service or privileged installation to uninstall.

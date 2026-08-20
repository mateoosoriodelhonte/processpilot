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

The browser suite starts a labeled demo server on `127.0.0.1:7346`, runs desktop and mobile flows, and fails on unexpected remote requests.

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

The packaging script validates the architecture, builds both sibling binaries, creates `dist/processpilot-<version>-darwin-<arch>.tar.gz`, and writes a SHA-256 file. CI builds both `arm64` and `amd64` targets. It never installs a launch daemon or writes to system locations.

## Removing a development build

Stop the server, remove repository build outputs if desired, and separately remove the application-owned `ProcessPilot` configuration directory only if you intend to delete its retained history. There is no background service or privileged installation to uninstall.

# ProcessPilot

Understand what is using your Mac without giving an app invasive access.

ProcessPilot uses normal unprivileged macOS process telemetry to show what is consuming CPU and memory and explain it in plain language. It does not inspect process memory, files, passwords, browser data, or network traffic, and it never automatically terminates processes.

[![CI](https://github.com/mateoosoriodelhonte/processpilot/actions/workflows/ci.yml/badge.svg)](https://github.com/mateoosoriodelhonte/processpilot/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-087f6b.svg)](LICENSE)

## Privacy and security first

ProcessPilot runs as an ordinary user. It does not ask for administrator access, Full Disk Access, Accessibility, Screen Recording, Input Monitoring, Keychain access, or privileged helpers.

It can observe safe resource facts such as process names, sanitized executable identity, PID relationships, CPU, resident memory, physical memory, swap, and load. It cannot see passwords, file contents, emails, browser history, process-memory contents, keystrokes, clipboard contents, the screen, microphone, camera, or network packet contents.

The dashboard is fixed to `127.0.0.1`, telemetry is not uploaded, raw process snapshots remain in memory, and historical storage contains only one-minute system and top-application aggregates. ProcessPilot has no process-control endpoint, no arbitrary command endpoint, and no control button.

Read the detailed [privacy model](docs/PRIVACY.md), [threat model](docs/THREAT_MODEL.md), and [security policy](SECURITY.md).

## What it does

- Shows current machine CPU, memory, swap, load, and deterministic pressure.
- Groups related processes into applications without guessing unknown ownership.
- Explains known browsers, developer tools, databases, containers, virtual machines, system services, and local AI runtimes.
- Separates observed evidence, deterministic classification, conservative stopping risk, and optional explanation text.
- Detects documented memory spikes, sustained CPU, monotonic growth, and new major consumers.
- Retains bounded local application history for seven days by default.
- Serves Overview, Applications, Processes, Process Detail, History, Anomalies, Privacy, and Settings pages.
- Provides read-only `status`, `top`, `inspect`, and `explain` CLI commands.
- Works fully without AI. Optional Ollama explanations use a loopback-only, allowlisted payload and fail back to deterministic text.

ProcessPilot observes and explains. It never controls processes.

## Run a release archive

Download the archive and matching `.sha256` file for `darwin-arm64` (Apple Silicon) or `darwin-amd64` (Intel) from [Releases](https://github.com/mateoosoriodelhonte/processpilot/releases).

```sh
shasum -a 256 -c processpilot-1.0.0-darwin-arm64.tar.gz.sha256
tar -xzf processpilot-1.0.0-darwin-arm64.tar.gz
cd processpilot-1.0.0-darwin-arm64
./processpilot
```

Then visit [http://127.0.0.1:7345](http://127.0.0.1:7345). Press Control-C to stop both owned processes cleanly.

V1 release archives are checksummed but not signed or notarized because the project has no Apple Developer signing credentials. If macOS refuses an unsigned download, build from reviewed source instead of weakening Gatekeeper.

## Build from source

Requirements: macOS, Rust 1.98, Go 1.27, and `make`. Node.js 22 is needed only for browser tests.

```sh
git clone https://github.com/mateoosoriodelhonte/processpilot.git
cd processpilot
make check
make build
./bin/processpilot
```

`make build` coordinates both languages and places the two required sibling binaries in `bin/`. No global install or root access is needed.

For a safe product tour that never reads this Mac's telemetry:

```sh
./bin/processpilot demo
```

Every demo page is visibly labeled `Demo data`.

## CLI

Start ProcessPilot in one terminal, then use another:

```sh
./bin/processpilot status
./bin/processpilot top
./bin/processpilot inspect 95707
./bin/processpilot explain 95707
```

The CLI calls fixed GET routes on `127.0.0.1:7345`. There are deliberately no `kill`, `stop`, `restart`, or `exec` commands.

If the server uses a different allowed port, append `--port 8123` to the CLI command (after the PID for `inspect` or `explain`).

## Local API

All V1 endpoints are read-only GET routes below `http://127.0.0.1:<port>`:

- `/api/v1/system`
- `/api/v1/applications?page=1&pageSize=50`
- `/api/v1/processes?page=1&pageSize=50`
- `/api/v1/processes/{pid}`
- `/api/v1/history?application=Ollama&since=<RFC3339>&limit=1000`
- `/api/v1/anomalies`
- `/api/v1/events` (Server-Sent Events)

Success responses use a `data` field; list endpoints add bounded pagination metadata. Errors use `{"error":{"code":"...","message":"..."}}`. Unknown query parameters and unsupported methods fail closed. There is no CORS opt-in, command body, control resource, arbitrary path parameter, or remote bind option.

## Configuration

```sh
./bin/processpilot \
  --port 7345 \
  --interval 2s \
  --retention 7d
```

Accepted collection intervals are 500 milliseconds through 60 seconds. Retention is one hour through 30 days. The host is not configurable in V1.

Optional local Ollama explanation:

```sh
./bin/processpilot \
  --ollama-model gemma3 \
  --ollama-endpoint http://127.0.0.1:11434
```

ProcessPilot never starts or stops Ollama. Non-loopback endpoints, credentials in URLs, redirects, and unsafe model names are rejected. If Ollama is unavailable, explanations remain available through the deterministic provider.

## Architecture

Rust owns narrow, unprivileged telemetry collection, normalization, and edge redaction. Go owns strict protocol validation, the fixed child-process supervisor, deterministic analysis, SQLite history, localhost HTTP/SSE, CLI, templates, and optional explanations.

The only collector boundary is a one-way, versioned JSON Lines stdout pipe:

```text
ordinary macOS APIs
        │
        ▼
Rust collector ── bounded JSONL/stdout ──▶ Go validator and service
                                                │
                         ┌──────────────────────┼─────────────────────┐
                         ▼                      ▼                     ▼
                  in-memory current       bounded SQLite       localhost UI/API
                   process evidence       aggregate history      and read-only CLI
```

See [ARCHITECTURE.md](ARCHITECTURE.md), the [collector protocol](docs/COLLECTOR_PROTOCOL.md), and [classification rules](docs/PROCESS_CLASSIFICATION.md).

## Development and contribution

See [local development](docs/LOCAL_DEVELOPMENT.md) and [CONTRIBUTING.md](CONTRIBUTING.md). The complete local gate is:

```sh
make check
npm ci
npx playwright install chromium
npm test
```

Measured overhead and the storage redesign are recorded in [docs/PERFORMANCE.md](docs/PERFORMANCE.md). Project changes follow the issue → branch → tests → PR → green CI → merge workflow.

## License

[MIT](LICENSE)

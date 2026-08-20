# Threat model

## Security objective

ProcessPilot should help one local macOS user understand resource consumption while collecting the least information needed and remaining architecturally unable to control the machine.

## Protected assets

- Process and application telemetry, including names and executable identity.
- Historical system/application aggregates in SQLite.
- Credentials, files, browser stores, process memory, input, screen/audio/video, and network contents that must remain outside the product.
- Integrity of deterministic classifications, stopping-risk guidance, pressure, and anomaly evidence.
- The localhost-only network boundary.

## Assumptions

- The user obtained ProcessPilot from source or verified a release checksum.
- The packaged sibling collector has not been replaced by malware.
- The macOS kernel and the user's account are not fully compromised.
- Other code running as the same user is not considered an authenticated remote tenant.
- A configured Ollama server is controlled by the same user and listens on loopback.

## Allowed telemetry

Only sanitized process name/executable identity, PID relationships, CPU, resident memory, start time, process state, physical memory, swap, system CPU/load, timestamp, and logical CPU count are allowed across protocol V1. Current process evidence is memory-only. SQLite receives one-minute system and top-100 application aggregates.

## Prohibited access

The collector and application must not acquire raw command lines, environments, working directories, arbitrary files, file contents, open files, process memory, credentials, Keychain, browser data, input, clipboard, screen, microphone, camera, contacts, location, packets, or request bodies. They must not request root, sudo, invasive privacy permissions, privileged helpers, daemons, kernel/system extensions, or entitlements.

## Trust boundaries

### macOS to Rust

Operating-system process data is treated as untrusted text and potentially inconsistent state. The collector bounds, normalizes, redacts, and serializes only the allowlist. Invalid OS strings are handled lossily and control characters are removed.

### Rust subprocess to Go

Go launches one resolved regular sibling named `processpilot-collector` with fixed arguments and no shell. Stdout is a one-way channel. Go applies its own 4 MiB, strict-schema, version, range, length, cardinality, and identity validation. A malformed line stops ingestion. No browser/API/model value can reach collector arguments.

### Localhost HTTP

The listener address is constructed as `127.0.0.1:<validated-port>`; there is no host option. Routes and methods are explicit GET allowlists. Query keys and values are validated. Static files come only from an embedded filesystem. Arbitrary filesystem paths, process control, and commands have no route.

Telemetry responses are no-store and receive CSP, frame, MIME-sniffing, and referrer protections. SSE carries aggregate summaries only and caps concurrent subscribers.

### SQLite

Only application-owned absolute database paths are used in production. Statements are parameterized, migrations are transactional, values are validated before storage, history reads have hard limits, collection is downsampled, and retention is bounded. Raw process tables are removed by migration 3.

### AI

The provider interface has one method: explain an allowlisted value object and return text. Optional Ollama must be HTTP loopback, denies redirects, and has request timeout, response-size, output-length, model-name, and completion checks.

The allowlist includes a sanitized application/process display name, which can still be sensitive for an unknown process. It excludes PID, executable path, user, command, environment, file, credential, and token fields. Model text is escaped by Go templates/JSON and has no interpreter or action consumer. Deterministic classification and risk remain authoritative. Provider failure falls back to deterministic text.

### Subprocess lifecycle

The Go parent owns and cancels the collector it starts. This lifecycle is not exposed through HTTP or CLI. The product contains no signal, kill, reprioritization, arbitrary exec, or remote subprocess interface.

## Secret-redaction strategy

1. Do not collect broad sources such as command lines or environments.
2. Preserve only identity needed for classification.
3. Collapse private home paths before the protocol boundary.
4. Remove URL credentials, obvious secret assignments/arguments, bearer values, controls, and invalid text.
5. Bound all wire values and reject unexpected Go fields.
6. Avoid telemetry logging and remote error reporting.

Redaction cannot guarantee that a malicious process name contains no secret; this is why network egress is absent and persistence is aggregate-only.

## Threats and mitigations

| Threat | Mitigation |
| --- | --- |
| Public telemetry exposure | Literal loopback construction, no host flag, bind regression tests |
| Shell/command injection | No shell; only fixed sibling exec; no command route or field |
| Process control | No owned control API, signal call, UI button, or CLI command; static regression scan |
| Path traversal | Embedded static FS, explicit routes, no user filesystem path, traversal probes |
| Malformed collector output | Size cap, strict decoder, version and range validation, fail-closed stream |
| SQL injection or growth | Parameterized SQL, limits, indexes, one-minute/top-100 downsampling, retention |
| Secret leakage to AI | Reflection-tested input allowlist and loopback-only destination |
| AI prompt injection | Model output is inert text; no action/tool interface |
| Cross-site framing/script injection | CSP, frame denial, escaped templates, local static assets |
| False confidence | Unknown remains unknown; risk is conservative; evidence and inference are separated |
| Supply-chain replacement | Locked Rust/Go/npm dependencies, CI, checksums; sibling regular-file validation |

## Known limitations and residual risk

- V1 localhost endpoints have no authentication. Hostile same-user software could read them.
- Release archives are not Apple-signed or notarized.
- The sibling collector is checked by name/type but not a cryptographic runtime signature.
- Process identity signatures can be incomplete or stale as third-party software changes.
- PID relationships are a point-in-time view and can race process exit/reuse.
- CPU and memory meanings follow `sysinfo` and macOS reporting; they are observational, not billing-quality.
- Secret redaction recognizes obvious patterns, not every possible secret embedded in an allowed name.
- V1 has no encrypted database. It relies on per-user permissions and platform disk protection.
- Port forwarding or reverse proxying defeats the intended network boundary and is unsupported.

## Security verification

`internal/security/architecture_test.go`, Rust privacy/binary tests, protocol tests, AI allowlist tests, API negative-route tests, and browser remote-request checks encode this model as regression gates. Run `make security` and `make check` before release.

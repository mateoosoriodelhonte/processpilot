# Collector Protocol

ProcessPilot protocol version 1 is a one-way JSON Lines stream from the trusted Rust collector's stdout to the Go supervisor. The collector does not listen on a socket, and the Go service never sends process identifiers or commands back to it.

Each line is one complete UTF-8 JSON snapshot. The maximum accepted encoded line is 4 MiB. Go treats collector output as untrusted even when it launched the expected binary.

## Snapshot

| Field | Type | Meaning |
| --- | --- | --- |
| `protocolVersion` | unsigned integer | Must equal `1` |
| `timestampUnixMs` | unsigned integer | Snapshot time in Unix milliseconds |
| `sequence` | unsigned integer | Monotonic collector-local sequence number |
| `system` | object | Machine-wide unprivileged resource measurements |
| `processes` | array | At most 100,000 sanitized process samples |

The system object contains physical memory totals, available/used memory, swap totals, system CPU percentage, 1/5/15-minute load averages, and logical CPU count.

Each process contains only:

- PID and optional parent PID
- sanitized process name
- optional sanitized executable identity
- CPU percentage and resident memory bytes
- process start time in Unix seconds
- normalized process state

There is deliberately no command-line, environment, working-directory, open-file, network, file-content, process-memory, or process-control field.

Raw process snapshots are current-state input only and remain in Go memory. SQLite history receives one-minute system and top-application aggregates, not this process array.

## Validation

The Go decoder rejects:

- empty or larger-than-4-MiB lines
- malformed JSON or trailing JSON values
- unknown fields at any object level
- protocol versions other than 1
- missing, empty, duplicate, self-parenting, or out-of-range process data
- impossible memory, CPU, load, swap, timestamp, or logical-CPU values
- overlong names, executable identities, states, or process arrays

Protocol errors stop ingestion of the offending snapshot. They do not trigger retries with elevated privileges, alternate collectors, shell commands, or system changes.

## Compatibility

V1 follows a strict one-version rule. Additive protocol changes require a new protocol version when strict V1 decoders would reject the field. Rust serialization tests and Go decoding tests share `testdata/protocol-v1.json`; cross-language CI compiles the collector and validates its output with the Go decoder.

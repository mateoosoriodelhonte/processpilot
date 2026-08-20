# Privacy model

Privacy is a product feature in ProcessPilot, not a policy added after implementation.

## What ProcessPilot can see

Through ordinary unprivileged macOS process APIs, the Rust collector can observe:

- sanitized process names and executable identity;
- PID and optional parent PID relationships;
- process CPU percentage, resident memory, start time, and state;
- physical memory totals, available/used memory, swap totals, system CPU, load averages, and logical CPU count.

The current process list exists in Go memory so the local user can inspect it. Once per minute, SQLite stores only the system sample and up to the 100 highest-memory application aggregates. Raw per-process snapshots are not persisted. Default retention is seven days, with a configurable range of one hour through 30 days.

## What ProcessPilot cannot see

ProcessPilot does not request or inspect:

- passwords, credentials, API keys, tokens, or Keychain contents;
- raw process command lines or environment variables;
- working directories, arbitrary paths, open files, or file contents;
- process-memory or RAM contents;
- browser history, cookies, profiles, databases, or form contents;
- emails, messages, documents, photos, contacts, calendars, or location;
- keystrokes, input events, clipboard contents, screen contents, or screenshots;
- microphone or camera data;
- network packets, request/response bodies, DNS history, or socket contents.

ProcessPilot does not require administrator/root access, Full Disk Access, Accessibility, Input Monitoring, Screen Recording, Keychain access, Automation permission, privileged daemons, kernel extensions, or system extensions.

## Executable identity normalization

Executable paths help distinguish a browser helper from an unknown process, but user paths can reveal project names. The Rust edge applies these rules before data crosses into Go:

- system `/Applications/*.app` identity is preserved;
- a user's `~/Applications/*.app` identity retains only the application-relative form;
- other home-directory executables become `~/<private>/<basename>`;
- invalid text, control characters, URL credentials, and obvious secret assignments or arguments are removed or replaced;
- strings are bounded before serialization.

Redaction is defense in depth, not permission to collect raw command lines. Those are excluded at acquisition and from the protocol entirely.

## Local processing and network behavior

The dashboard and API listen on literal `127.0.0.1`; the host cannot be configured in V1. Static assets are embedded. There is no analytics, remote dashboard, telemetry upload, crash reporter, CDN asset, web font, or CORS broadening.

No-AI mode performs every important feature locally. If a user explicitly configures Ollama, ProcessPilot sends a small allowlisted explanation summary only to an HTTP loopback address: sanitized application/process display name, category, risk, CPU, memory, and deterministic reason/impact. It never sends PID, executable path, username, command line, environment, or file data to the model. Redirects and non-loopback endpoints are rejected.

## Local files and removal

Live mode stores `processpilot.db` below the operating system's per-user configuration directory, under `ProcessPilot/`. On macOS this is normally `~/Library/Application Support/ProcessPilot/`. The directory is protected as `0700` and the database as `0600`.

Stop ProcessPilot before removing the database. Removing that one application-owned directory deletes retained history. Demo mode creates no persistent database.

## Limits

A process name itself can be sensitive, and any software running as the same macOS user may be able to connect to localhost or inspect the user's memory/files subject to operating-system controls. ProcessPilot does not claim to defend against a fully compromised same-user account or operating system. Do not expose its port through a proxy, tunnel, container port mapping, or SSH forwarding.

See [THREAT_MODEL.md](THREAT_MODEL.md) for the full trust analysis.

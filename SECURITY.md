# Security policy

## Supported versions

| Version | Security fixes |
| --- | --- |
| 1.0.x | Supported |
| Pre-release commits | Best effort on current `main` |

## Report a vulnerability privately

Use GitHub's **Report a vulnerability** flow on the repository Security page. Include the affected version, boundary, minimal reproduction, and impact.

Do not include real process lists, executable paths, usernames, secrets, database files, browser data, or other machine telemetry. Use the deterministic demo fixture or synthetic values. Do not open a public issue until a maintainer confirms disclosure timing.

## Security invariants

A report is especially important if ProcessPilot can:

- bind outside loopback or upload telemetry;
- read files, process memory, command lines, environment variables, credentials, browser stores, input, screen, audio/video, or packet contents;
- execute arbitrary or AI-generated commands;
- stop, signal, reprioritize, or otherwise control a process;
- require root, privileged helpers, or invasive macOS permissions;
- traverse arbitrary filesystem paths through HTTP;
- bypass strict collector validation or persist prohibited data.

These are release-blocking architectural boundaries, not optional hardening.

## Response expectations

The maintainer will acknowledge a complete private report as capacity allows, reproduce it against a supported version, and coordinate a fix and disclosure. This volunteer project does not promise a paid bounty or fixed SLA.

## Deployment notes

- ProcessPilot is intended for one trusted local user on one Mac.
- V1 archives are checksummed but not Apple-signed or notarized.
- The localhost service has no authentication because it is not designed for hostile same-user local code. Do not proxy or port-forward it.
- Optional Ollama inherits the security and model-trust properties of the user's local Ollama installation, but model output remains text-only in ProcessPilot.

See [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) for assumptions and limitations.

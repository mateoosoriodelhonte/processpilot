# Contributing

Thank you for helping make ProcessPilot useful without weakening its trust model.

## Before changing code

1. Search the [issue tracker](https://github.com/mateoosoriodelhonte/processpilot/issues).
2. Open or claim a focused issue for non-trivial work.
3. Read [ARCHITECTURE.md](ARCHITECTURE.md), [docs/PRIVACY.md](docs/PRIVACY.md), and [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md).
4. Branch from current `main`. Use a descriptive branch such as `feature/history-export` or `fix/protocol-range`.

Changes that add root requirements, invasive macOS permissions, public binding, telemetry upload, raw command-line persistence, arbitrary command execution, or process control are outside V1's product boundary and will not be accepted as ordinary feature work.

## Set up

Follow [docs/LOCAL_DEVELOPMENT.md](docs/LOCAL_DEVELOPMENT.md). Rust 1.98 and Go 1.27 are pinned for V1. Node.js 22 is needed only for browser tests.

## Develop incrementally

- Add or update a failing test before changing behavior.
- Keep the Rust/Go protocol strict and update its shared fixture deliberately.
- Keep classifications deterministic and document any new signature and impact.
- Treat collector output, query values, model output, and SQLite strings as untrusted.
- Do not log live process names, executable identities, model prompts, or telemetry payloads.
- Update public docs and an ADR when a boundary or compatibility contract changes.

## Verify

```sh
make check
npm ci
npx playwright install chromium
npm test
make build
```

For packaging changes, run `make package` and verify the generated checksum. Never knowingly open a PR with red local gates.

## Pull requests

PRs should link their issue and explain:

- the outcome and architectural fit;
- tests and runtime evidence;
- privacy/security impact;
- compatibility and migration impact;
- performance impact where applicable;
- rollback or removal path.

Keep commits cohesive and reviewer-readable. Do not mix unrelated formatting or generated artifacts. A maintainer merges only after required CI is green.

## Security reports

Do not place vulnerabilities or live telemetry in public issues. Follow [SECURITY.md](SECURITY.md).

Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

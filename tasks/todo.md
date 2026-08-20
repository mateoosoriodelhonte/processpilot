# ProcessPilot V1 Task List

- [ ] #1 Bootstrap Rust/Go repository
- [x] #2 Define the versioned collector protocol
- [x] #3 Implement unprivileged macOS telemetry collector
- [x] #4 Add privacy normalization and redaction layer
- [x] #5 Build the Go collector supervisor
- [x] #6 Add SQLite persistence and migrations
- [x] #7 Implement bounded telemetry retention
- [x] #8 Build deterministic process grouping
- [x] #9 Build deterministic classification and stopping-risk rules
- [x] #10 Build process ownership tree
- [x] #11 Add system-pressure model
- [x] #12 Implement deterministic anomaly detection
- [ ] #13 Build the localhost-only v1 API
- [ ] #14 Build the semantic dashboard
- [ ] #15 Add Server-Sent Events updates
- [ ] #16 Add history and anomaly views
- [ ] #17 Build the read-only CLI
- [ ] #18 Add NoAI explanation provider
- [ ] #19 Add optional local Ollama explanations
- [ ] #20 Add security regression suite
- [ ] #21 Add cross-language contract tests
- [ ] #22 Measure and bound performance
- [ ] #23 Complete privacy, architecture, and threat-model documentation
- [ ] #24 Package ProcessPilot for macOS
- [ ] #25 Release ProcessPilot v1.0

## Standing Definition of Done

- [ ] Acceptance criteria and negative security criteria pass.
- [ ] Runtime behavior is verified, not only compiled.
- [ ] New behavior has tests that fail without it and pass with it.
- [ ] Formatting, linting, tests, builds, and integration checks are green.
- [ ] Public behavior, contracts, and architectural decisions are documented.
- [ ] Final diff contains only intended work and no secrets or generated build output.
- [ ] Security, privacy, observability, rollback/removal, and compatibility are reviewed.

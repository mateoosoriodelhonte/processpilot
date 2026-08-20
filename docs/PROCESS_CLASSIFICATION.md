# Process Classification

ProcessPilot separates measurement from interpretation. Classification is deterministic Go code; optional AI cannot change a category, application owner, stopping-risk level, system-pressure state, or anomaly.

## Inputs

Classification may use only the sanitized process name, sanitized executable identity, PID/parent PID, and collected parent hierarchy. It never uses raw command lines, environment variables, working directories, files, process memory, network contents, or model output.

## Categories

V1 recognizes browsers and helpers, editors/IDEs, compilers, databases, container runtimes, virtual machines, AI inference, language servers, system services, user application bundles, and unknown processes. Signatures use exact executable/name matches, anchored worker prefixes, or exact `.app` bundle identities; incidental substrings remain unknown. An application bundle alone identifies ownership but does not prove purpose.

## Stopping risk

- `LOW`: interruption is normally limited to a replaceable developer task such as a compiler or language server.
- `MEDIUM`: interruption can close active application work or an inference/browser session.
- `HIGH`: interruption can affect the operating system, a database, containers, a virtual machine, or dependent services.
- `UNKNOWN`: available safe metadata is insufficient. The guidance is always: do not terminate the process based solely on ProcessPilot.

ProcessPilot never says “safe to kill.” Risk is informational and conservative because system behavior, unsaved state, and dependent applications vary.

## Ownership and grouping

Ownership walks parent PIDs with a visited set and a 64-node bound. Cycles, missing parents, and unknown identities remain low-confidence. The outermost recognized non-system ancestor or `.app` bundle may establish an owner; a generic `launchd` ancestor does not make an unknown child a known system service. Unknown processes are keyed internally by PID plus process start time rather than grouped merely because names match. This internal key keeps storage rows and PID reuse distinct and is not serialized in the API.

Application CPU and memory are sums of member observations. Member PIDs are retained so users can move from grouped application evidence to raw process evidence.

History is sampled once per minute and retains at most the 100 highest-memory application groups. Current classifications and raw-process inspection still use every accepted live snapshot.

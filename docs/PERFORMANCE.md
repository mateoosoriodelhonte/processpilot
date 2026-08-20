# Performance

ProcessPilot's performance policy is to measure first, fix demonstrated problems, and publish the measurement limits.

## V1 measurement environment

- Date: 2026-08-20
- macOS 26.5.2 (build 25F84), arm64
- 64 GiB physical memory, 12 logical CPUs as reported by the collector
- Rust 1.98.0 and Go 1.27.0
- Release binaries; default two-second collection interval
- CPU/RSS sampled with macOS `ps` once per second after warm-up

These are measurements from one development Mac, not universal guarantees. `ps` reports CPU to one decimal place and sampling can intersect or miss short collector refresh bursts.

## Runtime measurements

| Component/workload | Samples | Mean CPU | Maximum CPU | Mean RSS | Maximum RSS |
| --- | ---: | ---: | ---: | ---: | ---: |
| Rust collector, live process scan every 2 s | 30 × 1 s | 0.523% | 1.2% | 8,944 KiB | 8,944 KiB |
| Go demo server, deterministic analysis every 2 s, no browser connected | 30 × 1 s after 5 s warm-up | below `ps` 0.1% resolution | below `ps` 0.1% resolution | 18,137.6 KiB | 18,144 KiB |

The demo server measurement exercises Go state replacement, deterministic analysis, anomaly evaluation, and the local HTTP listener. It excludes live SQLite writes; those are measured independently below.

Commands used were equivalent to:

```sh
./target/release/processpilot-collector --interval-ms 2000 >/dev/null
./bin/processpilot demo --port 7348 --interval 2s
ps -o %cpu=,rss= -p <owned-pid>
```

Each owned process was stopped after sampling. No administrator privileges were used.

## SQLite growth: measured problem and fix

The first profile persisted every process and application every two seconds. A 30-minute logical workload based on a real 421-process/303-application snapshot created a 128,233,472-byte database and spent 15.6 seconds writing. That was not acceptable for a lightweight monitor.

V1 now:

- keeps raw process snapshots in memory only;
- stores history once per minute;
- stores at most the 100 highest-memory application aggregates per retained sample;
- keeps the system sample, parameterized/indexed queries, and bounded retention;
- runs retention cleanup at most once per ten minutes.

The final profile used a fresh real snapshot with 416 processes and 304 application groups. It fed 900 two-second collector snapshots (30 logical minutes), retained 30 one-minute samples, and produced:

| Metric | Measured value |
| --- | ---: |
| SQLite database including any WAL/SHM files after close | 450,560 bytes (440 KiB) |
| Actual write time for the 900 calls / 30 retained transactions | 26 ms |
| Bytes per retained sample including schema/page overhead | 15,018.7 bytes |

That run was 99.6% smaller than the pre-fix profile, although the live process counts differed slightly between runs. A straight-line estimate at the measured maximum 100 applications is about 144.4 MiB for the default seven-day window; this is an estimate, not a seven-day observation. SQLite page allocation, application count, name lengths, and churn can change actual growth.

Reproduce the aggregate-only storage measurement after building the release collector:

```sh
cargo build --release --locked
go run ./tools/performance
```

The tool creates and removes its own temporary database and prints only counts, durations, and byte totals—never process names or paths.

## Web asset budget

The server-rendered UI has no runtime package/CDN dependency. At V1 measurement:

- JavaScript: 838 bytes (budget: 10 KiB)
- CSS: 9,838 bytes (budget: 50 KiB)

`internal/performance/budget_test.go` enforces these uncompressed limits. JavaScript is deferred and only maintains the SSE status/timestamp/pressure enhancement; the pages remain usable without it.

## Operational guidance

The two-second default stayed because measured collector overhead remained modest and current-state responsiveness benefits from it. Historical writes are independently downsampled to one minute. If a future collector regression materially raises CPU/RSS, profile the refresh set and consider a longer collection default rather than hiding the cost.

Repeat measurements for material collector, analysis, SQLite schema, history cadence, template, or dependency changes. Record before/after values and do not substitute projections for observations.

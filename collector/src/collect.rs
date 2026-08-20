use std::fmt;
use std::time::{SystemTime, UNIX_EPOCH};

use sysinfo::{CpuRefreshKind, ProcessRefreshKind, ProcessesToUpdate, System, UpdateKind};

use crate::protocol::{PROTOCOL_VERSION, ProcessSample, Snapshot, SystemSample};
use crate::redaction::{sanitize_executable_path, sanitize_process_name};

#[derive(Debug)]
pub struct CollectError(&'static str);

impl fmt::Display for CollectError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.0)
    }
}

impl std::error::Error for CollectError {}

pub struct TelemetryCollector {
    system: System,
    sequence: u64,
}

impl Default for TelemetryCollector {
    fn default() -> Self {
        Self::new()
    }
}

impl TelemetryCollector {
    pub fn new() -> Self {
        let mut collector = Self {
            system: System::new(),
            sequence: 0,
        };
        collector
            .system
            .refresh_cpu_list(CpuRefreshKind::nothing().with_cpu_usage());
        collector.refresh();
        collector
    }

    pub fn sample(&mut self) -> Result<Snapshot, CollectError> {
        self.refresh();
        let timestamp_unix_ms = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|_| CollectError("system time is before the Unix epoch"))?
            .as_millis()
            .try_into()
            .map_err(|_| CollectError("system timestamp exceeds protocol range"))?;

        self.sequence = self.sequence.saturating_add(1);
        let logical_cpu_count = self.system.cpus().len().max(1);
        let load = System::load_average();
        let mut processes: Vec<ProcessSample> = self
            .system
            .processes()
            .values()
            .filter_map(|process| {
                let pid = process.pid().as_u32();
                let start_time_unix_seconds = process.start_time();
                if pid == 0 || start_time_unix_seconds == 0 {
                    return None;
                }

                let parent_pid = process
                    .parent()
                    .map(|parent| parent.as_u32())
                    .filter(|parent| *parent != pid && *parent != 0);
                Some(ProcessSample {
                    pid,
                    parent_pid,
                    name: sanitize_process_name(&process.name().to_string_lossy()),
                    executable: process.exe().and_then(sanitize_executable_path),
                    cpu_percent: finite_percent(
                        process.cpu_usage(),
                        logical_cpu_count as f32 * 100.0,
                    ),
                    memory_bytes: process.memory(),
                    start_time_unix_seconds,
                    status: format!("{:?}", process.status()),
                })
            })
            .collect();
        processes.sort_unstable_by_key(|process| process.pid);

        Ok(Snapshot {
            protocol_version: PROTOCOL_VERSION,
            timestamp_unix_ms,
            sequence: self.sequence,
            system: SystemSample {
                total_memory_bytes: self.system.total_memory(),
                used_memory_bytes: self.system.used_memory(),
                available_memory_bytes: self.system.available_memory(),
                total_swap_bytes: self.system.total_swap(),
                used_swap_bytes: self.system.used_swap(),
                cpu_percent: finite_percent(self.system.global_cpu_usage(), 100.0),
                load_average_1: finite_non_negative(load.one),
                load_average_5: finite_non_negative(load.five),
                load_average_15: finite_non_negative(load.fifteen),
                logical_cpu_count,
            },
            processes,
        })
    }

    fn refresh(&mut self) {
        self.system.refresh_memory();
        self.system.refresh_cpu_usage();
        self.system.refresh_processes_specifics(
            ProcessesToUpdate::All,
            true,
            ProcessRefreshKind::nothing()
                .with_cpu()
                .with_memory()
                .with_exe(UpdateKind::OnlyIfNotSet)
                .without_tasks(),
        );
    }
}

fn finite_percent(value: f32, maximum: f32) -> f32 {
    if value.is_finite() {
        value.clamp(0.0, maximum)
    } else {
        0.0
    }
}

fn finite_non_negative(value: f64) -> f64 {
    if value.is_finite() {
        value.max(0.0)
    } else {
        0.0
    }
}

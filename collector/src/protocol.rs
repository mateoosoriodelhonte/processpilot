use serde::{Deserialize, Serialize};

pub const PROTOCOL_VERSION: u32 = 1;

#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Snapshot {
    pub protocol_version: u32,
    pub timestamp_unix_ms: u64,
    pub sequence: u64,
    pub system: SystemSample,
    pub processes: Vec<ProcessSample>,
}

#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct SystemSample {
    pub total_memory_bytes: u64,
    pub used_memory_bytes: u64,
    pub available_memory_bytes: u64,
    pub total_swap_bytes: u64,
    pub used_swap_bytes: u64,
    pub cpu_percent: f32,
    pub load_average_1: f64,
    pub load_average_5: f64,
    pub load_average_15: f64,
    pub logical_cpu_count: usize,
}

#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessSample {
    pub pid: u32,
    pub parent_pid: Option<u32>,
    pub name: String,
    pub executable: Option<String>,
    pub cpu_percent: f32,
    pub memory_bytes: u64,
    pub start_time_unix_seconds: u64,
    pub status: String,
}

use serde::{Deserialize, Serialize};

pub const PROTOCOL_VERSION: u32 = 1;
pub const MAX_LINE_BYTES: usize = 4 << 20;

#[derive(Clone, Debug, Deserialize, PartialEq, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Snapshot {
    pub protocol_version: u32,
    pub timestamp_unix_ms: u64,
    pub sequence: u64,
    pub system: SystemSample,
    pub processes_truncated: bool,
    pub processes: Vec<ProcessSample>,
}

pub fn encode_bounded_snapshot(mut snapshot: Snapshot) -> Result<Vec<u8>, serde_json::Error> {
    let estimated_size = 1_024_usize.saturating_add(
        snapshot
            .processes
            .iter()
            .map(estimated_process_wire_size)
            .fold(0_usize, usize::saturating_add),
    );
    if estimated_size <= MAX_LINE_BYTES {
        let encoded = serde_json::to_vec(&snapshot)?;
        if encoded.len() <= MAX_LINE_BYTES {
            return Ok(encoded);
        }
    }

    snapshot.processes_truncated = true;
    snapshot.processes.sort_by(|left, right| {
        right
            .memory_bytes
            .cmp(&left.memory_bytes)
            .then_with(|| right.cpu_percent.total_cmp(&left.cpu_percent))
            .then_with(|| left.pid.cmp(&right.pid))
    });
    let mut retained_size = 1_024_usize;
    let retained = snapshot
        .processes
        .iter()
        .take_while(|process| {
            let next = retained_size.saturating_add(estimated_process_wire_size(process));
            if next > MAX_LINE_BYTES {
                return false;
            }
            retained_size = next;
            true
        })
        .count();
    snapshot.processes.truncate(retained);
    let mut encoded = serde_json::to_vec(&snapshot)?;
    while encoded.len() > MAX_LINE_BYTES && !snapshot.processes.is_empty() {
        snapshot.processes.pop();
        encoded = serde_json::to_vec(&snapshot)?;
    }
    snapshot
        .processes
        .sort_unstable_by_key(|process| process.pid);
    serde_json::to_vec(&snapshot)
}

fn estimated_process_wire_size(process: &ProcessSample) -> usize {
    let text_bytes = process
        .name
        .len()
        .saturating_add(process.executable.as_ref().map_or(0, String::len))
        .saturating_add(process.status.len());
    512_usize.saturating_add(text_bytes.saturating_mul(2))
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

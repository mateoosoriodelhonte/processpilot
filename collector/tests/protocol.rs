use processpilot_collector::protocol::{PROTOCOL_VERSION, ProcessSample, Snapshot, SystemSample};

fn sample_snapshot() -> Snapshot {
    Snapshot {
        protocol_version: PROTOCOL_VERSION,
        timestamp_unix_ms: 1_787_256_000_000,
        sequence: 42,
        system: SystemSample {
            total_memory_bytes: 68_719_476_736,
            used_memory_bytes: 54_760_833_024,
            available_memory_bytes: 13_958_643_712,
            total_swap_bytes: 17_179_869_184,
            used_swap_bytes: 11_381_663_334,
            cpu_percent: 42.5,
            load_average_1: 3.1,
            load_average_5: 2.8,
            load_average_15: 2.4,
            logical_cpu_count: 12,
        },
        processes: vec![ProcessSample {
            pid: 95_707,
            parent_pid: Some(95_000),
            name: "ollama runner".to_owned(),
            executable: Some("~/Applications/Ollama.app/Contents/MacOS/ollama".to_owned()),
            cpu_percent: 4.2,
            memory_bytes: 32_427_003_085,
            start_time_unix_seconds: 1_787_252_400,
            status: "Run".to_owned(),
        }],
    }
}

#[test]
fn serializes_the_versioned_camel_case_contract() {
    let json = serde_json::to_value(sample_snapshot()).expect("snapshot should serialize");

    assert_eq!(json["protocolVersion"], 1);
    assert_eq!(json["timestampUnixMs"], 1_787_256_000_000_u64);
    assert_eq!(json["system"]["totalMemoryBytes"], 68_719_476_736_u64);
    assert_eq!(json["processes"][0]["parentPid"], 95_000);
    assert!(json["processes"][0].get("commandLine").is_none());
}

#[test]
fn deserializes_the_shared_go_fixture() {
    let raw = include_str!("../../testdata/protocol-v1.json");
    let snapshot: Snapshot = serde_json::from_str(raw).expect("fixture should deserialize");

    assert_eq!(snapshot.protocol_version, PROTOCOL_VERSION);
    assert_eq!(snapshot.processes.len(), 2);
    assert_eq!(snapshot.processes[0].name, "ollama runner");
}

#[test]
fn rejects_unknown_fields_to_prevent_accidental_data_expansion() {
    let mut value = serde_json::to_value(sample_snapshot()).expect("snapshot should serialize");
    value
        .as_object_mut()
        .expect("snapshot is an object")
        .insert(
            "rawCommandLine".to_owned(),
            serde_json::json!("--token secret"),
        );

    let error = serde_json::from_value::<Snapshot>(value).expect_err("unknown field must fail");
    assert!(error.to_string().contains("unknown field"));
}

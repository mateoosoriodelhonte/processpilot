use std::process::Command;

use processpilot_collector::protocol::{PROTOCOL_VERSION, Snapshot};

#[test]
fn once_emits_one_real_sanitized_snapshot_without_privileges() {
    let output = Command::new(env!("CARGO_BIN_EXE_processpilot-collector"))
        .arg("--once")
        .output()
        .expect("collector should start");

    assert!(
        output.status.success(),
        "collector failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout = String::from_utf8(output.stdout).expect("collector output should be UTF-8");
    assert_eq!(
        stdout.lines().count(),
        1,
        "--once must emit exactly one line"
    );

    let snapshot: Snapshot = serde_json::from_str(stdout.trim()).expect("valid protocol snapshot");
    assert_eq!(snapshot.protocol_version, PROTOCOL_VERSION);
    assert!(snapshot.timestamp_unix_ms > 0);
    assert!(snapshot.system.total_memory_bytes > 0);
    assert!(snapshot.system.logical_cpu_count > 0);
    assert!(!snapshot.processes.is_empty());

    let lower = stdout.to_ascii_lowercase();
    for prohibited in [
        "commandline",
        "rawcommand",
        "environment",
        "workingdirectory",
        "authorization",
    ] {
        assert!(
            !lower.contains(prohibited),
            "found prohibited field: {prohibited}"
        );
    }
}

#[test]
fn rejects_any_command_execution_style_arguments() {
    let output = Command::new(env!("CARGO_BIN_EXE_processpilot-collector"))
        .args(["--command", "whoami"])
        .output()
        .expect("collector should start");

    assert!(!output.status.success());
    assert!(output.stdout.is_empty());
    assert!(String::from_utf8_lossy(&output.stderr).contains("unsupported collector argument"));
}

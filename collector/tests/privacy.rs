use std::ffi::OsString;
use std::os::unix::ffi::OsStringExt;
use std::path::{Path, PathBuf};

use processpilot_collector::redaction::{
    sanitize_executable_path, sanitize_process_name, sanitize_text,
};

#[test]
fn redacts_obvious_secret_arguments_and_assignments() {
    let cases = [
        (
            "worker --token abc123 --port 80",
            "worker --token [REDACTED] --port 80",
        ),
        ("worker --password=hunter2", "worker --password=[REDACTED]"),
        ("worker API_KEY=sk-local-value", "worker API_KEY=[REDACTED]"),
        (
            "Authorization: Bearer ey.secret.token",
            "Authorization: [REDACTED] [REDACTED]",
        ),
        ("worker --secret top-secret", "worker --secret [REDACTED]"),
    ];

    for (input, want) in cases {
        assert_eq!(sanitize_text(input, 256), want, "input: {input}");
    }
}

#[test]
fn redacts_credentials_embedded_in_urls() {
    let sanitized = sanitize_text("https://alice:password@example.test/v1", 256);

    assert_eq!(sanitized, "https://[REDACTED]@example.test/v1");
}

#[test]
fn normalizes_private_home_paths_without_reading_the_filesystem() {
    let private = sanitize_executable_path(Path::new(
        "/Users/alice/private-company-project/target/debug/server",
    ));
    let application = sanitize_executable_path(Path::new(
        "/Users/alice/Applications/Ollama.app/Contents/MacOS/ollama",
    ));

    assert_eq!(private.as_deref(), Some("~/<private>/server"));
    assert_eq!(
        application.as_deref(),
        Some("~/Applications/Ollama.app/Contents/MacOS/ollama")
    );
}

#[test]
fn preserves_system_application_identity() {
    let path = sanitize_executable_path(Path::new(
        "/Applications/Firefox.app/Contents/MacOS/firefox",
    ));

    assert_eq!(
        path.as_deref(),
        Some("/Applications/Firefox.app/Contents/MacOS/firefox")
    );
}

#[test]
fn handles_invalid_os_strings_and_control_characters_safely() {
    let path = PathBuf::from(OsString::from_vec(vec![
        b'/', b'U', b's', b'e', b'r', b's', b'/', 0xff, b'/', b'b', b'i', b'n', 0,
    ]));
    let sanitized = sanitize_executable_path(&path).expect("lossy path should sanitize");

    assert!(!sanitized.contains('\0'));
    assert!(!sanitized.contains("Users"));
}

#[test]
fn process_names_are_trimmed_bounded_and_never_empty() {
    assert_eq!(sanitize_process_name("  ollama runner\n"), "ollama runner");
    assert_eq!(sanitize_process_name("\n\t"), "Unknown process");
    assert!(sanitize_process_name(&"a".repeat(500)).len() <= 256);
}

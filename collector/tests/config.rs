use processpilot_collector::config::{CollectorConfig, parse_args};

#[test]
fn defaults_to_a_bounded_two_second_interval() {
    let config = parse_args(Vec::<String>::new()).expect("defaults should be valid");

    assert_eq!(
        config,
        CollectorConfig {
            once: false,
            interval_ms: 2_000,
        }
    );
}

#[test]
fn supports_a_single_snapshot_mode() {
    let config = parse_args(["--once".to_owned()]).expect("--once should be valid");

    assert!(config.once);
}

#[test]
fn rejects_untrusted_or_out_of_range_arguments() {
    let cases = [
        vec!["--interval-ms".to_owned(), "499".to_owned()],
        vec!["--interval-ms".to_owned(), "60001".to_owned()],
        vec!["--interval-ms".to_owned(), "2000; rm -rf /".to_owned()],
        vec!["--command".to_owned(), "whoami".to_owned()],
        vec!["--once".to_owned(), "unexpected".to_owned()],
    ];

    for args in cases {
        assert!(
            parse_args(args.clone()).is_err(),
            "args should fail: {args:?}"
        );
    }
}

use std::env;
use std::error::Error;
use std::io::{self, Write};
use std::process::ExitCode;
use std::thread;
use std::time::Duration;

use processpilot_collector::collect::TelemetryCollector;
use processpilot_collector::config::parse_args;
use processpilot_collector::protocol::encode_bounded_snapshot;
use sysinfo::MINIMUM_CPU_UPDATE_INTERVAL;

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("processpilot collector: {error}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<(), Box<dyn Error>> {
    let config = parse_args(env::args().skip(1))?;
    let mut collector = TelemetryCollector::new();

    thread::sleep(MINIMUM_CPU_UPDATE_INTERVAL);
    let stdout = io::stdout();
    let mut output = stdout.lock();

    loop {
        let snapshot = collector.sample()?;
        output.write_all(&encode_bounded_snapshot(snapshot)?)?;
        writeln!(output)?;
        output.flush()?;

        if config.once {
            return Ok(());
        }
        thread::sleep(Duration::from_millis(config.interval_ms));
    }
}

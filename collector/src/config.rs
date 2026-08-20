use std::fmt;

pub const MIN_INTERVAL_MS: u64 = 500;
pub const MAX_INTERVAL_MS: u64 = 60_000;
pub const DEFAULT_INTERVAL_MS: u64 = 2_000;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CollectorConfig {
    pub once: bool,
    pub interval_ms: u64,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ConfigError(String);

impl fmt::Display for ConfigError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl std::error::Error for ConfigError {}

pub fn parse_args(
    arguments: impl IntoIterator<Item = String>,
) -> Result<CollectorConfig, ConfigError> {
    let mut once = false;
    let mut interval_ms = DEFAULT_INTERVAL_MS;
    let mut arguments = arguments.into_iter();

    while let Some(argument) = arguments.next() {
        match argument.as_str() {
            "--once" if !once => once = true,
            "--interval-ms" => {
                let value = arguments
                    .next()
                    .ok_or_else(|| ConfigError("--interval-ms requires a value".to_owned()))?;
                interval_ms = value
                    .parse::<u64>()
                    .map_err(|_| ConfigError("interval must be an integer".to_owned()))?;
            }
            _ => {
                return Err(ConfigError(format!(
                    "unsupported collector argument: {argument}"
                )));
            }
        }
    }

    if !(MIN_INTERVAL_MS..=MAX_INTERVAL_MS).contains(&interval_ms) {
        return Err(ConfigError(format!(
            "interval must be between {MIN_INTERVAL_MS} and {MAX_INTERVAL_MS} milliseconds"
        )));
    }

    Ok(CollectorConfig { once, interval_ms })
}

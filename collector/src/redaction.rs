use std::path::Path;

const REDACTED: &str = "[REDACTED]";
const MAX_PROCESS_NAME_BYTES: usize = 256;
const MAX_EXECUTABLE_BYTES: usize = 2_048;

pub fn sanitize_process_name(raw: &str) -> String {
    let value = sanitize_text(raw, MAX_PROCESS_NAME_BYTES);
    if value.is_empty() {
        "Unknown process".to_owned()
    } else {
        value
    }
}

pub fn sanitize_executable_path(path: &Path) -> Option<String> {
    let raw = path.to_string_lossy();
    let cleaned = clean_controls(&raw);
    if cleaned.trim().is_empty() {
        return None;
    }

    let segments: Vec<String> = cleaned
        .split('/')
        .filter(|segment| !segment.is_empty())
        .map(|segment| sanitize_text(segment, MAX_EXECUTABLE_BYTES))
        .collect();

    let normalized = if cleaned.starts_with("/Users/") && segments.len() >= 2 {
        let private_segments = &segments[2..];
        if private_segments
            .first()
            .is_some_and(|value| value == "Applications")
        {
            format!("~/{}", private_segments.join("/"))
        } else {
            let identity = private_segments
                .last()
                .filter(|value| !value.is_empty())
                .map(String::as_str)
                .unwrap_or("executable");
            format!("~/<private>/{identity}")
        }
    } else {
        format!("/{}", segments.join("/"))
    };

    Some(truncate_utf8(&normalized, MAX_EXECUTABLE_BYTES))
}

pub fn sanitize_text(raw: &str, max_bytes: usize) -> String {
    if max_bytes == 0 {
        return String::new();
    }

    let cleaned = clean_controls(raw);
    let mut output = Vec::new();
    let mut redact_following = 0_u8;

    for token in cleaned.split_whitespace() {
        if redact_following > 0 {
            output.push(REDACTED.to_owned());
            redact_following -= 1;
            continue;
        }

        let lower = token.to_ascii_lowercase();
        if let Some((key, _)) = token.split_once('=')
            && is_secret_key(key)
        {
            output.push(format!("{key}={REDACTED}"));
            continue;
        }

        if is_authorization_key(&lower) {
            output.push(token.to_owned());
            redact_following = 2;
            continue;
        }
        if is_secret_key(&lower) {
            output.push(token.to_owned());
            redact_following = 1;
            continue;
        }
        if lower == "bearer" {
            output.push(token.to_owned());
            redact_following = 1;
            continue;
        }

        output.push(redact_url_credentials(token));
    }

    truncate_utf8(&output.join(" "), max_bytes)
}

fn clean_controls(raw: &str) -> String {
    raw.chars()
        .map(|character| {
            if character.is_control() {
                ' '
            } else {
                character
            }
        })
        .collect::<String>()
        .trim()
        .to_owned()
}

fn is_secret_key(raw: &str) -> bool {
    matches!(
        raw.trim_end_matches(':').to_ascii_lowercase().as_str(),
        "--token"
            | "--password"
            | "--secret"
            | "--api-key"
            | "api_key"
            | "api-key"
            | "token"
            | "password"
            | "secret"
    )
}

fn is_authorization_key(raw: &str) -> bool {
    raw.trim_end_matches(':')
        .eq_ignore_ascii_case("authorization")
}

fn redact_url_credentials(token: &str) -> String {
    let Some(scheme_end) = token.find("://").map(|index| index + 3) else {
        return token.to_owned();
    };
    let Some(relative_at) = token[scheme_end..].find('@') else {
        return token.to_owned();
    };
    let at = scheme_end + relative_at;
    format!("{}{REDACTED}{}", &token[..scheme_end], &token[at..])
}

fn truncate_utf8(value: &str, max_bytes: usize) -> String {
    if value.len() <= max_bytes {
        return value.to_owned();
    }
    let mut boundary = max_bytes;
    while !value.is_char_boundary(boundary) {
        boundary -= 1;
    }
    value[..boundary].to_owned()
}

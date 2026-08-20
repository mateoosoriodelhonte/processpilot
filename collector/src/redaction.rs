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

    let mut segments: Vec<String> = Vec::new();
    for raw_segment in cleaned.split('/') {
        let segment = sanitize_text(raw_segment, MAX_EXECUTABLE_BYTES);
        match segment.as_str() {
            "" | "." => {}
            ".." => {
                segments.pop();
            }
            _ => segments.push(segment),
        }
    }
    if segments.starts_with(&["System".to_owned(), "Volumes".to_owned(), "Data".to_owned()]) {
        segments.drain(..3);
    }

    let normalized =
        if segments.first().is_some_and(|value| value == "Users") && segments.len() >= 2 {
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
        } else if is_public_executable_path(&segments) {
            format!("/{}", segments.join("/"))
        } else {
            let identity = segments
                .last()
                .filter(|value| !value.is_empty())
                .map(String::as_str)
                .unwrap_or("executable");
            format!("/<private>/{identity}")
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
            if token == "=" {
                output.push(token.to_owned());
                continue;
            }
            output.push(REDACTED.to_owned());
            redact_following -= 1;
            continue;
        }

        let lower = token.to_ascii_lowercase();
        if let Some(index) = lower.find("authorization:bearer") {
            output.push(format!(
                "{}:{REDACTED}",
                &token[..index + "authorization".len()]
            ));
            redact_following = 1;
            continue;
        }
        if let Some(redacted) = redact_inline_secret_assignment(token) {
            output.push(redacted);
            if lower.contains("authorization=") {
                redact_following = 1;
            }
            continue;
        }
        if let Some((key, _)) = token.split_once('=') {
            if is_authorization_key(key) {
                output.push(format!("{key}={REDACTED}"));
                redact_following = 1;
                continue;
            }
            if is_secret_key(key) {
                output.push(format!("{key}={REDACTED}"));
                continue;
            }
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
    while redact_following > 0 {
        output.push(REDACTED.to_owned());
        redact_following -= 1;
    }

    truncate_utf8(&output.join(" "), max_bytes)
}

fn redact_inline_secret_assignment(token: &str) -> Option<String> {
    let lower = token.to_ascii_lowercase();
    let marker = [
        "authorization=",
        "--password=",
        "--secret=",
        "--api-key=",
        "api_key=",
        "api-key=",
        "password=",
        "secret=",
        "token=",
    ]
    .into_iter()
    .filter_map(|candidate| lower.find(candidate).map(|index| (index, candidate)))
    .min_by_key(|(index, _)| *index);
    marker.map(|(index, marker)| format!("{}{REDACTED}", &token[..index + marker.len()]))
}

fn is_public_executable_path(segments: &[String]) -> bool {
    matches!(
        segments.first().map(String::as_str),
        Some("Applications" | "System" | "Library" | "usr" | "bin" | "sbin")
    ) || segments
        .get(..2)
        .is_some_and(|prefix| prefix[0] == "opt" && prefix[1] == "homebrew")
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

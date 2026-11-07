//! Reading a TaxYearScenarios file: parse.go.

use buffa_descriptor::{DynamicMessage, FieldKind, SingularKind};
use proto::scenariosv1::TaxYearScenarios;

use crate::reflect;
use crate::{Error, protojson};

/// Parse reads a TaxYearScenarios file as JSON with comments: comments
/// removed, then the protobuf JSON mapping, strictly. An unknown field is
/// refused, which catches a computed figure added to the file, and so is a
/// message whose oneof has no case and an enum written as its UNSPECIFIED
/// value. It returns every such failure with its path.
///
/// # Errors
///
/// [`Error::Parse`] with the text Go's Parse gives.
pub fn parse(jsonc: &[u8]) -> Result<TaxYearScenarios, Error> {
    let text = strip_comments(jsonc)?;
    let pool =
        reflect::pool().ok_or_else(|| Error::Parse("taxyear: no descriptors".to_string()))?;
    let file = protojson::unmarshal(
        pool,
        <TaxYearScenarios as buffa::MessageName>::FULL_NAME,
        &text,
    )
    .map_err(|e| Error::Parse(format!("taxyear: the file is not a TaxYearScenarios: {e}")))?;
    let mut problems = Vec::new();
    if let Ok(raw) = serde_json::from_slice::<serde_json::Value>(&text) {
        unspecified("", &raw, &mut problems);
    }
    empty_oneofs("", &file, &mut problems);
    if !problems.is_empty() {
        problems.sort();
        return Err(Error::Parse(format!(
            "taxyear: the file is refused:\n  {}",
            problems.join("\n  ")
        )));
    }
    reflect::from_dyn(&file)
        .ok_or_else(|| Error::Parse("taxyear: the file is not a TaxYearScenarios".to_string()))
}

/// StripComments removes JSONC comments: "//" to the end of the line, and
/// "/*" to "*/". Text inside a JSON string is kept, and each comment's
/// newlines are kept, so a JSON error's line number still points into the
/// original.
///
/// # Errors
///
/// [`Error::Parse`] for a /* comment that is not closed.
pub fn strip_comments(input: &[u8]) -> Result<Vec<u8>, Error> {
    let mut out = Vec::with_capacity(input.len());
    let mut i = 0;
    while i < input.len() {
        if input[i] == b'"' {
            let end = string_end(input, i);
            out.extend_from_slice(&input[i..end]);
            i = end;
            continue;
        }
        if input[i..].starts_with(b"//") {
            while i < input.len() && input[i] != b'\n' {
                i += 1;
            }
            if i < input.len() {
                out.push(b'\n');
            }
            i += 1;
            continue;
        }
        if input[i..].starts_with(b"/*") {
            let Some(end) = find(&input[i + 2..], b"*/") else {
                return Err(Error::Parse(
                    "taxyear: a /* comment is not closed".to_string(),
                ));
            };
            let newlines = input[i..i + 2 + end + 2]
                .iter()
                .filter(|&&c| c == b'\n')
                .count();
            out.extend(std::iter::repeat_n(b'\n', newlines));
            i += 2 + end + 2;
            continue;
        }
        out.push(input[i]);
        i += 1;
    }
    Ok(out)
}

fn find(hay: &[u8], needle: &[u8]) -> Option<usize> {
    hay.windows(needle.len()).position(|w| w == needle)
}

/// The index just past the JSON string that starts at input[start], or
/// the input's length if it is not closed.
fn string_end(input: &[u8], start: usize) -> usize {
    let mut i = start + 1;
    while i < input.len() {
        match input[i] {
            b'\\' => i += 1,
            b'"' => return i + 1,
            _ => {}
        }
        i += 1;
    }
    input.len()
}

/// Records each string written as an enum's UNSPECIFIED value.
fn unspecified(path: &str, v: &serde_json::Value, problems: &mut Vec<String>) {
    match v {
        serde_json::Value::Object(o) => {
            for (k, c) in o {
                unspecified(&format!("{path}.{k}"), c, problems);
            }
        }
        serde_json::Value::Array(a) => {
            for (i, c) in a.iter().enumerate() {
                unspecified(&format!("{path}[{i}]"), c, problems);
            }
        }
        serde_json::Value::String(s) if s.ends_with("_UNSPECIFIED") => {
            problems.push(format!(
                "{}: {s} is not a value",
                path.strip_prefix('.').unwrap_or(path)
            ));
        }
        _ => {}
    }
}

/// Records each message present whose oneof has no case.
fn empty_oneofs(path: &str, m: &DynamicMessage, problems: &mut Vec<String>) {
    let Some(md) = reflect::desc(m) else {
        return;
    };
    for o in md.oneofs() {
        if o.is_synthetic() || o.name() == "supplement" {
            continue;
        }
        if reflect::which_oneof(m, o).is_none() {
            problems.push(format!(
                "{}: {} has no case set",
                path.strip_prefix('.').unwrap_or(path),
                o.name()
            ));
        }
    }
    for fd in reflect::set_fields(m) {
        let p = format!("{path}.{}", fd.name());
        match fd.kind() {
            FieldKind::List(SingularKind::Message(_)) => {
                for (i, v) in reflect::list(m, fd).iter().enumerate() {
                    if let Some(sub) = reflect::as_message(v) {
                        empty_oneofs(&format!("{p}[{i}]"), sub, problems);
                    }
                }
            }
            FieldKind::Singular(SingularKind::Message(_)) => {
                if let Some(sub) = reflect::message(m, fd) {
                    empty_oneofs(&p, sub, problems);
                }
            }
            _ => {}
        }
    }
}

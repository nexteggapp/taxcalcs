//! Go's %q: strconv.Quote and strconv.QuoteRune, which the error and
//! problem texts quote names and ids with.

/// Whether Go's unicode.IsPrint holds for a rune: letters, marks, numbers,
/// punctuation, symbols and the ASCII space. Rust has no general category
/// table, so a rune is printable unless it is a control, a space other than
/// U+0020, or a format character.
fn printable(c: char) -> bool {
    if c == ' ' {
        return true;
    }
    if c.is_control() || c.is_whitespace() {
        return false;
    }
    !matches!(c as u32,
        0x00AD | 0x0600..=0x0605 | 0x061C | 0x06DD | 0x070F | 0x0890..=0x0891 | 0x08E2 | 0x180E
        | 0x200B..=0x200F | 0x202A..=0x202E | 0x2060..=0x2064 | 0x2066..=0x206F | 0xFEFF
        | 0xFFF9..=0xFFFB | 0x110BD | 0x110CD | 0x13430..=0x1343F | 0x1BCA0..=0x1BCA3
        | 0x1D173..=0x1D17A | 0xE0001 | 0xE0020..=0xE007F)
}

/// Appends c to out as Go quotes it inside the quote character.
pub(crate) fn escape_into(out: &mut String, c: char, quote: char) {
    match c {
        '\u{7}' => out.push_str("\\a"),
        '\u{8}' => out.push_str("\\b"),
        '\u{c}' => out.push_str("\\f"),
        '\n' => out.push_str("\\n"),
        '\r' => out.push_str("\\r"),
        '\t' => out.push_str("\\t"),
        '\u{b}' => out.push_str("\\v"),
        '\\' => out.push_str("\\\\"),
        c if c == quote => {
            out.push('\\');
            out.push(c);
        }
        c if printable(c) => out.push(c),
        c if (c as u32) < 0x80 => out.push_str(&format!("\\x{:02x}", c as u32)),
        c if (c as u32) < 0x10000 => out.push_str(&format!("\\u{:04x}", c as u32)),
        c => out.push_str(&format!("\\U{:08x}", c as u32)),
    }
}

/// strconv.Quote: what %q makes of a string.
pub(crate) fn quote(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for c in s.chars() {
        escape_into(&mut out, c, '"');
    }
    out.push('"');
    out
}

/// strconv.QuoteRune: what %q makes of a rune or a byte.
pub(crate) fn quote_rune(c: char) -> String {
    let mut out = String::from("'");
    escape_into(&mut out, c, '\'');
    out.push('\'');
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn quotes_as_go_does() {
        assert_eq!(quote("a\"b\\c\n"), "\"a\\\"b\\\\c\\n\"");
        assert_eq!(quote("é\u{a0}"), "\"é\\u00a0\"");
        assert_eq!(quote_rune('\t'), "'\\t'");
        assert_eq!(quote_rune('\''), "'\\''");
        assert_eq!(quote_rune('\u{1}'), "'\\x01'");
    }
}

//! The protobuf JSON mapping, read strictly into a [`DynamicMessage`], as
//! Go's `protojson.UnmarshalOptions{}.Unmarshal` reads it: the JSON token
//! decoder of `google.golang.org/protobuf/internal/encoding/json` and the
//! message decoder of `encoding/protojson`, at protobuf-go v1.36.12, with
//! their error texts. Only what a TaxYearScenarios can hold is here: the
//! only well-known type it reaches is google.protobuf.Empty, which reads as
//! any other message with no fields does.
//!
//! An error's text is Go's: "proto: " and the message. Go writes that space
//! as U+0020 or U+00A0, chosen per build so that no one compares the text;
//! this writes U+0020.

use std::sync::Arc;

use base64::Engine as _;
use buffa_descriptor::{
    DescriptorPool, DynamicMessage, FieldDescriptor, FieldKind, MapKey, MapValue,
    MessageDescriptor, ScalarType, SingularKind, Value,
};

/// The deepest messages may nest, protowire.DefaultRecursionLimit.
const RECURSION_LIMIT: i64 = 10_000;

/// A protojson error: its text after "proto: ".
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Error(String);

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "proto: {}", self.0)
    }
}

fn unexpected_eof() -> Error {
    Error("unexpected EOF".to_string())
}

/// A JSON token's kind, as the Go decoder's bit flags.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u16)]
enum Kind {
    Invalid = 0,
    Eof = 1,
    Null = 2,
    Bool = 4,
    Number = 8,
    String = 16,
    Name = 32,
    ObjectOpen = 64,
    ObjectClose = 128,
    ArrayOpen = 256,
    ArrayClose = 512,
    Comma = 1024,
}

impl Kind {
    fn bits(self) -> u16 {
        self as u16
    }

    fn any(self, mask: u16) -> bool {
        self.bits() & mask != 0
    }
}

const SCALAR: u16 =
    Kind::Null as u16 | Kind::Bool as u16 | Kind::Number as u16 | Kind::String as u16;

/// One JSON token.
#[derive(Clone, Debug)]
struct Token {
    kind: Kind,
    pos: usize,
    raw: (usize, usize),
    boo: bool,
    str: String,
}

impl Default for Token {
    fn default() -> Self {
        Token {
            kind: Kind::Invalid,
            pos: 0,
            raw: (0, 0),
            boo: false,
            str: String::new(),
        }
    }
}

/// Which Decoder method was called last.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Call {
    Read,
    Peek,
}

/// A token-based JSON decoder.
struct Decoder<'a> {
    last_call: Call,
    last_token: Token,
    last_err: Option<Error>,
    open_stack: Vec<Kind>,
    orig: &'a [u8],
    /// The unconsumed input, as an offset into orig.
    at: usize,
}

impl<'a> Decoder<'a> {
    fn new(b: &'a [u8]) -> Self {
        Decoder {
            last_call: Call::Read,
            last_token: Token::default(),
            last_err: None,
            open_stack: Vec::new(),
            orig: b,
            at: 0,
        }
    }

    fn input(&self) -> &'a [u8] {
        &self.orig[self.at..]
    }

    fn raw(&self, t: &Token) -> String {
        String::from_utf8_lossy(&self.orig[t.raw.0..t.raw.1]).into_owned()
    }

    /// Looks ahead and returns the next token without advancing a read.
    fn peek(&mut self) -> Result<Token, Error> {
        if self.last_call == Call::Read {
            match self.read() {
                Ok(t) => {
                    self.last_token = t;
                    self.last_err = None;
                }
                Err(e) => {
                    self.last_token = Token::default();
                    self.last_err = Some(e);
                }
            }
        }
        self.last_call = Call::Peek;
        match &self.last_err {
            Some(e) => Err(e.clone()),
            None => Ok(self.last_token.clone()),
        }
    }

    /// Returns the next JSON token, or an error if there is no valid one.
    fn read(&mut self) -> Result<Token, Error> {
        if self.last_call == Call::Peek {
            self.last_call = Call::Read;
            return match &self.last_err {
                Some(e) => Err(e.clone()),
                None => Ok(self.last_token.clone()),
            };
        }
        let r = self.read_next();
        self.last_call = Call::Read;
        r
    }

    fn read_next(&mut self) -> Result<Token, Error> {
        let mut tok = self.parse_next()?;
        match tok.kind {
            Kind::Eof => {
                // Go's `lastToken.kind&scalar|ObjectClose|ArrayClose == 0` is
                // never true, so only an open object or array is an early EOF.
                if !self.open_stack.is_empty() {
                    return Err(unexpected_eof());
                }
            }
            Kind::Null | Kind::Bool | Kind::Number => {
                if !self.is_value_next() {
                    return Err(
                        self.syntax_error(tok.pos, &format!("unexpected token {}", self.raw(&tok)))
                    );
                }
            }
            Kind::String => {
                if !self.is_value_next() {
                    // This string token should only be for a field name.
                    if !self
                        .last_token
                        .kind
                        .any(Kind::ObjectOpen.bits() | Kind::Comma.bits())
                    {
                        return Err(self.syntax_error(
                            tok.pos,
                            &format!("unexpected token {}", self.raw(&tok)),
                        ));
                    }
                    let input = self.input();
                    if input.is_empty() {
                        return Err(unexpected_eof());
                    }
                    if input[0] != b':' {
                        // Go's string(c) of a byte is the rune of its value.
                        let c = char::from(input[0]);
                        return Err(self.syntax_error(
                            self.at,
                            &format!("unexpected character {c}, missing \":\" after field name"),
                        ));
                    }
                    tok.kind = Kind::Name;
                    self.consume(1);
                }
            }
            Kind::ObjectOpen | Kind::ArrayOpen => {
                if !self.is_value_next() {
                    return Err(
                        self.syntax_error(tok.pos, &format!("unexpected token {}", self.raw(&tok)))
                    );
                }
                self.open_stack.push(tok.kind);
            }
            Kind::ObjectClose => {
                if self.open_stack.last() != Some(&Kind::ObjectOpen)
                    || self
                        .last_token
                        .kind
                        .any(Kind::Name.bits() | Kind::Comma.bits())
                {
                    return Err(
                        self.syntax_error(tok.pos, &format!("unexpected token {}", self.raw(&tok)))
                    );
                }
                self.open_stack.pop();
            }
            Kind::ArrayClose => {
                if self.open_stack.last() != Some(&Kind::ArrayOpen)
                    || self.last_token.kind == Kind::Comma
                {
                    return Err(
                        self.syntax_error(tok.pos, &format!("unexpected token {}", self.raw(&tok)))
                    );
                }
                self.open_stack.pop();
            }
            Kind::Comma => {
                if self.open_stack.is_empty()
                    || !self
                        .last_token
                        .kind
                        .any(SCALAR | Kind::ObjectClose.bits() | Kind::ArrayClose.bits())
                {
                    return Err(
                        self.syntax_error(tok.pos, &format!("unexpected token {}", self.raw(&tok)))
                    );
                }
            }
            Kind::Invalid | Kind::Name => {}
        }
        // Update the last token only after validating it is in sequence.
        self.last_token = tok.clone();
        if tok.kind == Kind::Comma {
            return self.read_next();
        }
        Ok(tok)
    }

    /// Parses the next token, any kind but Name, without checking its
    /// place in the sequence.
    fn parse_next(&mut self) -> Result<Token, Error> {
        self.consume(0);
        let input = self.input();
        if input.is_empty() {
            return Ok(self.consume_token(Kind::Eof, 0));
        }
        match input[0] {
            b'n' => {
                if let Some(n) = match_with_delim(b"null", input) {
                    return Ok(self.consume_token(Kind::Null, n));
                }
            }
            b't' => {
                if let Some(n) = match_with_delim(b"true", input) {
                    let mut t = self.consume_token(Kind::Bool, n);
                    t.boo = true;
                    return Ok(t);
                }
            }
            b'f' => {
                if let Some(n) = match_with_delim(b"false", input) {
                    return Ok(self.consume_token(Kind::Bool, n));
                }
            }
            b'-' | b'0'..=b'9' => {
                if let Some(n) = parse_number(input) {
                    return Ok(self.consume_token(Kind::Number, n));
                }
            }
            b'"' => {
                let (s, n) = self.parse_string(input)?;
                let mut t = self.consume_token(Kind::String, n);
                t.str = s;
                return Ok(t);
            }
            b'{' => return Ok(self.consume_token(Kind::ObjectOpen, 1)),
            b'}' => return Ok(self.consume_token(Kind::ObjectClose, 1)),
            b'[' => return Ok(self.consume_token(Kind::ArrayOpen, 1)),
            b']' => return Ok(self.consume_token(Kind::ArrayClose, 1)),
            b',' => return Ok(self.consume_token(Kind::Comma, 1)),
            _ => {}
        }
        Err(self.syntax_error(self.at, &format!("invalid value {}", err_regexp(input))))
    }

    /// A syntax error at the position.
    fn syntax_error(&self, pos: usize, text: &str) -> Error {
        let (line, column) = self.position(pos);
        Error(format!("syntax error (line {line}:{column}): {text}"))
    }

    /// The line and column of an index of the original input.
    fn position(&self, idx: usize) -> (usize, usize) {
        let b = &self.orig[..idx.min(self.orig.len())];
        let line = b.iter().filter(|&&c| c == b'\n').count() + 1;
        let last = match b.iter().rposition(|&c| c == b'\n') {
            Some(i) => &b[i + 1..],
            None => b,
        };
        (line, rune_count(last) + 1)
    }

    /// Consumes n bytes and any whitespace after them.
    fn consume(&mut self, n: usize) {
        self.at += n;
        while let Some(&c) = self.orig.get(self.at) {
            match c {
                b' ' | b'\n' | b'\r' | b'\t' => self.at += 1,
                _ => return,
            }
        }
    }

    /// Whether the next token should be a value.
    fn is_value_next(&self) -> bool {
        match self.open_stack.last() {
            None => self.last_token.kind == Kind::Invalid,
            Some(Kind::ObjectOpen) => self.last_token.kind.any(Kind::Name.bits()),
            Some(_) => self
                .last_token
                .kind
                .any(Kind::ArrayOpen.bits() | Kind::Comma.bits()),
        }
    }

    fn consume_token(&mut self, kind: Kind, size: usize) -> Token {
        let t = Token {
            kind,
            pos: self.at,
            raw: (self.at, self.at + size),
            ..Token::default()
        };
        self.consume(size);
        t
    }

    /// Reads the string at the start of input, returning its value and its
    /// length in the input.
    fn parse_string(&self, input: &[u8]) -> Result<(String, usize), Error> {
        let mut out: Vec<u8> = Vec::new();
        let mut i = 1;
        // Errors are at the start of the string: Go's currPos does not
        // advance while a token is parsed.
        let here = self.at;
        while i < input.len() {
            let (r, n) = decode_rune(&input[i..]);
            match r {
                None => return Err(self.syntax_error(here, "invalid UTF-8 in string")),
                Some(c) if (c as u32) < 0x20 => {
                    return Err(self.syntax_error(
                        here,
                        &format!("invalid character {} in string", quote_rune(c)),
                    ));
                }
                Some('"') => return Ok((String::from_utf8_lossy(&out).into_owned(), i + 1)),
                Some('\\') => {
                    let Some(&e) = input.get(i + 1) else {
                        return Err(unexpected_eof());
                    };
                    match e {
                        b'"' | b'\\' | b'/' => {
                            out.push(e);
                            i += 2;
                        }
                        b'b' => {
                            out.push(8);
                            i += 2;
                        }
                        b'f' => {
                            out.push(12);
                            i += 2;
                        }
                        b'n' => {
                            out.push(b'\n');
                            i += 2;
                        }
                        b'r' => {
                            out.push(b'\r');
                            i += 2;
                        }
                        b't' => {
                            out.push(b'\t');
                            i += 2;
                        }
                        b'u' => {
                            if input.len() - i < 6 {
                                return Err(unexpected_eof());
                            }
                            let Some(v) = hex4(&input[i + 2..i + 6]) else {
                                return Err(self.syntax_error(
                                    here,
                                    &format!(
                                        "invalid escape code {} in string",
                                        quote_bytes(&input[i..i + 6])
                                    ),
                                ));
                            };
                            i += 6;
                            let mut r = u32::from(v);
                            if (0xD800..0xE000).contains(&r) {
                                if input.len() - i < 6 {
                                    return Err(unexpected_eof());
                                }
                                let v2 = hex4(&input[i + 2..i + 6]);
                                let pair = match v2 {
                                    Some(lo) => decode_surrogates(r, u32::from(lo)),
                                    None => 0xFFFD,
                                };
                                if input[i] != b'\\'
                                    || input[i + 1] != b'u'
                                    || pair == 0xFFFD
                                    || v2.is_none()
                                {
                                    return Err(self.syntax_error(
                                        here,
                                        &format!(
                                            "invalid escape code {} in string",
                                            quote_bytes(&input[i..i + 6])
                                        ),
                                    ));
                                }
                                r = pair;
                                i += 6;
                            }
                            let c = char::from_u32(r).unwrap_or('\u{FFFD}');
                            let mut buf = [0u8; 4];
                            out.extend_from_slice(c.encode_utf8(&mut buf).as_bytes());
                        }
                        _ => {
                            return Err(self.syntax_error(
                                here,
                                &format!(
                                    "invalid escape code {} in string",
                                    quote_bytes(&input[i..i + 2])
                                ),
                            ));
                        }
                    }
                }
                Some(_) => {
                    out.extend_from_slice(&input[i..i + n]);
                    i += n;
                }
            }
        }
        Err(unexpected_eof())
    }
}

/// Four hex digits, as strconv.ParseUint(s, 16, 16) reads them.
fn hex4(b: &[u8]) -> Option<u16> {
    if b.len() != 4 || !b.iter().all(u8::is_ascii_hexdigit) {
        return None;
    }
    u16::from_str_radix(std::str::from_utf8(b).ok()?, 16).ok()
}

/// utf16.DecodeRune: the code point of a surrogate pair, or U+FFFD.
fn decode_surrogates(r1: u32, r2: u32) -> u32 {
    if (0xD800..0xDC00).contains(&r1) && (0xDC00..0xE000).contains(&r2) {
        return ((r1 - 0xD800) << 10 | (r2 - 0xDC00)) + 0x10000;
    }
    0xFFFD
}

/// utf8.DecodeRune: the first rune of b and its length, or None and 1 for
/// an invalid encoding.
fn decode_rune(b: &[u8]) -> (Option<char>, usize) {
    let Some(&c) = b.first() else {
        return (None, 0);
    };
    let n = match c {
        0x00..=0x7F => return (Some(char::from(c)), 1),
        0xC2..=0xDF => 2,
        0xE0..=0xEF => 3,
        0xF0..=0xF4 => 4,
        _ => return (None, 1),
    };
    match b.get(..n).and_then(|s| std::str::from_utf8(s).ok()) {
        Some(s) => (s.chars().next(), n),
        None => (None, 1),
    }
}

/// utf8.RuneCount: invalid bytes count one each.
fn rune_count(b: &[u8]) -> usize {
    let mut i = 0;
    let mut n = 0;
    while i < b.len() {
        let (_, size) = decode_rune(&b[i..]);
        i += size.max(1);
        n += 1;
    }
    n
}

fn quote_rune(c: char) -> String {
    crate::goquote::quote_rune(c)
}

/// strconv.Quote of bytes, which %q gives a []byte.
fn quote_bytes(b: &[u8]) -> String {
    let mut out = String::from("\"");
    let mut i = 0;
    while i < b.len() {
        let (r, n) = decode_rune(&b[i..]);
        match r {
            Some(c) => crate::goquote::escape_into(&mut out, c, '"'),
            None => out.push_str(&format!("\\x{:02x}", b[i])),
        }
        i += n.max(1);
    }
    out.push('"');
    out
}

/// `^([-+._a-zA-Z0-9]{1,32}|.)`: what an invalid value error quotes.
fn err_regexp(input: &[u8]) -> String {
    let n = input
        .iter()
        .take(32)
        .take_while(|&&c| is_not_delim(c))
        .count();
    if n > 0 {
        return String::from_utf8_lossy(&input[..n]).into_owned();
    }
    match decode_rune(input) {
        (Some('\n'), _) => String::new(),
        (Some(c), _) => c.to_string(),
        (None, _) => String::from_utf8_lossy(&input[..1]).into_owned(),
    }
}

/// Matches s at the start of b when a delimiter, or the end, follows.
fn match_with_delim(s: &[u8], b: &[u8]) -> Option<usize> {
    if !b.starts_with(s) {
        return None;
    }
    let n = s.len();
    if n < b.len() && is_not_delim(b[n]) {
        return None;
    }
    Some(n)
}

fn is_not_delim(c: u8) -> bool {
    c == b'-' || c == b'+' || c == b'.' || c == b'_' || c.is_ascii_alphanumeric()
}

/// The length of the JSON number at the start of input.
fn parse_number(input: &[u8]) -> Option<usize> {
    let mut s = input;
    let mut n = 0;
    if s.is_empty() {
        return None;
    }
    if s[0] == b'-' {
        s = &s[1..];
        n += 1;
        if s.is_empty() {
            return None;
        }
    }
    match s[0] {
        b'0' => {
            s = &s[1..];
            n += 1;
        }
        b'1'..=b'9' => {
            s = &s[1..];
            n += 1;
            while !s.is_empty() && s[0].is_ascii_digit() {
                s = &s[1..];
                n += 1;
            }
        }
        _ => return None,
    }
    if s.len() >= 2 && s[0] == b'.' && s[1].is_ascii_digit() {
        s = &s[2..];
        n += 2;
        while !s.is_empty() && s[0].is_ascii_digit() {
            s = &s[1..];
            n += 1;
        }
    }
    if s.len() >= 2 && (s[0] == b'e' || s[0] == b'E') {
        s = &s[1..];
        n += 1;
        if s[0] == b'+' || s[0] == b'-' {
            s = &s[1..];
            n += 1;
            if s.is_empty() {
                return None;
            }
        }
        while !s.is_empty() && s[0].is_ascii_digit() {
            s = &s[1..];
            n += 1;
        }
    }
    if n < input.len() && is_not_delim(input[n]) {
        return None;
    }
    Some(n)
}

/// A number's parts, for converting it to an integer.
struct NumberParts<'a> {
    neg: bool,
    intp: &'a [u8],
    frac: &'a [u8],
    exp: &'a [u8],
}

fn parse_number_parts(input: &[u8]) -> Option<NumberParts<'_>> {
    let mut s = input;
    let mut neg = false;
    let mut intp: &[u8] = &[];
    let mut frac: &[u8] = &[];
    let mut exp: &[u8] = &[];
    if s.is_empty() {
        return None;
    }
    if s[0] == b'-' {
        neg = true;
        s = &s[1..];
        if s.is_empty() {
            return None;
        }
    }
    match s[0] {
        b'0' => s = &s[1..],
        b'1'..=b'9' => {
            let start = s;
            let mut n = 1;
            s = &s[1..];
            while !s.is_empty() && s[0].is_ascii_digit() {
                s = &s[1..];
                n += 1;
            }
            intp = &start[..n];
        }
        _ => return None,
    }
    if s.len() >= 2 && s[0] == b'.' && s[1].is_ascii_digit() {
        let start = &s[1..];
        let mut n = 1;
        s = &s[2..];
        while !s.is_empty() && s[0].is_ascii_digit() {
            s = &s[1..];
            n += 1;
        }
        frac = &start[..n];
    }
    if s.len() >= 2 && (s[0] == b'e' || s[0] == b'E') {
        s = &s[1..];
        let start = s;
        let mut n = 0;
        if s[0] == b'+' || s[0] == b'-' {
            s = &s[1..];
            n += 1;
            if s.is_empty() {
                return None;
            }
        }
        while !s.is_empty() && s[0].is_ascii_digit() {
            s = &s[1..];
            n += 1;
        }
        exp = &start[..n];
    }
    while let [rest @ .., b'0'] = frac {
        frac = rest;
    }
    Some(NumberParts {
        neg,
        intp,
        frac,
        exp,
    })
}

/// The integer a number's parts make, in normal form, or None when it is
/// not an integer.
fn normalize_to_int_string(n: &NumberParts<'_>) -> Option<String> {
    let intp_size = n.intp.len();
    let frac_size = n.frac.len();
    if intp_size == 0 && frac_size == 0 {
        return Some("0".to_string());
    }
    let mut exp: i64 = 0;
    if !n.exp.is_empty() {
        exp = go_parse_int(std::str::from_utf8(n.exp).ok()?, 32)?;
    }
    let mut num: Vec<u8>;
    if exp >= 0 {
        let e = usize::try_from(exp).ok()?;
        if frac_size > e {
            return None;
        }
        const MAX_DIGITS: usize = 20;
        if intp_size + e > MAX_DIGITS {
            return None;
        }
        num = n.intp.to_vec();
        num.extend_from_slice(n.frac);
        num.extend(std::iter::repeat_n(b'0', e - frac_size));
    } else {
        if frac_size > 0 {
            return None;
        }
        let index = i64::try_from(intp_size).ok()? + exp;
        if index < 0 {
            return None;
        }
        let index = usize::try_from(index).ok()?;
        if n.intp[index..].iter().any(|&c| c != b'0') {
            return None;
        }
        num = n.intp[..index].to_vec();
    }
    let s = String::from_utf8(num).ok()?;
    if n.neg {
        return Some(format!("-{s}"));
    }
    Some(s)
}

/// strconv.ParseInt(s, 10, bits).
fn go_parse_int(s: &str, bits: u32) -> Option<i64> {
    let digits = s.strip_prefix(['+', '-']).unwrap_or(s);
    if digits.is_empty() || !digits.bytes().all(|c| c.is_ascii_digit()) {
        return None;
    }
    let v: i64 = s.parse().ok()?;
    if bits == 32 && i32::try_from(v).is_err() {
        return None;
    }
    Some(v)
}

/// strconv.ParseUint(s, 10, bits).
fn go_parse_uint(s: &str, bits: u32) -> Option<u64> {
    if s.is_empty() || !s.bytes().all(|c| c.is_ascii_digit()) {
        return None;
    }
    let v: u64 = s.parse().ok()?;
    if bits == 32 && u32::try_from(v).is_err() {
        return None;
    }
    Some(v)
}

/// The integer a number token's raw text is, in range for bits.
fn token_int(raw: &[u8], bits: u32) -> Option<i64> {
    go_parse_int(&normalize_to_int_string(&parse_number_parts(raw)?)?, bits)
}

/// The unsigned integer a number token's raw text is.
fn token_uint(raw: &[u8], bits: u32) -> Option<u64> {
    go_parse_uint(&normalize_to_int_string(&parse_number_parts(raw)?)?, bits)
}

/// strconv.ParseFloat of a number token's raw text: None out of range.
fn token_float(raw: &[u8], bits: u32) -> Option<f64> {
    let s = std::str::from_utf8(raw).ok()?;
    if bits == 32 {
        let f: f32 = s.parse().ok()?;
        return f.is_finite().then_some(f64::from(f));
    }
    let f: f64 = s.parse().ok()?;
    f.is_finite().then_some(f)
}

/// Go's protoreflect Kind name of a field.
fn kind_name(fd: &FieldDescriptor) -> &'static str {
    let k = match fd.kind() {
        FieldKind::Singular(k) | FieldKind::List(k) => k,
        FieldKind::Map { .. } => return "message",
    };
    singular_name(k)
}

fn singular_name(k: SingularKind) -> &'static str {
    match k {
        SingularKind::Enum(_) => "enum",
        SingularKind::Message(_) => "message",
        SingularKind::Scalar(s) => scalar_name(s),
    }
}

fn scalar_name(s: ScalarType) -> &'static str {
    match s {
        ScalarType::Double => "double",
        ScalarType::Float => "float",
        ScalarType::Int64 => "int64",
        ScalarType::Uint64 => "uint64",
        ScalarType::Int32 => "int32",
        ScalarType::Fixed64 => "fixed64",
        ScalarType::Fixed32 => "fixed32",
        ScalarType::Bool => "bool",
        ScalarType::String => "string",
        ScalarType::Bytes => "bytes",
        ScalarType::Uint32 => "uint32",
        ScalarType::Sfixed32 => "sfixed32",
        ScalarType::Sfixed64 => "sfixed64",
        ScalarType::Sint32 => "sint32",
        ScalarType::Sint64 => "sint64",
    }
}

/// Reads b, a JSON object, as a message of type `full_name` of the pool,
/// strictly: an unknown field, a field twice or two cases of a oneof are
/// refused, as is anything after the object.
pub(crate) fn unmarshal(
    pool: &'static Arc<DescriptorPool>,
    full_name: &str,
    b: &[u8],
) -> Result<DynamicMessage, Error> {
    let Some(mut m) = DynamicMessage::new_by_name(Arc::clone(pool), full_name) else {
        return Err(Error(format!("{full_name} is not in the descriptors")));
    };
    let mut d = MessageDecoder {
        dec: Decoder::new(b),
        pool,
    };
    d.unmarshal_message(&mut m, RECURSION_LIMIT)?;
    let tok = d.dec.read()?;
    if tok.kind != Kind::Eof {
        return Err(d.unexpected_token_error(&tok));
    }
    Ok(m)
}

/// The message decoder over the token decoder.
struct MessageDecoder<'a> {
    dec: Decoder<'a>,
    pool: &'static Arc<DescriptorPool>,
}

impl MessageDecoder<'_> {
    fn new_error(&self, pos: usize, text: &str) -> Error {
        let (line, column) = self.dec.position(pos);
        Error(format!("(line {line}:{column}): {text}"))
    }

    fn unexpected_token_error(&self, tok: &Token) -> Error {
        self.dec
            .syntax_error(tok.pos, &format!("unexpected token {}", self.dec.raw(tok)))
    }

    fn message_desc(&self, m: &DynamicMessage) -> &'static MessageDescriptor {
        self.pool.message(m.message_index())
    }

    fn unmarshal_message(&mut self, m: &mut DynamicMessage, limit: i64) -> Result<(), Error> {
        let limit = limit - 1;
        if limit < 0 {
            return Err(Error("exceeded max recursion depth".to_string()));
        }
        let md = self.message_desc(m);
        let name = md.full_name();
        if let Some(rest) = name.strip_prefix("google.protobuf.")
            && rest != "Empty"
            && !rest.contains('.')
        {
            return Err(Error(format!("no support for the JSON form of {name}")));
        }
        let tok = self.dec.read()?;
        if tok.kind != Kind::ObjectOpen {
            return Err(self.unexpected_token_error(&tok));
        }
        let mut seen_nums: Vec<u32> = Vec::new();
        let mut seen_oneofs: Vec<u16> = Vec::new();
        loop {
            let tok = self.dec.read()?;
            match tok.kind {
                Kind::ObjectClose => return Ok(()),
                Kind::Name => {}
                _ => return Err(self.unexpected_token_error(&tok)),
            }
            let name = tok.str.as_str();
            let fd = if name.starts_with('[') && name.ends_with(']') {
                // An extension, which no message here has.
                None
            } else {
                md.fields()
                    .iter()
                    .find(|f| f.json_name() == name)
                    .or_else(|| md.fields().iter().find(|f| f.name() == name))
            };
            let Some(fd) = fd else {
                return Err(
                    self.new_error(tok.pos, &format!("unknown field {}", self.dec.raw(&tok)))
                );
            };
            if seen_nums.contains(&fd.number()) {
                return Err(
                    self.new_error(tok.pos, &format!("duplicate field {}", self.dec.raw(&tok)))
                );
            }
            seen_nums.push(fd.number());
            // A null sets nothing.
            if let Ok(next) = self.dec.peek()
                && next.kind == Kind::Null
            {
                self.dec.read()?;
                continue;
            }
            match fd.kind() {
                FieldKind::List(k) => {
                    let mut out = Vec::new();
                    self.unmarshal_list(&mut out, fd, k, limit)?;
                    use buffa_descriptor::ReflectMessageMut as _;
                    let _ = m.try_set(fd, Value::List(out));
                }
                FieldKind::Map { key, value } => {
                    let mut out = MapValue::new();
                    self.unmarshal_map(&mut out, key, value, limit)?;
                    use buffa_descriptor::ReflectMessageMut as _;
                    let _ = m.try_set(fd, Value::Map(out));
                }
                FieldKind::Singular(k) => {
                    if let Some(oi) = fd.oneof_index() {
                        if seen_oneofs.contains(&oi) {
                            let full = md
                                .oneofs()
                                .get(usize::from(oi))
                                .map(|o| format!("{}.{}", md.full_name(), o.name()))
                                .unwrap_or_default();
                            return Err(self.new_error(
                                tok.pos,
                                &format!(
                                    "error parsing {}, oneof {full} is already set",
                                    self.dec.raw(&tok)
                                ),
                            ));
                        }
                        seen_oneofs.push(oi);
                    }
                    let v = match k {
                        SingularKind::Message(idx) => {
                            let mut sub = DynamicMessage::new(Arc::clone(self.pool), idx);
                            self.unmarshal_message(&mut sub, limit)?;
                            Value::Message(sub)
                        }
                        _ => self.unmarshal_scalar(fd, k)?,
                    };
                    use buffa_descriptor::ReflectMessageMut as _;
                    let _ = m.try_set(fd, v);
                }
            }
        }
    }

    fn unmarshal_scalar(&mut self, fd: &FieldDescriptor, k: SingularKind) -> Result<Value, Error> {
        self.scalar_value(k, kind_name(fd), fd.json_name())
    }

    /// Reads one scalar or enum value of kind k; an error names the field
    /// by its kind and JSON name.
    fn scalar_value(
        &mut self,
        k: SingularKind,
        kind: &str,
        json_name: &str,
    ) -> Result<Value, Error> {
        let tok = self.dec.read()?;
        let v = match k {
            SingularKind::Scalar(s) => match s {
                ScalarType::Bool => (tok.kind == Kind::Bool).then_some(Value::Bool(tok.boo)),
                ScalarType::Int32 | ScalarType::Sint32 | ScalarType::Sfixed32 => {
                    self.unmarshal_int(&tok, 32).map(|n| Value::I32(n as i32))
                }
                ScalarType::Int64 | ScalarType::Sint64 | ScalarType::Sfixed64 => {
                    self.unmarshal_int(&tok, 64).map(Value::I64)
                }
                ScalarType::Uint32 | ScalarType::Fixed32 => {
                    self.unmarshal_uint(&tok, 32).map(|n| Value::U32(n as u32))
                }
                ScalarType::Uint64 | ScalarType::Fixed64 => {
                    self.unmarshal_uint(&tok, 64).map(Value::U64)
                }
                ScalarType::Float => self.unmarshal_float(&tok, 32).map(|f| Value::F32(f as f32)),
                ScalarType::Double => self.unmarshal_float(&tok, 64).map(Value::F64),
                ScalarType::String => {
                    (tok.kind == Kind::String).then(|| Value::String(tok.str.clone()))
                }
                ScalarType::Bytes => unmarshal_bytes(&tok).map(Value::Bytes),
            },
            SingularKind::Enum(idx) => {
                let ed = self.pool.enumeration(idx);
                match tok.kind {
                    Kind::String => ed
                        .value_by_name(&tok.str)
                        .map(|v| Value::EnumNumber(v.number())),
                    Kind::Number => token_int(&self.dec.orig[tok.raw.0..tok.raw.1], 32)
                        .map(|n| Value::EnumNumber(n as i32)),
                    Kind::Null if ed.full_name() == "google.protobuf.NullValue" => {
                        Some(Value::EnumNumber(0))
                    }
                    _ => None,
                }
            }
            SingularKind::Message(_) => None,
        };
        match v {
            Some(v) => Ok(v),
            None => Err(self.new_error(
                tok.pos,
                &format!(
                    "invalid value for {kind} field {json_name}: {}",
                    self.dec.raw(&tok)
                ),
            )),
        }
    }

    /// The raw text of a number written as itself or in a string, as Go
    /// reads a string again with a decoder of its own.
    fn number_in(&self, tok: &Token) -> Option<Vec<u8>> {
        match tok.kind {
            Kind::Number => Some(self.dec.orig[tok.raw.0..tok.raw.1].to_vec()),
            Kind::String => {
                let s = tok.str.as_str();
                if s.trim() != s {
                    return None;
                }
                let bytes = s.as_bytes();
                let mut d = Decoder::new(bytes);
                let t = d.read().ok()?;
                let next = d.read().ok()?;
                if next.kind != Kind::Eof || t.kind != Kind::Number {
                    return None;
                }
                Some(bytes[t.raw.0..t.raw.1].to_vec())
            }
            _ => None,
        }
    }

    fn unmarshal_int(&self, tok: &Token, bits: u32) -> Option<i64> {
        token_int(&self.number_in(tok)?, bits)
    }

    fn unmarshal_uint(&self, tok: &Token, bits: u32) -> Option<u64> {
        token_uint(&self.number_in(tok)?, bits)
    }

    fn unmarshal_float(&self, tok: &Token, bits: u32) -> Option<f64> {
        if tok.kind == Kind::String {
            match tok.str.as_str() {
                "NaN" => return Some(f64::NAN),
                "Infinity" => return Some(f64::INFINITY),
                "-Infinity" => return Some(f64::NEG_INFINITY),
                _ => {}
            }
        }
        token_float(&self.number_in(tok)?, bits)
    }

    fn unmarshal_list(
        &mut self,
        out: &mut Vec<Value>,
        fd: &FieldDescriptor,
        k: SingularKind,
        limit: i64,
    ) -> Result<(), Error> {
        let tok = self.dec.read()?;
        if tok.kind != Kind::ArrayOpen {
            return Err(self.unexpected_token_error(&tok));
        }
        loop {
            let tok = self.dec.peek()?;
            if tok.kind == Kind::ArrayClose {
                self.dec.read()?;
                return Ok(());
            }
            match k {
                SingularKind::Message(idx) => {
                    let mut sub = DynamicMessage::new(Arc::clone(self.pool), idx);
                    self.unmarshal_message(&mut sub, limit)?;
                    out.push(Value::Message(sub));
                }
                _ => {
                    out.push(self.unmarshal_scalar(fd, k)?);
                }
            }
        }
    }

    fn unmarshal_map(
        &mut self,
        out: &mut MapValue,
        key: ScalarType,
        value: SingularKind,
        limit: i64,
    ) -> Result<(), Error> {
        let tok = self.dec.read()?;
        if tok.kind != Kind::ObjectOpen {
            return Err(self.unexpected_token_error(&tok));
        }
        loop {
            let tok = self.dec.read()?;
            match tok.kind {
                Kind::ObjectClose => return Ok(()),
                Kind::Name => {}
                _ => return Err(self.unexpected_token_error(&tok)),
            }
            let k = self.unmarshal_map_key(&tok, key)?;
            if out.get(&k).is_some() {
                return Err(self.new_error(
                    tok.pos,
                    &format!("duplicate map key {}", self.dec.raw(&tok)),
                ));
            }
            let v = match value {
                SingularKind::Message(idx) => {
                    let mut sub = DynamicMessage::new(Arc::clone(self.pool), idx);
                    self.unmarshal_message(&mut sub, limit)?;
                    Value::Message(sub)
                }
                _ => self.unmarshal_map_value(value)?,
            };
            out.insert(k, v);
        }
    }

    /// A map's scalar value: Go's map value field is named "value".
    fn unmarshal_map_value(&mut self, k: SingularKind) -> Result<Value, Error> {
        self.scalar_value(k, singular_name(k), "value")
    }

    fn unmarshal_map_key(&self, tok: &Token, key: ScalarType) -> Result<MapKey, Error> {
        let name = tok.str.as_str();
        let k = match key {
            ScalarType::String => Some(MapKey::String(name.to_string())),
            ScalarType::Bool => match name {
                "true" => Some(MapKey::Bool(true)),
                "false" => Some(MapKey::Bool(false)),
                _ => None,
            },
            ScalarType::Int32 | ScalarType::Sint32 | ScalarType::Sfixed32 => {
                go_parse_int(name, 32).map(|n| MapKey::I32(n as i32))
            }
            ScalarType::Int64 | ScalarType::Sint64 | ScalarType::Sfixed64 => {
                go_parse_int(name, 64).map(MapKey::I64)
            }
            ScalarType::Uint32 | ScalarType::Fixed32 => {
                go_parse_uint(name, 32).map(|n| MapKey::U32(n as u32))
            }
            ScalarType::Uint64 | ScalarType::Fixed64 => go_parse_uint(name, 64).map(MapKey::U64),
            _ => None,
        };
        k.ok_or_else(|| {
            self.new_error(
                tok.pos,
                &format!(
                    "invalid value for {} key: {}",
                    scalar_name(key),
                    self.dec.raw(tok)
                ),
            )
        })
    }
}

/// A bytes field's base64, standard or URL-safe, padded or not, as Go
/// reads it: line breaks are skipped and trailing bits allowed.
fn unmarshal_bytes(tok: &Token) -> Option<Vec<u8>> {
    if tok.kind != Kind::String {
        return None;
    }
    let s = tok.str.as_str();
    let alphabet = if s.contains(['-', '_']) {
        base64::alphabet::URL_SAFE
    } else {
        base64::alphabet::STANDARD
    };
    let padded = s.len().is_multiple_of(4);
    let config = base64::engine::GeneralPurposeConfig::new()
        .with_decode_allow_trailing_bits(true)
        .with_decode_padding_mode(if padded {
            base64::engine::DecodePaddingMode::RequireCanonical
        } else {
            base64::engine::DecodePaddingMode::RequireNone
        });
    let engine = base64::engine::GeneralPurpose::new(&alphabet, config);
    let cleaned: String = s.chars().filter(|&c| c != '\r' && c != '\n').collect();
    engine.decode(cleaned).ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn numbers_normalize_as_go_does() {
        for (raw, want) in [
            ("2026", Some("2026")),
            ("2.026e3", Some("2026")),
            ("2026.000", Some("2026")),
            ("2026.5", None),
            ("-0", Some("0")),
            ("58333300e-2", Some("583333")),
            ("5833.33e-2", None),
            ("1e", Some("1")),
            ("0.5e1", Some("5")),
        ] {
            let parts = parse_number_parts(raw.as_bytes());
            let got = parts.and_then(|p| normalize_to_int_string(&p));
            assert_eq!(got.as_deref(), want, "{raw}");
        }
    }

    #[test]
    fn quoting_is_go_quoting() {
        assert_eq!(quote_rune('\t'), "'\\t'");
        assert_eq!(quote_rune('\u{1}'), "'\\x01'");
        assert_eq!(quote_bytes(b"\\x"), "\"\\\\x\"");
        assert_eq!(quote_bytes(b"\\u12zz"), "\"\\\\u12zz\"");
    }
}

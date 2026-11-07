//! Parses and formats taxlisp S-expression source files (.tlisp) into and
//! from the canonical taxlisp.v1.ModuleFile protobuf AST, ported from the Go
//! package `taxlisp/parser`.
//!
//! The module also carries the pieces of Go's `strconv` the parser and the
//! formatter rely on ([`quote`], [`quote_rune`], [`go_format_float_g`] and the
//! `ParseFloat` grammar), because their output is part of the formatted text
//! and the error messages.

use std::fmt;

use buffa::MessageField;
use proto::commonv1;
use proto::taxlispv1;
use proto::taxlispv1::__buffa::oneof::atom::Primitive;
use proto::taxlispv1::__buffa::oneof::expression::Value;

/// A syntax error, whose text is the Go error's: `path:line:col: message`
/// for most errors.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ParseError(pub String);

impl fmt::Display for ParseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for ParseError {}

/// ParseFile parses a taxlisp source string into a ModuleFile protobuf AST.
pub fn parse_file(path: &str, src: &str) -> Result<taxlispv1::ModuleFile, ParseError> {
    let mut p = Parser {
        path: path.to_string(),
        src,
        pos: 0,
        line: 1,
        col: 1,
    };
    let exprs = p.parse_top_level()?;
    Ok(taxlispv1::ModuleFile {
        path: path.to_string(),
        expressions: exprs,
        ..Default::default()
    })
}

/// ParseExpr parses a single S-expression from src.
pub fn parse_expr(src: &str) -> Result<taxlispv1::Expression, ParseError> {
    let mf = parse_file("<expr>", src)?;
    if mf.expressions.len() != 1 {
        return Err(ParseError(format!(
            "expected 1 expression, got {}",
            mf.expressions.len()
        )));
    }
    let mut exprs = mf.expressions;
    Ok(exprs.remove(0))
}

/// FormatFile formats a ModuleFile into canonical taxlisp S-expression text.
pub fn format_file(mf: &taxlispv1::ModuleFile) -> String {
    let mut b = String::new();
    for (i, expr) in mf.expressions.iter().enumerate() {
        if i > 0 {
            b.push_str("\n\n");
        }
        write_leading_comments(&mut b, expr_syntax(expr), 0);
        format_expr_into(&mut b, expr, 0);
    }
    if !mf.expressions.is_empty() {
        b.push('\n');
    }
    b
}

/// FormatExpr formats a single Expression into canonical S-expression text.
pub fn format_expr(expr: &taxlispv1::Expression) -> String {
    let mut b = String::new();
    format_expr_into(&mut b, expr, 0);
    b
}

struct Parser<'a> {
    path: String,
    src: &'a str,
    pos: usize,
    line: i64,
    col: i64,
}

fn atom_expr(primitive: Primitive, syn: taxlispv1::SyntaxDetails) -> taxlispv1::Expression {
    taxlispv1::Expression {
        value: Some(Value::Atom(Box::new(taxlispv1::Atom {
            primitive: Some(primitive),
            syntax: MessageField::some(syn),
            ..Default::default()
        }))),
        ..Default::default()
    }
}

fn list_expr(
    expressions: Vec<taxlispv1::Expression>,
    syn: taxlispv1::SyntaxDetails,
) -> taxlispv1::Expression {
    taxlispv1::Expression {
        value: Some(Value::List(Box::new(taxlispv1::List {
            expressions,
            syntax: MessageField::some(syn),
            ..Default::default()
        }))),
        ..Default::default()
    }
}

fn symbol(package: &str, name: &str) -> Primitive {
    Primitive::Symbol(Box::new(taxlispv1::Symbol {
        package: package.to_string(),
        name: name.to_string(),
        ..Default::default()
    }))
}

impl Parser<'_> {
    fn parse_top_level(&mut self) -> Result<Vec<taxlispv1::Expression>, ParseError> {
        let mut out = Vec::new();
        loop {
            let comments = self.skip_whitespace_and_comments();
            if self.eof() {
                break;
            }
            let expr = self.parse_expr(Some(comments))?;
            out.push(expr);
        }
        Ok(out)
    }

    /// Go passes a nil slice to mean "skip whitespace first"; a skip that
    /// found no comments also yields nil there, so `Some(empty)` behaves
    /// like `None`.
    fn parse_expr(
        &mut self,
        comments: Option<Vec<String>>,
    ) -> Result<taxlispv1::Expression, ParseError> {
        let comments = match comments {
            Some(c) if !c.is_empty() => c,
            _ => self.skip_whitespace_and_comments(),
        };
        if self.eof() {
            return Err(self.errorf("unexpected EOF"));
        }
        let (start_line, start_col) = (self.line, self.col);
        let ch = self.peek();

        match ch {
            '(' => self.parse_list(')', start_line, start_col, comments),
            '[' => self.parse_bracket_fn(start_line, start_col, comments),
            '\'' => {
                self.advance();
                let inner = self.parse_expr(None)?;
                let syn = self.make_syntax(start_line, start_col, self.line, self.col, comments);
                let head = atom_expr(symbol("", "quote"), syn.clone());
                Ok(list_expr(vec![head, inner], syn))
            }
            '"' => {
                let s = self.parse_string()?;
                let syn = self.make_syntax(start_line, start_col, self.line, self.col, comments);
                Ok(atom_expr(Primitive::StringValue(s), syn))
            }
            '#' => self.parse_tagged_literal(start_line, start_col, comments),
            ')' | ']' => {
                Err(self.errorf(&format!("unexpected closing delimiter {}", quote_rune(ch))))
            }
            _ => self.parse_atom(start_line, start_col, comments),
        }
    }

    fn parse_list(
        &mut self,
        close_delim: char,
        start_line: i64,
        start_col: i64,
        comments: Vec<String>,
    ) -> Result<taxlispv1::Expression, ParseError> {
        self.advance(); // consume '('
        let mut elems = Vec::new();
        loop {
            let inner_comments = self.skip_whitespace_and_comments();
            if self.eof() {
                return Err(ParseError(format!(
                    "{}:{}:{}: unclosed list '(' (expected {})",
                    self.path,
                    start_line,
                    start_col,
                    quote_rune(close_delim)
                )));
            }
            if self.peek() == close_delim {
                self.advance();
                break;
            }
            let e = self.parse_expr(Some(inner_comments))?;
            elems.push(e);
        }
        let syn = self.make_syntax(start_line, start_col, self.line, self.col, comments);
        Ok(list_expr(elems, syn))
    }

    /// parseBracketFn parses `[...]` as `(slice ...)` so it can represent a
    /// slice literal or typed bracket form cleanly without ambiguity.
    fn parse_bracket_fn(
        &mut self,
        start_line: i64,
        start_col: i64,
        comments: Vec<String>,
    ) -> Result<taxlispv1::Expression, ParseError> {
        self.advance(); // consume '['
        let mut elems = Vec::new();
        loop {
            let inner_comments = self.skip_whitespace_and_comments();
            if self.eof() {
                return Err(ParseError(format!(
                    "{}:{}:{}: unclosed '['",
                    self.path, start_line, start_col
                )));
            }
            if self.peek() == ']' {
                self.advance();
                break;
            }
            let e = self.parse_expr(Some(inner_comments))?;
            elems.push(e);
        }
        let syn = self.make_syntax(start_line, start_col, self.line, self.col, comments);
        let head = atom_expr(symbol("", "slice"), syn.clone());
        let mut all = Vec::with_capacity(elems.len() + 1);
        all.push(head);
        all.extend(elems);
        Ok(list_expr(all, syn))
    }

    fn parse_tagged_literal(
        &mut self,
        start_line: i64,
        start_col: i64,
        comments: Vec<String>,
    ) -> Result<taxlispv1::Expression, ParseError> {
        self.advance(); // consume '#'
        let tag_start = self.pos;
        while !self.eof() && (is_letter(self.peek()) || is_digit(self.peek())) {
            self.advance();
        }
        let tag = &self.src[tag_start..self.pos];
        if self.eof() || self.peek() != '"' {
            return Err(self.errorf(&format!("expected quoted string after #{tag}")));
        }
        let tag = tag.to_string();
        let raw = self.parse_string()?;
        let syn = self.make_syntax(start_line, start_col, self.line, self.col, comments);
        match tag.as_str() {
            "usd" => match parse_usd(&raw) {
                Ok(m) => Ok(atom_expr(Primitive::Money(Box::new(m)), syn)),
                Err(e) => Err(ParseError(format!(
                    "{}:{}:{}: invalid #usd literal {}: {}",
                    self.path,
                    start_line,
                    start_col,
                    quote(&raw),
                    e
                ))),
            },
            "date" => match parse_date(&raw) {
                Ok(d) => Ok(atom_expr(Primitive::Date(Box::new(d)), syn)),
                Err(e) => Err(ParseError(format!(
                    "{}:{}:{}: invalid #date literal {}: {}",
                    self.path,
                    start_line,
                    start_col,
                    quote(&raw),
                    e
                ))),
            },
            _ => Err(ParseError(format!(
                "{}:{}:{}: unknown reader tag #{}",
                self.path, start_line, start_col, tag
            ))),
        }
    }

    fn parse_atom(
        &mut self,
        start_line: i64,
        start_col: i64,
        comments: Vec<String>,
    ) -> Result<taxlispv1::Expression, ParseError> {
        let tok_start = self.pos;
        while !self.eof() && !is_delimiter(self.peek()) {
            self.advance();
        }
        let tok = &self.src[tok_start..self.pos];
        if tok.is_empty() {
            return Err(self.errorf(&format!("unexpected character {}", quote_rune(self.peek()))));
        }
        let syn = self.make_syntax(start_line, start_col, self.line, self.col, comments);

        if let Some(kw) = tok.strip_prefix(':')
            && !kw.is_empty()
        {
            return Ok(atom_expr(Primitive::Keyword(kw.to_string()), syn));
        }
        if tok == "true" || tok == "false" {
            return Ok(atom_expr(Primitive::BoolValue(tok == "true"), syn));
        }
        if let Ok(n) = parse_int(tok) {
            return Ok(atom_expr(Primitive::IntValue(n), syn));
        }
        if let Some(f) = parse_float_literal(tok) {
            return Ok(atom_expr(Primitive::FloatValue(f), syn));
        }

        let sym = parse_symbol_token(tok);
        Ok(atom_expr(Primitive::Symbol(Box::new(sym)), syn))
    }

    fn parse_string(&mut self) -> Result<String, ParseError> {
        let (start_line, start_col) = (self.line, self.col);
        self.advance(); // consume opening '"'
        let mut b = String::new();
        while !self.eof() {
            let ch = self.peek();
            if ch == '"' {
                self.advance();
                return Ok(b);
            }
            if ch == '\\' {
                self.advance();
                if self.eof() {
                    return Err(ParseError(format!(
                        "{}:{}:{}: unclosed string literal",
                        self.path, start_line, start_col
                    )));
                }
                let esc = self.peek();
                self.advance();
                match esc {
                    'n' => b.push('\n'),
                    't' => b.push('\t'),
                    'r' => b.push('\r'),
                    '"' => b.push('"'),
                    '\\' => b.push('\\'),
                    _ => return Err(self.errorf(&format!("invalid string escape \\{esc}"))),
                }
                continue;
            }
            b.push(ch);
            self.advance();
        }
        Err(ParseError(format!(
            "{}:{}:{}: unclosed string literal",
            self.path, start_line, start_col
        )))
    }

    fn skip_whitespace_and_comments(&mut self) -> Vec<String> {
        let mut comments = Vec::new();
        while !self.eof() {
            let ch = self.peek();
            if ch.is_whitespace() {
                self.advance();
                continue;
            }
            if ch == ';' {
                self.advance(); // consume ';'
                let start = self.pos;
                while !self.eof() && self.peek() != '\n' {
                    self.advance();
                }
                let line_text = self.src[start..self.pos].trim_end_matches('\r');
                comments.push(line_text.to_string());
                continue;
            }
            break;
        }
        comments
    }

    fn make_syntax(
        &self,
        start_line: i64,
        start_col: i64,
        end_line: i64,
        end_col: i64,
        comments: Vec<String>,
    ) -> taxlispv1::SyntaxDetails {
        taxlispv1::SyntaxDetails {
            file: self.path.clone(),
            start_line: start_line as i32,
            start_column: start_col as i32,
            end_line: end_line as i32,
            end_column: end_col as i32,
            leading_comments: comments,
            ..Default::default()
        }
    }

    fn eof(&self) -> bool {
        self.pos >= self.src.len()
    }

    /// The rune at the current position, or U+FFFD at EOF as Go's
    /// utf8.DecodeRuneInString returns for an empty string.
    fn peek(&self) -> char {
        self.src[self.pos..].chars().next().unwrap_or('\u{FFFD}')
    }

    fn advance(&mut self) {
        let Some(r) = self.src[self.pos..].chars().next() else {
            return;
        };
        self.pos += r.len_utf8();
        if r == '\n' {
            self.line += 1;
            self.col = 1;
        } else {
            self.col += 1;
        }
    }

    fn errorf(&self, msg: &str) -> ParseError {
        ParseError(format!("{}:{}:{}: {}", self.path, self.line, self.col, msg))
    }
}

fn parse_symbol_token(tok: &str) -> taxlispv1::Symbol {
    // Division operator "/" is an unqualified symbol.
    if tok == "/" {
        return taxlispv1::Symbol {
            name: "/".to_string(),
            ..Default::default()
        };
    }
    if let Some(idx) = tok.find('/')
        && idx > 0
        && idx < tok.len() - 1
    {
        return taxlispv1::Symbol {
            package: tok[..idx].to_string(),
            name: tok[idx + 1..].to_string(),
            ..Default::default()
        };
    }
    taxlispv1::Symbol {
        name: tok.to_string(),
        ..Default::default()
    }
}

/// Go's unicode.IsLetter, approximated by the Alphabetic property beyond
/// ASCII.
fn is_letter(r: char) -> bool {
    r.is_alphabetic()
}

/// Go's unicode.IsDigit (category Nd), approximated by Rust's numeric
/// classes beyond ASCII.
fn is_digit(r: char) -> bool {
    r.is_ascii_digit() || (!r.is_ascii() && r.is_numeric())
}

fn is_delimiter(r: char) -> bool {
    r.is_whitespace() || matches!(r, '(' | ')' | '[' | ']' | '"' | ';' | '\'')
}

/// The error Go's strconv.ParseInt returns, by its text.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NumError(pub String);

impl fmt::Display for NumError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

/// Go's strconv.ParseInt(s, 10, 64), with its error text.
pub fn parse_int(s: &str) -> Result<i64, NumError> {
    // Rust accepts exactly Go's base-10 syntax: an optional sign and digits.
    s.parse::<i64>().map_err(|e| {
        let what = match e.kind() {
            std::num::IntErrorKind::PosOverflow | std::num::IntErrorKind::NegOverflow => {
                "value out of range"
            }
            _ => "invalid syntax",
        };
        NumError(format!("strconv.ParseInt: parsing {}: {}", quote(s), what))
    })
}

fn parse_usd(s: &str) -> Result<commonv1::Money, NumError> {
    let mut s = s.trim();
    if s.is_empty() {
        return Err(NumError("empty money string".to_string()));
    }
    let mut neg = false;
    if let Some(rest) = s.strip_prefix('-') {
        neg = true;
        s = rest;
    } else if let Some(rest) = s.strip_prefix('+') {
        s = rest;
    }
    let parts: Vec<&str> = s.split('.').collect();
    if parts.len() > 2 {
        return Err(NumError("too many decimal points".to_string()));
    }
    let dollars = parse_int(parts[0])?;
    let mut cents: i64 = 0;
    if parts.len() == 2 {
        let frac = parts[1];
        match frac.len() {
            0 => cents = 0,
            1 => cents = parse_int(frac)?.wrapping_mul(10),
            2 => cents = parse_int(frac)?,
            _ => {
                return Err(NumError(format!(
                    "USD literal {} has more than 2 decimal places",
                    quote(s)
                )));
            }
        }
    }
    let mut minor = dollars.wrapping_mul(100).wrapping_add(cents);
    if neg {
        minor = minor.wrapping_neg();
    }
    Ok(commonv1::Money {
        currency: "USD".to_string(),
        minor,
        ..Default::default()
    })
}

fn parse_float_literal(tok: &str) -> Option<f64> {
    if tok.is_empty() {
        return None;
    }
    let mut body = tok;
    if body.starts_with('+') || body.starts_with('-') {
        body = &body[1..];
    }
    if body.is_empty() || !body.as_bytes()[0].is_ascii_digit() {
        return None;
    }
    if !body.contains(['.', 'e', 'E']) {
        return None;
    }
    parse_float(tok)
}

/// Go's strconv.ParseFloat(s, 64) when it returns no error: the value, or
/// None for a syntax error or an overflow to infinity. Only the numeric
/// forms are handled (decimal and hexadecimal, with Go's underscore rules);
/// "inf" and "nan" never reach here because the caller requires a leading
/// digit.
pub fn parse_float(s: &str) -> Option<f64> {
    let rf = read_float(s)?;
    if rf.end != s.len() {
        return None;
    }
    let f = if rf.hex {
        atof_hex(rf.mantissa, rf.exp, rf.neg, rf.trunc)?
    } else {
        let cleaned: String = s.chars().filter(|&c| c != '_').collect();
        cleaned.parse::<f64>().ok()?
    };
    if f.is_infinite() {
        return None;
    }
    Some(f)
}

struct ReadFloat {
    mantissa: u64,
    exp: i64,
    neg: bool,
    trunc: bool,
    hex: bool,
    end: usize,
}

/// Go's strconv.readFloat: the syntax check, and for hexadecimal the
/// mantissa and exponent.
fn read_float(s: &str) -> Option<ReadFloat> {
    let b = s.as_bytes();
    let lower = |c: u8| c | 0x20;
    let mut i = 0usize;
    let mut neg = false;
    let mut underscores = false;
    if i >= b.len() {
        return None;
    }
    if b[i] == b'+' {
        i += 1;
    } else if b[i] == b'-' {
        neg = true;
        i += 1;
    }

    let mut base: u64 = 10;
    let mut max_mant_digits = 19;
    let mut exp_char = b'e';
    let mut hex = false;
    if i + 2 < b.len() && b[i] == b'0' && lower(b[i + 1]) == b'x' {
        base = 16;
        max_mant_digits = 16;
        i += 2;
        exp_char = b'p';
        hex = true;
    }
    let mut sawdot = false;
    let mut sawdigits = false;
    let mut nd: i64 = 0;
    let mut nd_mant: i64 = 0;
    let mut dp: i64 = 0;
    let mut mantissa: u64 = 0;
    let mut trunc = false;
    while i < b.len() {
        let c = b[i];
        if c == b'_' {
            underscores = true;
            i += 1;
            continue;
        } else if c == b'.' {
            if sawdot {
                break;
            }
            sawdot = true;
            dp = nd;
            i += 1;
            continue;
        } else if c.is_ascii_digit() {
            sawdigits = true;
            if c == b'0' && nd == 0 {
                dp -= 1;
                i += 1;
                continue;
            }
            nd += 1;
            if nd_mant < max_mant_digits {
                mantissa = mantissa * base + u64::from(c - b'0');
                nd_mant += 1;
            } else if c != b'0' {
                trunc = true;
            }
            i += 1;
            continue;
        } else if base == 16 && (b'a'..=b'f').contains(&lower(c)) {
            sawdigits = true;
            nd += 1;
            if nd_mant < max_mant_digits {
                mantissa = mantissa * 16 + u64::from(lower(c) - b'a' + 10);
                nd_mant += 1;
            } else {
                trunc = true;
            }
            i += 1;
            continue;
        }
        break;
    }
    if !sawdigits {
        return None;
    }
    if !sawdot {
        dp = nd;
    }
    if base == 16 {
        dp *= 4;
        nd_mant *= 4;
    }

    if i < b.len() && lower(b[i]) == exp_char {
        i += 1;
        if i >= b.len() {
            return None;
        }
        let mut esign: i64 = 1;
        if b[i] == b'+' {
            i += 1;
        } else if b[i] == b'-' {
            i += 1;
            esign = -1;
        }
        if i >= b.len() || !b[i].is_ascii_digit() {
            return None;
        }
        let mut e: i64 = 0;
        while i < b.len() && (b[i].is_ascii_digit() || b[i] == b'_') {
            if b[i] == b'_' {
                underscores = true;
                i += 1;
                continue;
            }
            if e < 10000 {
                e = e * 10 + i64::from(b[i] - b'0');
            }
            i += 1;
        }
        dp += e * esign;
    } else if base == 16 {
        // Must have exponent.
        return None;
    }

    let mut exp: i64 = 0;
    if mantissa != 0 {
        exp = dp - nd_mant;
    }

    if underscores && !underscore_ok(&s[..i]) {
        return None;
    }
    Some(ReadFloat {
        mantissa,
        exp,
        neg,
        trunc,
        hex,
        end: i,
    })
}

/// Go's strconv.underscoreOK: whether underscores in s appear only between
/// digits or between a base prefix and a digit.
fn underscore_ok(s: &str) -> bool {
    let mut b = s.as_bytes();
    let lower = |c: u8| c | 0x20;
    let mut saw = b'^';
    let mut i = 0usize;
    if !b.is_empty() && (b[0] == b'-' || b[0] == b'+') {
        b = &b[1..];
    }
    let mut hex = false;
    if b.len() >= 2 && b[0] == b'0' && matches!(lower(b[1]), b'b' | b'o' | b'x') {
        i = 2;
        saw = b'0';
        hex = lower(b[1]) == b'x';
    }
    while i < b.len() {
        let c = b[i];
        if c.is_ascii_digit() || (hex && (b'a'..=b'f').contains(&lower(c))) {
            saw = b'0';
            i += 1;
            continue;
        }
        if c == b'_' {
            if saw != b'0' {
                return false;
            }
            saw = b'_';
            i += 1;
            continue;
        }
        if saw == b'_' {
            return false;
        }
        saw = b'!';
        i += 1;
    }
    saw != b'_'
}

/// Go's strconv.atofHex for float64; None on overflow (Go's range error).
fn atof_hex(mut mantissa: u64, mut exp: i64, neg: bool, trunc: bool) -> Option<f64> {
    const MANTBITS: u32 = 52;
    const EXPBITS: u32 = 11;
    const BIAS: i64 = -1023;
    let max_exp: i64 = (1i64 << EXPBITS) + BIAS - 2;
    let min_exp: i64 = BIAS + 1;
    exp += i64::from(MANTBITS);

    while mantissa != 0 && mantissa >> (MANTBITS + 2) == 0 {
        mantissa <<= 1;
        exp -= 1;
    }
    if trunc {
        mantissa |= 1;
    }
    while mantissa >> (1 + MANTBITS + 2) != 0 {
        mantissa = (mantissa >> 1) | (mantissa & 1);
        exp += 1;
    }

    while mantissa > 1 && exp < min_exp - 2 {
        mantissa = (mantissa >> 1) | (mantissa & 1);
        exp += 1;
    }

    let mut round = mantissa & 3;
    mantissa >>= 2;
    round |= mantissa & 1;
    exp += 2;
    if round == 3 {
        mantissa += 1;
        if mantissa == 1 << (1 + MANTBITS) {
            mantissa >>= 1;
            exp += 1;
        }
    }

    if mantissa >> MANTBITS == 0 {
        exp = BIAS;
    }
    if exp > max_exp {
        return None;
    }
    let mut bits = mantissa & ((1u64 << MANTBITS) - 1);
    bits |= (((exp - BIAS) as u64) & ((1u64 << EXPBITS) - 1)) << MANTBITS;
    if neg {
        bits |= 1u64 << MANTBITS << EXPBITS;
    }
    Some(f64::from_bits(bits))
}

fn parse_date(s: &str) -> Result<commonv1::Date, NumError> {
    let parts: Vec<&str> = s.trim().split('-').collect();
    if parts.len() != 3 {
        return Err(NumError("expected YYYY-MM-DD".to_string()));
    }
    let field = |p: &str, lo: i64, hi: i64| -> Option<i32> {
        let v = p.parse::<i32>().ok()?;
        (i64::from(v) >= lo && i64::from(v) <= hi).then_some(v)
    };
    let y = field(parts[0], 1, 9999)
        .ok_or_else(|| NumError(format!("invalid year {}", quote(parts[0]))))?;
    let m = field(parts[1], 1, 12)
        .ok_or_else(|| NumError(format!("invalid month {}", quote(parts[1]))))?;
    let d = field(parts[2], 1, 31)
        .ok_or_else(|| NumError(format!("invalid day {}", quote(parts[2]))))?;
    Ok(commonv1::Date {
        year: y,
        month: m,
        day: d,
        ..Default::default()
    })
}

fn expr_syntax(e: &taxlispv1::Expression) -> Option<&taxlispv1::SyntaxDetails> {
    match &e.value {
        Some(Value::Atom(a)) => a.syntax.as_option(),
        Some(Value::List(l)) => l.syntax.as_option(),
        None => None,
    }
}

fn write_leading_comments(b: &mut String, syn: Option<&taxlispv1::SyntaxDetails>, indent: usize) {
    let Some(syn) = syn else {
        return;
    };
    let prefix = " ".repeat(indent);
    for c in &syn.leading_comments {
        b.push_str(&prefix);
        b.push(';');
        b.push_str(c);
        b.push('\n');
    }
}

// Go threads an indent it never prints; it is kept for a literal port.
#[allow(clippy::only_used_in_recursion)]
fn format_expr_into(b: &mut String, e: &taxlispv1::Expression, indent: usize) {
    match &e.value {
        Some(Value::Atom(a)) => format_atom(b, a),
        Some(Value::List(l)) => {
            let elems = &l.expressions;
            if elems.len() == 2 && is_symbol_named(&elems[0], "", "quote") {
                b.push('\'');
                format_expr_into(b, &elems[1], indent + 1);
                return;
            }
            b.push('(');
            for (i, child) in elems.iter().enumerate() {
                if i > 0 {
                    b.push(' ');
                }
                format_expr_into(b, child, indent + 1);
            }
            b.push(')');
        }
        None => {}
    }
}

fn format_atom(b: &mut String, a: &taxlispv1::Atom) {
    match &a.primitive {
        Some(Primitive::Symbol(s)) => {
            if !s.package.is_empty() {
                b.push_str(&s.package);
                b.push('/');
            }
            b.push_str(&s.name);
        }
        Some(Primitive::Keyword(k)) => {
            b.push(':');
            b.push_str(k);
        }
        Some(Primitive::StringValue(s)) => b.push_str(&quote(s)),
        Some(Primitive::IntValue(n)) => b.push_str(&n.to_string()),
        Some(Primitive::BoolValue(v)) => b.push_str(if *v { "true" } else { "false" }),
        Some(Primitive::Money(m)) => {
            b.push_str("#usd\"");
            b.push_str(&format_usd(m));
            b.push('"');
        }
        Some(Primitive::FloatValue(f)) => b.push_str(&format_float(*f)),
        Some(Primitive::Date(d)) => {
            b.push_str(&format!(
                "#date\"{}-{}-{}\"",
                go_pad(d.year, 4),
                go_pad(d.month, 2),
                go_pad(d.day, 2)
            ));
        }
        None => {}
    }
}

/// Go's `%0Nd`: zero-padded to width, with the sign before the zeros.
fn go_pad(v: i32, width: usize) -> String {
    if v < 0 {
        format!(
            "-{:0w$}",
            i64::from(v).unsigned_abs(),
            w = width.saturating_sub(1)
        )
    } else {
        format!("{v:0width$}")
    }
}

/// FormatUSD formats a Money message as an exact decimal string (e.g.
/// "15750.00" or "-45.50").
pub fn format_usd(m: &commonv1::Money) -> String {
    let mut minor = m.minor;
    let neg = minor < 0;
    if neg {
        minor = minor.wrapping_neg();
    }
    let dollars = minor / 100;
    let cents = minor % 100;
    let sign = if neg { "-" } else { "" };
    if cents == 0 {
        return format!("{sign}{dollars}.00");
    }
    format!("{sign}{dollars}.{cents:02}")
}

/// FormatFloat formats a float64 as a canonical taxlisp floating-point
/// literal, ensuring a decimal point is present so it round-trips as a Float
/// atom.
pub fn format_float(f: f64) -> String {
    let mut s = go_format_float_g(f);
    if !s.contains(['.', 'e', 'E']) {
        s.push_str(".0");
    }
    s
}

/// Go's strconv.FormatFloat(f, 'g', -1, 64), which is also what `%v` prints
/// for a float64: the shortest decimal that round-trips, in %e form (with at
/// least two exponent digits) when the decimal exponent is below -4 or at
/// least 6, as Go uses precision 6 for that decision in the shortest form.
pub fn go_format_float_g(f: f64) -> String {
    if f.is_nan() {
        return "NaN".to_string();
    }
    if f.is_infinite() {
        return if f > 0.0 { "+Inf" } else { "-Inf" }.to_string();
    }
    // Rust's {:e} prints the shortest round-tripping digits, as Go does.
    let e = format!("{f:e}");
    let (mant, exp) = match e.split_once('e') {
        Some(parts) => parts,
        None => (e.as_str(), "0"),
    };
    let exp10: i64 = exp.parse().unwrap_or(0);
    let (neg, mant) = match mant.strip_prefix('-') {
        Some(m) => (true, m),
        None => (false, mant),
    };
    let digits: Vec<u8> = mant.bytes().filter(|c| c.is_ascii_digit()).collect();
    let nd = digits.len() as i64;
    let dp = exp10 + 1;
    let mut out = String::new();
    if neg {
        out.push('-');
    }
    let x = dp - 1;
    if !(-4..6).contains(&x) {
        // %e with nd-1 fraction digits.
        out.push(digits[0] as char);
        if nd > 1 {
            out.push('.');
            for &d in &digits[1..] {
                out.push(d as char);
            }
        }
        out.push('e');
        let (sign, ax) = if x < 0 { ('-', -x) } else { ('+', x) };
        out.push(sign);
        if ax < 10 {
            out.push('0');
        }
        out.push_str(&ax.to_string());
        return out;
    }
    // %f with max(nd-dp, 0) fraction digits.
    if dp > 0 {
        for i in 0..dp {
            out.push(if i < nd {
                digits[i as usize] as char
            } else {
                '0'
            });
        }
    } else {
        out.push('0');
    }
    let prec = (nd - dp).max(0);
    if prec > 0 {
        out.push('.');
        for i in 1..=prec {
            let j = dp + i - 1;
            out.push(if (0..nd).contains(&j) {
                digits[j as usize] as char
            } else {
                '0'
            });
        }
    }
    out
}

fn is_symbol_named(e: &taxlispv1::Expression, pkg: &str, name: &str) -> bool {
    let Some(Value::Atom(a)) = &e.value else {
        return false;
    };
    let Some(Primitive::Symbol(sym)) = &a.primitive else {
        return false;
    };
    sym.package == pkg && sym.name == name
}

/// Go's strconv.IsPrint, approximated: Go's tables are Unicode's letters,
/// marks, numbers, punctuation, symbols and U+0020. Beyond ASCII this treats
/// every character as printable except controls, spaces other than U+0020,
/// format characters, private-use characters and noncharacters.
fn is_print(r: char) -> bool {
    let c = r as u32;
    if c < 0x80 {
        return (0x20..0x7f).contains(&c);
    }
    if r.is_control() || r.is_whitespace() {
        return false;
    }
    !matches!(c,
        0x00AD | 0x0600..=0x0605 | 0x061C | 0x06DD | 0x070F | 0x0890..=0x0891 | 0x08E2
        | 0x180E | 0x200B..=0x200F | 0x202A..=0x202E | 0x2060..=0x206F | 0xFEFF
        | 0xFFF9..=0xFFFB | 0x110BD | 0x110CD | 0x13430..=0x1343F | 0x1BCA0..=0x1BCA3
        | 0x1D173..=0x1D17A | 0xE0001 | 0xE0020..=0xE007F
        | 0xE000..=0xF8FF | 0xF0000..=0x10FFFF | 0xFDD0..=0xFDEF)
        && (c & 0xFFFE) != 0xFFFE
}

fn append_quoted_rune(out: &mut String, r: char, quote_char: char) {
    if r == quote_char || r == '\\' {
        out.push('\\');
        out.push(r);
        return;
    }
    if is_print(r) {
        out.push(r);
        return;
    }
    match r {
        '\u{7}' => out.push_str("\\a"),
        '\u{8}' => out.push_str("\\b"),
        '\u{c}' => out.push_str("\\f"),
        '\n' => out.push_str("\\n"),
        '\r' => out.push_str("\\r"),
        '\t' => out.push_str("\\t"),
        '\u{b}' => out.push_str("\\v"),
        _ => {
            let c = r as u32;
            if c < 0x20 || c == 0x7f {
                out.push_str(&format!("\\x{c:02x}"));
            } else if c < 0x10000 {
                out.push_str(&format!("\\u{c:04x}"));
            } else {
                out.push_str(&format!("\\U{c:08x}"));
            }
        }
    }
}

/// Go's strconv.Quote (and `%q` of a string): a double-quoted Go string
/// literal with Go escapes for non-printable characters.
pub fn quote(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for r in s.chars() {
        append_quoted_rune(&mut out, r, '"');
    }
    out.push('"');
    out
}

/// Go's strconv.QuoteRune (and `%q` of a rune): a single-quoted Go
/// character literal.
pub fn quote_rune(r: char) -> String {
    let mut out = String::from("'");
    append_quoted_rune(&mut out, r, '\'');
    out.push('\'');
    out
}

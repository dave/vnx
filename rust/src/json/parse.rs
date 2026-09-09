//! A strict [RFC 8259] JSON parser.
//!
//! Strict matters here. SPEC 4.2 forbids non-finite numbers throughout a
//! document and notes that "some language runtimes emit and accept them by
//! default"; `corpus/reject/nan-literal.vnx` and `infinity-literal.vnx` exist
//! because a permissive parser turns a refusal into a coordinate that poisons
//! every distance computed downstream. This parser has no permissive mode.

use super::{object_set, Json};

/// Why a document is not JSON this crate will read.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ParseErrorKind {
    /// Not well-formed JSON — including the `NaN`, `Infinity` and `-Infinity`
    /// literals, which are not JSON at all.
    Malformed,
    /// Well-formed JSON whose decimal literal overflows binary64. SPEC 6.2
    /// lists "a number in the document is not finite" as a refusal cause.
    NotFinite,
    /// Nesting past the crate's depth limit. SPEC 9 asks a reader that accepts
    /// untrusted documents to bound what one may consume; the format itself
    /// places no limit on depth.
    TooDeep,
}

#[derive(Debug, Clone)]
pub struct ParseError {
    pub kind: ParseErrorKind,
    /// Byte offset into the input at which the fault was found.
    pub offset: usize,
    pub detail: String,
}

impl std::fmt::Display for ParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self.kind {
            ParseErrorKind::Malformed => {
                write!(f, "malformed JSON at byte {}: {}", self.offset, self.detail)
            }
            ParseErrorKind::NotFinite => write!(
                f,
                "a number in the document is not finite, at byte {}: {}",
                self.offset, self.detail
            ),
            ParseErrorKind::TooDeep => write!(
                f,
                "malformed JSON at byte {}: nesting deeper than {} levels",
                self.offset, MAX_DEPTH
            ),
        }
    }
}

impl std::error::Error for ParseError {}

/// How deep a document may nest. A VNX document defined by SPEC 3 nests about
/// eight levels; the headroom is for opaque data record bodies and `rulesets`.
const MAX_DEPTH: usize = 256;

/// Parse a whole JSON document. Trailing content other than whitespace is an
/// error.
pub fn parse(input: &[u8]) -> Result<Json, ParseError> {
    let mut parser = Parser {
        input,
        at: 0,
        depth: 0,
    };
    parser.skip_whitespace();
    let value = parser.value()?;
    parser.skip_whitespace();
    if parser.at != parser.input.len() {
        return Err(parser.malformed("trailing content after the document"));
    }
    Ok(value)
}

struct Parser<'a> {
    input: &'a [u8],
    at: usize,
    depth: usize,
}

impl<'a> Parser<'a> {
    fn malformed(&self, detail: &str) -> ParseError {
        ParseError {
            kind: ParseErrorKind::Malformed,
            offset: self.at,
            detail: detail.to_string(),
        }
    }

    fn peek(&self) -> Option<u8> {
        self.input.get(self.at).copied()
    }

    fn skip_whitespace(&mut self) {
        while let Some(b) = self.peek() {
            // RFC 8259 section 2: space, tab, line feed, carriage return.
            if b == b' ' || b == b'\t' || b == b'\n' || b == b'\r' {
                self.at += 1;
            } else {
                break;
            }
        }
    }

    fn literal(&mut self, word: &str) -> bool {
        if self.input[self.at..].starts_with(word.as_bytes()) {
            self.at += word.len();
            true
        } else {
            false
        }
    }

    fn value(&mut self) -> Result<Json, ParseError> {
        let Some(b) = self.peek() else {
            return Err(self.malformed("the document ended where a value was expected"));
        };
        match b {
            b'{' => self.object(),
            b'[' => self.array(),
            b'"' => Ok(Json::String(self.string()?)),
            b't' if self.literal("true") => Ok(Json::Bool(true)),
            b'f' if self.literal("false") => Ok(Json::Bool(false)),
            b'n' if self.literal("null") => Ok(Json::Null),
            // Named explicitly so the refusal says what it saw. These three are
            // not JSON; SPEC 4.2 forbids them and the corpus refuses them.
            b'N' if self.input[self.at..].starts_with(b"NaN") => {
                Err(self.malformed("`NaN` is not a JSON value"))
            }
            b'I' if self.input[self.at..].starts_with(b"Infinity") => {
                Err(self.malformed("`Infinity` is not a JSON value"))
            }
            b'-' if self.input[self.at..].starts_with(b"-Infinity") => {
                Err(self.malformed("`-Infinity` is not a JSON value"))
            }
            b'-' | b'0'..=b'9' => self.number(),
            _ => Err(self.malformed("a value cannot start here")),
        }
    }

    fn enter(&mut self) -> Result<(), ParseError> {
        self.depth += 1;
        if self.depth > MAX_DEPTH {
            return Err(ParseError {
                kind: ParseErrorKind::TooDeep,
                offset: self.at,
                detail: String::new(),
            });
        }
        Ok(())
    }

    fn object(&mut self) -> Result<Json, ParseError> {
        self.enter()?;
        self.at += 1; // '{'
        let mut entries: Vec<(String, Json)> = Vec::new();
        self.skip_whitespace();
        if self.peek() == Some(b'}') {
            self.at += 1;
            self.depth -= 1;
            return Ok(Json::Object(entries));
        }
        loop {
            self.skip_whitespace();
            if self.peek() != Some(b'"') {
                return Err(self.malformed("an object member name must be a string"));
            }
            let key = self.string()?;
            self.skip_whitespace();
            if self.peek() != Some(b':') {
                return Err(self.malformed("a colon must follow an object member name"));
            }
            self.at += 1;
            self.skip_whitespace();
            let value = self.value()?;
            // SPEC 6.4: a repeated key resolves last-wins, before any VNX rule
            // sees the document. Applied here so no later stage can see two.
            object_set(&mut entries, key, value);
            self.skip_whitespace();
            match self.peek() {
                Some(b',') => self.at += 1,
                Some(b'}') => {
                    self.at += 1;
                    self.depth -= 1;
                    return Ok(Json::Object(entries));
                }
                _ => return Err(self.malformed("an object member must be followed by `,` or `}`")),
            }
        }
    }

    fn array(&mut self) -> Result<Json, ParseError> {
        self.enter()?;
        self.at += 1; // '['
        let mut items = Vec::new();
        self.skip_whitespace();
        if self.peek() == Some(b']') {
            self.at += 1;
            self.depth -= 1;
            return Ok(Json::Array(items));
        }
        loop {
            self.skip_whitespace();
            items.push(self.value()?);
            self.skip_whitespace();
            match self.peek() {
                Some(b',') => self.at += 1,
                Some(b']') => {
                    self.at += 1;
                    self.depth -= 1;
                    return Ok(Json::Array(items));
                }
                _ => return Err(self.malformed("an array element must be followed by `,` or `]`")),
            }
        }
    }

    fn string(&mut self) -> Result<String, ParseError> {
        self.at += 1; // '"'
        let mut out = String::new();
        loop {
            let Some(b) = self.peek() else {
                return Err(self.malformed("the document ended inside a string"));
            };
            match b {
                b'"' => {
                    self.at += 1;
                    return Ok(out);
                }
                b'\\' => {
                    self.at += 1;
                    self.escape(&mut out)?;
                }
                0x00..=0x1f => {
                    return Err(self.malformed(
                        "a control character below U+0020 must be escaped inside a string",
                    ))
                }
                _ => {
                    // Take one whole UTF-8 sequence. Invalid UTF-8 is malformed
                    // JSON: RFC 8259 section 8.1 requires UTF-8.
                    let rest = &self.input[self.at..];
                    let text = std::str::from_utf8(rest);
                    let ch = match text {
                        Ok(s) => s.chars().next().expect("the slice is non-empty"),
                        Err(e) if e.valid_up_to() > 0 => {
                            std::str::from_utf8(&rest[..e.valid_up_to()])
                                .expect("valid_up_to marks valid UTF-8")
                                .chars()
                                .next()
                                .expect("the slice is non-empty")
                        }
                        Err(_) => return Err(self.malformed("a string is not valid UTF-8")),
                    };
                    out.push(ch);
                    self.at += ch.len_utf8();
                }
            }
        }
    }

    fn escape(&mut self, out: &mut String) -> Result<(), ParseError> {
        let Some(b) = self.peek() else {
            return Err(self.malformed("the document ended inside an escape"));
        };
        self.at += 1;
        let ch = match b {
            b'"' => '"',
            b'\\' => '\\',
            b'/' => '/',
            b'b' => '\u{08}',
            b'f' => '\u{0c}',
            b'n' => '\n',
            b'r' => '\r',
            b't' => '\t',
            b'u' => return self.unicode_escape(out),
            _ => return Err(self.malformed("unknown escape sequence")),
        };
        out.push(ch);
        Ok(())
    }

    fn unicode_escape(&mut self, out: &mut String) -> Result<(), ParseError> {
        let first = self.hex4()?;
        let code = if (0xd800..0xdc00).contains(&first) {
            // A high surrogate must be followed by its low surrogate. A lone
            // surrogate has no UTF-8 encoding, so it cannot be carried through
            // a Rust string and is refused rather than substituted.
            if !self.input[self.at..].starts_with(b"\\u") {
                return Err(self.malformed("a high surrogate is not followed by a low surrogate"));
            }
            self.at += 2;
            let second = self.hex4()?;
            if !(0xdc00..0xe000).contains(&second) {
                return Err(self.malformed("a high surrogate is not followed by a low surrogate"));
            }
            0x1_0000 + ((first - 0xd800) << 10) + (second - 0xdc00)
        } else if (0xdc00..0xe000).contains(&first) {
            return Err(self.malformed("a low surrogate stands alone"));
        } else {
            first
        };
        let ch = char::from_u32(code)
            .ok_or_else(|| self.malformed("an escape names no Unicode scalar value"))?;
        out.push(ch);
        Ok(())
    }

    fn hex4(&mut self) -> Result<u32, ParseError> {
        if self.at + 4 > self.input.len() {
            return Err(self.malformed("a `\\u` escape needs four hexadecimal digits"));
        }
        let mut value = 0u32;
        for i in 0..4 {
            let b = self.input[self.at + i];
            let digit = (b as char)
                .to_digit(16)
                .ok_or_else(|| self.malformed("a `\\u` escape needs four hexadecimal digits"))?;
            value = value * 16 + digit;
        }
        self.at += 4;
        Ok(value)
    }

    fn number(&mut self) -> Result<Json, ParseError> {
        let start = self.at;
        if self.peek() == Some(b'-') {
            self.at += 1;
        }
        // int: `0` or a digit 1-9 followed by digits. A leading zero is not
        // JSON, and accepting one would make `01` mean 1 in this reader and
        // nothing at all in the next.
        match self.peek() {
            Some(b'0') => self.at += 1,
            Some(b'1'..=b'9') => {
                while matches!(self.peek(), Some(b'0'..=b'9')) {
                    self.at += 1;
                }
            }
            _ => return Err(self.malformed("a number needs at least one digit")),
        }
        if self.peek() == Some(b'.') {
            self.at += 1;
            if !matches!(self.peek(), Some(b'0'..=b'9')) {
                return Err(self.malformed("a decimal point needs a digit after it"));
            }
            while matches!(self.peek(), Some(b'0'..=b'9')) {
                self.at += 1;
            }
        }
        if matches!(self.peek(), Some(b'e') | Some(b'E')) {
            self.at += 1;
            if matches!(self.peek(), Some(b'+') | Some(b'-')) {
                self.at += 1;
            }
            if !matches!(self.peek(), Some(b'0'..=b'9')) {
                return Err(self.malformed("an exponent needs at least one digit"));
            }
            while matches!(self.peek(), Some(b'0'..=b'9')) {
                self.at += 1;
            }
        }
        let text = std::str::from_utf8(&self.input[start..self.at])
            .expect("a number literal is ASCII by construction");
        let value: f64 = text
            .parse()
            .map_err(|_| self.malformed("a number literal will not parse"))?;
        if !value.is_finite() {
            // `1e400` is well-formed JSON and overflows binary64. SPEC 6.2
            // makes a number that is not finite a refusal cause, so it is
            // refused here rather than becoming an infinity in the model.
            return Err(ParseError {
                kind: ParseErrorKind::NotFinite,
                offset: start,
                detail: format!("`{text}` is outside the range of a binary64"),
            });
        }
        Ok(Json::Number(value))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ok(text: &str) -> Json {
        parse(text.as_bytes()).expect("parses")
    }

    fn err(text: &str) -> ParseError {
        parse(text.as_bytes()).expect_err("does not parse")
    }

    #[test]
    fn refuses_the_three_non_json_literals() {
        for text in [
            r#"{"a": NaN}"#,
            r#"{"a": Infinity}"#,
            r#"{"a": -Infinity}"#,
        ] {
            assert_eq!(err(text).kind, ParseErrorKind::Malformed, "{text}");
        }
    }

    #[test]
    fn refuses_a_literal_that_overflows_binary64() {
        assert_eq!(err("1e400").kind, ParseErrorKind::NotFinite);
        assert_eq!(err("-1e400").kind, ParseErrorKind::NotFinite);
        // Underflow to zero is not an overflow and is kept.
        assert_eq!(ok("1e-400"), Json::Number(0.0));
    }

    #[test]
    fn duplicate_keys_resolve_last_wins() {
        let value = ok(r#"{"n1": 1, "n1": 2, "n2": 3}"#);
        assert_eq!(value.get("n1"), Some(&Json::Number(2.0)));
        assert_eq!(value.as_object().expect("an object").len(), 2);
    }

    #[test]
    fn refuses_what_a_permissive_parser_would_take() {
        for text in [
            "{'a': 1}",
            r#"{"a": 1,}"#,
            "[1,]",
            "{a: 1}",
            "01",
            "+1",
            ".5",
            "1.",
            "1e",
            "nul",
            r#""a" "b""#,
            "",
            r#"{"a": 1"#,
        ] {
            assert_eq!(err(text).kind, ParseErrorKind::Malformed, "{text} parsed");
        }
    }

    #[test]
    fn refuses_a_raw_control_character_in_a_string() {
        assert_eq!(err("\"a\nb\"").kind, ParseErrorKind::Malformed);
    }

    #[test]
    fn reads_escapes_and_surrogate_pairs() {
        assert_eq!(ok(r#""A""#), Json::String("A".into()));
        assert_eq!(ok(r#""😀""#), Json::String("\u{1f600}".into()));
        assert_eq!(ok(r#""\/\b\f\n\r\t""#), Json::String("/\u{8}\u{c}\n\r\t".into()));
    }

    #[test]
    fn refuses_a_lone_surrogate() {
        assert_eq!(err(r#""\ud83d""#).kind, ParseErrorKind::Malformed);
        assert_eq!(err(r#""\udc00""#).kind, ParseErrorKind::Malformed);
        assert_eq!(err(r#""\ud83dA""#).kind, ParseErrorKind::Malformed);
    }

    #[test]
    fn refuses_nesting_past_the_limit() {
        let deep = format!("{}{}", "[".repeat(300), "]".repeat(300));
        assert_eq!(err(&deep).kind, ParseErrorKind::TooDeep);
        let fine = format!("{}{}", "[".repeat(200), "]".repeat(200));
        parse(fine.as_bytes()).expect("200 levels is within the limit");
    }

    #[test]
    fn keeps_raw_non_ascii() {
        assert_eq!(ok("\"Càrn\""), Json::String("Càrn".into()));
    }

    #[test]
    fn refuses_a_string_that_is_not_utf8() {
        // A lone 0x80 continuation byte inside a string.
        let bytes = [b'"', 0x80, b'"'];
        let error = parse(&bytes).expect_err("does not parse");
        assert_eq!(error.kind, ParseErrorKind::Malformed);
    }
}

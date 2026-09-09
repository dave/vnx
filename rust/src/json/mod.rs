//! A JSON value, a parser and the pieces of a renderer.
//!
//! Written by hand because the crate takes no dependencies, and because a
//! stock encoder could not emit VNX canonical form in any case (SPEC 4.5.6).

pub mod number;
mod parse;

pub use parse::{parse, ParseError, ParseErrorKind};

/// A JSON value.
///
/// An object keeps its entries in a `Vec` rather than a map. Order is not
/// significant in this format (SPEC 4.4) and canonical form sorts on render,
/// but a `Vec` keeps the type free of a hashing dependency and keeps the
/// parser's last-wins duplicate handling (SPEC 6.4) explicit.
#[derive(Debug, Clone, PartialEq)]
pub enum Json {
    Null,
    Bool(bool),
    /// Always finite. The parser refuses a literal that is not, and nothing
    /// else in this crate constructs one.
    Number(f64),
    String(String),
    Array(Vec<Json>),
    Object(Vec<(String, Json)>),
}

impl Json {
    /// True for the four scalar kinds, which canonical form sorts ahead of the
    /// two container kinds (SPEC 4.5.1).
    pub fn is_scalar(&self) -> bool {
        matches!(
            self,
            Json::Null | Json::Bool(_) | Json::Number(_) | Json::String(_)
        )
    }

    pub fn as_object(&self) -> Option<&[(String, Json)]> {
        match self {
            Json::Object(entries) => Some(entries),
            _ => None,
        }
    }

    pub fn as_array(&self) -> Option<&[Json]> {
        match self {
            Json::Array(items) => Some(items),
            _ => None,
        }
    }

    pub fn as_str(&self) -> Option<&str> {
        match self {
            Json::String(s) => Some(s),
            _ => None,
        }
    }

    pub fn as_f64(&self) -> Option<f64> {
        match self {
            Json::Number(n) => Some(*n),
            _ => None,
        }
    }

    pub fn as_bool(&self) -> Option<bool> {
        match self {
            Json::Bool(b) => Some(*b),
            _ => None,
        }
    }

    /// Look a key up in an object. `None` for a non-object.
    pub fn get(&self, key: &str) -> Option<&Json> {
        self.as_object()?
            .iter()
            .find(|(k, _)| k == key)
            .map(|(_, v)| v)
    }

    /// The name of this value's JSON type, for diagnostic text.
    pub fn type_name(&self) -> &'static str {
        match self {
            Json::Null => "null",
            Json::Bool(_) => "a boolean",
            Json::Number(_) => "a number",
            Json::String(_) => "a string",
            Json::Array(_) => "an array",
            Json::Object(_) => "an object",
        }
    }
}

/// Set `key` on an object, last-wins: an existing entry is replaced in place.
///
/// This is how SPEC 6.4's duplicate-key resolution is applied, and it is
/// applied in the parser so that no later stage can see two entries with one
/// name.
pub(crate) fn object_set(entries: &mut Vec<(String, Json)>, key: String, value: Json) {
    if let Some(slot) = entries.iter_mut().find(|(k, _)| *k == key) {
        slot.1 = value;
        return;
    }
    entries.push((key, value));
}

/// Escape a string for a JSON document under SPEC 4.5.5.
///
/// Only `"`, `\` and the control characters below U+0020 are escaped. Every
/// other character — including non-ASCII, `<`, `>`, `&`, U+2028 and U+2029 —
/// is written raw.
///
/// SPEC 4.5.5 does not say how a control character is spelled, and the two
/// candidate answers differ byte for byte: ECMAScript `JSON.stringify` uses
/// the two-character escapes `\b \t \n \f \r` and `\u00XX` for the rest, while
/// Go's `encoding/json` writes those two as \u0008 and \u000c, not as \b and \f, for backspace and form
/// feed. This crate follows ECMAScript, because SPEC 4.5.3 already defines
/// number rendering by reference to ECMAScript. Reported as a spec gap; no
/// corpus case reaches it.
pub fn escape_string(s: &str, out: &mut String) {
    out.push('"');
    for ch in s.chars() {
        match ch {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{08}' => out.push_str("\\b"),
            '\u{09}' => out.push_str("\\t"),
            '\u{0a}' => out.push_str("\\n"),
            '\u{0c}' => out.push_str("\\f"),
            '\u{0d}' => out.push_str("\\r"),
            c if (c as u32) < 0x20 => {
                out.push_str("\\u");
                for shift in [12, 8, 4, 0] {
                    let nibble = ((c as u32) >> shift) & 0xf;
                    out.push(char::from_digit(nibble, 16).expect("nibble is below 16"));
                }
            }
            c => out.push(c),
        }
    }
    out.push('"');
}

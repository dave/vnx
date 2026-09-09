//! Canonical form (SPEC 4.5): the byte layout every conforming writer emits.
//!
//! This renders a `Json` tree, not a model. Both `re_render` and `write` build
//! a `Json` tree and hand it here, so the two operations differ only in what
//! they put into the tree — SPEC 8.1's distinction — and not in how it is laid
//! out.

use crate::json::{escape_string, number, Json};

/// Render a JSON tree in canonical form, with a trailing newline.
pub fn render(value: &Json) -> String {
    let mut out = String::new();
    write_value(value, 0, &mut out);
    out.push('\n');
    out
}

const INDENT: &str = "  ";

fn write_indent(depth: usize, out: &mut String) {
    for _ in 0..depth {
        out.push_str(INDENT);
    }
}

fn write_value(value: &Json, depth: usize, out: &mut String) {
    match value {
        Json::Null => out.push_str("null"),
        Json::Bool(true) => out.push_str("true"),
        Json::Bool(false) => out.push_str("false"),
        Json::Number(n) => out.push_str(&number::render(*n)),
        Json::String(s) => escape_string(s, out),
        Json::Array(items) => write_array(items, depth, out),
        Json::Object(entries) => write_object(entries, depth, out),
    }
}

/// SPEC 4.5.2. An array holding only numbers and `null` goes on one line, with
/// `, ` between elements. That is every position in the format, and `null`
/// counts, so a position does not change shape when its elevation goes
/// missing.
fn inline_worthy(items: &[Json]) -> bool {
    items
        .iter()
        .all(|item| matches!(item, Json::Number(_) | Json::Null))
}

fn write_array(items: &[Json], depth: usize, out: &mut String) {
    if items.is_empty() {
        out.push_str("[]");
        return;
    }
    if inline_worthy(items) {
        out.push('[');
        for (i, item) in items.iter().enumerate() {
            if i > 0 {
                out.push_str(", ");
            }
            write_value(item, depth, out);
        }
        out.push(']');
        return;
    }
    out.push_str("[\n");
    for (i, item) in items.iter().enumerate() {
        write_indent(depth + 1, out);
        write_value(item, depth + 1, out);
        if i + 1 < items.len() {
            out.push(',');
        }
        out.push('\n');
    }
    write_indent(depth, out);
    out.push(']');
}

fn write_object(entries: &[(String, Json)], depth: usize, out: &mut String) {
    if entries.is_empty() {
        out.push_str("{}");
        return;
    }
    // SPEC 4.5.1: scalars first in lexicographic order, then containers in
    // lexicographic order, classified by the value present in THIS object.
    //
    // Lexicographic means by Unicode code point. Rust's `str` ordering is by
    // UTF-8 byte, which agrees with code point order at every code point — the
    // property UTF-16 ordering lacks above U+FFFF. So no custom comparator is
    // needed here, unlike in JavaScript.
    let mut order: Vec<&(String, Json)> = entries.iter().collect();
    order.sort_by(|a, b| {
        let a_container = !a.1.is_scalar();
        let b_container = !b.1.is_scalar();
        a_container.cmp(&b_container).then_with(|| a.0.cmp(&b.0))
    });

    out.push_str("{\n");
    for (i, (key, value)) in order.iter().enumerate() {
        write_indent(depth + 1, out);
        escape_string(key, out);
        out.push_str(": ");
        write_value(value, depth + 1, out);
        if i + 1 < order.len() {
            out.push(',');
        }
        out.push('\n');
    }
    write_indent(depth, out);
    out.push('}');
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::json::parse;

    fn canonicalise(text: &str) -> String {
        render(&parse(text.as_bytes()).expect("parses"))
    }

    #[test]
    fn scalars_precede_containers_and_each_group_sorts() {
        let out = canonicalise(r#"{"nodes":{},"vnx":"0.1.0","notes":"n","arcs":[]}"#);
        assert_eq!(
            out,
            "{\n  \"notes\": \"n\",\n  \"vnx\": \"0.1.0\",\n  \"arcs\": [],\n  \"nodes\": {}\n}\n"
        );
    }

    /// SPEC 4.5.1: classification is by the value present in this object, not
    /// by the key name. The same key sorts differently in two objects.
    #[test]
    fn classification_follows_the_value_not_the_name() {
        assert!(canonicalise(r#"{"a":[1],"b":"s"}"#).find("\"b\"") < canonicalise(r#"{"a":[1],"b":"s"}"#).find("\"a\""));
        assert!(canonicalise(r#"{"a":"s","b":[1]}"#).find("\"a\"") < canonicalise(r#"{"a":"s","b":[1]}"#).find("\"b\""));
    }

    #[test]
    fn a_number_and_null_array_is_inline_and_anything_else_is_not() {
        assert_eq!(canonicalise("[1,null,2.5]"), "[1, null, 2.5]\n");
        assert_eq!(canonicalise("[]"), "[]\n");
        assert_eq!(canonicalise("[[1,2],[3,4]]"), "[\n  [1, 2],\n  [3, 4]\n]\n");
        assert_eq!(canonicalise(r#"[1,"a"]"#), "[\n  1,\n  \"a\"\n]\n");
        assert_eq!(canonicalise("[true]"), "[\n  true\n]\n");
    }

    #[test]
    fn empty_containers_are_one_line() {
        assert_eq!(canonicalise(r#"{"a":{},"b":[]}"#), "{\n  \"a\": {},\n  \"b\": []\n}\n");
    }

    /// SPEC 4.5.5. `<`, `>`, `&`, U+2028 and U+2029 are written raw, which
    /// Go's `encoding/json` does not do by default.
    #[test]
    fn only_quote_and_backslash_are_escaped_among_printable_characters() {
        let raw = "<&>\u{2028}\u{2029}\u{e0}";
        let mut out = String::new();
        crate::json::escape_string(raw, &mut out);
        assert_eq!(out, format!("\"{raw}\""), "nothing here needs escaping");

        assert_eq!(canonicalise(r#"["a\"b\\c"]"#), "[\n  \"a\\\"b\\\\c\"\n]\n");
    }

    /// SPEC 4.5.5 requires the control characters below U+0020 to be escaped
    /// but does not say HOW, and the two candidate answers differ byte for
    /// byte. This crate follows ECMAScript `JSON.stringify`: the five
    /// two-character escapes, then `\u00XX`. Go's `encoding/json` writes
    /// backspace and form feed as `\u0008` and `\u000c` instead. No corpus
    /// case reaches this; reported as a spec gap.
    #[test]
    fn control_characters_take_the_ecmascript_spelling() {
        let mut out = String::new();
        crate::json::escape_string(
            "\u{0}\u{1}\u{8}\u{9}\u{a}\u{b}\u{c}\u{d}\u{1f}",
            &mut out,
        );
        assert_eq!(out, r#""\u0000\u0001\b\t\n\u000b\f\r\u001f""#);
    }

    /// SPEC 4.5.1: lexicographic means by Unicode CODE POINT. Two pairs
    /// separate that from the defaults other languages hand you, and both are
    /// reachable because a data record body may carry any key (SPEC 5.1).
    ///
    /// Rust's `str` ordering is by UTF-8 byte, which agrees with code point
    /// order everywhere, so this crate needs no comparator and these
    /// assertions cannot fail by someone deleting one. They are here because a
    /// future maintainer reaching for `char_indices` or a locale-aware
    /// comparison would break them, and because the pairs are the ones the
    /// other seats had to fix.
    #[test]
    fn keys_sort_by_code_point() {
        // U+FF3A FULLWIDTH LATIN CAPITAL LETTER Z is below U+10000 by code
        // point and above it by UTF-16 code unit, because an astral character
        // is stored as a surrogate pair beginning in U+D800. JavaScript's
        // `Array.prototype.sort()` and Java's `String.compareTo` get this pair
        // the wrong way round.
        let out = canonicalise("{\"\u{10000}\":1,\"\u{ff3a}\":2}");
        assert!(
            out.find('\u{ff3a}') < out.find('\u{10000}'),
            "sorted by UTF-16 code unit rather than by code point: {out}"
        );
        assert!(
            "\u{ff3a}" < "\u{10000}",
            "Rust's own ordering no longer agrees with code point order"
        );

        // U+212B ANGSTROM SIGN is above U+00C6 by code point, and below it
        // under Unicode canonical equivalence, because it decomposes to
        // U+00C5. Swift's default `<` on String orders by equivalence and gets
        // this pair wrong while getting the astral pair right.
        let out = canonicalise("{\"\u{212b}\":1,\"\u{c6}\":2}");
        assert!(
            out.find('\u{c6}') < out.find('\u{212b}'),
            "sorted by canonical equivalence rather than by code point: {out}"
        );
    }

    #[test]
    fn ends_with_exactly_one_newline() {
        let out = canonicalise(r#"{"vnx":"0.1.0"}"#);
        assert!(out.ends_with("}\n"));
        assert!(!out.ends_with("\n\n"));
    }
}

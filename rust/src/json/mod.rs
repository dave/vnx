//! A JSON value, parsed and serialised with `serde_json`.
//!
//! The format does not constrain a document's bytes (SPEC 8.1): key order,
//! number spelling and whitespace are the writer's own business. So there is
//! no hand-written parser, renderer or number formatter here — a stock library
//! does both halves.
//!
//! # Why this type still exists beside `serde_json::Value`
//!
//! An object keeps its entries in a `Vec` rather than a map, so it holds the
//! order the document used. `serde_json::Value` sorts, and the regions this
//! format carries through untouched — a style, an application key, a data record's body
//! (SPEC 5.1, 3.9) — are read back out and written again. Keeping the order
//! costs one conversion and means a third party's object leaves this crate
//! spelled the way it arrived.
//!
//! Duplicate keys are resolved by `serde_json` on the way in, last-wins, which
//! is SPEC 6.4's rule.

/// A JSON value.
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

/// Parse JSON bytes.
///
/// The error is `serde_json`'s own text. It is the detail of a refusal and
/// never a diagnostic: bytes that will not parse leave no document to return.
///
/// ⚠ THIS IS ALSO WHERE SPEC 4.2's NON-FINITE RULE IS ENFORCED, and it is
/// enforced by JSON itself rather than by a check of ours. `NaN` and
/// `Infinity` are not JSON tokens, and a decimal literal too large for a
/// binary64 is out of range, so all three are refused here. There is no later
/// stage that could see one.
pub fn parse(bytes: &[u8]) -> Result<Json, String> {
    let value: serde_json::Value = serde_json::from_slice(bytes).map_err(|e| e.to_string())?;
    Ok(from_serde(value))
}

/// Serialise a value, COMPACT.
///
/// ⚠ THE DOCUMENT IS NOT INDENTED, AND THAT IS A DECISION RATHER THAN AN
/// OMISSION. Nothing about a VNX document's whitespace is constrained and
/// indenting costs about three times the file. Pretty-printing belongs to a
/// tool, not to the format.
pub fn to_string(value: &Json) -> String {
    serde_json::to_string(&to_serde(value)).expect("a JSON tree of finite numbers serialises")
}

fn from_serde(value: serde_json::Value) -> Json {
    match value {
        serde_json::Value::Null => Json::Null,
        serde_json::Value::Bool(b) => Json::Bool(b),
        serde_json::Value::Number(n) => Json::Number(
            n.as_f64()
                .expect("serde_json parses every number to a binary64"),
        ),
        serde_json::Value::String(s) => Json::String(s),
        serde_json::Value::Array(items) => Json::Array(items.into_iter().map(from_serde).collect()),
        serde_json::Value::Object(members) => {
            Json::Object(members.into_iter().map(|(k, v)| (k, from_serde(v))).collect())
        }
    }
}

fn to_serde(value: &Json) -> serde_json::Value {
    match value {
        Json::Null => serde_json::Value::Null,
        Json::Bool(b) => serde_json::Value::Bool(*b),
        Json::Number(n) => serde_json::Value::Number(number(*n)),
        Json::String(s) => serde_json::Value::String(s.clone()),
        Json::Array(items) => serde_json::Value::Array(items.iter().map(to_serde).collect()),
        Json::Object(entries) => serde_json::Value::Object(
            entries
                .iter()
                .map(|(k, v)| (k.clone(), to_serde(v)))
                .collect(),
        ),
    }
}

/// Build a JSON number from a double.
///
/// A value that is a whole number is spelled as an integer, which is what
/// `time_ms` is (SPEC 4.2) and what every other implementation of this format
/// emits. `serde_json` would otherwise write `1778914991000.0`. Both spellings
/// carry the same value and either is conforming; this one is the one a reader
/// of the file expects to see.
///
/// Negative zero is a whole number and so leaves as `0`. That is a SPELLING
/// decision, not a rounding one — the two zeros are the same binary64 value
/// and SPEC 4.2 makes the round trip value-level. It used to be load-bearing
/// for the writer, which manufactured negative zeros by rounding; the writer
/// no longer rounds, so the only way one reaches here is a document that
/// stated it.
///
/// # Panics
///
/// Panics on a non-finite value. JSON cannot spell one, and returning
/// something else would put a number in the file that no reader accepts.
fn number(value: f64) -> serde_json::Number {
    assert!(value.is_finite(), "VNX carries no non-finite number");
    if value.fract() == 0.0 && value.abs() <= i64::MAX as f64 {
        return serde_json::Number::from(value as i64);
    }
    serde_json::Number::from_f64(value).expect("a finite double is a JSON number")
}

#[cfg(test)]
mod tests {
    use super::*;

    /// ⚠ THE `float_roundtrip` FEATURE IS WHAT MAKES THIS PASS. Without it
    /// `serde_json` returns bits c00d5eb03aa8431d for this literal, one below
    /// the double `str::parse` gives. The literal is the longitude in
    /// `corpus/accept/full-precision-coordinate`, whose whole purpose is to
    /// prove a reader does not lose precision, so the feature is part of the
    /// implementation rather than a tuning choice.
    #[test]
    fn a_full_precision_coordinate_parses_to_the_same_double_the_standard_library_gives() {
        let text = "-3.6712345678901235";
        let Json::Number(parsed) = parse(text.as_bytes()).expect("parses") else {
            panic!("not a number");
        };
        assert_eq!(
            parsed.to_bits(),
            text.parse::<f64>().expect("parses").to_bits(),
            "serde_json disagrees with the standard library on the last bit"
        );
    }

    /// ⚠ `serde_json` DISTINGUISHES `1` FROM `1.0` AND THIS CRATE MUST NOT.
    ///
    /// `Value::Number` remembers whether a literal parsed as an integer or as a
    /// float, so `json!(1) == json!(1.0)` is FALSE. SPEC 4.2 makes the round
    /// trip value-level and the format states no rule about spelling, so two
    /// conforming writers legitimately spell an integral coordinate,
    /// elevation or timestamp differently — Go writes `1`, Python's
    /// `json.dumps` writes `1.0`.
    ///
    /// Every number is collapsed to a binary64 here, at the parse, so nothing
    /// downstream can compare a type tag by accident. The first half of this
    /// test measures that `serde_json` really does distinguish them, or the
    /// second half would be guarding against nothing.
    #[test]
    fn a_number_is_a_value_and_not_a_spelling() {
        let integer: serde_json::Value = serde_json::from_str("1").expect("parses");
        let float: serde_json::Value = serde_json::from_str("1.0").expect("parses");
        assert_ne!(
            integer, float,
            "serde_json no longer distinguishes the two spellings, so this \
             crate's normalisation is guarding against nothing"
        );

        for pair in [("1", "1.0"), ("340", "340.0"), ("1e3", "1000")] {
            assert_eq!(
                parse(pair.0.as_bytes()).expect("parses"),
                parse(pair.1.as_bytes()).expect("parses"),
                "{} and {} are the same value", pair.0, pair.1
            );
        }
        // And two different values still differ, or the above passes vacuously.
        assert_ne!(
            parse(b"1").expect("parses"),
            parse(b"2").expect("parses")
        );
    }

    /// SPEC 6.4: a repeated key resolves last-wins, and it resolves in the
    /// parser so that no later stage can see two entries with one name.
    #[test]
    fn a_duplicate_key_resolves_to_the_last_one() {
        let value = parse(br#"{"a":1,"a":2}"#).expect("parses");
        assert_eq!(value.as_object().expect("an object").len(), 1);
        assert_eq!(value.get("a").and_then(Json::as_f64), Some(2.0));
    }

    /// SPEC 4.2: none of the three spellings of a non-finite number is JSON,
    /// and each is refused here rather than reaching a later stage.
    #[test]
    fn a_non_finite_literal_is_refused() {
        for text in ["NaN", "Infinity", "-Infinity", "1e400", "-1e400"] {
            assert!(parse(text.as_bytes()).is_err(), "{text} was accepted");
        }
    }

    /// A whole number is spelled as an integer, and a negative zero is one.
    #[test]
    fn a_whole_number_is_spelled_without_a_fractional_part() {
        let value = Json::Array(vec![
            Json::Number(1778914991000.0),
            Json::Number(340.0),
            Json::Number(-0.0),
            Json::Number(57.118123),
        ]);
        assert_eq!(to_string(&value), "[1778914991000,340,0,57.118123]");
    }

    /// Deep nesting is refused rather than overflowing the stack. `serde_json`
    /// carries its own recursion limit; this pins that the crate inherits it.
    #[test]
    fn deep_nesting_is_refused_rather_than_overflowing_the_stack() {
        let text = "[".repeat(5000) + &"]".repeat(5000);
        assert!(parse(text.as_bytes()).is_err());
    }
}

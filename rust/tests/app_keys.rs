//! Application keys (SPEC 3.9): a top-level key containing a full stop holds
//! any JSON value, kept verbatim and never reported. The accept tier
//! round-trips a corpus file holding several; these pin the rule directly.

use vnx::json::{parse, Json};
use vnx::model::{AppKey, Document};
use vnx::{decode, encode, EncodeError};

#[path = "support/tree.rs"]
mod tree;
use tree::EmitBoundary;

#[test]
fn app_keys_are_preserved_opaquely_and_sorted() {
    let src = r#"{"vnx":"0.1.0",
      "org.example.z": {"anything":{"at":["all"]}},
      "app.viatrail.rulesets":[{"name":"Hide minor","rules":[{"op":"equals"}]}],
      "com.example.null": null,
      "com.example.empty": [],
      "com.example.string": "text"}"#;
    let reading = decode(src.as_bytes()).expect("accepted");
    assert!(
        reading.diagnostics.is_empty(),
        "an application key must not be reported: {:?}",
        reading.diagnostics
    );
    let Json::Object(tree) = parse(src.as_bytes()).expect("JSON") else {
        panic!("not an object");
    };
    let keys: Vec<&str> = reading.document.app_keys.iter().map(|k| k.key.as_str()).collect();
    assert_eq!(
        keys,
        [
            "app.viatrail.rulesets",
            "com.example.empty",
            "com.example.null",
            "com.example.string",
            "org.example.z"
        ]
    );
    for app_key in &reading.document.app_keys {
        let want = &tree.iter().find(|(k, _)| *k == app_key.key).unwrap().1;
        assert_eq!(&app_key.value, want, "{} not preserved verbatim", app_key.key);
    }
    let null = &reading.document.app_keys[2];
    assert_eq!(null.value, Json::Null, "a null value is a present key holding null");
}

/// The rule is TOP-LEVEL ONLY. On any other object a dotted key is an
/// unrecognised key; an undotted unknown key at the top level is still unknown.
#[test]
fn a_dotted_key_below_the_top_level_is_reported() {
    let reading = decode(
        br#"{"vnx":"0.1.0","rulesets":[],"waypoints":[{"at":[1,2],"app.viatrail.x":1}]}"#,
    )
    .expect("accepted");
    let mut got: Vec<(String, &str)> = reading
        .diagnostics
        .iter()
        .map(|d| (d.path.clone(), d.code()))
        .collect();
    got.sort();
    assert_eq!(
        got,
        [
            ("/rulesets".to_string(), "unknown-key"),
            ("/waypoints/0/app.viatrail.x".to_string(), "unknown-key"),
        ]
    );
}

/// A null stays null and an empty value stays empty through a
/// read-modify-write: SPEC 3.9 exempts application keys from SPEC 8.1's
/// absent-equals-empty normalisation.
#[test]
fn app_keys_survive_a_read_modify_write() {
    let src = br#"{"vnx":"0.1.0","app.viatrail.rulesets":[{"zebra":1,"apple":[2,{"nested":true}]}],
      "com.example.null":null,"com.example.empty-array":[],"com.example.empty-object":{},"com.example.n":0}"#;
    let document = decode(src).expect("accepted").document;
    let out = encode(&document).expect("encodable");
    assert_eq!(tree::compare(out.as_bytes(), src, EmitBoundary::Normalise), None);
}

/// The comparison must not normalise an application key's empty value away,
/// or the round trip above could not fail on a writer that dropped one.
#[test]
fn the_tree_comparison_sees_a_dropped_empty_app_key() {
    assert!(
        tree::compare(
            br#"{"vnx":"0.1.0"}"#,
            br#"{"vnx":"0.1.0","com.example.empty":[]}"#,
            EmitBoundary::Normalise
        )
        .is_some(),
        "a dropped application key holding [] compared equal; SPEC 3.9 forbids that"
    );
}

#[test]
fn encode_refuses_an_app_key_it_cannot_write() {
    let key = |k: &str, v: Json| AppKey {
        key: k.to_string(),
        value: v,
    };
    let cases = [
        (vec![key("rulesets", Json::Array(vec![]))], "no full stop"),
        (vec![key("folders", Json::Array(vec![]))], "no full stop"),
        (vec![key("", Json::Null)], "no full stop"),
        (
            vec![key("a.b", Json::Number(1.0)), key("a.b", Json::Number(2.0))],
            "twice",
        ),
    ];
    for (keys, want) in cases {
        let document = Document {
            app_keys: keys,
            ..Document::default()
        };
        let err = encode(&document).expect_err("refused");
        let matches = match (&err, want) {
            (EncodeError::AppKeyWithoutFullStop { .. }, "no full stop") => true,
            (EncodeError::AppKeyRepeated { .. }, "twice") => true,
            _ => false,
        };
        assert!(matches, "{err} for {want}");
    }
}

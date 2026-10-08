//! SPEC 4.2: JSON cannot spell a non-finite number. The parser refuses one,
//! but a caller can build a document holding one, and the encoder refuses it
//! with an error naming where it is, rather than panicking.

use vnx::json::Json;
use vnx::model::{AppKey, Position};
use vnx::{decode, encode, EncodeError};

const SRC: &str = r#"{"vnx":"0.10.0","waypoints":[{"at":[1,2],"style":{"importance":3}}]}"#;

fn refused_at(doc: &vnx::Document, want: &str) {
    match encode(doc) {
        Err(EncodeError::NonFiniteNumber { path }) => assert_eq!(path, want),
        other => panic!("encode = {other:?}, want NonFiniteNumber at {want}"),
    }
    match vnx::encode::to_json(doc) {
        Err(EncodeError::NonFiniteNumber { path }) => assert_eq!(path, want),
        other => panic!("to_json = {other:?}, want NonFiniteNumber at {want}"),
    }
}

#[test]
fn a_non_finite_number_anywhere_is_an_encode_error() {
    let base = decode(SRC.as_bytes()).expect("accepted").document;
    assert!(
        encode(&base).is_ok(),
        "premise: the unspoiled document encodes"
    );

    for value in [f64::NAN, f64::INFINITY, f64::NEG_INFINITY] {
        // A position channel.
        let mut doc = base.clone();
        doc.waypoints.as_mut().expect("waypoints")[0].at =
            Position::new(vec![Some(value), Some(2.0)]);
        refused_at(&doc, "/waypoints/0/at/0");

        // A style value.
        let mut doc = base.clone();
        doc.waypoints.as_mut().expect("waypoints")[0].style =
            Some(Json::Object(vec![("width".into(), Json::Number(value))]));
        refused_at(&doc, "/waypoints/0/style/width");

        // An application key's value, nested.
        let mut doc = base.clone();
        doc.app_keys = vec![AppKey {
            key: "com.example/x".into(),
            value: Json::Array(vec![Json::Number(1.0), Json::Number(value)]),
        }];
        refused_at(&doc, "/com.example~1x/1");
    }
}

#[test]
fn to_string_refuses_a_hand_built_non_finite_number() {
    let err = vnx::json::to_string(&Json::Array(vec![Json::Number(f64::NAN)]))
        .expect_err("a NaN was serialised");
    assert!(
        matches!(err, EncodeError::NonFiniteNumber { ref path } if path == "/0"),
        "{err:?}"
    );
}

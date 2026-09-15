//! Turning a model back into a document (SPEC 4.1, 8.1).
//!
//! There is one operation. `encode` applies the emit-boundary rule — emit the
//! shortest spelling of a position — and hands the tree to `serde_json`.
//!
//! ⚠ NO VALUE CHANGES ON THE WAY OUT. SPEC 4.1 does not constrain how
//! precisely a writer states a coordinate, so the binary64 the model holds is
//! the binary64 that is written. The shortest spelling is about a position's
//! ARITY, and the accept tier applies it to both sides of its comparison
//! (SPEC 8.1); the write tier does not, because there it is the rule under
//! test.

use crate::json::{self, Json};
use crate::model::*;

/// Encode a document.
///
/// The bytes are compact. Nothing about a VNX document's whitespace is
/// constrained (SPEC 8.1), and indenting costs about three times the file.
pub fn encode(document: &Document) -> String {
    json::to_string(&to_json(document))
}

/// Build the JSON tree for a document.
///
/// Key order is not significant (SPEC 4.4), so the keys are listed in the
/// order SPEC 3.10 tabulates them: identity first, geometry next, opaque
/// payload last. That is a property of THIS implementation's output and is not
/// promised to any other.
pub fn to_json(document: &Document) -> Json {
    let mut o = Obj::new();
    if let Some(version) = &document.vnx {
        // SPEC 2.2: write the version the DOCUMENT declared, not the reader's.
        o.str("vnx", Some(&version.text));
    }
    o.str("generator", document.generator.as_deref());
    o.str("created", document.created.as_deref());
    o.list("folders", &document.folders, folder);
    o.list("networks", &document.networks, network);
    o.list("routes", &document.routes, route);
    o.list("tracks", &document.tracks, track);
    o.list("waypoints", &document.waypoints, waypoint);
    o.list("areas", &document.areas, area);
    if let Some(rulesets) = &document.rulesets {
        o.raw("rulesets", rulesets.clone());
    }
    o.done()
}

// ---------------------------------------------------------------------------

fn folder(f: &Folder) -> Json {
    let mut o = Obj::new();
    o.str("id", Some(&f.id));
    o.str("parent", f.parent.as_deref());
    o.str("name", f.name.as_deref());
    o.str("notes", f.notes.as_deref());
    o.hidden(f.hidden);
    o.records(&f.data);
    o.done()
}

fn network(n: &Network) -> Json {
    let mut o = Obj::new();
    o.str("name", n.name.as_deref());
    o.str("notes", n.notes.as_deref());
    o.str("folder", n.folder.as_deref());
    o.hidden(n.hidden);
    if let Some(nodes) = &n.nodes {
        o.raw(
            "nodes",
            Json::Object(
                nodes
                    .iter()
                    .map(|(k, p)| (k.clone(), position(p)))
                    .collect(),
            ),
        );
    }
    if let Some(arcs) = &n.arcs {
        o.raw(
            "arcs",
            Json::Object(
                arcs.iter()
                    .map(|(k, a)| (k.clone(), network_arc(a)))
                    .collect(),
            ),
        );
    }
    o.list("chains", &n.chains, chain);
    o.list("points", &n.points, waypoint);
    o.list("areas", &n.areas, area);
    o.records(&n.data);
    o.done()
}

fn network_arc(a: &NetworkArc) -> Json {
    let mut o = Obj::new();
    o.str("from", Some(&a.from));
    o.str("to", Some(&a.to));
    o.raw("pips", pips(&a.pips));
    o.records(&a.data);
    o.done()
}

fn chain(c: &Chain) -> Json {
    let mut o = Obj::new();
    o.str("name", c.name.as_deref());
    o.str("notes", c.notes.as_deref());
    o.hidden(c.hidden);
    o.opaque("style", c.style.as_ref());
    o.raw(
        "links",
        Json::Array(c.links.iter().map(link).collect()),
    );
    o.records(&c.data);
    o.done()
}

fn link(l: &Link) -> Json {
    let mut o = Obj::new();
    o.str("arc", Some(&l.arc));
    o.raw("from", endpoint(&l.from));
    o.raw("to", endpoint(&l.to));
    o.done()
}

fn endpoint(e: &Endpoint) -> Json {
    match e {
        Endpoint::Node(key) => Json::String(key.clone()),
        Endpoint::Inline(p) => position(p),
    }
}

fn route(r: &Route) -> Json {
    let mut o = Obj::new();
    o.str("name", r.name.as_deref());
    o.str("notes", r.notes.as_deref());
    o.str("folder", r.folder.as_deref());
    o.hidden(r.hidden);
    o.opaque("style", r.style.as_ref());
    o.raw(
        "arcs",
        Json::Array(r.arcs.iter().map(route_arc).collect()),
    );
    o.records(&r.data);
    o.done()
}

fn route_arc(a: &RouteArc) -> Json {
    let mut o = Obj::new();
    o.raw("pips", pips(&a.pips));
    o.records(&a.data);
    o.done()
}

fn track(t: &Track) -> Json {
    let mut o = Obj::new();
    o.str("name", t.name.as_deref());
    o.str("notes", t.notes.as_deref());
    o.str("folder", t.folder.as_deref());
    o.hidden(t.hidden);
    o.str("started", t.started.as_deref());
    o.str("ended", t.ended.as_deref());
    o.opaque("style", t.style.as_ref());
    o.raw(
        "segments",
        Json::Array(
            t.segments
                .iter()
                .map(|s| {
                    let mut o = Obj::new();
                    o.raw("pips", pips(&s.pips));
                    o.done()
                })
                .collect(),
        ),
    );
    o.records(&t.data);
    o.done()
}

fn waypoint(w: &Waypoint) -> Json {
    let mut o = Obj::new();
    o.str("name", w.name.as_deref());
    o.str("notes", w.notes.as_deref());
    o.str("folder", w.folder.as_deref());
    o.hidden(w.hidden);
    o.opaque("style", w.style.as_ref());
    o.raw("at", position(&w.at));
    o.records(&w.data);
    o.done()
}

fn area(a: &Area) -> Json {
    let mut o = Obj::new();
    o.str("name", a.name.as_deref());
    o.str("notes", a.notes.as_deref());
    o.str("folder", a.folder.as_deref());
    o.hidden(a.hidden);
    o.opaque("style", a.style.as_ref());
    o.raw("polygon", pips(&a.polygon));
    o.records(&a.data);
    o.done()
}

fn pips(list: &[Position]) -> Json {
    Json::Array(list.iter().map(position).collect())
}

/// SPEC 4.1: a writer emits the shortest spelling of a position, and this is
/// the only place that rule is applied.
///
/// ⚠ IT IS A RULE ABOUT ARITY, NOT ABOUT PRECISION. This crate does not round,
/// and SPEC 4.1 does not ask it to: it states that a writer SHOULD consider
/// what precision costs and leaves the choice to the producer, the way
/// [RFC 7946] Section 11.2 does. A rounded coordinate and an unrounded one are
/// both conforming, and every conforming reader reads them identically, so the
/// value the model holds is the value that is written.
///
/// A caller that wants smaller files rounds its own model before handing it
/// here. Rounding inside this crate would be a change to the geometry rather
/// than to its spelling: endpoint comparison and de-duplication compare
/// positions by full value.
fn position(p: &Position) -> Json {
    let mut channels: Vec<Option<f64>> = p.channels().to_vec();
    // The shortest spelling: drop TRAILING absences only. An interior `null`
    // keeps its place, because dropping it would move the time into the
    // elevation channel.
    while channels.len() > 2 && channels[channels.len() - 1].is_none() {
        channels.pop();
    }
    Json::Array(
        channels
            .into_iter()
            .map(|c| match c {
                Some(v) => Json::Number(v),
                None => Json::Null,
            })
            .collect(),
    )
}

/// A small builder that drops absent keys.
struct Obj(Vec<(String, Json)>);

impl Obj {
    fn new() -> Self {
        Obj(Vec::new())
    }

    fn raw(&mut self, key: &str, value: Json) {
        self.0.push((key.to_string(), value));
    }

    fn str(&mut self, key: &str, value: Option<&str>) {
        if let Some(v) = value {
            self.raw(key, Json::String(v.to_string()));
        }
    }

    /// SPEC 3.8: a writer emits `hidden` only when it is `true`, and a
    /// document SHOULD NOT carry `"hidden": false`. An absent `hidden` and a
    /// false one say the same thing, so dropping it loses nothing.
    fn hidden(&mut self, value: Option<bool>) {
        if value == Some(true) {
            self.raw("hidden", Json::Bool(true));
        }
    }

    /// A style object, `rulesets`, or anything else this format carries
    /// through untouched.
    fn opaque(&mut self, key: &str, value: Option<&Json>) {
        if let Some(v) = value {
            self.raw(key, v.clone());
        }
    }

    fn records(&mut self, records: &Option<Vec<Record>>) {
        if let Some(list) = records {
            self.raw(
                "data",
                Json::Array(
                    list.iter()
                        .map(|r| Json::Object(r.entries().to_vec()))
                        .collect(),
                ),
            );
        }
    }

    fn list<T>(&mut self, key: &str, items: &Option<Vec<T>>, f: impl Fn(&T) -> Json) {
        if let Some(list) = items {
            self.raw(key, Json::Array(list.iter().map(f).collect()));
        }
    }

    fn done(self) -> Json {
        Json::Object(self.0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::decode::decode;

    fn encode_text(text: &str) -> String {
        let reading = decode(text.as_bytes()).expect("accepted");
        encode(&reading.document)
    }

    #[test]
    fn a_writer_emits_the_shortest_spelling_of_a_position() {
        let out = encode_text(r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6712,57.118,null]}]}"#);
        assert!(out.contains("[-3.6712,57.118]"), "{out}");
    }

    #[test]
    fn an_interior_null_is_never_dropped() {
        let out = encode_text(
            r#"{"vnx":"0.1.0","tracks":[{"segments":[{"pips":[[-3.6,57.1,null,1778914991000]]}]}]}"#,
        );
        assert!(out.contains("[-3.6,57.1,null,1778914991000]"), "{out}");
    }

    /// SPEC 4.1: the write path does not round, so every digit the document
    /// carried is still there after a decode-encode round trip.
    ///
    /// ⚠ THE LONGITUDE HERE IS THE ONE `float_roundtrip` EXISTS FOR. Without
    /// that `serde_json` feature the parse lands one bit out, and the
    /// shortest round-trip spelling of the wrong double is a different string.
    /// So this also fails if the feature is dropped from `Cargo.toml`.
    #[test]
    fn a_writer_changes_no_value_it_was_handed() {
        let out = encode_text(
            r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6712345678901235,57.11812345678901,340.06]}]}"#,
        );
        assert!(
            out.contains("[-3.6712345678901235,57.11812345678901,340.06]"),
            "a value changed on the way out: {out}"
        );
    }

    /// A value a rounding writer would have moved onto an exact tie, or turned
    /// into a negative zero, is written back unchanged. These are the cases
    /// the deleted tie-breaking machinery existed to decide; nothing decides
    /// them now, because nothing touches the value.
    #[test]
    fn the_values_that_used_to_need_a_tie_rule_pass_through() {
        let out = encode_text(
            r#"{"vnx":"0.1.0","waypoints":[{"at":[-0.00000004,0.15,340.25]},{"at":[340.75,1.85,-0.04]}]}"#,
        );
        assert!(out.contains("[-4e-8,0.15,340.25]"), "{out}");
        assert!(out.contains("[340.75,1.85,-0.04]"), "{out}");
    }

    /// SPEC 4.1: a reader MUST NOT round, so the full precision the document
    /// carried survives into the model. Neither path rounds now, and this is
    /// the half of it that is still a REQUIREMENT rather than a choice.
    #[test]
    fn a_reader_keeps_the_precision_the_document_carried() {
        let text = r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6712345678901235,57.11812345678901,340.06]}]}"#;
        let document = decode(text.as_bytes()).expect("accepted").document;
        let at = &document.waypoints.as_ref().expect("waypoints")[0].at;
        assert_eq!(
            at.channels()[0],
            Some(-3.6712345678901235),
            "the model was rounded, and SPEC 4.1 forbids that"
        );
    }

    /// SPEC 3.8: `"hidden": false` says what an absent `hidden` says, and a
    /// writer emits neither. `corpus/write/emit-boundary` pins it.
    #[test]
    fn a_false_hidden_is_not_written() {
        let out = encode_text(r#"{"vnx":"0.1.0","waypoints":[{"at":[1,2],"hidden":false}]}"#);
        assert!(!out.contains("hidden"), "{out}");
        let shown = encode_text(r#"{"vnx":"0.1.0","waypoints":[{"at":[1,2],"hidden":true}]}"#);
        assert!(shown.contains(r#""hidden":true"#), "{shown}");
    }

    #[test]
    fn the_declared_version_is_written_back_not_the_readers() {
        assert!(encode_text(r#"{"vnx":"0.1.99"}"#).contains("0.1.99"));
        assert!(encode_text(r#"{"vnx":"00.01.0000"}"#).contains("00.01.0000"));
    }

    /// The output is compact. Nothing constrains a document's whitespace
    /// (SPEC 8.1), and this pins the decision rather than the rule.
    #[test]
    fn the_output_carries_no_whitespace() {
        let out = encode_text(r#"{"vnx":"0.1.0","waypoints":[{"at":[1,2],"name":"Spring"}]}"#);
        assert!(!out.contains('\n') && !out.contains("  "), "{out}");
    }
}

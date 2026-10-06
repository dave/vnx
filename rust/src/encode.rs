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

use crate::diag::Path;
use crate::error::EncodeError;
use crate::json::{self, Json};
use crate::model::*;
use crate::walk::{check_arcs, check_walk};

/// Encode a document.
///
/// The bytes are compact. Nothing about a VNX document's whitespace is
/// constrained (SPEC 8.1), and indenting costs about three times the file.
///
/// It fails on an application key it cannot write, on a number that is not
/// finite, and on an arc, a walk or a ring its own reader would drop or
/// repair: see [`EncodeError`].
pub fn encode(document: &Document) -> Result<String, EncodeError> {
    json::to_string(&to_json(document)?)
}

/// Build the JSON tree for a document.
///
/// Key order is not significant (SPEC 4.4), so the keys are listed in the
/// order SPEC 3.10 tabulates them: identity first, geometry next, opaque
/// payload last. That is a property of THIS implementation's output and is not
/// promised to any other.
///
/// It fails as [`encode`] does.
pub fn to_json(document: &Document) -> Result<Json, EncodeError> {
    let mut o = Obj::new();
    if let Some(version) = &document.vnx {
        // SPEC 2.2: write the version the DOCUMENT declared, not the reader's.
        o.str("vnx", Some(&version.text));
    }
    o.str("generator", document.generator.as_deref());
    o.str("created", document.created.as_deref());
    let root = Path::root();
    o.list("folders", &document.folders, &root, folder)?;
    o.list("networks", &document.networks, &root, network)?;
    o.list("routes", &document.routes, &root, route)?;
    o.list("tracks", &document.tracks, &root, track)?;
    o.list("waypoints", &document.waypoints, &root, waypoint)?;
    o.list("areas", &document.areas, &root, area)?;
    // SPEC 3.9: each application key as held, `null` and empty values
    // included. A key with no full stop, or one written twice, is refused
    // rather than written into a document that would lose it.
    let mut seen = std::collections::HashSet::new();
    for app_key in &document.app_keys {
        if !is_app_key(&app_key.key) {
            return Err(EncodeError::AppKeyWithoutFullStop {
                key: app_key.key.clone(),
            });
        }
        if !seen.insert(app_key.key.as_str()) {
            return Err(EncodeError::AppKeyRepeated {
                key: app_key.key.clone(),
            });
        }
        o.raw(&app_key.key, app_key.value.clone());
    }
    // SPEC 4.2: JSON cannot spell a non-finite number, and a model field or
    // an opaque value can hold one. Refused here, with where it is.
    let tree = o.done();
    json::check_finite(&tree)?;
    Ok(tree)
}

// ---------------------------------------------------------------------------

type Built = Result<Json, EncodeError>;

fn folder(f: &Folder, _path: &Path) -> Built {
    let mut o = Obj::new();
    o.str("id", Some(&f.id));
    o.str("parent", f.parent.as_deref());
    o.str("name", f.name.as_deref());
    o.str("notes", f.notes.as_deref());
    o.hidden(f.hidden);
    o.records(&f.data);
    Ok(o.done())
}

fn network(n: &Network, path: &Path) -> Built {
    let mut o = Obj::new();
    o.str("name", n.name.as_deref());
    o.str("notes", n.notes.as_deref());
    o.str("folder", n.folder.as_deref());
    o.hidden(n.hidden);
    graph(&mut o, &n.nodes, &n.arcs, path)?;
    let highlights_path = path.key("highlights");
    if let Some(highlights) = &n.highlights {
        let mut out = Vec::with_capacity(highlights.len());
        for (i, c) in highlights.iter().enumerate() {
            out.push(highlight(c, &n.arcs, &highlights_path.index(i))?);
        }
        o.raw("highlights", Json::Array(out));
    }
    o.list("points", &n.points, path, waypoint)?;
    o.list("areas", &n.areas, path, area)?;
    o.records(&n.data);
    Ok(o.done())
}

/// A network's or a route's `nodes` and `arcs`, checked first: a writer never
/// emits an arc its own reader would drop.
fn graph(
    o: &mut Obj,
    nodes: &Option<Vec<(String, Vertex)>>,
    arcs: &Option<Vec<(String, Arc)>>,
    path: &Path,
) -> Result<(), EncodeError> {
    check_arcs(nodes, arcs, &path.key("arcs"))?;
    if let Some(nodes) = nodes {
        o.raw(
            "nodes",
            Json::Object(nodes.iter().map(|(k, v)| (k.clone(), vertex(v))).collect()),
        );
    }
    if let Some(arcs) = arcs {
        o.raw(
            "arcs",
            Json::Object(arcs.iter().map(|(k, a)| (k.clone(), arc(a))).collect()),
        );
    }
    Ok(())
}

fn arc(a: &Arc) -> Json {
    let mut o = Obj::new();
    o.str("from", Some(&a.from));
    o.str("to", Some(&a.to));
    o.raw("pips", Json::Array(a.pips.iter().map(vertex).collect()));
    o.records(&a.data);
    o.done()
}

/// A highlight of a network whose arcs are `arcs`. The walk is checked against
/// them first: a writer never emits a walk its own reader would drop.
fn highlight(c: &Highlight, arcs: &Option<Vec<(String, Arc)>>, path: &Path) -> Built {
    check_walk(&c.walk, arcs, false, &path.key("walk"))?;
    let mut o = Obj::new();
    o.str("name", c.name.as_deref());
    o.str("notes", c.notes.as_deref());
    o.hidden(c.hidden);
    o.opaque("style", c.style.as_ref());
    o.raw("walk", walk(&c.walk));
    o.records(&c.data);
    Ok(o.done())
}

fn walk(passes: &[Pass]) -> Json {
    Json::Array(
        passes
            .iter()
            .map(|p| {
                let mut o = Obj::new();
                o.str("arc", Some(&p.arc));
                if let Some(from) = &p.from {
                    o.raw("from", endpoint(from));
                }
                if let Some(to) = &p.to {
                    o.raw("to", endpoint(to));
                }
                if let Some(turn) = &p.turn {
                    o.raw("turn", lon_lat(turn));
                }
                if let Some(pin) = &p.pin {
                    o.raw("pin", lon_lat(pin));
                }
                o.done()
            })
            .collect(),
    )
}

fn endpoint(e: &Endpoint) -> Json {
    match e {
        Endpoint::Node(key) => Json::String(key.clone()),
        Endpoint::At(p) => lon_lat(p),
    }
}

fn route(r: &Route, path: &Path) -> Built {
    let mut o = Obj::new();
    o.str("name", r.name.as_deref());
    o.str("notes", r.notes.as_deref());
    o.str("folder", r.folder.as_deref());
    o.hidden(r.hidden);
    o.opaque("style", r.style.as_ref());
    graph(&mut o, &r.nodes, &r.arcs, path)?;
    check_walk(&r.walk, &r.arcs, true, &path.key("walk"))?;
    o.raw("walk", walk(&r.walk));
    o.records(&r.data);
    Ok(o.done())
}

fn track(t: &Track, _path: &Path) -> Built {
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
    Ok(o.done())
}

fn waypoint(w: &Waypoint, _path: &Path) -> Built {
    let mut o = Obj::new();
    o.str("name", w.name.as_deref());
    o.str("notes", w.notes.as_deref());
    o.str("folder", w.folder.as_deref());
    o.hidden(w.hidden);
    o.opaque("style", w.style.as_ref());
    o.raw("at", position(&w.at));
    o.records(&w.data);
    Ok(o.done())
}

fn area(a: &Area, path: &Path) -> Built {
    check_ring(&a.polygon, &path.key("polygon"))?;
    let mut o = Obj::new();
    o.str("name", a.name.as_deref());
    o.str("notes", a.notes.as_deref());
    o.str("folder", a.folder.as_deref());
    o.hidden(a.hidden);
    o.opaque("style", a.style.as_ref());
    o.raw("polygon", pips(&a.polygon));
    o.records(&a.data);
    Ok(o.done())
}

/// SPEC 3.7: a writer emits a ring open, with at least three distinct
/// positions. It refuses a ring its own reader would repair or drop, and does
/// not open a closed ring itself: that would guess that the caller meant the
/// repeat, which is the caller's to fix.
fn check_ring(ring: &[Position], path: &Path) -> Result<(), EncodeError> {
    let refuse = |detail: String| {
        Err(EncodeError::Ring {
            path: path.as_str().to_string(),
            detail,
        })
    };
    if ring.len() >= 2 && ring[0].same_point(&ring[ring.len() - 1]) {
        return refuse(
            "a ring is written open, and this one's last position repeats its first".into(),
        );
    }
    let mut distinct: Vec<&Position> = Vec::with_capacity(3);
    for p in ring {
        if distinct.len() == 3 {
            break;
        }
        if !distinct.iter().any(|q| q.same_point(p)) {
            distinct.push(p);
        }
    }
    if distinct.len() < 3 {
        return refuse(format!(
            "a ring needs at least 3 distinct positions, and this one has {}",
            distinct.len()
        ));
    }
    Ok(())
}

fn pips(list: &[Position]) -> Json {
    Json::Array(list.iter().map(position).collect())
}

/// A node or an arc pip: `[lon, lat]`, or `[lon, lat, ele]` when the elevation
/// is known. It has no time to write.
fn vertex(v: &Vertex) -> Json {
    let mut channels = vec![Json::Number(v.lon), Json::Number(v.lat)];
    if let Some(ele) = v.ele {
        channels.push(Json::Number(ele));
    }
    Json::Array(channels)
}

/// A walk position: `[lon, lat]` and nothing else (SPEC 3.3.3).
fn lon_lat(p: &LonLat) -> Json {
    Json::Array(vec![Json::Number(p.lon), Json::Number(p.lat)])
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

    /// A style object, or anything else this format carries
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

    /// A collection of items under `key`, each built with its own JSON
    /// Pointer so that a refusal can say where it is.
    fn list<T>(
        &mut self,
        key: &str,
        items: &Option<Vec<T>>,
        path: &Path,
        f: impl Fn(&T, &Path) -> Built,
    ) -> Result<(), EncodeError> {
        if let Some(list) = items {
            let at = path.key(key);
            let mut out = Vec::with_capacity(list.len());
            for (i, item) in list.iter().enumerate() {
                out.push(f(item, &at.index(i))?);
            }
            self.raw(key, Json::Array(out));
        }
        Ok(())
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
        encode(&reading.document).expect("encodable")
    }

    #[test]
    fn a_writer_emits_the_shortest_spelling_of_a_position() {
        let out = encode_text(r#"{"vnx":"0.9.0","waypoints":[{"at":[-3.6712,57.118,null]}]}"#);
        assert!(out.contains("[-3.6712,57.118]"), "{out}");
    }

    #[test]
    fn an_interior_null_is_never_dropped() {
        let out = encode_text(
            r#"{"vnx":"0.9.0","tracks":[{"segments":[{"pips":[[-3.6,57.1,null,1778914991000]]}]}]}"#,
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
            r#"{"vnx":"0.9.0","waypoints":[{"at":[-3.6712345678901235,57.11812345678901,340.06]}]}"#,
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
            r#"{"vnx":"0.9.0","waypoints":[{"at":[-0.00000004,0.15,340.25]},{"at":[1.85,-0.04,340.75]}]}"#,
        );
        assert!(out.contains("[-4e-8,0.15,340.25]"), "{out}");
        assert!(out.contains("[1.85,-0.04,340.75]"), "{out}");
    }

    /// SPEC 4.1: a reader MUST NOT round, so the full precision the document
    /// carried survives into the model. Neither path rounds now, and this is
    /// the half of it that is still a REQUIREMENT rather than a choice.
    #[test]
    fn a_reader_keeps_the_precision_the_document_carried() {
        let text = r#"{"vnx":"0.9.0","waypoints":[{"at":[-3.6712345678901235,57.11812345678901,340.06]}]}"#;
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
        let out = encode_text(r#"{"vnx":"0.9.0","waypoints":[{"at":[1,2],"hidden":false}]}"#);
        assert!(!out.contains("hidden"), "{out}");
        let shown = encode_text(r#"{"vnx":"0.9.0","waypoints":[{"at":[1,2],"hidden":true}]}"#);
        assert!(shown.contains(r#""hidden":true"#), "{shown}");
    }

    #[test]
    fn the_declared_version_is_written_back_not_the_readers() {
        assert!(encode_text(r#"{"vnx":"0.9.99"}"#).contains("0.9.99"));
        assert!(encode_text(r#"{"vnx":"00.09.0000"}"#).contains("00.09.0000"));
    }

    /// The output is compact. Nothing constrains a document's whitespace
    /// (SPEC 8.1), and this pins the decision rather than the rule.
    #[test]
    fn the_output_carries_no_whitespace() {
        let out = encode_text(r#"{"vnx":"0.9.0","waypoints":[{"at":[1,2],"name":"Spring"}]}"#);
        assert!(!out.contains('\n') && !out.contains("  "), "{out}");
    }

    fn network_document() -> Document {
        decode(
            br#"{"vnx":"0.9.0","networks":[{
                 "nodes":{"a":[-3.60,57.10,410],"b":[-3.59,57.10]},
                 "arcs":{"ab":{"from":"a","to":"b","pips":[[-3.595,57.101]]}},
                 "highlights":[{"walk":[{"arc":"ab","from":[-3.597,57.1005],"to":"b"}]}]}],
                 "routes":[{"nodes":{"a":[-3.60,57.10],"b":[-3.59,57.10]},
                 "arcs":{"ab":{"from":"a","to":"b","pips":[]}},
                 "walk":[{"arc":"ab","from":"b","to":"a"}]}],
                 "areas":[{"polygon":[[-3.7,57.09],[-3.6,57.09],[-3.6,57.0]]}]}"#,
        )
        .expect("accepted")
        .document
    }

    /// A writer never emits what its own reader would drop or repair, and its
    /// refusal names where the fault is.
    #[test]
    fn a_writer_refuses_what_its_reader_would_drop() {
        assert!(
            encode(&network_document()).is_ok(),
            "the sound document is written"
        );

        let refused = |spoil: &dyn Fn(&mut Document)| {
            let mut document = network_document();
            spoil(&mut document);
            encode(&document).expect_err("refused")
        };
        fn network(d: &mut Document) -> &mut Network {
            &mut d.networks.as_mut().expect("networks")[0]
        }
        fn route(d: &mut Document) -> &mut Route {
            &mut d.routes.as_mut().expect("routes")[0]
        }

        let loop_arc =
            refused(&|d: &mut Document| network(d).arcs.as_mut().unwrap()[0].1.to = "a".into());
        assert!(
            matches!(&loop_arc, EncodeError::Arc { path, .. } if path == "/networks/0/arcs/ab"),
            "{loop_arc:?}"
        );

        let absent =
            refused(&|d: &mut Document| route(d).arcs.as_mut().unwrap()[0].1.from = "x".into());
        assert!(
            matches!(&absent, EncodeError::Arc { path, .. } if path == "/routes/0/arcs/ab"),
            "{absent:?}"
        );

        let spelling = refused(&|d: &mut Document| {
            network(d).highlights.as_mut().unwrap()[0].walk[0].to = None
        });
        assert!(
            matches!(&spelling, EncodeError::Walk { path, cause } if path == "/networks/0/highlights/0/walk/0" && cause.code() == "walk-spelling"),
            "{spelling:?}"
        );

        let ghost = refused(&|d: &mut Document| {
            network(d).highlights.as_mut().unwrap()[0].walk[0].arc = "ghost".into()
        });
        assert!(
            matches!(&ghost, EncodeError::Walk { cause, .. } if cause.code() == "pass-arc-absent"),
            "{ghost:?}"
        );

        let pinned_route = refused(&|d: &mut Document| {
            route(d).walk[0].pin = Some(LonLat {
                lon: -3.6,
                lat: 57.1,
            })
        });
        assert!(
            matches!(&pinned_route, EncodeError::Walk { path, cause } if path == "/routes/0/walk/0/pin" && cause.code() == "route-pin-or-position"),
            "{pinned_route:?}"
        );

        let closed = refused(&|d: &mut Document| {
            let ring = &mut d.areas.as_mut().unwrap()[0].polygon;
            ring.push(ring[0].clone());
        });
        assert!(
            matches!(&closed, EncodeError::Ring { path, .. } if path == "/areas/0/polygon"),
            "{closed:?}"
        );

        let thin = refused(&|d: &mut Document| {
            let ring = &mut d.areas.as_mut().unwrap()[0].polygon;
            ring[2] = Position::new(vec![Some(-3.6), Some(57.09), Some(12.0)]);
        });
        assert!(matches!(&thin, EncodeError::Ring { .. }), "{thin:?}");
    }

    /// SPEC 3.3.3: a pin is written as a position, and a walk position, a node
    /// and an arc pip carry no channel the format does not give them.
    #[test]
    fn a_walk_is_written_in_its_one_spelling() {
        let out = encode_text(
            r#"{"vnx":"0.9.0","networks":[{
                 "nodes":{"a":[-3.60,57.10,410,5],"b":[-3.59,57.10]},
                 "arcs":{"ab":{"from":"a","to":"b","pips":[[-3.595,57.101,null,9]]}},
                 "highlights":[{"walk":[{"arc":"ab","from":[-3.597,57.1005,3],"to":"b","pin":[-3.596,57.1003,4]}]}]}]}"#,
        );
        assert!(out.contains(r#""pin":[-3.596,57.1003]"#), "{out}");
        assert!(
            out.contains(r#""a":[-3.6,57.1,410]"#),
            "the node's time is gone: {out}"
        );
        assert!(
            out.contains(r#""pips":[[-3.595,57.101]]"#),
            "the pip's time is gone: {out}"
        );
        assert!(
            out.contains(r#""from":[-3.597,57.1005]"#),
            "the walk position is lon and lat: {out}"
        );
    }
}

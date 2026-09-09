//! Turning a model back into a document.
//!
//! SPEC 8.1 names two operations, and the accept tier requires the second:
//!
//! | Operation | What it does |
//! |---|---|
//! | WRITE | applies the emit-boundary rules: round coordinates (4.5.4) and emit the shortest spelling of a position (4.1) |
//! | RE-RENDER | applies neither, so arity and precision survive |
//!
//! Writing what you read is legitimately not what you read, because a writer
//! rounds. Both go through one tree-builder that differs only in [`Mode`].

use crate::canonical;
use crate::json::Json;
use crate::model::*;

/// Which of SPEC 8.1's two operations to perform.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    /// Reproduce the document the reader was handed: no rounding, and a
    /// position keeps the arity it was written with.
    ReRender,
    /// Emit a document from a model, applying the emit-boundary rules.
    Write,
}

/// Re-render a document: canonical bytes, with precision and position arity as
/// the document carried them (SPEC 8.1).
pub fn re_render(document: &Document) -> String {
    canonical::render(&to_json(document, Mode::ReRender))
}

/// Write a document: canonical bytes, with coordinates rounded at the emit
/// boundary and positions shortened (SPEC 4.5.4, 4.1).
pub fn write(document: &Document) -> String {
    canonical::render(&to_json(document, Mode::Write))
}

/// Build the JSON tree for a document. Key ORDER here is irrelevant — the
/// canonical renderer sorts (SPEC 4.5.1) — so keys are listed in the order
/// SPEC 3.10 tabulates them, for reading.
pub fn to_json(document: &Document, mode: Mode) -> Json {
    let mut o = Obj::new();
    if let Some(version) = &document.vnx {
        // SPEC 2.2: write the version the DOCUMENT declared, not the reader's.
        o.str("vnx", Some(&version.text));
    }
    o.str("profile", document.profile.as_deref());
    o.str("generator", document.generator.as_deref());
    o.str("created", document.created.as_deref());
    o.list("folders", &document.folders, |f| folder(f, mode));
    o.list("networks", &document.networks, |n| network(n, mode));
    o.list("routes", &document.routes, |r| route(r, mode));
    o.list("tracks", &document.tracks, |t| track(t, mode));
    o.list("waypoints", &document.waypoints, |w| waypoint(w, mode));
    o.list("areas", &document.areas, |a| area(a, mode));
    if let Some(rulesets) = &document.rulesets {
        o.raw("rulesets", rulesets.clone());
    }
    o.done()
}

// ---------------------------------------------------------------------------

fn folder(f: &Folder, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("id", Some(&f.id));
    o.str("parent", f.parent.as_deref());
    o.str("name", f.name.as_deref());
    o.str("notes", f.notes.as_deref());
    o.bool("hidden", f.hidden);
    o.records(&f.data, mode);
    o.done()
}

fn network(n: &Network, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("name", n.name.as_deref());
    o.str("notes", n.notes.as_deref());
    o.str("folder", n.folder.as_deref());
    o.bool("hidden", n.hidden);
    if let Some(nodes) = &n.nodes {
        o.raw(
            "nodes",
            Json::Object(
                nodes
                    .iter()
                    .map(|(k, p)| (k.clone(), position(p, mode)))
                    .collect(),
            ),
        );
    }
    if let Some(arcs) = &n.arcs {
        o.raw(
            "arcs",
            Json::Object(
                arcs.iter()
                    .map(|(k, a)| (k.clone(), network_arc(a, mode)))
                    .collect(),
            ),
        );
    }
    o.list("chains", &n.chains, |c| chain(c, mode));
    o.list("points", &n.points, |p| waypoint(p, mode));
    o.list("areas", &n.areas, |a| area(a, mode));
    o.records(&n.data, mode);
    o.done()
}

fn network_arc(a: &NetworkArc, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("from", Some(&a.from));
    o.str("to", Some(&a.to));
    o.raw("pips", pips(&a.pips, mode));
    o.records(&a.data, mode);
    o.done()
}

fn chain(c: &Chain, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("name", c.name.as_deref());
    o.str("notes", c.notes.as_deref());
    o.bool("hidden", c.hidden);
    o.opaque("style", c.style.as_ref());
    o.raw(
        "links",
        Json::Array(c.links.iter().map(|l| link(l, mode)).collect()),
    );
    o.records(&c.data, mode);
    o.done()
}

fn link(l: &Link, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("arc", Some(&l.arc));
    o.raw("from", endpoint(&l.from, mode));
    o.raw("to", endpoint(&l.to, mode));
    o.done()
}

fn endpoint(e: &Endpoint, mode: Mode) -> Json {
    match e {
        Endpoint::Node(key) => Json::String(key.clone()),
        Endpoint::Inline(p) => position(p, mode),
    }
}

fn route(r: &Route, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("name", r.name.as_deref());
    o.str("notes", r.notes.as_deref());
    o.str("folder", r.folder.as_deref());
    o.bool("hidden", r.hidden);
    o.opaque("style", r.style.as_ref());
    o.raw(
        "arcs",
        Json::Array(r.arcs.iter().map(|a| route_arc(a, mode)).collect()),
    );
    o.records(&r.data, mode);
    o.done()
}

fn route_arc(a: &RouteArc, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.raw("pips", pips(&a.pips, mode));
    o.records(&a.data, mode);
    o.done()
}

fn track(t: &Track, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("name", t.name.as_deref());
    o.str("notes", t.notes.as_deref());
    o.str("folder", t.folder.as_deref());
    o.bool("hidden", t.hidden);
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
                    o.raw("pips", pips(&s.pips, mode));
                    o.done()
                })
                .collect(),
        ),
    );
    o.records(&t.data, mode);
    o.done()
}

fn waypoint(w: &Waypoint, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("name", w.name.as_deref());
    o.str("notes", w.notes.as_deref());
    o.str("folder", w.folder.as_deref());
    o.bool("hidden", w.hidden);
    o.opaque("style", w.style.as_ref());
    o.raw("at", position(&w.at, mode));
    o.records(&w.data, mode);
    o.done()
}

fn area(a: &Area, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("name", a.name.as_deref());
    o.str("notes", a.notes.as_deref());
    o.str("folder", a.folder.as_deref());
    o.bool("hidden", a.hidden);
    o.opaque("style", a.style.as_ref());
    o.raw("polygon", pips(&a.polygon, mode));
    o.records(&a.data, mode);
    o.done()
}

fn pips(list: &[Position], mode: Mode) -> Json {
    Json::Array(list.iter().map(|p| position(p, mode)).collect())
}

/// SPEC 4.5.4: a writer rounds longitude and latitude to 6 decimal places and
/// elevation to 1, and SPEC 4.1 makes it emit the shortest spelling. Both are
/// emit-boundary rules and neither is part of canonical form, so a re-render
/// applies neither.
///
/// Rounding in the MODEL is forbidden: endpoint comparison and de-duplication
/// compare positions by full value, and rounding ahead of them would change
/// which positions they consider equal — a change to the geometry rather than
/// to its spelling.
fn position(p: &Position, mode: Mode) -> Json {
    let mut channels: Vec<Option<f64>> = p.channels().to_vec();
    if mode == Mode::Write {
        channels[0] = channels[0].map(|v| round_to(v, 6));
        channels[1] = channels[1].map(|v| round_to(v, 6));
        if let Some(slot) = channels.get_mut(2) {
            *slot = slot.map(|v| round_to(v, 1));
        }
        // Time is an integer count of milliseconds and is not rounded.
        // Channels beyond it are unknown to this reader and are left alone.

        // The shortest spelling: drop TRAILING absences only. An interior
        // `null` keeps its place, because dropping it would move the time into
        // the elevation channel.
        while channels.len() > 2 && channels[channels.len() - 1].is_none() {
            channels.pop();
        }
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

/// SPEC 4.5.4: a tie rounds to EVEN. `340.25` to one place is `340.2`, not
/// `340.3`. These are exact doubles and a quarter-metre elevation grid
/// produces them, so the case is ordinary rather than contrived.
///
/// Rust's `f64::round` rounds a tie AWAY from zero, so it cannot be used here.
/// `f64::round_ties_even` is what this wants and is stable since 1.77.
///
/// No corpus tier reaches this: every tier re-renders, and nothing ever asks a
/// library to WRITE. The tests below are this crate's own, as
/// `corpus/format/README.md` asks each implementation to carry.
fn round_to(value: f64, places: u32) -> f64 {
    let scale = 10f64.powi(places as i32);
    (value * scale).round_ties_even() / scale
}

/// A small builder that drops absent keys. Nothing here decides key order —
/// the canonical renderer does that (SPEC 4.5.1).
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

    /// SPEC 3.8: `hidden` is emitted only when it is `true`, and a document
    /// SHOULD NOT carry `"hidden": false`. A model that read `false` keeps it,
    /// so a re-render reproduces the document it was handed.
    fn bool(&mut self, key: &str, value: Option<bool>) {
        if let Some(v) = value {
            self.raw(key, Json::Bool(v));
        }
    }

    /// A style object, `rulesets`, or anything else this format carries
    /// through untouched.
    fn opaque(&mut self, key: &str, value: Option<&Json>) {
        if let Some(v) = value {
            self.raw(key, v.clone());
        }
    }

    fn records(&mut self, records: &Option<Vec<Record>>, _mode: Mode) {
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

    fn round_trip(text: &str) -> String {
        let reading = decode(text.as_bytes()).expect("accepted");
        re_render(&reading.document)
    }

    #[test]
    fn a_re_render_keeps_arity_and_a_write_shortens_it() {
        let text = "{\n  \"vnx\": \"0.1.0\",\n  \"waypoints\": [\n    {\n      \"at\": [-3.6712, 57.118, null]\n    }\n  ]\n}\n";
        assert_eq!(round_trip(text), text, "the longer spelling survives");

        let document = decode(text.as_bytes()).expect("accepted").document;
        assert!(
            write(&document).contains("[-3.6712, 57.118]"),
            "a writer emits the shortest spelling: {}",
            write(&document)
        );
    }

    #[test]
    fn an_interior_null_is_never_dropped_by_a_write() {
        let text = r#"{"vnx":"0.1.0","tracks":[{"segments":[{"pips":[[-3.6,57.1,null,1778914991000]]}]}]}"#;
        let document = decode(text.as_bytes()).expect("accepted").document;
        assert!(
            write(&document).contains("[-3.6, 57.1, null, 1778914991000]"),
            "{}",
            write(&document)
        );
    }

    #[test]
    fn a_write_rounds_and_a_re_render_does_not() {
        let text = r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6712345678901235,57.11812345678901,340.06]}]}"#;
        let document = decode(text.as_bytes()).expect("accepted").document;
        assert!(
            re_render(&document).contains("[-3.6712345678901235, 57.11812345678901, 340.06]"),
            "{}",
            re_render(&document)
        );
        assert!(
            write(&document).contains("[-3.671235, 57.118123, 340.1]"),
            "{}",
            write(&document)
        );
    }

    /// SPEC 4.5.4: a rounding tie goes to even, at the WRITER's boundary.
    /// `corpus/format/README.md` names this as one of three rules no corpus
    /// tier can reach, because no tier ever asks a library to write.
    #[test]
    fn a_rounding_tie_goes_to_even() {
        assert_eq!(round_to(340.25, 1), 340.2, "not 340.3");
        assert_eq!(round_to(340.35, 1), 340.4);
        assert_eq!(round_to(-340.25, 1), -340.2);
        assert_eq!(round_to(340.75, 1), 340.8);
        // Rust's own `round` is the wrong one, and this says so out loud.
        assert_eq!((340.25f64 * 10.0).round() / 10.0, 340.3);
    }

    /// SPEC 4.5.4: rounding can CREATE a negative zero that was never in the
    /// model, and the renderer is what collapses it. A writer that collapsed
    /// negative zero only across the values it was handed would emit `-0`.
    #[test]
    fn a_negative_zero_that_rounding_created_is_still_written_as_zero() {
        assert_eq!(round_to(-0.00000004, 6).to_bits(), (-0.0f64).to_bits());
        let text = r#"{"vnx":"0.1.0","waypoints":[{"at":[-0.00000004,57.1,-0.04]}]}"#;
        let document = decode(text.as_bytes()).expect("accepted").document;
        assert!(
            write(&document).contains("[0, 57.1, 0]"),
            "{}",
            write(&document)
        );
    }

    #[test]
    fn an_absent_collection_and_an_empty_one_render_differently() {
        assert_eq!(
            round_trip(r#"{"vnx":"0.1.0","waypoints":[]}"#),
            "{\n  \"vnx\": \"0.1.0\",\n  \"waypoints\": []\n}\n"
        );
        assert_eq!(
            round_trip(r#"{"vnx":"0.1.0"}"#),
            "{\n  \"vnx\": \"0.1.0\"\n}\n"
        );
    }

    #[test]
    fn the_declared_version_is_written_back_not_the_readers() {
        assert!(round_trip(r#"{"vnx":"0.1.99"}"#).contains("0.1.99"));
        assert!(round_trip(r#"{"vnx":"00.01.0000"}"#).contains("00.01.0000"));
    }
}

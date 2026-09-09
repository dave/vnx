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
    o.hidden(f.hidden, mode);
    o.records(&f.data, mode);
    o.done()
}

fn network(n: &Network, mode: Mode) -> Json {
    let mut o = Obj::new();
    o.str("name", n.name.as_deref());
    o.str("notes", n.notes.as_deref());
    o.str("folder", n.folder.as_deref());
    o.hidden(n.hidden, mode);
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
    o.hidden(c.hidden, mode);
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
    o.hidden(r.hidden, mode);
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
    o.hidden(t.hidden, mode);
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
    o.hidden(w.hidden, mode);
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
    o.hidden(a.hidden, mode);
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

/// Round to `places` decimal places, ties to EVEN (SPEC 4.5.4).
///
/// The obvious spelling — multiply by 10^places, round, divide — is WRONG, and
/// it is wrong in a way no corpus tier can reach, because no tier ever asks a
/// library to write.
///
/// Multiplying by the scale is itself a rounding step, and it moves values onto
/// an exact half that were not on one. Measured on this machine:
///
/// ```text
///   value   nearest 1 dp of the ACTUAL double   (v * 10).round_ties_even() / 10
///   0.15    0.1   (the double is 0.1499999999999999944…)   0.2
///   0.05    0.1                                            0
///   1.05    1.1                                            1
///   0.45    0.5                                            0.4
///   1.85    1.9                                            1.8
///   340.25  340.2  (exact, a true tie)                      340.2
/// ```
///
/// Six of ten ordinary elevation-shaped values disagree, in both directions. So
/// "round to one place, ties to even" is not one rule: it is two, depending on
/// whether the tie is judged against the double or against the scaled product,
/// and SPEC 4.5.4 does not say which. Reported.
///
/// This crate takes the reading the prose states: the nearest value with that
/// many decimal places to the double it was handed. Rust's `{:.*}` expands the
/// double exactly and rounds ties to even, which is that rule, so the rounding
/// is done by formatting and then parsing back.
///
/// The formatted text is NEVER the emitted bytes — `{:.1}` of 340.0 is `340.0`
/// and of −0.04 is `-0.0`, both wrong for this format. Only the canonical
/// renderer decides bytes; this returns a value.
/// The rounding SPEC 4.5.4 applies at the writer boundary, exposed so the
/// number-conformance table (`tests/numbers.rs`) can compare it directly. It is
/// otherwise internal: a caller rounds by calling [`write`].
pub fn round_for_test(value: f64, places: usize) -> f64 {
    round_to(value, places)
}

fn round_to(value: f64, places: usize) -> f64 {
    format!("{value:.places$}")
        .parse()
        .expect("a fixed-precision formatting of a finite double parses back")
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

    /// SPEC 3.8: a WRITER emits `hidden` only when it is `true`, and a
    /// document SHOULD NOT carry `"hidden": false`.
    ///
    /// A RE-RENDER keeps whatever the document carried, `false` included, or it
    /// would not reproduce the document it was handed. So this is one of the
    /// two places the two emit paths differ — the other is a position.
    fn hidden(&mut self, value: Option<bool>, mode: Mode) {
        match (value, mode) {
            (Some(false), Mode::Write) => {}
            (Some(v), _) => self.raw("hidden", Json::Bool(v)),
            (None, _) => {}
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
    ///
    /// BOTH tie values are load-bearing and neither is insurance. Each catches
    /// a different pair of wrong rules, and only the two together identify
    /// half-to-even. Measured here rather than asserted, so that deleting
    /// either line as redundant fails rather than silently narrows the test:
    ///
    /// ```text
    ///            to-even  half-odd  away-0   toward-0  half-up  half-down
    ///   340.25    340.2    340.3    340.3     340.2     340.3    340.2
    ///   340.75    340.8    340.7    340.8     340.7     340.8    340.7
    /// ```
    ///
    /// `340.25` separates away-from-zero and half-up; `340.75` separates
    /// toward-zero and half-down; each separates half-odd on its own. A
    /// fixture carrying only one of them looks like tie coverage and leaves
    /// two rules undetected.
    #[test]
    fn a_rounding_tie_goes_to_even() {
        assert_eq!(round_to(340.25, 1), 340.2, "not 340.3");
        assert_eq!(round_to(340.75, 1), 340.8, "not 340.7");
        assert_eq!(round_to(-340.25, 1), -340.2);
        assert_eq!(round_to(-340.75, 1), -340.8);
        // Not a tie at all, so every rule agrees; here to show the tie cases
        // above are not just "rounds down".
        assert_eq!(round_to(340.35, 1), 340.4);
    }

    /// Each tie value must actually SEPARATE the rules it is credited with, or
    /// the table above is decoration. This computes the five wrong rules from
    /// their definitions and requires each value to catch its own pair.
    ///
    /// The lead's first framing had `340.25` as the instrument and `340.75` as
    /// insurance against a reader rounding everything up. That framing is what
    /// gets the "decorative" value deleted by the next person tidying, so the
    /// discrimination is asserted rather than described.
    #[test]
    fn each_tie_value_separates_its_own_pair_of_wrong_rules() {
        // The five wrong rules, at one decimal place, on an exact tie.
        let scaled = |v: f64| v * 10.0;
        let half_odd = |v: f64| {
            let lo = scaled(v).floor();
            (if lo.rem_euclid(2.0) == 1.0 { lo } else { lo + 1.0 }) / 10.0
        };
        let away_from_zero = |v: f64| scaled(v).abs().ceil().copysign(v) / 10.0;
        let toward_zero = |v: f64| scaled(v).abs().floor().copysign(v) / 10.0;
        let half_up = |v: f64| (scaled(v) + 0.5).floor() / 10.0;
        let half_down = |v: f64| (scaled(v) - 0.5).ceil() / 10.0;

        for (value, caught) in [
            (340.25_f64, ["away-from-zero", "half-up"]),
            (340.75_f64, ["toward-zero", "half-down"]),
        ] {
            let correct = round_to(value, 1);
            let named: Vec<(&str, f64)> = vec![
                ("half-odd", half_odd(value)),
                ("away-from-zero", away_from_zero(value)),
                ("toward-zero", toward_zero(value)),
                ("half-up", half_up(value)),
                ("half-down", half_down(value)),
            ];
            let separated: Vec<&str> = named
                .iter()
                .filter(|(_, answer)| *answer != correct)
                .map(|(name, _)| *name)
                .collect();
            // Every tie separates half-odd; the pair is what each value adds.
            assert!(
                separated.contains(&"half-odd"),
                "{value} does not separate half-odd"
            );
            for rule in caught {
                assert!(
                    separated.contains(&rule),
                    "{value} was credited with catching {rule} and does not: \
                     correct is {correct}, and the rules give {named:?}"
                );
            }
        }

        // And neither value catches the other's pair, which is why both are
        // needed. This is the assertion that fails if one line is deleted.
        assert_eq!(away_from_zero(340.75), round_to(340.75, 1));
        assert_eq!(half_up(340.75), round_to(340.75, 1));
        assert_eq!(toward_zero(340.25), round_to(340.25, 1));
        assert_eq!(half_down(340.25), round_to(340.25, 1));
    }

    /// "The naive route" is TWO functions and they are not the same one.
    ///
    /// | | positive ties | negative ties |
    /// |---|---|---|
    /// | `Math.round(x*10)/10` — half-UP, toward +infinity (JS, Java) | wrong on 340.25 | wrong on -340.75 |
    /// | `f64::round(x*10)/10` — half-AWAY-from-zero (Rust, C) | wrong on 340.25 | wrong on -340.25 |
    ///
    /// They agree on every positive tie and disagree on every negative one, so
    /// a suite that catches one and not the other has a gap that looks closed:
    /// "the naive route is caught" reads as done. Both are in
    /// `tools/redproof.py` as separate mutations.
    ///
    /// Python's `round(x*10)/10` is a third answer again — half-even on the
    /// SCALED float, which is the scale-then-round operation rather than a tie
    /// direction, and it is caught by different values entirely.
    ///
    /// Naming the function rather than saying "the naive route" is the same
    /// rule as citing the radius behind a distance: a number that does not say
    /// which artefact AND which constant it was measured under outlives both.
    #[test]
    fn the_two_idioms_are_each_wrong_on_only_some_ties() {
        let half_up = |v: f64| ((v * 10.0) + 0.5).floor() / 10.0;
        let rust_round = |v: f64| (v * 10.0).round() / 10.0;

        // half-up: wrong on 340.25 and on -340.75, right on the other two.
        assert_ne!(half_up(340.25), round_to(340.25, 1));
        assert_eq!(half_up(340.75), round_to(340.75, 1));
        assert_eq!(half_up(-340.25), round_to(-340.25, 1));
        assert_ne!(half_up(-340.75), round_to(-340.75, 1));

        // Rust's `round`: wrong on both positives' odd-ward ties and on the
        // negative mirror of each.
        assert_ne!(rust_round(340.25), round_to(340.25, 1));
        assert_eq!(rust_round(340.75), round_to(340.75, 1));
        assert_ne!(rust_round(-340.25), round_to(-340.25, 1));
        assert_eq!(rust_round(-340.75), round_to(-340.75, 1));
    }

    /// Decimal intuition lies about which way a value rounds. `3.05` is really
    /// 3.04999999999999982…, so it rounds DOWN, and a test written from the
    /// decimal spelling asserts 3.1 and reports a correct writer as broken.
    #[test]
    fn a_value_that_only_looks_like_a_tie_rounds_by_its_real_value() {
        assert_eq!(round_to(3.05, 1), 3.0);
        assert_eq!(round_to(0.15, 1), 0.1);
        assert_eq!(round_to(1.05, 1), 1.1);
    }

    /// The scale-then-round spelling of the same rule, measured against this
    /// one. Six of these ten disagree, in both directions, so SPEC 4.5.4's
    /// "round to one place, ties to even" is two different rules depending on
    /// whether the tie is judged against the double or against the scaled
    /// product. This test is the measurement, and it goes red if either answer
    /// moves.
    #[test]
    fn scale_then_round_is_a_different_rule_and_this_pins_the_difference() {
        let scale_then_round = |v: f64| (v * 10.0).round_ties_even() / 10.0;
        let cases: [(f64, f64, f64); 10] = [
            //  value,     this crate, scale-then-round
            (340.25, 340.2, 340.2),
            (340.35, 340.4, 340.4),
            (3.05, 3.0, 3.0),
            (-340.25, -340.2, -340.2),
            (0.15, 0.1, 0.2),
            (0.05, 0.1, 0.0),
            (1.05, 1.1, 1.0),
            (0.45, 0.5, 0.4),
            (1.85, 1.9, 1.8),
            (0.95, 0.9, 1.0),
        ];
        let mut disagreements = 0;
        for (value, ours, theirs) in cases {
            assert_eq!(round_to(value, 1), ours, "{value} through this crate");
            assert_eq!(scale_then_round(value), theirs, "{value} scaled first");
            if ours != theirs {
                disagreements += 1;
            }
        }
        assert_eq!(
            disagreements, 6,
            "the two spellings of SPEC 4.5.4 no longer differ where they did"
        );
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

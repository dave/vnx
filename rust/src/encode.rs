//! Turning a model back into a document (SPEC 4.1, 8.1).
//!
//! There is one operation. `encode` applies the emit-boundary rules — round
//! longitude and latitude to six decimal places and elevation to one, and emit
//! the shortest spelling of a position — and hands the tree to `serde_json`.
//!
//! ⚠ WRITING WHAT YOU READ IS LEGITIMATELY NOT WHAT YOU READ, because a writer
//! rounds. The accept tier absorbs that by applying the same two rules to both
//! sides of its comparison (SPEC 8.1); the write tier does not, because there
//! they are the rules under test.

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
    o.str("profile", document.profile.as_deref());
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

/// SPEC 4.1: a writer rounds longitude and latitude to 6 decimal places and
/// elevation to 1, and emits the shortest spelling of a position. Both are
/// emit-boundary rules, and this is the only place either is applied.
///
/// Rounding in the MODEL is forbidden: endpoint comparison and de-duplication
/// compare positions by full value, and rounding ahead of them would change
/// which positions they consider equal — a change to the geometry rather than
/// to its spelling.
fn position(p: &Position) -> Json {
    let mut channels: Vec<Option<f64>> = p.channels().to_vec();
    channels[0] = channels[0].map(|v| round_to(v, 6));
    channels[1] = channels[1].map(|v| round_to(v, 6));
    if let Some(slot) = channels.get_mut(2) {
        *slot = slot.map(|v| round_to(v, 1));
    }
    // Time is an integer count of milliseconds and is not rounded. Channels
    // beyond it are unknown to this reader and are left alone.

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

/// Round to `places` decimal places, ties to EVEN (SPEC 4.1).
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
/// and SPEC 4.1 does not say which. Reported.
///
/// This crate takes the reading the prose states: the nearest value with that
/// many decimal places to the double it was handed. Rust's `{:.*}` expands the
/// double exactly and rounds ties to even, which is that rule, so the rounding
/// is done by formatting and then parsing back.
///
/// The formatted text is NEVER the emitted bytes. This returns a VALUE, and
/// how that value is spelled is `serde_json`'s business (SPEC 4.2).
///
/// Exposed so the number-conformance table (`tests/numbers.rs`) can compare it
/// directly. It is otherwise internal: a caller rounds by calling [`encode`].
pub fn round_for_test(value: f64, places: usize) -> f64 {
    round_to(value, places)
}

fn round_to(value: f64, places: usize) -> f64 {
    let rounded: f64 = format!("{value:.places$}")
        .parse()
        .expect("a fixed-precision formatting of a finite double parses back");
    // ⚠ ROUNDING CREATES NEGATIVE ZEROS THAT WERE NEVER IN THE MODEL. -4e-08
    // to six places and -0.04 to one both come back with the sign bit set.
    // Negative zero is the same VALUE as zero and carries no information, and
    // `corpus/numbers/` gives `0` on all 993 of its rows that reach it, so the
    // sign is normalised here rather than left for whatever spells the bytes.
    // `rounded == 0.0` is true for both zeros, so this catches it.
    if rounded == 0.0 {
        return 0.0;
    }
    rounded
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

    #[test]
    fn a_writer_rounds_at_the_emit_boundary() {
        let out = encode_text(
            r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6712345678901235,57.11812345678901,340.06]}]}"#,
        );
        assert!(out.contains("[-3.671235,57.118123,340.1]"), "{out}");
    }

    /// SPEC 4.1: a reader MUST NOT round, so the full precision the document
    /// carried survives into the model. Only the emit path rounds.
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

    /// SPEC 4.1: a rounding tie goes to even, at the WRITER's boundary.
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
    /// one. Six of these ten disagree, in both directions, so SPEC 4.1's
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
            "the two spellings of SPEC 4.1 no longer differ where they did"
        );
    }

    /// SPEC 4.1: rounding can CREATE a negative zero that was never in the
    /// model, and `round_to` is what collapses it. A writer that collapsed
    /// negative zero only across the values it was HANDED would emit `-0` for
    /// one it produced itself.
    #[test]
    fn a_negative_zero_that_rounding_created_is_written_as_zero() {
        assert_eq!(round_to(-0.00000004, 6).to_bits(), 0.0f64.to_bits());
        assert_eq!(round_to(-0.04, 1).to_bits(), 0.0f64.to_bits());
        let out = encode_text(r#"{"vnx":"0.1.0","waypoints":[{"at":[-0.00000004,57.1,-0.04]}]}"#);
        assert!(out.contains("[0,57.1,0]"), "{out}");
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

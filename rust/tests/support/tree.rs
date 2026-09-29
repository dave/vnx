//! JSON-tree comparison, which is what the corpus compares now that the bytes
//! of a document are not constrained (SPEC 8.1).
//!
//! ⚠ IT IS NEVER MODEL-VERSUS-MODEL, and that is the finding the accept tier
//! rests on. A reader that discarded every data record, every application key, every
//! style and every `notes` produces a model that compares equal to itself, and
//! passes an accept tier built that way GREEN — that is the whole extensibility
//! mechanism, all styling and all user prose. Comparing the tree parsed from
//! the ORIGINAL FILE against the tree parsed from `encode(decode(file))` is what
//! proves a reader kept what it read. `a_reader_that_discards_what_it_read_is_caught`
//! in `corpus.rs` freezes the proof.

use vnx::json::{parse, Json};

/// Whether the comparison applies SPEC 4.1's writer rule.
///
/// ⚠ THE TWO TIERS NORMALISE DIFFERENTLY, AND MUST. The accept tier compares a
/// document against a re-encoding of itself, so the shortest spelling of a
/// position is applied to both sides — otherwise it would require the file to
/// have been written by this library. The write tier compares one writer's
/// output against another's, where shortening is the very rule under test:
/// normalising it there would make the tier prove nothing.
///
/// ⚠ NEITHER SETTING ROUNDS, AND THAT IS NOW A RULE RATHER THAN AN OMISSION.
/// SPEC 8.1 permits exactly two normalisations — a trailing `null` channel
/// dropped, and an absent container equal to an empty one. A comparison that
/// also rounded would accept a writer that lost precision the document
/// carried, which SPEC 4.1 no longer licenses anyone to lose on another
/// implementation's behalf.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EmitBoundary {
    /// Drop a trailing `null` channel, on both sides.
    Normalise,
    /// Leave both alone. The write tier's rule is this one.
    Keep,
}

/// Compare two documents' trees. Returns the first difference in JSON Pointer
/// form, or `None` when they are equal.
///
/// A PATH rather than a boolean, because a failing corpus case has to say
/// which rule broke to be actionable.
pub fn compare(got: &[u8], want: &[u8], emit: EmitBoundary) -> Option<String> {
    let g = parse(got).unwrap_or_else(|e| panic!("the encoded document is not JSON: {e}"));
    let w = parse(want).unwrap_or_else(|e| panic!("the expected document is not JSON: {e}"));
    diff(&document(&g, emit), &document(&w, emit), "")
}

// ---------------------------------------------------------------------------
// The two permitted normalisations (SPEC 8.1), and nothing else.
// ---------------------------------------------------------------------------

/// Walk a document and apply the permitted normalisations.
///
/// It is SCHEMA-AWARE rather than a heuristic over shapes, because the two
/// regions the format declares opaque — a data record's body and a style — may
/// hold arrays of numbers that are not positions and must not be rounded, and
/// may hold empty containers that a third party means to keep.
fn document(value: &Json, emit: EmitBoundary) -> Json {
    let Json::Object(entries) = value else {
        return value.clone();
    };
    let mut out = entries.clone();
    items(&mut out, "folders", emit, folder);
    items(&mut out, "networks", emit, network);
    items(&mut out, "routes", emit, route);
    items(&mut out, "tracks", emit, track);
    items(&mut out, "waypoints", emit, waypoint);
    items(&mut out, "areas", emit, area);
    // Application keys are not touched: SPEC 3.9 exempts them from SPEC 8.1's
    // absent-equals-empty normalisation, so an empty or `null` value is
    // compared as written and a key dropped for holding one is a difference.
    Json::Object(out)
}

type Entries = Vec<(String, Json)>;

fn folder(o: &mut Entries, _emit: EmitBoundary) {
    drop_empty(o, "data");
}

fn waypoint(o: &mut Entries, emit: EmitBoundary) {
    map_value(o, "at", |v| position(v, emit));
    drop_empty(o, "style");
    drop_empty(o, "data");
}

fn area(o: &mut Entries, emit: EmitBoundary) {
    position_list(o, "polygon", emit);
    drop_empty(o, "style");
    drop_empty(o, "data");
}

fn track(o: &mut Entries, emit: EmitBoundary) {
    items(o, "segments", emit, |s, emit| {
        position_list(s, "pips", emit)
    });
    drop_empty(o, "style");
    drop_empty(o, "data");
}

fn route(o: &mut Entries, emit: EmitBoundary) {
    graph(o, emit);
    walk(o, emit);
    drop_empty(o, "style");
    drop_empty(o, "data");
}

fn arc(o: &mut Entries, emit: EmitBoundary) {
    position_list(o, "pips", emit);
    drop_empty(o, "data");
}

fn network(o: &mut Entries, emit: EmitBoundary) {
    graph(o, emit);
    items(o, "highlights", emit, highlight);
    items(o, "points", emit, waypoint);
    items(o, "areas", emit, area);
    drop_empty(o, "data");
}

/// A network's or a route's `nodes` and `arcs`, which are objects keyed by
/// identifier (SPEC 3.3).
fn graph(o: &mut Entries, emit: EmitBoundary) {
    map_value(o, "nodes", |v| match v {
        Json::Object(nodes) => Json::Object(
            nodes
                .iter()
                .map(|(k, p)| (k.clone(), position(p, emit)))
                .collect(),
        ),
        other => other.clone(),
    });
    map_value(o, "arcs", |v| match v {
        Json::Object(arcs) => Json::Object(
            arcs.iter()
                .map(|(k, a)| {
                    let Json::Object(members) = a else {
                        return (k.clone(), a.clone());
                    };
                    let mut members = members.clone();
                    arc(&mut members, emit);
                    (k.clone(), Json::Object(members))
                })
                .collect(),
        ),
        other => other.clone(),
    });
    drop_empty(o, "nodes");
    drop_empty(o, "arcs");
}

fn highlight(o: &mut Entries, emit: EmitBoundary) {
    walk(o, emit);
    drop_empty(o, "style");
    drop_empty(o, "data");
}

/// A walk's passes. `from` and `to` are a node key or a position (SPEC 3.3.3);
/// only the second is a position, and `position` leaves a string alone.
fn walk(o: &mut Entries, emit: EmitBoundary) {
    items(o, "walk", emit, |pass, emit| {
        for key in ["from", "to", "turn"] {
            map_value(pass, key, |v| position(v, emit));
        }
    });
}

/// Normalise an array of objects under `key`, and drop the key when the array
/// is empty.
fn items(o: &mut Entries, key: &str, emit: EmitBoundary, each: fn(&mut Entries, EmitBoundary)) {
    let Some(slot) = o.iter().position(|(k, _)| k == key) else {
        return;
    };
    let Json::Array(list) = &o[slot].1 else {
        return;
    };
    if list.is_empty() {
        o.remove(slot);
        return;
    }
    let normalised: Vec<Json> = list
        .iter()
        .map(|item| {
            let Json::Object(members) = item else {
                return item.clone();
            };
            let mut members = members.clone();
            each(&mut members, emit);
            Json::Object(members)
        })
        .collect();
    o[slot].1 = Json::Array(normalised);
}

fn position_list(o: &mut Entries, key: &str, emit: EmitBoundary) {
    let Some(slot) = o.iter().position(|(k, _)| k == key) else {
        return;
    };
    let Json::Array(list) = &o[slot].1 else {
        return;
    };
    if list.is_empty() {
        o.remove(slot);
        return;
    }
    let normalised: Vec<Json> = list.iter().map(|p| position(p, emit)).collect();
    o[slot].1 = Json::Array(normalised);
}

/// SPEC 8.1's first normalisation: on a position, trailing `null` channels are
/// dropped. Nothing else — a value is compared exactly as both sides state it.
///
/// ⚠ A CHANNEL THIS VERSION DOES NOT NAME IS NOT NORMALISED AWAY, and no corpus
/// file carries one. SPEC 4.1 has a reader ignore an extra channel; it does not
/// ask a writer to reproduce one, so a fixture demanding that would be asking
/// for a rule the format does not state.
fn position(value: &Json, emit: EmitBoundary) -> Json {
    let Json::Array(channels) = value else {
        return value.clone();
    };
    if emit == EmitBoundary::Keep {
        return value.clone();
    }
    let mut out: Vec<Json> = channels.clone();
    while out.len() > 2 && out[out.len() - 1] == Json::Null {
        out.pop();
    }
    Json::Array(out)
}

/// SPEC 8.1's second normalisation, which SPEC 3.1 requires a reader to make:
/// an absent collection and an empty one carry the same content. So the
/// comparison treats them alike, and a writer that omits an empty collection
/// stays conforming.
fn drop_empty(o: &mut Entries, key: &str) {
    let Some(slot) = o.iter().position(|(k, _)| k == key) else {
        return;
    };
    let empty = match &o[slot].1 {
        Json::Array(items) => items.is_empty(),
        Json::Object(entries) => entries.is_empty(),
        _ => false,
    };
    if empty {
        o.remove(slot);
    }
}

fn map_value(o: &mut Entries, key: &str, f: impl Fn(&Json) -> Json) {
    if let Some(slot) = o.iter().position(|(k, _)| k == key) {
        o[slot].1 = f(&o[slot].1);
    }
}

// ---------------------------------------------------------------------------
// The comparison
// ---------------------------------------------------------------------------

fn diff(got: &Json, want: &Json, path: &str) -> Option<String> {
    match want {
        Json::Object(w) => {
            let Json::Object(g) = got else {
                return Some(format!(
                    "{}: got {}, want an object",
                    at(path),
                    got.type_name()
                ));
            };
            let mut keys: Vec<&String> = w.iter().map(|(k, _)| k).collect();
            keys.sort();
            for key in keys {
                let wv = w
                    .iter()
                    .find(|(k, _)| k == key)
                    .map(|(_, v)| v)
                    .expect("the key came from this object");
                let Some(gv) = g.iter().find(|(k, _)| k == key).map(|(_, v)| v) else {
                    return Some(format!("{}: key {key:?} is missing", at(path)));
                };
                if let Some(d) = diff(gv, wv, &format!("{path}/{}", pointer(key))) {
                    return Some(d);
                }
            }
            let mut extra: Vec<&str> = g
                .iter()
                .map(|(k, _)| k.as_str())
                .filter(|k| !w.iter().any(|(wk, _)| wk == k))
                .collect();
            extra.sort();
            if !extra.is_empty() {
                return Some(format!("{}: unexpected key(s) {extra:?}", at(path)));
            }
            None
        }
        Json::Array(w) => {
            let Json::Array(g) = got else {
                return Some(format!(
                    "{}: got {}, want an array",
                    at(path),
                    got.type_name()
                ));
            };
            if g.len() != w.len() {
                return Some(format!(
                    "{}: got {} element(s), want {}",
                    at(path),
                    g.len(),
                    w.len()
                ));
            }
            for (index, wv) in w.iter().enumerate() {
                if let Some(d) = diff(&g[index], wv, &format!("{path}/{index}")) {
                    return Some(d);
                }
            }
            None
        }
        // A number compares BY VALUE, never by spelling (SPEC 4.2). -0 and 0
        // are the same value and compare equal.
        Json::Number(w) => match got {
            Json::Number(g) if g == w => None,
            other => Some(format!("{}: got {}, want {w}", at(path), describe(other))),
        },
        _ => {
            if got == want {
                None
            } else {
                Some(format!(
                    "{}: got {}, want {}",
                    at(path),
                    describe(got),
                    describe(want)
                ))
            }
        }
    }
}

fn at(path: &str) -> &str {
    if path.is_empty() {
        "the document"
    } else {
        path
    }
}

fn pointer(key: &str) -> String {
    key.replace('~', "~0").replace('/', "~1")
}

fn describe(value: &Json) -> String {
    match value {
        Json::Null => "null".to_string(),
        Json::Bool(b) => b.to_string(),
        Json::Number(n) => n.to_string(),
        Json::String(s) => format!("{s:?}"),
        Json::Array(_) => "an array".to_string(),
        Json::Object(_) => "an object".to_string(),
    }
}

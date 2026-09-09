//! The conformance corpus runner (SPEC 8).
//!
//! `corpus/` decides whether an implementation conforms, and passing all four
//! tiers is the definition of done. A file with no sidecar is a FAILURE, not a
//! skip: the file everyone assumes is covered is otherwise exactly the one
//! that is not.
//!
//! Three things this runner does deliberately, each because doing the obvious
//! thing instead would make a tier pass vacuously:
//!
//! * The accept tier RE-RENDERS the decoded document and requires the original
//!   bytes back. It does not use the writer — a writer rounds coordinates and
//!   shortens positions, so writing what you read is legitimately not what you
//!   read (SPEC 8.1) — and it does not reformat the bytes, which never builds
//!   a model and so cannot see a reader that discarded what it read.
//! * `positions` is resolved against the DECODED MODEL, never against the raw
//!   JSON. Resolving it against the file compares the file to itself and
//!   cannot see a decoder that swapped longitude and latitude.
//! * Diagnostics are matched on `path` and `code`, as a SET with distinct
//!   pairing: one reported diagnostic may not satisfy two expectations, or a
//!   reader that found half the defects would pass. The sidecar's
//!   `message_advisory` is NOT compared — SPEC 6.3 forbids it, and the field is
//!   named that way so a runner cannot half-couple to English by accident.

use std::collections::BTreeSet;
use std::fmt::Write as _;
use std::path::{Path, PathBuf};

use vnx::json::{parse, Json};
use vnx::model::{Area, Document, Network, Position, Route, Track, Waypoint};
use vnx::{decode, re_render, vnz};

fn corpus_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("the crate sits inside the repository")
        .join("corpus")
}

// ---------------------------------------------------------------------------
// The four tiers
// ---------------------------------------------------------------------------

#[test]
fn accept_tier() {
    run_tier("accept", |case| {
        let sidecar = case.sidecar();
        expect_kind(&sidecar, "accept", case);

        let (document, diagnostics, canonical_source, attachments) = match case.extension() {
            "vnz" => {
                let container = vnz::read(&case.bytes).unwrap_or_else(|e| {
                    panic!("{}: refused, and the corpus says accept: {e}", case.name)
                });
                let entry = container.document_entry.clone();
                let bytes = document_bytes_from_container(&case.bytes, &entry);
                (
                    container.reading.document,
                    container.reading.diagnostics,
                    bytes,
                    Some(
                        container
                            .attachments
                            .iter()
                            .map(|a| a.path.clone())
                            .collect::<Vec<_>>(),
                    ),
                )
            }
            _ => {
                let reading = decode(&case.bytes).unwrap_or_else(|e| {
                    panic!("{}: refused, and the corpus says accept: {e}", case.name)
                });
                (
                    reading.document,
                    reading.diagnostics,
                    case.bytes.clone(),
                    None,
                )
            }
        };

        assert!(
            diagnostics.is_empty(),
            "{}: the accept tier requires zero diagnostics, and this reported {}",
            case.name,
            render_diagnostics(&diagnostics)
        );

        check_counts(&sidecar, &document, case);
        check_positions(&sidecar, &document, case);
        check_tracks(&sidecar, &document, case);
        check_attachments(&sidecar, attachments.as_deref(), case);

        // SPEC 8.1: decode, RE-RENDER, and require the original bytes back.
        let rendered = re_render(&document);
        assert_eq!(
            rendered.as_bytes(),
            canonical_source.as_slice(),
            "{}: a re-render does not reproduce the stored canonical bytes.\n\
             --- expected ---\n{}\n--- got ---\n{rendered}",
            case.name,
            String::from_utf8_lossy(&canonical_source)
        );
    });
}

#[test]
fn reject_tier() {
    run_tier("reject", |case| {
        let sidecar = case.sidecar();
        expect_kind(&sidecar, "reject", case);
        let expected = sidecar
            .get("refusal")
            .and_then(Json::as_str)
            .unwrap_or_else(|| panic!("{}: the sidecar carries no `refusal`", case.name));

        let refusal = match case.extension() {
            "vnz" => vnz::read(&case.bytes).err(),
            _ => decode(&case.bytes).err(),
        };
        let refusal = refusal.unwrap_or_else(|| {
            panic!("{}: read cleanly, and the corpus says refuse", case.name)
        });
        let text = refusal.to_string();
        assert!(
            text.contains(expected),
            "{}: the refusal must name its cause.\n  expected substring: {expected:?}\n  got: {text:?}",
            case.name
        );
    });
}

#[test]
fn diagnose_tier() {
    run_tier("diagnose", |case| {
        let sidecar = case.sidecar();
        expect_kind(&sidecar, "diagnose", case);

        let (document, diagnostics) = match case.extension() {
            "vnz" => {
                let c = vnz::read(&case.bytes)
                    .unwrap_or_else(|e| panic!("{}: refused: {e}", case.name));
                (c.reading.document, c.reading.diagnostics)
            }
            _ => {
                let r = decode(&case.bytes)
                    .unwrap_or_else(|e| panic!("{}: refused: {e}", case.name));
                (r.document, r.diagnostics)
            }
        };

        check_counts(&sidecar, &document, case);
        check_positions(&sidecar, &document, case);
        check_tracks(&sidecar, &document, case);

        let expected = sidecar
            .get("diagnostics")
            .and_then(Json::as_array)
            .unwrap_or_else(|| {
                panic!("{}: the sidecar carries no `diagnostics` list", case.name)
            });

        // Compared as a SET (SPEC 6.3 makes emission order insignificant), but
        // with distinct pairing: each expectation must claim its own reported
        // diagnostic.
        let mut unclaimed: Vec<usize> = (0..diagnostics.len()).collect();
        for want in expected {
            let path = want.get("path").and_then(Json::as_str).unwrap_or_else(|| {
                panic!("{}: an expected diagnostic has no `path`", case.name)
            });
            // SPEC 6.3: the code is what the case compares, and the English
            // MUST NOT be compared. `message_advisory` is read only so a
            // failure can say what the case was about.
            let code = want.get("code").and_then(Json::as_str).unwrap_or_else(|| {
                panic!(
                    "{}: an expected diagnostic has no `code`. SPEC 6.3 requires one \
                     on any diagnostic a corpus case pins.",
                    case.name
                )
            });
            let advisory = want
                .get("message_advisory")
                .and_then(Json::as_str)
                .unwrap_or("");
            assert!(
                want.get("message").is_none(),
                "{}: the sidecar carries a `message` field. SPEC 6.3 forbids comparing \
                 English, and the field is called `message_advisory` for that reason.",
                case.name
            );
            let found = unclaimed
                .iter()
                .position(|&i| diagnostics[i].path == path && diagnostics[i].code() == code);
            match found {
                Some(slot) => {
                    unclaimed.remove(slot);
                }
                None => panic!(
                    "{}: expected a diagnostic at {path:?} with code {code:?} \
                     ({advisory}).\nReported: {}",
                    case.name,
                    render_diagnostics(&diagnostics)
                ),
            }
        }
        assert!(
            unclaimed.is_empty(),
            "{}: reported {} diagnostic(s) the corpus does not expect: {}",
            case.name,
            unclaimed.len(),
            render_diagnostics(
                &unclaimed
                    .iter()
                    .map(|&i| diagnostics[i].clone())
                    .collect::<Vec<_>>()
            )
        );
    });
}

/// SPEC 8: a `format/` case is a pair rather than a sidecar — `X.in.vnx` and
/// `X.canonical.vnx`. The runner MUST assert that each input is not already
/// canonical; without that, a formatter that echoed its input would pass.
#[test]
fn format_tier() {
    let dir = corpus_root().join("format");
    let mut cases = 0;
    for entry in read_dir_sorted(&dir) {
        let name = entry.file_name().to_string_lossy().into_owned();
        let Some(stem) = name.strip_suffix(".in.vnx") else {
            continue;
        };
        cases += 1;
        let input = std::fs::read(entry.path()).expect("the input reads");
        let expected_path = dir.join(format!("{stem}.canonical.vnx"));
        let expected = std::fs::read(&expected_path).unwrap_or_else(|_| {
            panic!("format/{stem}.in.vnx has no {stem}.canonical.vnx beside it")
        });

        assert_ne!(
            input, expected,
            "format/{stem}.in.vnx is already canonical, so this case cannot fail \
             for a formatter that echoes its input"
        );

        let (rendered, diagnostics) = vnx::canonicalise(&input)
            .unwrap_or_else(|e| panic!("format/{stem}.in.vnx was refused: {e}"));
        assert!(
            diagnostics.is_empty(),
            "format/{stem}.in.vnx reported {}",
            render_diagnostics(&diagnostics)
        );
        assert_eq!(
            rendered.as_bytes(),
            expected.as_slice(),
            "format/{stem}: canonicalising the input does not give the expected bytes.\n\
             --- expected ---\n{}\n--- got ---\n{rendered}",
            String::from_utf8_lossy(&expected)
        );
    }
    assert!(cases > 0, "the format tier found no cases");
}

/// A corpus case with no sidecar is a failure, not a skip. The three sidecar
/// tiers assert this per file; this test asserts that no stray document sits
/// anywhere in the corpus outside a tier the runner walks.
#[test]
fn every_corpus_document_is_reached_by_a_tier() {
    let root = corpus_root();
    let mut unreached = Vec::new();
    let mut walk = vec![root.clone()];
    while let Some(dir) = walk.pop() {
        for entry in read_dir_sorted(&dir) {
            let path = entry.path();
            if path.is_dir() {
                walk.push(path);
                continue;
            }
            let name = path.file_name().expect("a file has a name").to_string_lossy();
            if !(name.ends_with(".vnx") || name.ends_with(".vnz")) {
                continue;
            }
            let tier = path
                .parent()
                .and_then(|p| p.file_name())
                .map(|n| n.to_string_lossy().into_owned())
                .unwrap_or_default();
            if !matches!(tier.as_str(), "accept" | "reject" | "diagnose" | "format") {
                unreached.push(path.display().to_string());
            }
        }
    }
    assert!(
        unreached.is_empty(),
        "corpus documents outside the four tiers, so no runner reads them: {unreached:?}"
    );
}

// ---------------------------------------------------------------------------
// Case plumbing
// ---------------------------------------------------------------------------

struct Case {
    name: String,
    path: PathBuf,
    bytes: Vec<u8>,
}

impl Case {
    fn extension(&self) -> &str {
        self.path
            .extension()
            .and_then(|e| e.to_str())
            .unwrap_or_default()
    }

    /// A file with no sidecar is a failure, not a skip.
    fn sidecar(&self) -> Json {
        let stem = self
            .path
            .file_name()
            .expect("a file has a name")
            .to_string_lossy();
        let stem = stem
            .rsplit_once('.')
            .expect("a corpus file has an extension")
            .0;
        let path = self
            .path
            .parent()
            .expect("a file has a directory")
            .join(format!("{stem}.expect.json"));
        let bytes = std::fs::read(&path).unwrap_or_else(|_| {
            panic!(
                "{}: no sidecar at {}. A file with no sidecar is a failure, not a skip.",
                self.name,
                path.display()
            )
        });
        parse(&bytes).unwrap_or_else(|e| panic!("{}: the sidecar will not parse: {e}", self.name))
    }
}

fn run_tier(tier: &str, mut check: impl FnMut(&Case)) {
    let dir = corpus_root().join(tier);
    let mut cases = 0;
    for entry in read_dir_sorted(&dir) {
        let path = entry.path();
        let name = path.file_name().expect("a file has a name").to_string_lossy();
        if !(name.ends_with(".vnx") || name.ends_with(".vnz")) {
            continue;
        }
        cases += 1;
        let case = Case {
            name: format!("{tier}/{name}"),
            bytes: std::fs::read(&path).expect("a corpus file reads"),
            path: path.clone(),
        };
        check(&case);
    }
    assert!(cases > 0, "the {tier} tier found no cases");
}

fn read_dir_sorted(dir: &Path) -> Vec<std::fs::DirEntry> {
    let mut entries: Vec<_> = std::fs::read_dir(dir)
        .unwrap_or_else(|e| panic!("{} will not open: {e}", dir.display()))
        .map(|e| e.expect("a directory entry reads"))
        .collect();
    entries.sort_by_key(|e| e.file_name());
    entries
}

fn expect_kind(sidecar: &Json, tier: &str, case: &Case) {
    let kind = sidecar.get("kind").and_then(Json::as_str);
    assert_eq!(
        kind,
        Some(tier),
        "{}: the sidecar's `kind` must match its directory",
        case.name
    );
    assert!(
        sidecar
            .get("note")
            .and_then(Json::as_str)
            .is_some_and(|n| !n.is_empty()),
        "{}: the sidecar carries no `note` saying why the case exists",
        case.name
    );
}

fn render_diagnostics(list: &[vnx::Diagnostic]) -> String {
    if list.is_empty() {
        return "nothing".to_string();
    }
    let mut out = String::new();
    for d in list {
        let _ = write!(out, "\n    {} [{}] — {}", d.path, d.code(), d.message());
    }
    out
}

// ---------------------------------------------------------------------------
// Sidecar expectations
// ---------------------------------------------------------------------------

/// `counts` stops an accept case passing vacuously: a decoder that returned an
/// empty document for every input would otherwise satisfy "no error and no
/// diagnostics" on all of them.
fn check_counts(sidecar: &Json, document: &Document, case: &Case) {
    let Some(counts) = sidecar.get("counts").and_then(Json::as_object) else {
        return;
    };
    for (collection, want) in counts {
        let want = want
            .as_f64()
            .unwrap_or_else(|| panic!("{}: a count is not a number", case.name))
            as usize;
        let got = match collection.as_str() {
            "folders" => document.folders.as_ref().map_or(0, Vec::len),
            "networks" => document.networks.as_ref().map_or(0, Vec::len),
            "routes" => document.routes.as_ref().map_or(0, Vec::len),
            "tracks" => document.tracks.as_ref().map_or(0, Vec::len),
            "waypoints" => document.waypoints.as_ref().map_or(0, Vec::len),
            "areas" => document.areas.as_ref().map_or(0, Vec::len),
            "rulesets" => document
                .rulesets
                .as_ref()
                .and_then(Json::as_array)
                .map_or(0, <[Json]>::len),
            other => panic!("{}: the sidecar counts an unknown collection {other:?}", case.name),
        };
        assert_eq!(
            got, want,
            "{}: expected {want} item(s) in `{collection}` and found {got}",
            case.name
        );
    }
}

/// `positions` is the only available defence against a longitude and latitude
/// transposition: both orderings parse, both are in range, and the point lands
/// 8,000 km away with every structural check still passing.
fn check_positions(sidecar: &Json, document: &Document, case: &Case) {
    let Some(positions) = sidecar.get("positions").and_then(Json::as_array) else {
        return;
    };
    for want in positions {
        let pointer = want
            .get("at")
            .and_then(Json::as_str)
            .unwrap_or_else(|| panic!("{}: a `positions` entry has no `at`", case.name));
        let position = resolve_position(document, pointer).unwrap_or_else(|| {
            panic!(
                "{}: the pointer {pointer} names no position in the decoded model",
                case.name
            )
        });
        for (channel, reading) in [("lon", position.lon()), ("lat", position.lat())] {
            let Some(expected) = want.get(channel).and_then(Json::as_f64) else {
                continue;
            };
            assert_eq!(
                reading.to_bits(),
                expected.to_bits(),
                "{}: {pointer} has {channel} = {reading} and the corpus says {expected}",
                case.name
            );
        }
    }
}

/// `moving_time_ms: null` means UNKNOWN, which is not zero. A reader that
/// answers 0 for a track carrying no pip times has answered a question it
/// cannot answer.
fn check_tracks(sidecar: &Json, document: &Document, case: &Case) {
    let Some(tracks) = sidecar.get("tracks").and_then(Json::as_array) else {
        return;
    };
    for want in tracks {
        let name = want
            .get("name")
            .and_then(Json::as_str)
            .unwrap_or_else(|| panic!("{}: a `tracks` expectation has no `name`", case.name));
        let track: &Track = document
            .tracks
            .iter()
            .flatten()
            .find(|t| t.name.as_deref() == Some(name))
            .unwrap_or_else(|| panic!("{}: no track named {name:?}", case.name));
        let got = track.moving_time_ms();
        match want.get("moving_time_ms") {
            Some(Json::Null) => assert_eq!(
                got, None,
                "{}: {name:?} must report moving time as UNKNOWN, not {got:?}",
                case.name
            ),
            Some(Json::Number(expected)) => assert_eq!(
                got,
                Some(*expected),
                "{}: {name:?} moving time",
                case.name
            ),
            _ => panic!("{}: {name:?} has no `moving_time_ms`", case.name),
        }
    }
}

fn check_attachments(sidecar: &Json, got: Option<&[String]>, case: &Case) {
    let Some(want) = sidecar.get("attachments").and_then(Json::as_array) else {
        return;
    };
    let want: BTreeSet<String> = want
        .iter()
        .map(|v| {
            v.as_str()
                .unwrap_or_else(|| panic!("{}: an attachment path is not a string", case.name))
                .to_string()
        })
        .collect();
    let got: BTreeSet<String> = got
        .unwrap_or_else(|| panic!("{}: `attachments` is a container-only field", case.name))
        .iter()
        .cloned()
        .collect();
    assert_eq!(got, want, "{}: attachments", case.name);
}

// ---------------------------------------------------------------------------
// Resolving a JSON Pointer against the DECODED MODEL
// ---------------------------------------------------------------------------

/// Walk a `positions` pointer through the model, in the wire's own key names.
///
/// Deliberately not a walk over the raw JSON. Resolving against the file
/// compares the file to itself and cannot see a decoder that swapped two
/// channels, which is the entire point of the check.
fn resolve_position<'a>(document: &'a Document, pointer: &str) -> Option<&'a Position> {
    let tokens: Vec<String> = pointer
        .strip_prefix('/')?
        .split('/')
        .map(|t| t.replace("~1", "/").replace("~0", "~"))
        .collect();
    let token = |i: usize| tokens.get(i).map(String::as_str);
    let index = |i: usize| tokens.get(i)?.parse::<usize>().ok();

    match token(0)? {
        "waypoints" => waypoint_at(document.waypoints.as_ref()?.get(index(1)?)?, token(2)?),
        "areas" => ring_vertex(document.areas.as_ref()?.get(index(1)?)?, token(2)?, index(3)?),
        "tracks" => track_pip(document.tracks.as_ref()?.get(index(1)?)?, &tokens),
        "routes" => route_pip(document.routes.as_ref()?.get(index(1)?)?, &tokens),
        "networks" => network_position(document.networks.as_ref()?.get(index(1)?)?, &tokens),
        _ => None,
    }
}

fn waypoint_at<'a>(waypoint: &'a Waypoint, key: &str) -> Option<&'a Position> {
    (key == "at").then_some(&waypoint.at)
}

fn ring_vertex<'a>(area: &'a Area, key: &str, index: usize) -> Option<&'a Position> {
    if key != "polygon" {
        return None;
    }
    area.polygon.get(index)
}

fn track_pip<'a>(track: &'a Track, tokens: &[String]) -> Option<&'a Position> {
    // /tracks/N/segments/S/pips/P
    if tokens.get(2)? != "segments" || tokens.get(4)? != "pips" {
        return None;
    }
    track
        .segments
        .get(tokens.get(3)?.parse::<usize>().ok()?)?
        .pips
        .get(tokens.get(5)?.parse::<usize>().ok()?)
}

fn route_pip<'a>(route: &'a Route, tokens: &[String]) -> Option<&'a Position> {
    // /routes/N/arcs/A/pips/P
    if tokens.get(2)? != "arcs" || tokens.get(4)? != "pips" {
        return None;
    }
    route
        .arcs
        .get(tokens.get(3)?.parse::<usize>().ok()?)?
        .pips
        .get(tokens.get(5)?.parse::<usize>().ok()?)
}

fn network_position<'a>(network: &'a Network, tokens: &[String]) -> Option<&'a Position> {
    match tokens.get(2)?.as_str() {
        // /networks/N/nodes/KEY
        "nodes" => network.node(tokens.get(3)?),
        // /networks/N/arcs/KEY/pips/P
        "arcs" => {
            if tokens.get(4)? != "pips" {
                return None;
            }
            network
                .arc(tokens.get(3)?)?
                .pips
                .get(tokens.get(5)?.parse::<usize>().ok()?)
        }
        // /networks/N/chains/C/links/L/{from,to}
        "chains" => {
            if tokens.get(4)? != "links" {
                return None;
            }
            let link = network
                .chains
                .as_ref()?
                .get(tokens.get(3)?.parse::<usize>().ok()?)?
                .links
                .get(tokens.get(5)?.parse::<usize>().ok()?)?;
            let endpoint = match tokens.get(6)?.as_str() {
                "from" => &link.from,
                "to" => &link.to,
                _ => return None,
            };
            match endpoint {
                vnx::model::Endpoint::Inline(position) => Some(position),
                // A node key is not a position of its own; the corpus points
                // at nodes through /nodes/KEY.
                vnx::model::Endpoint::Node(_) => None,
            }
        }
        // /networks/N/points/P/at
        "points" => waypoint_at(
            network
                .points
                .as_ref()?
                .get(tokens.get(3)?.parse::<usize>().ok()?)?,
            tokens.get(4)?,
        ),
        // /networks/N/areas/A/polygon/V
        "areas" => ring_vertex(
            network
                .areas
                .as_ref()?
                .get(tokens.get(3)?.parse::<usize>().ok()?)?,
            tokens.get(4)?,
            tokens.get(5)?.parse::<usize>().ok()?,
        ),
        _ => None,
    }
}

/// Pull the raw bytes of a container's root document, so the accept tier can
/// compare a re-render against them.
fn document_bytes_from_container(archive: &[u8], entry_name: &str) -> Vec<u8> {
    let archive = vnx::zip::Archive::open(archive).expect("the archive opens");
    let entry = archive
        .entries()
        .iter()
        .find(|e| e.name == entry_name)
        .expect("the chosen entry is in the archive");
    archive.read(entry).expect("the entry decompresses")
}

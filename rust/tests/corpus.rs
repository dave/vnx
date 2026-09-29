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
//! * The accept tier compares JSON TREES: the tree parsed from the original
//!   file against the tree parsed from `encode(decode(file))` (SPEC 8.1). It
//!   does not compare bytes, because nothing constrains them, and it does not
//!   compare one model against another, because a reader that discarded every
//!   data record, application key, style and `notes` produces a model equal to itself
//!   and would pass green. See `tests/support/tree.rs`.
//! * `positions` is resolved against the DECODED MODEL, never against the raw
//!   JSON. Resolving it against the file compares the file to itself and
//!   cannot see a decoder that swapped longitude and latitude.
//! * A failing case does not take the rest of its tier with it. One defect
//!   reported as a total collapse tells you nothing about how much is wrong.
//! * Diagnostics are matched on `path` and `code`, as a SET with distinct
//!   pairing: one reported diagnostic may not satisfy two expectations, or a
//!   reader that found half the defects would pass. The sidecar's
//!   `message_advisory` is NOT compared — SPEC 6.3 forbids it, and the field is
//!   named that way so a runner cannot half-couple to English by accident.

use std::collections::{BTreeMap, BTreeSet};
use std::fmt::Write as _;
use std::path::{Path, PathBuf};

use vnx::json::{parse, Json};
use vnx::model::{Arc, Area, Document, Endpoint, Network, Position, Track, Vertex, Waypoint};
use vnx::{decode, encode, vnz};

#[path = "support/tree.rs"]
mod tree;
use tree::EmitBoundary;

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

        let (document, diagnostics, original_bytes, attachments) = match case.extension() {
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
                    Some(container.attachments),
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

        // SPEC 8.1: decode, encode, and compare the two TREES. The emit
        // boundary is normalised on both sides, because this compares a
        // document against a re-encoding of itself and a writer shortens.
        // Values are compared exactly: nothing here rounds.
        let encoded = encode(&document).expect("encodable");
        if let Some(difference) = tree::compare(
            encoded.as_bytes(),
            &original_bytes,
            EmitBoundary::Normalise,
        ) {
            panic!(
                "{}: encoding the decoded document does not give the tree the file carries: {difference}",
                case.name
            );
        }
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

        let (document, diagnostics, attachments) = match case.extension() {
            "vnz" => {
                let c = vnz::read(&case.bytes)
                    .unwrap_or_else(|e| panic!("{}: refused: {e}", case.name));
                (c.reading.document, c.reading.diagnostics, Some(c.attachments))
            }
            _ => {
                let r = decode(&case.bytes)
                    .unwrap_or_else(|e| panic!("{}: refused: {e}", case.name));
                (r.document, r.diagnostics, None)
            }
        };

        check_counts(&sidecar, &document, case);
        check_positions(&sidecar, &document, case);
        check_tracks(&sidecar, &document, case);
        // A diagnose container pins its attachments too: SPEC 7 has an
        // unusable METHOD drop one entry and keep the rest, and a reader that
        // dropped them all would otherwise satisfy the diagnostic alone.
        check_attachments(&sidecar, attachments.as_deref(), case);

        if let Err(why) = well_formed(&diagnostics) {
            panic!("{}: {why}", case.name);
        }
        let expected = read_expected_diagnostics(&sidecar)
            .unwrap_or_else(|why| panic!("{}: {why}", case.name));
        if let Err(why) = pair_diagnostics(&expected, &diagnostics) {
            panic!("{}: {why}", case.name);
        }
    });
}

/// corpus/README.md, "Write a runner", step 7: the reader's own output is well
/// formed, with no sidecar consulted. `path` and `code` are the only two
/// fields a runner may compare (SPEC 6.3), so one path carrying two distinct
/// codes, or two reports at one path of which one has no code, are reports no
/// runner can tell apart from the wrong ones.
///
/// It is a different kind of check from `pair_diagnostics`, which compares a
/// reader against an expectation: no fixture prompts this one, so a runner
/// that skipped it would notice nothing.
fn well_formed(reported: &[vnx::Diagnostic]) -> Result<(), String> {
    let mut by_path: BTreeMap<&str, Vec<&'static str>> = BTreeMap::new();
    for d in reported {
        by_path.entry(d.path.as_str()).or_default().push(d.code());
    }
    for (path, codes) in &by_path {
        let mut distinct = codes.clone();
        distinct.sort_unstable();
        distinct.dedup();
        if distinct.len() > 1 {
            return Err(format!("the path {path:?} carries {} distinct codes: {distinct:?}", distinct.len()));
        }
        if codes.len() > 1 && codes.iter().any(|c| c.is_empty()) {
            return Err(format!("the path {path:?} carries {} reports, and one has no code", codes.len()));
        }
    }
    Ok(())
}

/// A self-test of `well_formed`: no corpus case carries two diagnostics at one
/// path, so the corpus cannot show that the check exists.
#[test]
fn a_path_carrying_two_codes_is_refused() {
    use vnx::{Cause, Diagnostic};
    let key = Diagnostic::new("/waypoints/0/nmae", Cause::UnknownKey);
    let utc = Diagnostic::new("/waypoints/0/nmae", Cause::TimestampNotUtc);
    let elsewhere = Diagnostic::new("/waypoints/1/nmae", Cause::TimestampNotUtc);
    assert!(well_formed(&[key.clone(), elsewhere]).is_ok());
    assert!(well_formed(&[key, utc]).is_err());
}

/// One expected diagnostic, as a sidecar states it.
#[derive(Debug, Clone, PartialEq, Eq)]
struct Expected {
    path: String,
    /// SPEC 6.3: the stable token, and the ONLY thing compared.
    code: String,
    /// Read so a failure can say what the case was about. Never compared.
    advisory: String,
}

/// Read a sidecar's `diagnostics` list, refusing the two shapes that would let
/// a matcher silently check less than the case pins.
///
/// Both refusals are live history rather than caution. When `67e1721` moved the
/// corpus from pinning English to pinning `code`, a matcher written against a
/// `message` field found nothing, fell through, and compared paths alone —
/// green on every case while checking half of what each pins. A matcher that
/// checks less always passes.
fn read_expected_diagnostics(sidecar: &Json) -> Result<Vec<Expected>, String> {
    let list = sidecar
        .get("diagnostics")
        .and_then(Json::as_array)
        .ok_or("the sidecar carries no `diagnostics` list")?;
    let mut out = Vec::with_capacity(list.len());
    for want in list {
        if want.get("message").is_some() {
            return Err("an expected diagnostic carries a `message` field. SPEC 6.3 \
                        forbids comparing English, and the field is called \
                        `message_advisory` for exactly that reason."
                .to_string());
        }
        let path = want
            .get("path")
            .and_then(Json::as_str)
            .ok_or("an expected diagnostic has no `path`")?;
        let code = want.get("code").and_then(Json::as_str).ok_or(
            "an expected diagnostic has no `code`. SPEC 6.3 requires one on any \
             diagnostic a corpus case pins, and a path-only match would pass a \
             reader that reported the wrong thing in the right place.",
        )?;
        out.push(Expected {
            path: path.to_string(),
            code: code.to_string(),
            advisory: want
                .get("message_advisory")
                .and_then(Json::as_str)
                .unwrap_or("")
                .to_string(),
        });
    }
    Ok(out)
}

/// Pair each expectation with one DISTINCT reported diagnostic, on `path` and
/// `code` (SPEC 6.3), in any order (SPEC 6.3 makes emission order
/// insignificant).
///
/// Distinct pairing matters: letting one reported diagnostic satisfy two
/// expectations would pass a reader that found half the defects.
fn pair_diagnostics(expected: &[Expected], reported: &[vnx::Diagnostic]) -> Result<(), String> {
    let mut unclaimed: Vec<usize> = (0..reported.len()).collect();
    for want in expected {
        let found = unclaimed
            .iter()
            .position(|&i| reported[i].path == want.path && reported[i].code() == want.code);
        match found {
            Some(slot) => {
                unclaimed.remove(slot);
            }
            None => {
                return Err(format!(
                    "expected a diagnostic at {:?} with code {:?} ({}).\nReported: {}",
                    want.path,
                    want.code,
                    want.advisory,
                    render_diagnostics(reported)
                ))
            }
        }
    }
    if !unclaimed.is_empty() {
        let extra: Vec<vnx::Diagnostic> =
            unclaimed.iter().map(|&i| reported[i].clone()).collect();
        return Err(format!(
            "reported {} diagnostic(s) the corpus does not expect: {}",
            extra.len(),
            render_diagnostics(&extra)
        ));
    }
    Ok(())
}

/// A self-test of the matcher, because the CORPUS CANNOT TEST IT.
///
/// Measured 2026-09-09 with `tools/redproof.py`: weakening `pair_diagnostics`
/// to compare `path` alone leaves the whole diagnose tier GREEN. Every diagnose
/// fixture carries at most one diagnostic and its path identifies it, so no
/// case can separate a matcher that compares the code from one that ignores it.
/// These assertions are the only thing that does.
#[test]
fn the_diagnostic_matcher_has_been_seen_to_fail() {
    use vnx::{Cause, Diagnostic};

    let expect = |path: &str, code: &str| Expected {
        path: path.to_string(),
        code: code.to_string(),
        advisory: String::new(),
    };
    let unknown_key = Diagnostic::new("/waypoints/0/nmae", Cause::UnknownKey);
    let not_utc = Diagnostic::new("/waypoints/0/nmae", Cause::TimestampNotUtc);

    // The pairing it must accept.
    assert!(pair_diagnostics(
        &[expect("/waypoints/0/nmae", "unknown-key")],
        std::slice::from_ref(&unknown_key)
    )
    .is_ok());

    // Right path, WRONG code. This is the hole an English substring could not
    // see and a path-only match cannot see either.
    assert!(pair_diagnostics(
        &[expect("/waypoints/0/nmae", "unknown-key")],
        &[not_utc]
    )
    .is_err());

    // Right code, wrong path.
    assert!(pair_diagnostics(
        &[expect("/waypoints/0/name", "unknown-key")],
        std::slice::from_ref(&unknown_key)
    )
    .is_err());

    // A reported diagnostic nobody expected.
    assert!(pair_diagnostics(&[], std::slice::from_ref(&unknown_key)).is_err());

    // An expected diagnostic nobody reported.
    assert!(pair_diagnostics(&[expect("/waypoints/0/nmae", "unknown-key")], &[]).is_err());

    // Distinct pairing: two expectations may not share one reported diagnostic.
    assert!(pair_diagnostics(
        &[
            expect("/waypoints/0/nmae", "unknown-key"),
            expect("/waypoints/0/nmae", "unknown-key"),
        ],
        std::slice::from_ref(&unknown_key)
    )
    .is_err());
    assert!(pair_diagnostics(
        &[
            expect("/waypoints/0/nmae", "unknown-key"),
            expect("/waypoints/0/nmae", "unknown-key"),
        ],
        &[unknown_key.clone(), unknown_key.clone()]
    )
    .is_ok());

    // Order is not significant (SPEC 6.3).
    let a = Diagnostic::new("/a", Cause::UnknownKey);
    let b = Diagnostic::new("/b", Cause::PinnedTurn);
    assert!(pair_diagnostics(
        &[expect("/b", "pinned-turn"), expect("/a", "unknown-key")],
        &[a, b]
    )
    .is_ok());
}

/// A self-test of the sidecar reader, for the same reason: no fixture in the
/// corpus carries either malformed shape, so nothing else can prove these
/// refusals exist.
#[test]
fn a_sidecar_that_pins_less_than_it_looks_is_refused() {
    let parse_sidecar = |text: &str| parse(text.as_bytes()).expect("parses");

    // The shape the corpus uses today.
    let good = parse_sidecar(
        r#"{"diagnostics":[{"path":"/a","code":"unknown-key","message_advisory":"x"}]}"#,
    );
    assert_eq!(
        read_expected_diagnostics(&good).expect("accepted"),
        vec![Expected {
            path: "/a".into(),
            code: "unknown-key".into(),
            advisory: "x".into()
        }]
    );

    // A bare `message` is the pre-`67e1721` shape. It must fail rather than be
    // ignored, because ignoring it degrades to a path-only match.
    let stale = parse_sidecar(r#"{"diagnostics":[{"path":"/a","message":"unknown key"}]}"#);
    assert!(read_expected_diagnostics(&stale).is_err());

    // A `code` alongside a `message` is still refused: the `message` is the
    // signal that the sidecar was written against the old contract.
    let mixed =
        parse_sidecar(r#"{"diagnostics":[{"path":"/a","code":"unknown-key","message":"x"}]}"#);
    assert!(read_expected_diagnostics(&mixed).is_err());

    // No `code` at all must fail, not degrade.
    let codeless = parse_sidecar(r#"{"diagnostics":[{"path":"/a","message_advisory":"x"}]}"#);
    assert!(read_expected_diagnostics(&codeless).is_err());

    // No `diagnostics` key at all must fail: a diagnose case that pins nothing
    // is a case that passes for any reader.
    assert!(read_expected_diagnostics(&parse_sidecar(r#"{"kind":"diagnose"}"#)).is_err());

    // An empty list is legitimate — `diagnose/duplicate-object-key` uses it.
    assert_eq!(
        read_expected_diagnostics(&parse_sidecar(r#"{"diagnostics":[]}"#)).expect("accepted"),
        vec![]
    );
}

/// A write case whose output is a CONTAINER (`corpus/write/README.md`).
///
/// The attachments come from a manifest rather than from real files, because a
/// name holding U+1F600 is subject to the checkout filesystem's Unicode
/// normalisation: a checked-out file could arrive under a name that is not the
/// one the case means, and the fixture would test the filesystem instead of the
/// writer.
///
/// The attachments are fed REVERSED. A writer's output must not depend on the
/// order it was handed, and feeding them in manifest order cannot see one that
/// does.
fn write_container_case(dir: &Path, stem: &str, input: &[u8], manifest: &Path) {
    let expected = std::fs::read(dir.join(format!("{stem}.written.vnz")))
        .unwrap_or_else(|_| panic!("write/{stem}: the manifest promises {stem}.written.vnz and there is none"));

    // A case with no sidecar is a failure, not a skip, as everywhere else.
    let sidecar_bytes = std::fs::read(dir.join(format!("{stem}.expect.json")))
        .unwrap_or_else(|_| panic!("write/{stem} has no sidecar"));
    let sidecar = parse(&sidecar_bytes).expect("the sidecar parses");
    assert_eq!(
        sidecar.get("kind").and_then(Json::as_str),
        Some("write"),
        "write/{stem}: the sidecar's `kind` must match its directory"
    );
    assert!(
        sidecar
            .get("note")
            .and_then(Json::as_str)
            .is_some_and(|n| !n.is_empty()),
        "write/{stem}: the sidecar carries no `note`"
    );
    assert_eq!(
        sidecar.get("root").and_then(Json::as_str),
        Some("doc.vnx"),
        "write/{stem}: this runner writes the document as doc.vnx"
    );


    // ⚠ THE ARCHIVE'S BYTES ARE NOT COMPARED. SPEC 7.1 constrains five rules
    // and leaves every other field to the writer, so two conforming writers
    // are not expected to agree byte for byte. What is compared is what 7.1
    // does constrain: the root document's tree, the attachment names and
    // bodies, and the document coming first.
    //
    // ⚠ AND THE ATTACHMENTS ARE AN ORDER-INSENSITIVE COLLECTION. Ruled
    // 2026-09-10 across all six implementations: 7.1 pins the FIRST entry and
    // then says "every other field of the archive is the writer's own
    // business", so comparing a name list in order enforces a rule the
    // specification explicitly disclaims. A test that fails on REORDERING but
    // not on LOSS is the signature — this one is measured both ways in
    // `redproof.py`.
    let want = archive_entries(&expected);
    assert!(!want.is_empty(), "write/{stem}: the expected archive holds no entries");
    assert_eq!(
        want[0].0, "doc.vnx",
        "write/{stem}: SPEC 7.1 requires the root document first"
    );
    // ⚠ NO "the input is not already the written form" GUARD HERE, and that is
    // deliberate. SPEC 8.1 asks for one because a writer that echoed its input
    // would otherwise pass — but what a container case tests is the ARCHIVE
    // this writer builds around the document, not the emit boundary. The
    // document cases below carry the guard, and they are where the emit rules
    // live.

    let attachments = read_manifest(manifest, stem);
    let reading = decode(input).unwrap_or_else(|e| panic!("write/{stem}.in.vnx: {e}"));
    check_write_diagnostics(&sidecar, &reading.diagnostics, stem);

    // The attachments are fed REVERSED. A writer's output must not depend on
    // the order it was handed, and feeding them in manifest order cannot see
    // one that does.
    let mut fed: Vec<vnz::Attachment> = attachments.clone();
    fed.reverse();
    let written = vnz::write(encode(&reading.document).expect("encodable").as_bytes(), &fed)
        .unwrap_or_else(|e| panic!("write/{stem}: writing the container: {e}"));
    let got = archive_entries(&written);

    assert!(!got.is_empty(), "write/{stem}: the archive holds no entries");
    assert_eq!(
        got[0].0, "doc.vnx",
        "write/{stem}: SPEC 7.1 requires the root document first, and this is {:?}",
        got[0].0
    );
    if let Some(difference) = tree::compare(&got[0].1, &want[0].1, EmitBoundary::Keep) {
        panic!("write/{stem}: the archive's root document is not the one the case expects: {difference}");
    }
    assert_eq!(
        by_name(&got[1..]),
        by_name(&want[1..]),
        "write/{stem}: the archive does not carry the expected attachments"
    );
    // An attachment is bytes and is compared as bytes. Nothing in this format
    // interprets one.
    assert_eq!(
        by_name(&got[1..]),
        attachments
            .iter()
            .map(|a| (a.path.clone(), a.bytes.clone()))
            .collect(),
        "write/{stem}: the archive does not carry the manifest's attachments"
    );

    // And the archive reads back through this library's own reader.
    let back = vnz::read(&written)
        .unwrap_or_else(|e| panic!("write/{stem}: the archive this library wrote is one it cannot read: {e}"));
    assert!(
        back.reading.diagnostics.is_empty(),
        "write/{stem}: the archive this library wrote reported {}",
        render_diagnostics(&back.reading.diagnostics)
    );
    assert_eq!(
        back.attachments
            .iter()
            .map(|a| (a.path.clone(), a.bytes.clone()))
            .collect::<BTreeMap<_, _>>(),
        attachments
            .iter()
            .map(|a| (a.path.clone(), a.bytes.clone()))
            .collect(),
        "write/{stem}: reading the archive back does not give the manifest's attachments"
    );
}

/// A container is worth being one: the archive is smaller than the document it
/// holds.
///
/// ⚠ MEASURED ON A REAL DOCUMENT, NOT ON A CORPUS WRITE CASE. A ZIP entry costs
/// about a hundred bytes of header and directory whatever it carries, so on a
/// hundred-byte fixture the overhead wins and the archive is legitimately
/// larger. Asserting this per write case would have measured the fixture sizes
/// rather than the compression.
#[test]
fn a_container_is_smaller_than_the_document_it_holds() {
    let path = corpus_root().join("accept").join("example-network.vnx");
    let bytes = std::fs::read(&path).expect("the worked example reads");
    let document = decode(&bytes).expect("the worked example is accepted").document;
    let encoded = encode(&document).expect("encodable");
    let archive = vnz::write(encoded.as_bytes(), &[]).expect("the container is written");
    assert!(
        archive.len() < encoded.len(),
        "the archive is {} bytes and the document alone is {}",
        archive.len(),
        encoded.len()
    );

    // And it reads back through this library's own reader, so the saving is
    // not a truncation.
    let back = vnz::read(&archive).expect("the archive this library wrote is one it reads");
    assert_eq!(back.reading.document, document);
}

/// The entries under `entries`, keyed by name.
///
/// A map rather than a list, because SPEC 7.1 makes entry names unique
/// case-insensitively and leaves their ORDER to the writer. Comparing two of
/// these compares presence and content and says nothing about order.
fn by_name(entries: &[(String, Vec<u8>)]) -> BTreeMap<String, Vec<u8>> {
    let mut out = BTreeMap::new();
    for (name, body) in entries {
        assert!(
            out.insert(name.clone(), body.clone()).is_none(),
            "the archive repeats the entry `{name}`, which SPEC 7 refuses"
        );
    }
    out
}

/// Every entry of an archive, in archive order, read through this library's
/// own ZIP reader.
fn archive_entries(bytes: &[u8]) -> Vec<(String, Vec<u8>)> {
    let archive = vnx::zip::Archive::open(bytes).expect("the archive opens");
    archive
        .entries()
        .iter()
        .filter(|e| !e.is_directory())
        .map(|e| {
            (
                e.name.clone(),
                archive.read(e).unwrap_or_else(|err| panic!("reading `{}`: {err}", e.name)),
            )
        })
        .collect()
}

/// The manifest's attachments, in the order it lists them.
fn read_manifest(manifest: &Path, stem: &str) -> Vec<vnz::Attachment> {
    let bytes = std::fs::read(manifest).expect("the manifest reads");
    let json = parse(&bytes).expect("the manifest parses");
    let listed = json
        .get("attachments")
        .and_then(Json::as_array)
        .unwrap_or_else(|| panic!("write/{stem}: the manifest carries no `attachments` array"));
    listed
        .iter()
        .map(|a| {
            let name = a
                .get("name")
                .and_then(Json::as_str)
                .unwrap_or_else(|| panic!("write/{stem}: an attachment has no `name`"));
            let body = a
                .get("body")
                .and_then(Json::as_str)
                .unwrap_or_else(|| panic!("write/{stem}: attachment {name} has no `body`"));
            vnz::Attachment {
                path: name.to_string(),
                bytes: body.as_bytes().to_vec(),
            }
        })
        .collect()
}

/// A write sidecar's `diagnostics` is what decoding the input must report,
/// compared exactly as the diagnose tier compares it. Absent, the input must
/// read cleanly: most write cases are documents a reader reads as written, and
/// the one that is not exists to write back what the reader repaired.
fn check_write_diagnostics(sidecar: &Json, reported: &[vnx::Diagnostic], stem: &str) {
    let expected = if sidecar.get("diagnostics").is_some() {
        read_expected_diagnostics(sidecar).unwrap_or_else(|why| panic!("write/{stem}: {why}"))
    } else {
        Vec::new()
    };
    if let Err(why) = pair_diagnostics(&expected, reported) {
        panic!("write/{stem}.in.vnx: {why}");
    }
}

/// SPEC 4.1's EMIT BOUNDARY, which is the one no other tier reaches.
///
/// A case is a pair — `X.in.vnx` and `X.written.vnx` — plus a sidecar. The
/// operation is decode-then-encode, and the input is a DOCUMENT rather than a
/// model, so no language-specific way to express a model is needed.
///
/// ⚠ IT NORMALISES ALMOST NOTHING. The accept tier drops a trailing `null`
/// from both sides, because it compares a document against a re-encoding of
/// itself. Doing that here would normalise away the rule under test. The one
/// normalisation left is the one that is not about the emit boundary at all:
/// SPEC 3.1's absent-versus-empty.
#[test]
fn write_tier() {
    let dir = corpus_root().join("write");
    let mut cases = 0;
    for entry in read_dir_sorted(&dir) {
        let name = entry.file_name().to_string_lossy().into_owned();
        let Some(stem) = name.strip_suffix(".in.vnx") else {
            continue;
        };
        cases += 1;
        let input = std::fs::read(entry.path()).expect("the input reads");
        // A case carrying an attachment manifest writes a CONTAINER, so its
        // expected output is an archive rather than a document. See
        // `corpus/write/README.md`.
        let manifest = dir.join(format!("{stem}.attachments.json"));
        if manifest.exists() {
            write_container_case(&dir, stem, &input, &manifest);
            continue;
        }
        let expected = std::fs::read(dir.join(format!("{stem}.written.vnx")))
            .unwrap_or_else(|_| panic!("write/{stem}.in.vnx has no {stem}.written.vnx beside it"));

        // A case with no sidecar is a failure, not a skip, as everywhere else.
        let sidecar_bytes = std::fs::read(dir.join(format!("{stem}.expect.json")))
            .unwrap_or_else(|_| panic!("write/{stem} has no sidecar"));
        let sidecar = parse(&sidecar_bytes).expect("the sidecar parses");
        assert_eq!(
            sidecar.get("kind").and_then(Json::as_str),
            Some("write"),
            "write/{stem}: the sidecar's `kind` must match its directory"
        );
        assert!(
            sidecar
                .get("note")
                .and_then(Json::as_str)
                .is_some_and(|n| !n.is_empty()),
            "write/{stem}: the sidecar carries no `note`"
        );

        // Without this, a writer that echoed its input would pass every case.
        // Compared as TREES: two files that differ only in whitespace or key
        // order are the same document, so a byte comparison here would report
        // a case as discriminating when it is not.
        assert!(
            tree::compare(&input, &expected, EmitBoundary::Keep).is_some(),
            "write/{stem}.in.vnx is already the written form, so this case \
             cannot fail for a writer that echoes its input"
        );

        let reading = decode(&input).unwrap_or_else(|e| panic!("write/{stem}.in.vnx: {e}"));
        check_write_diagnostics(&sidecar, &reading.diagnostics, stem);
        let written = encode(&reading.document).expect("encodable");
        if let Some(difference) =
            tree::compare(written.as_bytes(), &expected, EmitBoundary::Keep)
        {
            panic!("write/{stem}: the written document is not the one the case expects: {difference}");
        }

        // The written form is a fixed point: shortening is idempotent, so a
        // case that fails this is a defect in the fixture rather than in the
        // writer.
        let again = encode(
            &decode(&expected)
                .unwrap_or_else(|e| panic!("write/{stem}.written.vnx: {e}"))
                .document,
        ).expect("encodable");
        if let Some(difference) = tree::compare(again.as_bytes(), &expected, EmitBoundary::Keep) {
            panic!("write/{stem}: the expected output is not a fixed point of the writer: {difference}");
        }
    }
    assert!(cases > 0, "the write tier found no cases");
}

/// Freezes the proof that the accept tier's check is worth what SPEC 8.1
/// claims for it.
///
/// ⚠ THIS IS WHY THE TIER COMPARES AGAINST THE ORIGINAL FILE AND NOT AGAINST
/// ANOTHER MODEL. A reader that discarded every data record, every application
/// key, every style or every `notes` produces a model that compares equal to itself
/// and passes a model-versus-model tier GREEN. That is the entire extensibility
/// mechanism, all styling and all user prose, silently droppable by a
/// "conforming" implementation.
///
/// Each case below is one of those readers, made by spoiling a decoded document
/// one thing at a time. If any of them stops being caught, the tier has gone
/// back to proving nothing about the model.
#[test]
fn a_reader_that_discards_what_it_read_is_caught() {
    let path = corpus_root().join("accept").join("example-network.vnx");
    let bytes = std::fs::read(&path).expect("the worked example reads");

    /// One reader that drops something it read.
    type Spoiler = (&'static str, fn(&mut Document));

    let spoilers: [Spoiler; 4] = [
        ("drops every data record", |d| {
            for w in d.waypoints.iter_mut().flatten() {
                w.data = None;
            }
            for n in d.networks.iter_mut().flatten() {
                n.data = None;
                for (_, arc) in n.arcs.iter_mut().flatten() {
                    arc.data = None;
                }
                for c in n.chains.iter_mut().flatten() {
                    c.data = None;
                }
                for p in n.points.iter_mut().flatten() {
                    p.data = None;
                }
            }
        }),
        ("drops every application key", |d| d.app_keys.clear()),
        ("drops every style", |d| {
            for n in d.networks.iter_mut().flatten() {
                for c in n.chains.iter_mut().flatten() {
                    c.style = None;
                }
            }
        }),
        ("drops every notes", |d| {
            for n in d.networks.iter_mut().flatten() {
                n.notes = None;
                for c in n.chains.iter_mut().flatten() {
                    c.notes = None;
                }
            }
        }),
    ];

    for (what, spoil) in spoilers {
        let mut document = decode(&bytes).expect("the worked example is accepted").document;
        // LIVENESS. A spoiler that changed nothing would be caught by nothing,
        // and would look exactly like a check that works.
        let before = document.clone();
        spoil(&mut document);
        assert_ne!(document, before, "the `{what}` spoiler changed nothing");

        let encoded = encode(&document).expect("encodable");
        assert!(
            tree::compare(encoded.as_bytes(), &bytes, EmitBoundary::Normalise).is_some(),
            "a reader that {what} reproduced the file — the accept tier cannot see it"
        );
    }

    // And the UNSPOILED document passes, or the four assertions above would
    // hold for a comparison that never agrees with anything.
    let document = decode(&bytes).expect("accepted").document;
    assert_eq!(
        tree::compare(encode(&document).expect("encodable").as_bytes(), &bytes, EmitBoundary::Normalise),
        None
    );
}

/// The tree comparison compares numbers BY VALUE, never by spelling.
///
/// ⚠ THIS IS THE ONE THAT BITES RUST HARDEST. `serde_json::Value` remembers
/// whether a literal parsed as an integer or a float and compares the two as
/// unequal, so a comparator built on its own `PartialEq` reports a difference
/// that does not exist. SPEC 4.2 makes the round trip value-level, and Go
/// spells an integral elevation `340` where Python's `json.dumps` spells it
/// `340.0`. Ruled across all six implementations on 2026-09-10, ahead of the
/// task that has each of them read every other's output.
#[test]
fn a_number_compares_by_value_and_not_by_spelling() {
    let integers = br#"{"vnx":"0.2.0","waypoints":[{"at":[1,2,340],"name":"a"}]}"#;
    let floats = br#"{"vnx":"0.2.0","waypoints":[{"at":[1.0,2.0,340.0],"name":"a"}]}"#;
    for emit in [EmitBoundary::Normalise, EmitBoundary::Keep] {
        assert_eq!(
            tree::compare(integers, floats, emit),
            None,
            "the two spellings of one document compared unequal under {emit:?}"
        );
    }

    // And a real difference is still seen, or the above passes vacuously.
    let different = br#"{"vnx":"0.2.0","waypoints":[{"at":[1,2,341],"name":"a"}]}"#;
    assert!(tree::compare(integers, different, EmitBoundary::Keep).is_some());
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
            // `format/` is still on disk and no tier reads it any more: the
            // format stopped constraining bytes (SPEC 8.1) and Phase 4 of the
            // simplification deletes the directory. It is listed so this guard
            // reports stray documents rather than that known one.
            if !matches!(
                tier.as_str(),
                "accept" | "reject" | "diagnose" | "format" | "write"
            ) {
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

/// Run every case in a tier, and report EVERY failure rather than the first.
///
/// A case that panics must not take the rest of the tier with it. One defect
/// reported as a total collapse tells you nothing about how much is actually
/// wrong, and it is the same shape as a red-proof that under-reports which
/// guards caught a defect — in both, a single event hides the state of
/// everything else. The JavaScript seat found a refusal inside its `format/`
/// tier taking 34 passing cases down with it; in Rust the equivalent is a
/// panicking assertion inside a loop, so each case runs inside `catch_unwind`.
fn run_tier(tier: &str, check: impl Fn(&Case) + std::panic::RefUnwindSafe) {
    let dir = corpus_root().join(tier);
    let mut cases = 0;
    let mut failures: Vec<String> = Vec::new();

    // NOTE: the panic hook is deliberately left alone. Suppressing it to tidy
    // the output was a defect: `set_hook` is GLOBAL and cargo runs tests in
    // parallel, so one tier silencing the hook while it worked swallowed the
    // failure messages of every other tier running at the same time — and a
    // tier that reports FAILED with no message is worse than a noisy one.
    // `catch_unwind` returns the payload, so nothing is needed here to collect
    // the messages.

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
        if let Err(panic) = std::panic::catch_unwind(|| check(&case)) {
            failures.push(describe_panic(&case.name, panic.as_ref()));
        }
    }

    assert!(cases > 0, "the {tier} tier found no cases");
    assert!(
        failures.is_empty(),
        "{} of {cases} {tier} case(s) failed:\n\n{}",
        failures.len(),
        failures.join("\n\n")
    );
}

fn describe_panic(case: &str, panic: &(dyn std::any::Any + Send)) -> String {
    let message = panic
        .downcast_ref::<String>()
        .map(String::as_str)
        .or_else(|| panic.downcast_ref::<&str>().copied())
        .unwrap_or("(a panic carrying no message)");
    // The case name is already the first thing every assertion writes, so it
    // is not repeated when it is there.
    if message.starts_with(case) {
        message.to_string()
    } else {
        format!("{case}: {message}")
    }
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
    let mut got: BTreeMap<String, usize> = BTreeMap::new();
    got.insert("folders".into(), document.folders.as_ref().map_or(0, Vec::len));
    got.insert("networks".into(), document.networks.as_ref().map_or(0, Vec::len));
    got.insert("routes".into(), document.routes.as_ref().map_or(0, Vec::len));
    got.insert("tracks".into(), document.tracks.as_ref().map_or(0, Vec::len));
    got.insert("waypoints".into(), document.waypoints.as_ref().map_or(0, Vec::len));
    got.insert("areas".into(), document.areas.as_ref().map_or(0, Vec::len));
    // The nested containers, addressed by the same JSON Pointer vocabulary
    // `positions` uses. A bare name is a top-level collection; a name starting
    // with `/` is a container inside one.
    got.extend(container_counts_by_pointer(document));

    for (collection, want) in counts {
        let want = want
            .as_f64()
            .unwrap_or_else(|| panic!("{}: a count is not a number", case.name))
            as usize;
        let found = got.get(collection.as_str()).copied().unwrap_or_else(|| {
            panic!(
                "{}: the sidecar counts {collection:?}, which is not a collection the document carries",
                case.name
            )
        });
        assert_eq!(
            found, want,
            "{}: {collection}: got {found}, want {want}",
            case.name
        );
    }
    // A sidecar MUST name every container its document carries non-empty. An
    // omitted container used to mean "unchecked", which let a decoder invent or
    // drop items in it with nothing to say so. Naming a zero is legal; failing
    // to name a NON-empty container is the error.
    for (collection, found) in &got {
        if *found == 0 {
            continue;
        }
        if !counts.iter().any(|(k, _)| k == collection) {
            panic!(
                "{}: the document carries {found} {collection} and the sidecar does not name that \
                 collection; an unnamed collection is unchecked, not empty",
                case.name
            );
        }
    }
}

/// The number of children of every NESTED container in the decoded document,
/// keyed by a JSON Pointer in the wire's own key names — the same vocabulary
/// `positions` uses.
///
/// ⚠ IT EXISTS BECAUSE `counts` COUNTED ONLY THE TOP-LEVEL ARRAYS. A reader
/// that dropped every route's arcs, or truncated an arc's pips, or lost every
/// chain in a network, changed nothing a sidecar could see in the diagnose
/// tier, which SPEC 8.1 exempts from the round-trip comparison. Measured: a
/// dangling link taking the rest of its chain with it passed the whole corpus
/// green before this existed.
///
/// It is a producer rather than a pointer resolver, for the reason
/// `resolve_position`'s counterpart in the Go runner is: one traversal answers
/// every pointer, so a case can pin a container deep inside a network without
/// the runner growing a second pointer parser.
///
/// ⚠ ARC AND NODE CONTAINERS ARE KEYED BY THEIR OWN KEY, NOT BY AN INDEX.
/// `/networks/0/arcs/arc-1/pips` — SPEC 3.3 makes `arcs` and `nodes` objects,
/// and an object has no order. This crate happens to hold them in a `Vec` in
/// document order, which is exactly the temptation: an index here would export
/// that storage choice as a conformance requirement.
fn container_counts_by_pointer(document: &Document) -> BTreeMap<String, usize> {
    let mut out = BTreeMap::new();
    for (i, network) in document.networks.iter().flatten().enumerate() {
        out.insert(
            format!("/networks/{i}/nodes"),
            network.nodes.as_ref().map_or(0, Vec::len),
        );
        out.insert(
            format!("/networks/{i}/arcs"),
            network.arcs.as_ref().map_or(0, Vec::len),
        );
        for (key, arc) in network.arcs.iter().flatten() {
            out.insert(
                format!("/networks/{i}/arcs/{}/pips", escape(key)),
                arc.pips.len(),
            );
        }
        out.insert(
            format!("/networks/{i}/chains"),
            network.chains.as_ref().map_or(0, Vec::len),
        );
        for (j, chain) in network.chains.iter().flatten().enumerate() {
            out.insert(format!("/networks/{i}/chains/{j}/walk"), chain.walk.len());
        }
        out.insert(
            format!("/networks/{i}/points"),
            network.points.as_ref().map_or(0, Vec::len),
        );
        out.insert(
            format!("/networks/{i}/areas"),
            network.areas.as_ref().map_or(0, Vec::len),
        );
        for (j, area) in network.areas.iter().flatten().enumerate() {
            out.insert(
                format!("/networks/{i}/areas/{j}/polygon"),
                area.polygon.len(),
            );
        }
    }
    for (i, route) in document.routes.iter().flatten().enumerate() {
        out.insert(format!("/routes/{i}/nodes"), route.nodes.as_ref().map_or(0, Vec::len));
        out.insert(format!("/routes/{i}/arcs"), route.arcs.as_ref().map_or(0, Vec::len));
        for (key, arc) in route.arcs.iter().flatten() {
            out.insert(format!("/routes/{i}/arcs/{}/pips", escape(key)), arc.pips.len());
        }
        out.insert(format!("/routes/{i}/walk"), route.walk.len());
    }
    for (i, track) in document.tracks.iter().flatten().enumerate() {
        out.insert(format!("/tracks/{i}/segments"), track.segments.len());
        for (j, segment) in track.segments.iter().enumerate() {
            out.insert(format!("/tracks/{i}/segments/{j}/pips"), segment.pips.len());
        }
    }
    for (i, area) in document.areas.iter().flatten().enumerate() {
        out.insert(format!("/areas/{i}/polygon"), area.polygon.len());
    }
    out
}

/// RFC 6901 escaping for a key that becomes one pointer token. Arc keys are
/// user-chosen strings (SPEC 4.4), so `/` and `~` in one are legal.
fn escape(key: &str) -> String {
    key.replace('~', "~0").replace('/', "~1")
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
        for (channel, reading) in [("lon", position.0), ("lat", position.1)] {
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

/// `attachments` pins the entry NAMES the archive yields beside the document,
/// and `attachment_contents` pins their BYTES.
///
/// Names alone pass a reader that returns every attachment empty, or one that
/// hands back the same buffer for all of them, so the contents are the half
/// that proves an attachment was actually read. `attachment_contents` is
/// UTF-8 text, and the comparison is against the attachment's bytes.
///
/// ⚠ THE CONTENTS ARE THE ATTACHMENTS THAT SURVIVED, NOT THE ARCHIVE'S
/// ENTRIES. `diagnose/container-unusable-method.vnz` carries an entry SPEC 7
/// has the reader drop with a diagnostic, and its sidecar lists only the
/// readable one — so this compares key-for-key against what the reader
/// returned. A runner expecting an entry for the dropped one would fail a
/// conforming reader.
fn check_attachments(sidecar: &Json, got: Option<&[vnz::Attachment]>, case: &Case) {
    if sidecar.get("attachments").is_none() && sidecar.get("attachment_contents").is_none() {
        return;
    }
    let got = got.unwrap_or_else(|| {
        panic!(
            "{}: `attachments` and `attachment_contents` are container-only fields",
            case.name
        )
    });

    if let Some(want) = sidecar.get("attachments").and_then(Json::as_array) {
        let want: BTreeSet<String> = want
            .iter()
            .map(|v| {
                v.as_str()
                    .unwrap_or_else(|| panic!("{}: an attachment path is not a string", case.name))
                    .to_string()
            })
            .collect();
        let names: BTreeSet<String> = got.iter().map(|a| a.path.clone()).collect();
        assert_eq!(names, want, "{}: attachments", case.name);
    }

    let Some(want) = sidecar.get("attachment_contents") else {
        return;
    };
    let want = want.as_object().unwrap_or_else(|| {
        panic!("{}: `attachment_contents` is not an object", case.name)
    });
    let want: BTreeMap<String, String> = want
        .iter()
        .map(|(name, body)| {
            let body = body.as_str().unwrap_or_else(|| {
                panic!(
                    "{}: the content of attachment {name:?} is not a string",
                    case.name
                )
            });
            (name.clone(), body.to_string())
        })
        .collect();
    let got: BTreeMap<String, String> = got
        .iter()
        .map(|a| {
            let text = String::from_utf8(a.bytes.clone()).unwrap_or_else(|_| {
                panic!(
                    "{}: attachment {:?} is not UTF-8, and a sidecar can only pin text",
                    case.name, a.path
                )
            });
            (a.path.clone(), text)
        })
        .collect();
    assert_eq!(got, want, "{}: attachment contents", case.name);
}

// ---------------------------------------------------------------------------
// Resolving a JSON Pointer against the DECODED MODEL
// ---------------------------------------------------------------------------

/// Walk a `positions` pointer through the model, in the wire's own key names,
/// and answer the longitude and latitude found there.
///
/// Deliberately not a walk over the raw JSON. Resolving against the file
/// compares the file to itself and cannot see a decoder that swapped two
/// channels, which is the entire point of the check.
fn resolve_position(document: &Document, pointer: &str) -> Option<(f64, f64)> {
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
        "routes" => {
            let route = document.routes.as_ref()?.get(index(1)?)?;
            graph_position(&route.nodes, &route.arcs, &tokens)
        }
        "networks" => network_position(document.networks.as_ref()?.get(index(1)?)?, &tokens),
        _ => None,
    }
}

fn lon_lat(p: &Position) -> (f64, f64) {
    (p.lon(), p.lat())
}

fn waypoint_at(waypoint: &Waypoint, key: &str) -> Option<(f64, f64)> {
    (key == "at").then(|| lon_lat(&waypoint.at))
}

fn ring_vertex(area: &Area, key: &str, index: usize) -> Option<(f64, f64)> {
    if key != "polygon" {
        return None;
    }
    area.polygon.get(index).map(lon_lat)
}

fn track_pip(track: &Track, tokens: &[String]) -> Option<(f64, f64)> {
    // /tracks/N/segments/S/pips/P
    if tokens.get(2)? != "segments" || tokens.get(4)? != "pips" {
        return None;
    }
    track
        .segments
        .get(tokens.get(3)?.parse::<usize>().ok()?)?
        .pips
        .get(tokens.get(5)?.parse::<usize>().ok()?)
        .map(lon_lat)
}

/// A node or an arc pip of a network or a route, keyed by its own key:
/// `/…/nodes/KEY` or `/…/arcs/KEY/pips/P`.
fn graph_position(
    nodes: &Option<Vec<(String, Vertex)>>,
    arcs: &Option<Vec<(String, Arc)>>,
    tokens: &[String],
) -> Option<(f64, f64)> {
    let key = tokens.get(3)?.as_str();
    let vertex = match tokens.get(2)?.as_str() {
        "nodes" => nodes.iter().flatten().find(|(k, _)| k == key)?.1,
        "arcs" => {
            if tokens.get(4)? != "pips" {
                return None;
            }
            *arcs
                .iter()
                .flatten()
                .find(|(k, _)| k == key)?
                .1
                .pips
                .get(tokens.get(5)?.parse::<usize>().ok()?)?
        }
        _ => return None,
    };
    Some((vertex.lon, vertex.lat))
}

fn network_position(network: &Network, tokens: &[String]) -> Option<(f64, f64)> {
    match tokens.get(2)?.as_str() {
        "nodes" | "arcs" => graph_position(&network.nodes, &network.arcs, tokens),
        // /networks/N/chains/C/walk/P/{from,to,turn}
        "chains" => {
            if tokens.get(4)? != "walk" {
                return None;
            }
            let pass = network
                .chains
                .as_ref()?
                .get(tokens.get(3)?.parse::<usize>().ok()?)?
                .walk
                .get(tokens.get(5)?.parse::<usize>().ok()?)?;
            let at = match tokens.get(6)?.as_str() {
                "from" | "to" => {
                    let endpoint = if tokens[6] == "from" { &pass.from } else { &pass.to };
                    match endpoint.as_ref()? {
                        Endpoint::At(p) => *p,
                        // A node key is not a position of its own; the corpus
                        // points at nodes through /nodes/KEY.
                        Endpoint::Node(_) => return None,
                    }
                }
                "turn" => pass.turn?,
                _ => return None,
            };
            Some((at.lon, at.lat))
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
/// compare its tree against the one this library encodes.
fn document_bytes_from_container(archive: &[u8], entry_name: &str) -> Vec<u8> {
    let archive = vnx::zip::Archive::open(archive).expect("the archive opens");
    let entry = archive
        .entries()
        .iter()
        .find(|e| e.name == entry_name)
        .expect("the chosen entry is in the archive");
    archive.read(entry).expect("the entry decompresses")
}

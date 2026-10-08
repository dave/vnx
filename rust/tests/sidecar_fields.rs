//! The sidecar field guard.
//!
//! A sidecar field this runner does not read is a field that quietly stops
//! being tested: the expectation sits in the corpus looking like coverage and
//! nothing compares it. Python's runner and Kotlin's have failed on one since
//! they were written; this one did not, and on 2026-09-13 a new `schema` field
//! landed in thirty sidecars and passed here in silence while both of those
//! went red.
//!
//! So the lists below are not documentation. They are the claim that every
//! field in every sidecar is either read by this runner or knowingly skipped,
//! and the two tests are what make the claim checkable.

use std::collections::BTreeSet;
use std::path::{Path, PathBuf};

use vnx::json::{parse, Json};

/// Fields this runner compares. `kind` and `note` are structural.
const CHECKED: &[&str] = &[
    "kind",
    "note",
    "counts",
    "positions",
    "tracks",
    "attachments",
    "attachment_contents",
    "root",
    "refusal",
    "diagnostics",
];

/// Fields this runner knowingly does not compare, and why.
const UNCHECKED: &[&str] = &[
    // A prose record of a limitation (SPEC 6.4), paired with an empty
    // `diagnostics` list which IS checked.
    "cannot_detect",
    // The verdict corpus/schema/vnx-0.10.schema.json reaches on this case. It is
    // about the schema rather than about any reader, so no implementation
    // runner compares it — go/internal/vnxschema is the one thing that does,
    // and it fails if a case records no verdict.
    "schema",
];

fn corpus_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("the crate sits inside the repository")
        .join("corpus")
}

fn sidecars() -> Vec<(String, Vec<String>)> {
    let mut out = Vec::new();
    for kind in ["accept", "reject", "diagnose", "write"] {
        let dir = corpus_root().join(kind);
        let Ok(entries) = std::fs::read_dir(&dir) else {
            continue;
        };
        let mut names: Vec<_> = entries
            .filter_map(|e| e.ok())
            .map(|e| e.file_name().to_string_lossy().into_owned())
            .filter(|n| n.ends_with(".expect.json"))
            .collect();
        names.sort();
        for name in names {
            let bytes =
                std::fs::read(dir.join(&name)).expect("a sidecar the directory just listed");
            let value = parse(&bytes).unwrap_or_else(|e| panic!("{name}: {e:?}"));
            let Json::Object(members) = value else {
                panic!("{name} is not a JSON object")
            };
            out.push((name, members.into_iter().map(|(k, _)| k).collect()));
        }
    }
    out
}

#[test]
fn the_runner_knows_every_sidecar_field() {
    let found = sidecars();
    // The control. A sweep that read no sidecars would report a clean result.
    assert!(
        found.len() >= 20,
        "read only {} sidecars — dead probe, not a clean sweep",
        found.len()
    );
    let known: BTreeSet<&str> = CHECKED.iter().chain(UNCHECKED).copied().collect();
    for (name, fields) in &found {
        for field in fields {
            assert!(
                known.contains(field.as_str()),
                "{name}: this runner does not know '{field}', so the expectation would pass without being compared",
            );
        }
    }
}

#[test]
fn no_declared_field_has_outlived_the_sidecars_that_carried_it() {
    // A stale entry is worse than a missing one: it makes the mechanism look
    // more thorough than it is, and nothing goes red when the field goes away.
    let mut used: BTreeSet<String> = BTreeSet::new();
    for (_, fields) in sidecars() {
        used.extend(fields);
    }
    for field in CHECKED.iter().chain(UNCHECKED) {
        assert!(
            used.contains(*field),
            "'{field}' is declared here and no sidecar carries it any more; delete the entry",
        );
    }
}

//! The number-conformance table (`corpus/numbers/numbers.tsv`).
//!
//! A fifth conformance tier in everything but its location. Rounding happens
//! only when a WRITER emits a model, and every other tier re-encodes what it
//! decoded — which applies rounding to values that already satisfy it. An
//! implementation can round every coordinate in the format the wrong way and
//! pass all of them green.
//!
//! ⚠ IT TESTS THE ROUNDING OPERATION, NOT A SPELLING. How a number is spelled
//! is the writer's own business (SPEC 4.2), and comparing spellings across
//! implementations is the rule this format dropped.
//!
//! ⚠ AND BY BIT PATTERN RATHER THAN BY TEXT. Comparing the rounded value as
//! text would put the rendering rule back into the comparison through the side
//! door, and would then pass or fail on how Rust spells a double.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

/// Two locations are tried, and finding NEITHER is a loud failure naming both
/// rather than a skip: a number-table test that skips when the file moves is a
/// green light with no bulb in it, and a move is exactly the event that would
/// switch it off.
fn table_path() -> PathBuf {
    let repo = Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("the crate sits inside the repository");
    let current = repo.join("corpus").join("numbers").join("numbers.tsv");
    let former = repo.join("number-conformance").join("numbers.tsv");
    for candidate in [&current, &former] {
        if candidate.is_file() {
            return candidate.clone();
        }
    }
    panic!(
        "the number-conformance table is at neither {} nor {}. It is the only \
         instrument that reaches SPEC 4.1's rounding, so this is a failure \
         rather than a skip.",
        current.display(),
        former.display()
    )
}

struct Row {
    line: usize,
    bits: u64,
    kind: String,
    /// The reference rounding to six decimal places, as the double the table's
    /// decimal text names.
    round6: f64,
    /// The same, to one decimal place.
    round1: f64,
}

impl Row {
    fn value(&self) -> f64 {
        f64::from_bits(self.bits)
    }
}

fn rows() -> Vec<Row> {
    let path = table_path();
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|e| panic!("{} will not read: {e}", path.display()));
    let mut out = Vec::new();
    for (index, line) in text.lines().enumerate() {
        if line.starts_with('#') || line.trim().is_empty() {
            continue;
        }
        let fields: Vec<&str> = line.split('\t').collect();
        assert_eq!(
            fields.len(),
            4,
            "line {} has {} fields, not four: {line:?}",
            index + 1,
            fields.len()
        );
        // The header row names the columns; skip it rather than parse it.
        if fields[0] == "bits" {
            continue;
        }
        let number = |column: usize| -> f64 {
            fields[column].parse().unwrap_or_else(|e| {
                panic!(
                    "line {}: column {column} is {:?}, which is not a number: {e}",
                    index + 1,
                    fields[column]
                )
            })
        };
        out.push(Row {
            line: index + 1,
            bits: u64::from_str_radix(fields[0], 16)
                .unwrap_or_else(|e| panic!("line {}: {:?} is not hex: {e}", index + 1, fields[0])),
            kind: fields[1].to_string(),
            round6: number(2),
            round1: number(3),
        });
    }
    // LIVENESS. A table that failed to load, or a loop that skipped every row,
    // is the same green as agreement on all of them. The header declares what
    // the body must hold, and the generator refuses to write one that disagrees
    // with it.
    let (declared_rows, declared_counts) = header();
    assert_eq!(
        out.len(),
        declared_rows,
        "the body and the header disagree on the row count"
    );
    assert!(
        !declared_counts.is_empty(),
        "the header declares no per-kind counts"
    );
    let mut seen: BTreeMap<String, usize> = BTreeMap::new();
    for row in &out {
        *seen.entry(row.kind.clone()).or_default() += 1;
    }
    assert_eq!(
        seen, declared_counts,
        "the body and the header disagree on the kinds present"
    );
    out
}

/// The row count and the per-kind counts the header declares.
///
/// A hardcoded list of the categories this file depends on ages badly, and the
/// newest category is always the one nothing pins. Reading the header instead
/// means a category that vanishes is a failure rather than a silence.
fn header() -> (usize, BTreeMap<String, usize>) {
    let path = table_path();
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|e| panic!("{} will not read: {e}", path.display()));
    let mut rows = 0;
    let mut counts = BTreeMap::new();
    for line in text.lines() {
        let Some(rest) = line.strip_prefix('#') else {
            break;
        };
        if let Some(n) = rest.trim_start().strip_prefix("rows=") {
            rows = n.trim().parse().expect("the header's row count is a number");
        }
        if let Some(list) = rest.trim_start().strip_prefix("counts:") {
            for pair in list.split_whitespace() {
                let (kind, n) = pair.split_once('=').expect("a counts entry is kind=count");
                counts.insert(
                    kind.to_string(),
                    n.parse().expect("a counts entry holds a number"),
                );
            }
        }
    }
    (rows, counts)
}

/// SPEC 4.1: longitude and latitude are rounded to six decimal places.
#[test]
fn rounding_to_six_places_agrees_with_the_table() {
    check(6, |row| row.round6);
}

/// SPEC 4.1: elevation is rounded to one decimal place.
#[test]
fn rounding_to_one_place_agrees_with_the_table() {
    check(1, |row| row.round1);
}

/// Compare one column, BY BIT PATTERN.
///
/// -0 and 0 are different bit patterns and the same value. SPEC 4.1 has
/// rounding normalise the sign, and the table gives `0` on every row that
/// reaches it, so a mismatch here is real rather than a spelling difference.
fn check(places: usize, want: fn(&Row) -> f64) {
    let rows = rows();
    let mut mismatches = Vec::new();
    for row in &rows {
        let got = vnx::encode::round_for_test(row.value(), places);
        let expected = want(row);
        if got.to_bits() != expected.to_bits() {
            mismatches.push(format!(
                "line {}: {:016x} ({}) rounds to {got} ({:016x}) and the table says {expected} ({:016x})",
                row.line,
                row.bits,
                row.kind,
                got.to_bits(),
                expected.to_bits()
            ));
        }
    }
    if mismatches.is_empty() {
        return;
    }
    let shown: Vec<&str> = mismatches.iter().take(20).map(String::as_str).collect();
    panic!(
        "{} of {} rows disagree at {places} decimal place(s):\n  {}{}",
        mismatches.len(),
        rows.len(),
        shown.join("\n  "),
        if mismatches.len() > shown.len() {
            format!("\n  … and {} more", mismatches.len() - shown.len())
        } else {
            String::new()
        }
    );
}

/// The table has to contain rows that DISCRIMINATE, or agreeing with it proves
/// nothing. An undiscriminating table is the failure that would not announce
/// itself.
#[test]
fn the_table_discriminates_the_rounding_tie_rule() {
    let rows = rows();
    for kind in ["round1-tie", "round6-tie"] {
        assert!(
            rows.iter().any(|r| r.kind == kind),
            "the table carries no `{kind}` rows"
        );
    }

    // A rounding tie must be a row where half-away-from-zero — which is what
    // Rust's own `f64::round` does — gives a different answer from the one the
    // table asks for.
    let separating = rows
        .iter()
        .filter(|r| r.kind == "round1-tie")
        .filter(|r| {
            let away = (r.value() * 10.0).round() / 10.0;
            away.to_bits() != r.round1.to_bits()
        })
        .count();
    assert!(
        separating > 0,
        "no `round1-tie` row differs under half-away-from-zero, so agreeing \
         with the round1 column proves nothing about the tie rule"
    );
}

/// The table also settles a question SPEC 4.1's prose leaves open, and this
/// records that it does.
///
/// "Round to N decimal places, ties to even" is two different operations:
/// rounding against the DOUBLE, or scaling by 10^N and rounding the product.
/// Scaling is itself a rounding step and moves values onto an exact half that
/// were never on one — `0.15` is nearest `0.1` and comes out `0.2` that way.
///
/// The table's answers come from Go, so it decides which of the two the format
/// means. This measures how many rows discriminate. If that count is ever zero,
/// the table has stopped being able to see the difference and this crate's
/// choice would be unverified again.
#[test]
fn the_table_settles_rounding_against_the_double_rather_than_the_scaled_product() {
    let rows = rows();
    let mut discriminating = 0;
    let mut exact_agrees = 0;
    let mut overflowed = 0;
    for row in &rows {
        let value = row.value();
        if !(value * 10.0).is_finite() {
            // Scaling overflows near the top of the range, where rounding
            // against the double does not. A third way the scaled route is a
            // different operation, and one that would panic rather than
            // disagree.
            overflowed += 1;
            continue;
        }
        let scaled = (value * 10.0).round_ties_even() / 10.0;
        let exact = vnx::encode::round_for_test(value, 1);
        if scaled.to_bits() != exact.to_bits() {
            discriminating += 1;
            if exact.to_bits() == row.round1.to_bits() {
                exact_agrees += 1;
            }
        }
    }
    assert!(
        discriminating > 0,
        "no row separates rounding against the double from scaling first, so \
         the table cannot settle which SPEC 4.1 means"
    );
    assert_eq!(
        exact_agrees, discriminating,
        "{exact_agrees} of {discriminating} discriminating rows side with \
         rounding against the double; the rest side with scaling first, which \
         would mean SPEC 4.1 does not mean what this crate implements"
    );
    // Reported, so a reader of the test output sees the size of the gap.
    println!(
        "{discriminating} of {} rows separate the two readings of SPEC 4.1, and \
         all {discriminating} side with rounding against the double; \
         {overflowed} more overflow when scaled",
        rows.len()
    );
}

//! The number-conformance table (`number-conformance/numbers.tsv`).
//!
//! A fifth conformance tier in everything but its location. It exists because
//! two rules in `SPEC.md` decide bytes and the corpus can reach neither:
//! a rendering tie is a value rather than a shape, and rounding happens only
//! when a WRITER emits a model, while all four corpus tiers re-render an
//! existing document. An implementation can round every coordinate in the
//! format the wrong way and pass every corpus tier green.
//!
//! The answers come from Go's `strconv` and `encoding/json`, which SPEC 4.5.3
//! names as the reference. This crate's job is to agree with them.

use std::path::{Path, PathBuf};

use vnx::json::number;

fn table_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("the crate sits inside the repository")
        .join("number-conformance")
        .join("numbers.tsv")
}

struct Row {
    line: usize,
    bits: u64,
    kind: String,
    render: String,
    round6: String,
    round1: String,
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
            5,
            "line {} has {} fields, not five: {line:?}",
            index + 1,
            fields.len()
        );
        // The header row names the columns; skip it rather than parse it.
        if fields[0] == "bits" {
            continue;
        }
        out.push(Row {
            line: index + 1,
            bits: u64::from_str_radix(fields[0], 16)
                .unwrap_or_else(|e| panic!("line {}: {:?} is not hex: {e}", index + 1, fields[0])),
            kind: fields[1].to_string(),
            render: fields[2].to_string(),
            round6: fields[3].to_string(),
            round1: fields[4].to_string(),
        });
    }
    assert!(
        out.len() > 5000,
        "the table came back with only {} rows; the committed one is 5,315",
        out.len()
    );
    out
}

/// SPEC 4.5.3: the canonical spelling of a double.
#[test]
fn rendering_agrees_with_the_table() {
    let mut mismatches = Vec::new();
    for row in rows() {
        let value = f64::from_bits(row.bits);
        let ours = number::render(value);
        if ours != row.render {
            mismatches.push(format!(
                "line {}: {:016x} ({}) renders {ours} and the table says {}",
                row.line, row.bits, row.kind, row.render
            ));
        }
    }
    report("render", &mismatches);
}

/// SPEC 4.5.4: rounded to six places, which is longitude and latitude, then
/// spelled canonically.
#[test]
fn rounding_to_six_places_agrees_with_the_table() {
    let mut mismatches = Vec::new();
    for row in rows() {
        let value = f64::from_bits(row.bits);
        let ours = number::render(vnx::render::round_for_test(value, 6));
        if ours != row.round6 {
            mismatches.push(format!(
                "line {}: {:016x} ({}) rounds to {ours} and the table says {}",
                row.line, row.bits, row.kind, row.round6
            ));
        }
    }
    report("round6", &mismatches);
}

/// SPEC 4.5.4: rounded to one place, which is elevation.
#[test]
fn rounding_to_one_place_agrees_with_the_table() {
    let mut mismatches = Vec::new();
    for row in rows() {
        let value = f64::from_bits(row.bits);
        let ours = number::render(vnx::render::round_for_test(value, 1));
        if ours != row.round1 {
            mismatches.push(format!(
                "line {}: {:016x} ({}) rounds to {ours} and the table says {}",
                row.line, row.bits, row.kind, row.round1
            ));
        }
    }
    report("round1", &mismatches);
}

/// The table has to contain rows that discriminate, or agreeing with it proves
/// nothing. An undiscriminating table is the failure that would not announce
/// itself, so this asserts the tie categories are present and that they really
/// do differ from rounding away from zero.
#[test]
fn the_table_discriminates_the_two_tie_rules() {
    let rows = rows();
    for kind in ["render-tie", "round1-tie", "round6-tie"] {
        let count = rows.iter().filter(|r| r.kind == kind).count();
        assert!(count > 0, "the table carries no `{kind}` rows");
    }

    // A render tie must be a row where resolving away from zero gives a
    // different spelling from the one the table asks for.
    let render_ties = rows
        .iter()
        .filter(|r| r.kind == "render-tie")
        .filter(|r| {
            let value = f64::from_bits(r.bits);
            // Rust's own `{}` resolves a tie AWAY from zero.
            format!("{value}") != r.render
        })
        .count();
    assert!(
        render_ties > 0,
        "no `render-tie` row differs from Rust's own away-from-zero spelling, \
         so agreeing with the render column proves nothing about the tie rule"
    );

    // A rounding tie must be a row where half-away-from-zero differs.
    let round1_ties = rows
        .iter()
        .filter(|r| r.kind == "round1-tie")
        .filter(|r| {
            let value = f64::from_bits(r.bits);
            let away = (value * 10.0).round() / 10.0;
            number::render(away) != r.round1
        })
        .count();
    assert!(
        round1_ties > 0,
        "no `round1-tie` row differs under half-away-from-zero, so agreeing \
         with the round1 column proves nothing about the rounding tie rule"
    );
}

/// The table also settles a question SPEC 4.5.4's prose leaves open, and this
/// records that it does.
///
/// "Round to N decimal places, ties to even" is two different operations:
/// rounding against the DOUBLE, or scaling by 10^N and rounding the product.
/// Scaling is itself a rounding step and moves values onto an exact half that
/// were never on one — `0.15` is nearest `0.1` and comes out `0.2` that way.
///
/// The table's answers come from Go, so it decides which of the two the format
/// means. This test measures how many rows discriminate. If that count is ever
/// zero, the table has stopped being able to see the difference and this crate's
/// choice would be unverified again.
#[test]
fn the_table_settles_rounding_against_the_double_rather_than_the_scaled_product() {
    let rows = rows();
    let mut discriminating = 0;
    let mut exact_agrees = 0;
    let mut overflowed = 0;
    for row in &rows {
        let value = f64::from_bits(row.bits);
        if !(value * 10.0).is_finite() {
            // Scaling overflows near the top of the range, where rounding
            // against the double does not. A third way the scaled spelling is
            // a different operation, and one that would panic rather than
            // disagree.
            overflowed += 1;
            continue;
        }
        let scaled = number::render((value * 10.0).round_ties_even() / 10.0);
        let exact = number::render(vnx::render::round_for_test(value, 1));
        if scaled != exact {
            discriminating += 1;
            if exact == row.round1 {
                exact_agrees += 1;
            }
        }
    }
    assert!(
        discriminating > 0,
        "no row separates rounding against the double from scaling first, so the \
         table cannot settle which SPEC 4.5.4 means"
    );
    assert_eq!(
        exact_agrees, discriminating,
        "{} of {discriminating} discriminating rows side with rounding against \
         the double; the rest side with scaling first, which would mean SPEC \
         4.5.4 does not mean what this crate implements",
        exact_agrees
    );
    // Reported, so a reader of the test output sees the size of the gap.
    println!(
        "{discriminating} of {} rows separate the two readings of SPEC 4.5.4, \
         and all {discriminating} side with rounding against the double; \
         {overflowed} more overflow when scaled",
        rows.len()
    );
}

fn report(column: &str, mismatches: &[String]) {
    if mismatches.is_empty() {
        return;
    }
    let shown: Vec<&String> = mismatches.iter().take(20).collect();
    panic!(
        "{} of {} rows disagree in the `{column}` column:\n  {}{}",
        mismatches.len(),
        rows().len(),
        shown
            .iter()
            .map(|s| s.as_str())
            .collect::<Vec<_>>()
            .join("\n  "),
        if mismatches.len() > shown.len() {
            format!("\n  … and {} more", mismatches.len() - shown.len())
        } else {
            String::new()
        }
    );
}

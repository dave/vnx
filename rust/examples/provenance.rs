//! Provenance for the counts this crate quotes about `corpus/numbers/`.
//!
//! `cargo run --example provenance`
//!
//! A number that does not say which artefact and which constant it was measured
//! under outlives both, so the measurement is a script rather than a sentence.
//! It reads the committed table and uses this crate's own `round_for_test`, so
//! it cannot drift from what the library actually does. The two roundings are
//! compared BY BIT PATTERN: comparing spellings would put a rendering rule the
//! format no longer has back into the measurement.
//!
//! **The counts are not written down here.** Run it. The table's contents are
//! chosen and are changed by editing `makenumbers`, so a figure embedded in
//! this comment is stale the next time somebody adds a row. A measurement
//! belongs in the thing that measures.
//!
//! What IS stable is the structure, and it is the part worth quoting:
//!
//! * "differ" counts exact-decimal rounding against scale-by-10^n-then-round-
//!   ties-even. Rows whose scaled product overflows to infinity are counted
//!   BESIDE that, not inside it: there is no spelling to compare.
//! * At one decimal place the differing rows are `scaling-route`,
//!   `negative-zero` and `round6-tie` — and **not one `round1-tie`**. The
//!   deliberately-constructed tie rows separate a tie DIRECTION and do not
//!   separate the choice of OPERATION at all. Ordinary values do that.
//!
//! That second point is the same shape as `write/emit-boundary` being unable to
//! catch scale-then-round: a fixture built to exercise ties exercises ties, and
//! the larger defect sitting beside them needs different data entirely.

use std::collections::BTreeMap;
fn main() {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../corpus/numbers/numbers.tsv");
    let text = std::fs::read_to_string(path).expect("the table reads");
    let mut total = 0usize;
    for places in [1usize, 6usize] {
        let mut differ: BTreeMap<String, usize> = BTreeMap::new();
        let mut overflow: BTreeMap<String, usize> = BTreeMap::new();
        let mut agree = 0usize;
        let mut n = 0usize;
        for line in text.lines() {
            if line.starts_with('#') || line.trim().is_empty() { continue; }
            let f: Vec<&str> = line.split('\t').collect();
            if f[0] == "bits" { continue; }
            n += 1;
            let v = f64::from_bits(u64::from_str_radix(f[0], 16).unwrap());
            let kind = f[1].to_string();
            let scale = 10f64.powi(places as i32);
            if !(v * scale).is_finite() {
                *overflow.entry(kind).or_default() += 1;
                continue;
            }
            let exact = vnx::encode::round_for_test(v, places).to_bits();
            let scaled = ((v * scale).round_ties_even() / scale).to_bits();
            if exact != scaled { *differ.entry(kind).or_default() += 1; } else { agree += 1; }
        }
        total = n;
        let d: usize = differ.values().sum();
        let o: usize = overflow.values().sum();
        println!("places={places}  rows={n}  differ={d}  agree={agree}  overflow-when-scaled={o}  (d+agree+o={})", d + agree + o);
        println!("           differ by kind: {differ:?}");
        println!("        overflow by kind: {overflow:?}");
    }
    println!("total rows = {total}");
}

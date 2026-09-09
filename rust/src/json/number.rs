//! ECMAScript `Number::toString` for a finite `f64` (SPEC 4.5.3).
//!
//! Rust's `{}` for `f64` is also a shortest-round-trip formatter, but it never
//! uses exponent notation: it renders `1e21` as twenty-two digits and `1e-7` as
//! `0.0000001`, where ECMAScript renders `1e+21` and `1e-7`. Rust's `{:e}` uses
//! exponent notation always, and spells the exponent `e21` where ECMAScript
//! spells it `e+21`. Neither is the required rendering, so the choice of form
//! is made here.
//!
//! # The tie
//!
//! Rust supplies the shortest digit string that round-trips, which is the hard
//! half. It does NOT supply the digit SPEC 4.5.3 requires when that string is a
//! tie. Measured on this machine, rustc 1.95.0:
//!
//! ```text
//!   1497898211722073.25    Rust `{}`  1497898211722073.3
//!                          SPEC 4.5.3 1497898211722073.2   (round to even)
//! ```
//!
//! Rust resolves a tie away from zero. SPEC 4.5.3 requires round-to-even,
//! anchored on what the other implementations do, so this module detects a tie
//! and corrects it. About one double in 4,200 is a tie, concentrated between
//! 1e13 and 1e15 — which is where `time_ms` lives, so this is not a corner.
//!
//! The detection is EXACT integer arithmetic, not a round-trip probe. Asking
//! "does the neighbouring spelling also round-trip to this double" finds
//! phantoms: above 2^49 the spacing between doubles exceeds 0.1, so several
//! spellings map to one double and only one of them is nearest.

/// Render a finite `f64` exactly as ECMAScript `Number::toString` would, with
/// a tie resolved to even and negative zero collapsed to `0` (SPEC 4.5.3).
///
/// # Panics
///
/// Panics on a non-finite value. Nothing in this crate constructs one: the
/// parser refuses `NaN` and `Infinity` literals as malformed JSON and refuses
/// a decimal literal that overflows binary64.
pub fn render(value: f64) -> String {
    assert!(value.is_finite(), "VNX carries no non-finite number");

    // SPEC 4.5.3: negative zero is rendered `0`. Taken before the sign, so
    // `-0.0` never reaches the digit path carrying a minus. A writer relies on
    // this: rounding can CREATE a negative zero that was never in the model
    // (SPEC 4.5.4), and the renderer is what decides the final bytes.
    if value == 0.0 {
        return "0".to_string();
    }

    let negative = value < 0.0;
    let (digits, scale) = shortest_digits(value.abs());
    let k = digits.len() as i32;
    // ECMAScript's `n`: the value is `0.digits * 10^n`.
    let n = scale + k;

    let mut out = String::with_capacity(digits.len() + 8);
    if negative {
        out.push('-');
    }

    // ECMAScript Number::toString steps 6 to 10, with `s` spelled `digits`.
    if k <= n && n <= 21 {
        // 1778914991000 -> "1778914991" then three zeros.
        out.push_str(&digits);
        for _ in 0..(n - k) {
            out.push('0');
        }
    } else if 0 < n && n <= 21 {
        // 3.6712345678901235
        out.push_str(&digits[..n as usize]);
        out.push('.');
        out.push_str(&digits[n as usize..]);
    } else if -6 < n && n <= 0 {
        // 0.000001
        out.push_str("0.");
        for _ in 0..(-n) {
            out.push('0');
        }
        out.push_str(&digits);
    } else {
        // 1e-7, 1e+21
        if k == 1 {
            out.push_str(&digits);
        } else {
            out.push_str(&digits[..1]);
            out.push('.');
            out.push_str(&digits[1..]);
        }
        out.push('e');
        let exponent = n - 1;
        if exponent >= 0 {
            out.push('+');
        } else {
            out.push('-');
        }
        out.push_str(&exponent.abs().to_string());
    }

    out
}

/// The shortest round-trip decimal digits of a positive finite `f64`, with the
/// tie resolved to even, and the power of ten they scale by: the value is
/// `digits * 10^scale`.
fn shortest_digits(value: f64) -> (String, i32) {
    debug_assert!(value > 0.0 && value.is_finite());
    let (mut digits, mut scale) = rust_shortest_digits(value);

    // The digits are the shortest spelling; the only question left is whether
    // the last one is the right one. Only an odd last digit can be wrong,
    // because round-to-even and round-away agree whenever the answer is even.
    let last_is_odd = digits
        .as_bytes()
        .last()
        .is_some_and(|b| (b - b'0') % 2 == 1);
    if last_is_odd {
        let d: u128 = digits.parse().expect("a digit run parses");
        // A tie sits exactly at the midpoint of two consecutive candidates.
        // `d` is one of the pair; the other is `d-1` or `d+1`, and the
        // midpoints of those pairs are `2d-1` and `2d+1`.
        let even = if is_midpoint(value, 2 * d - 1, scale) {
            Some(d - 1)
        } else if is_midpoint(value, 2 * d + 1, scale) {
            Some(d + 1)
        } else {
            None
        };
        if let Some(even) = even {
            debug_assert!(even % 2 == 0, "the corrected digit must be the even one");
            digits = even.to_string();
            // `999 + 1` grows a digit and `100` should not keep its zeros:
            // normalise so the pair (digits, scale) stays canonical.
            while digits.len() > 1 && digits.ends_with('0') {
                digits.pop();
                scale += 1;
            }
        }
    }

    (digits, scale)
}

/// The digits and scale Rust's own shortest-round-trip formatter produces.
///
/// `{:e}` renders `d[.ddd]e<exp>`, meaning `d.ddd * 10^exp`, so with `k`
/// digits the value is `digits * 10^(exp - k + 1)`.
fn rust_shortest_digits(value: f64) -> (String, i32) {
    let text = format!("{value:e}");
    let (mantissa, exponent) = text
        .split_once('e')
        .expect("Rust's LowerExp for f64 always writes an exponent");
    let digits: String = mantissa.chars().filter(|c| *c != '.').collect();
    let exponent: i32 = exponent
        .parse()
        .expect("Rust's LowerExp for f64 writes a decimal exponent");
    debug_assert!(
        digits.bytes().all(|b| b.is_ascii_digit()),
        "a mantissa carries only digits and a point"
    );
    let scale = exponent - (digits.len() as i32 - 1);
    (digits, scale)
}

/// Is `value` exactly `half * 10^scale / 2`?
///
/// That is the midpoint of the two consecutive decimal candidates whose sum is
/// `half`. Written as an exact integer comparison:
///
/// ```text
///   value == half * 10^scale / 2
///   m * 2^e * 2 == half * 10^scale            with value = m * 2^e exactly
///   m * 2^(e+1-scale) == half * 5^scale
/// ```
///
/// `half` is odd by construction, so the right side's odd part is `half`
/// times a power of five and the left side's is the odd part of `m`, which is
/// below 2^53. Any growth past that cannot be an equality, which is why
/// bailing out on overflow is sound rather than a fallback.
fn is_midpoint(value: f64, half: u128, scale: i32) -> bool {
    debug_assert!(half % 2 == 1, "a midpoint numerator is odd");
    let (m, e) = mantissa_and_exponent(value);
    exactly_equal(m, e + 1 - scale, half, scale)
}

/// Is `a * 2^a2 == b * 5^b5`? Both sides exact; `false` on any overflow.
fn exactly_equal(a: u128, a2: i32, b: u128, b5: i32) -> bool {
    // Move the power of five onto whichever side keeps it a multiplication.
    let (mut left, mut right, shift) = if b5 >= 0 {
        let Some(scaled) = checked_pow5(b, b5) else {
            return false;
        };
        (a, scaled, a2)
    } else {
        let Some(scaled) = checked_pow5(a, -b5) else {
            return false;
        };
        (scaled, b, a2)
    };
    // Now compare `left * 2^shift` against `right`.
    if shift >= 0 {
        let Some(shifted) = checked_shl(left, shift) else {
            return false;
        };
        left = shifted;
    } else {
        let Some(shifted) = checked_shl(right, -shift) else {
            return false;
        };
        right = shifted;
    }
    left == right
}

fn checked_pow5(value: u128, exponent: i32) -> Option<u128> {
    let mut out = value;
    for _ in 0..exponent {
        out = out.checked_mul(5)?;
    }
    Some(out)
}

fn checked_shl(value: u128, bits: i32) -> Option<u128> {
    if bits >= 128 {
        return None;
    }
    value.checked_mul(1u128.checked_shl(bits as u32)?)
}

/// Split a positive finite `f64` into the exact integers `m` and `e` for which
/// the value is `m * 2^e`.
fn mantissa_and_exponent(value: f64) -> (u128, i32) {
    let bits = value.to_bits();
    let biased = ((bits >> 52) & 0x7ff) as i32;
    let fraction = bits & ((1u64 << 52) - 1);
    if biased == 0 {
        // Subnormal: no implicit leading bit.
        (fraction as u128, -1074)
    } else {
        ((fraction | (1u64 << 52)) as u128, biased - 1075)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The four cases SPEC 4.5.3 tabulates, plus the two the `format/` tier
    /// pins, plus negative zero.
    #[test]
    fn spec_table() {
        assert_eq!(render(340.0), "340");
        assert_eq!(render(0.000001), "0.000001");
        assert_eq!(render(1e-7), "1e-7");
        assert_eq!(render(1778914991000.0), "1778914991000");
        assert_eq!(render(1e21), "1e+21");
        assert_eq!(render(-3.6712345678901235), "-3.6712345678901235");
        assert_eq!(render(-0.0), "0");
        assert_eq!(render(0.0), "0");
    }

    /// SPEC 4.5.3: a tie rounds to EVEN. `corpus/format/number-tie` pins this
    /// one value; this test pins the mechanism, including the direction Rust
    /// gets wrong.
    #[test]
    fn a_tie_rounds_to_even() {
        // Written as .25 because that is the value EXACTLY; clippy objects
        // because Rust's own shortest spelling of this double is .3, which is
        // the whole subject of this test.
        #[allow(clippy::excessive_precision)]
        let tie = 1497898211722073.25_f64;
        assert_eq!(
            format!("{tie}"),
            "1497898211722073.3",
            "Rust resolves this tie away from zero; if that has changed, the \
             correction in this module needs revisiting rather than deleting"
        );
        assert_eq!(render(tie), "1497898211722073.2");
        assert_eq!(render(-tie), "-1497898211722073.2");
    }

    /// A number that is NOT a tie must not be corrected. Above 2^49 the gap
    /// between doubles exceeds 0.1, so a neighbouring spelling can round-trip
    /// without being equidistant — the phantom a round-trip probe would chase.
    #[test]
    fn a_near_miss_is_not_a_tie() {
        for value in [
            1497898211722073.5_f64,
            1497898211722071.0_f64,
            340.3_f64,
            0.3_f64,
            0.7_f64,
            1e15 + 1.0,
        ] {
            let expected = format!("{value}");
            assert_eq!(
                render(value),
                expected,
                "{value:e} was corrected and is not a tie"
            );
        }
    }

    /// Sweep a band where ties are common and require every correction to be
    /// to an even last digit, to still round-trip, and to be no longer than
    /// what Rust produced.
    #[test]
    fn every_correction_is_even_and_still_round_trips() {
        let mut corrections = 0;
        let mut state = 0x2545_f491_4f6c_dd1d_u64;
        for _ in 0..200_000 {
            state ^= state >> 12;
            state ^= state << 25;
            state ^= state >> 27;
            // The binade [2^50, 2^51), where a double is a multiple of 0.25
            // and the shortest spelling has sixteen digits. That is where SPEC
            // 4.5.3 says ties cluster, and an integer band produces none at
            // all, because an integer's shortest spelling is itself.
            let value = 1_125_899_906_842_624.0
                + (state % 4_503_599_627_370_496) as f64 * 0.25;
            let ours = render(value);
            let rust = format!("{value}");
            let back: f64 = ours.parse().expect("a rendering parses");
            assert_eq!(back.to_bits(), value.to_bits(), "{ours} for {value:e}");
            if ours != rust {
                corrections += 1;
                let last = ours
                    .bytes()
                    .rfind(u8::is_ascii_digit)
                    .expect("a rendering carries digits");
                assert_eq!((last - b'0') % 2, 0, "{ours} is not even");
                assert_eq!(
                    ours.bytes().filter(u8::is_ascii_digit).count(),
                    rust.bytes().filter(u8::is_ascii_digit).count(),
                    "a correction changed the digit count: {rust} -> {ours}"
                );
            }
        }
        assert!(
            corrections > 0,
            "this band produced no ties at all, so the correction path is untested here"
        );
    }

    /// The exponent boundaries, from both sides. These are where Rust's own
    /// formatting differs from ECMAScript's, so a boundary that moves by one
    /// shows up here rather than in a corpus file.
    #[test]
    fn exponent_boundaries() {
        assert_eq!(render(1e20), "100000000000000000000");
        assert_eq!(render(1e21), "1e+21");
        assert_eq!(render(1.2e21), "1.2e+21");
        assert_eq!(render(123456789012345680000.0), "123456789012345680000");
        assert_eq!(render(0.00001), "0.00001");
        assert_eq!(render(1e-6), "0.000001");
        assert_eq!(render(1e-7), "1e-7");
        assert_eq!(render(1.5e-7), "1.5e-7");
        assert_eq!(render(-1e-7), "-1e-7");
    }

    #[test]
    fn extremes() {
        assert_eq!(render(f64::MIN_POSITIVE), "2.2250738585072014e-308");
        assert_eq!(render(5e-324), "5e-324");
        assert_eq!(render(f64::MAX), "1.7976931348623157e+308");
        assert_eq!(render(-f64::MAX), "-1.7976931348623157e+308");
    }

    #[test]
    fn integers_stay_integers() {
        for v in [1.0, -1.0, 42.0, 100.0, 1e15, 9007199254740992.0] {
            let text = render(v);
            assert!(!text.contains('.'), "{v} rendered as {text}");
            assert!(!text.contains('e'), "{v} rendered as {text}");
        }
    }

    #[test]
    #[should_panic(expected = "non-finite")]
    fn refuses_infinity() {
        render(f64::INFINITY);
    }

    /// Every rendering must parse back to the same bits. This is the property
    /// the whole format rests on: SPEC 4.2 defines round-trip at value level.
    #[test]
    fn round_trips_over_a_wide_sample() {
        let mut state = 0x2545_f491_4f6c_dd1d_u64;
        for _ in 0..200_000 {
            // xorshift64*, so the sample is reproducible without a dependency.
            state ^= state >> 12;
            state ^= state << 25;
            state ^= state >> 27;
            let bits = state.wrapping_mul(0x2545_f491_4f6c_dd1d);
            let value = f64::from_bits(bits);
            if !value.is_finite() {
                continue;
            }
            let text = render(value);
            let back: f64 = text.parse().expect("a rendering parses");
            assert_eq!(
                back.to_bits(),
                value.to_bits(),
                "{value:e} rendered as {text} and parsed back as {back:e}"
            );
        }
    }

    /// A rendering must be the SHORTEST spelling that round-trips, not merely
    /// one that does.
    #[test]
    fn no_shorter_spelling_round_trips() {
        let mut state = 0x9e37_79b9_7f4a_7c15_u64;
        for _ in 0..20_000 {
            state ^= state >> 12;
            state ^= state << 25;
            state ^= state >> 27;
            let value = f64::from_bits(state.wrapping_mul(0x2545_f491_4f6c_dd1d));
            if !value.is_finite() || value == 0.0 {
                continue;
            }
            let significant = shortest_digits(value.abs()).0.len();
            if significant < 2 {
                continue;
            }
            let shorter = format!("{:.*e}", significant - 2, value);
            let back: f64 = shorter.parse().expect("a formatting parses");
            assert_ne!(
                back.to_bits(),
                value.to_bits(),
                "{value:e} rendered with {significant} digits, but {shorter} also round-trips"
            );
        }
    }

    #[test]
    fn the_exact_comparison_is_exact() {
        // 3 * 2^4 == 48, and 48 == 3 * 2^4.
        assert!(exactly_equal(3, 4, 48, 0));
        assert!(!exactly_equal(3, 4, 49, 0));
        // A negative power of five moves onto the other side: 2 * 2^0 == 10 * 5^-1.
        assert!(exactly_equal(2, 0, 10, -1));
        assert!(!exactly_equal(3, 0, 10, -1));
        // A negative power of two does the same: 1 * 2^-1 == 5 * 5^-1 asks
        // whether 0.5 == 0.2, which it is not.
        assert!(!exactly_equal(1, -1, 1, -1));
        // One true case on that branch: 1 * 2^0 == 25 * 5^-2, both being 1.
        assert!(exactly_equal(1, 0, 25, -2));
        assert!(!exactly_equal(1, 0, 25, -3));
        // Overflow bails out rather than wrapping.
        assert!(!exactly_equal(1, 0, 1, 200));
        assert!(!exactly_equal(1, 4000, 1, 0));
    }
}

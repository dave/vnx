// Decimal rounding for the emit path (SPEC.md 4.1).
//
// SPEC.md 4.1 rounds longitude and latitude to 6 decimal places and elevation
// to 1, with a tie resolved TO EVEN. The languages this format is implemented
// in do not agree by default:
//
//     340.25 to 1 place    Go 340.2   Python 340.2   JavaScript `toFixed` 340.3
//    -340.25 to 1 place    Go -340.2  Python -340.2  JavaScript `toFixed` -340.3
//
// measured 2026-09-09. Ties are reachable: 340.25 and 340.75 are exact doubles,
// and an elevation off a quarter-metre grid lands on one.
//
// The tie is decided on the double's EXACT value, computed in BigInt, and the
// result is reached by producing decimal text and parsing it back — the
// format-then-parse route SPEC.md 4.1 defines. Scaling by a power of ten is a
// different operation and is forbidden: the scaled product is itself a
// binary64, so it manufactures ties the true decimal never had and destroys
// ones it did.
//
// The obvious tie shortcut — take `v.toFixed(places + 1)` and look for a
// trailing 5 — is wrong: `(0.15).toFixed(2)` is `"0.15"` and
// `Number("0.15") === 0.15`, yet 0.15 as a double is 0.1499999999999999944...,
// which is below the midpoint and not a tie at all.

const FIVE = 5n;

/**
 * @param {number} v a finite double
 * @param {number} places decimal places, 0 or more
 * @returns {number} the nearest double to the rounded decimal value
 */
export function roundToPlaces(v, places) {
  if (!Number.isFinite(v)) throw new RangeError(`cannot round ${String(v)}`);
  // ⚠ ROUNDING CREATES NEGATIVE ZEROS THAT WERE NEVER IN THE MODEL: -0.04 to
  // one place and -4e-8 to six both land on a zero with its sign bit set. -0
  // and 0 are the same value and the sign carries no information, so it is
  // dropped here, where every caller reaches it. `v === 0` is true for both
  // zeros, so the one comparison catches an incoming -0 as well.
  if (v === 0) return 0;
  if (Number.isInteger(v)) return v;
  const negative = v < 0;
  const { mantissa, exponent } = decompose(Math.abs(v));
  if (mantissa === 0n) return v;

  // |v| * 10^places  ==  mantissa * 2^exponent * 2^places * 5^places
  const p = BigInt(places);
  const shift = BigInt(exponent) + p;
  let numerator = mantissa * FIVE ** p;
  let denominator = 1n;
  if (shift >= 0n) numerator <<= shift;
  else denominator = 1n << -shift;

  let q = numerator / denominator;
  const twiceRemainder = (numerator % denominator) * 2n;
  if (twiceRemainder > denominator) q += 1n;
  else if (twiceRemainder === denominator && q % 2n === 1n) q += 1n; // half to even

  return Number(decimalText(q, places, negative));
}

/** Exact decomposition: `x === mantissa * 2^exponent`, for finite x >= 0. */
function decompose(x) {
  const view = new DataView(new ArrayBuffer(8));
  view.setFloat64(0, x);
  const hi = view.getUint32(0);
  const lo = view.getUint32(4);
  const biasedExponent = (hi >>> 20) & 0x7ff;
  let mantissa = (BigInt(hi & 0xfffff) << 32n) | BigInt(lo);
  if (biasedExponent === 0) return { mantissa, exponent: -1074 }; // subnormal
  mantissa |= 1n << 52n;
  return { mantissa, exponent: biasedExponent - 1075 };
}

/** `q / 10^places` as decimal text, so the final parse is a single rounding. */
function decimalText(q, places, negative) {
  const sign = negative && q !== 0n ? '-' : '';
  if (places === 0) return sign + q.toString();
  const digits = q.toString().padStart(places + 1, '0');
  const cut = digits.length - places;
  return `${sign}${digits.slice(0, cut)}.${digits.slice(cut)}`;
}

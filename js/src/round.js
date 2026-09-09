// Decimal rounding for the write path (SPEC.md 4.5.4).
//
// SPEC.md 4.5.4 says a writer rounds longitude and latitude to 6 decimal places
// and elevation to 1. It does not say WHICH WAY TO ROUND A TIE, and the target
// languages do not agree:
//
//     340.25 to 1 place    Go 340.2   Python 340.2   JavaScript `toFixed` 340.3
//    -340.25 to 1 place    Go -340.2  Python -340.2  JavaScript `toFixed` -340.3
//
// measured 2026-09-09. Ties are reachable: 340.25 and 340.75 are exact doubles,
// and an elevation off a quarter-metre grid lands on one. Two conforming
// writers therefore emit different bytes for one model, which is the guarantee
// SPEC.md 4.5 exists to make. Neither the accept tier (a re-render, which does
// not round) nor the format tier can see it.
//
// Pending a ruling this rounds HALF TO EVEN, matching Go and Python — the
// reference implementation and the other non-JavaScript seat. Switching to
// half-away-from-zero is the one branch marked below.
//
// The tie is decided on the double's EXACT value, computed in BigInt. The
// obvious shortcut — take `v.toFixed(places + 1)` and look for a trailing 5 —
// is wrong: `(0.15).toFixed(2)` is `"0.15"` and `Number("0.15") === 0.15`, yet
// 0.15 as a double is 0.1499999999999999944..., which is below the midpoint and
// not a tie at all.

const FIVE = 5n;
const TEN = 10n;

/**
 * @param {number} v a finite double
 * @param {number} places decimal places, 0 or more
 * @returns {number} the nearest double to the rounded decimal value
 */
export function roundToPlaces(v, places) {
  if (!Number.isFinite(v)) throw new RangeError(`cannot round ${String(v)}`);
  if (Number.isInteger(v)) return v;
  const negative = v < 0 || Object.is(v, -0);
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

export const _internal = { decompose, decimalText, TEN };

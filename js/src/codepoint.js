// Lexicographic comparison by Unicode code point (SPEC.md 4.5.1).
//
// `Array.prototype.sort()` orders strings by UTF-16 CODE UNIT, which disagrees
// with code-point order above U+FFFF: a lone value in U+E000..U+FFFF sorts
// ABOVE an astral character, because the astral character's first code unit is
// a high surrogate in U+D800..U+DBFF. A data record body may carry any key
// (SPEC.md 5.1), so the disagreement is reachable and this comparator is
// required rather than decorative.

/**
 * @param {string} a
 * @param {string} b
 * @returns {number} negative, zero or positive
 */
export function compareCodePoints(a, b) {
  if (a === b) return 0;
  const ia = a[Symbol.iterator]();
  const ib = b[Symbol.iterator]();
  for (;;) {
    const x = ia.next();
    const y = ib.next();
    if (x.done) return y.done ? 0 : -1;
    if (y.done) return 1;
    const ca = x.value.codePointAt(0);
    const cb = y.value.codePointAt(0);
    if (ca !== cb) return ca < cb ? -1 : 1;
  }
}

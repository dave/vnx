// Number rendering (SPEC.md 4.5.3).
//
// Canonical number text is defined by reference to ECMAScript `Number::toString`,
// which `String(n)` implements directly, so this runtime IS the reference for
// this rule. `String(-0)` is already `"0"`, so the negative-zero correction the
// spec requires of Go costs nothing here; the assertion below freezes that,
// because a future change to how a value reaches this function (a sign-carrying
// wrapper, say) would otherwise reintroduce `-0` silently.

/**
 * @param {number} n a finite double
 * @returns {string}
 */
export function renderNumber(n) {
  if (typeof n !== 'number') throw new TypeError(`not a number: ${String(n)}`);
  if (!Number.isFinite(n)) {
    // SPEC.md 4.2: non-finite numbers are forbidden throughout a document. A
    // model holding one is a bug in the caller, not a document defect, so this
    // throws rather than producing a diagnostic.
    throw new RangeError(`non-finite number cannot be rendered: ${String(n)}`);
  }
  if (n === 0) return '0'; // collapses -0 as well as 0
  return String(n);
}

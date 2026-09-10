// Decimal rounding at the emit boundary (SPEC.md 4.1).

import test from 'node:test';
import assert from 'node:assert/strict';

import { roundToPlaces } from '../src/round.js';

test('an exact tie rounds to even, matching Go and Python', () => {
  // 340.25 and 340.75 are exact doubles, so these are real ties and not an
  // artefact of binary representation. `(340.25).toFixed(1)` is "340.3".
  assert.equal(roundToPlaces(340.25, 1), 340.2);
  assert.equal(roundToPlaces(340.75, 1), 340.8);
  assert.equal(roundToPlaces(-340.25, 1), -340.2);
  assert.equal(roundToPlaces(-340.75, 1), -340.8);
  assert.notEqual(roundToPlaces(340.25, 1), Number((340.25).toFixed(1)));
});

test('a value that only LOOKS like a tie is rounded on its exact value', () => {
  // 0.15 as a double is 0.1499999999999999944..., below the midpoint. A
  // tie-detector built on `toFixed(places + 1)` ending in "5" gets this wrong.
  assert.equal(roundToPlaces(0.15, 1), 0.1);
  assert.equal(roundToPlaces(-0.15, 1), -0.1);
  assert.equal(roundToPlaces(2.675, 2), 2.67);
  assert.equal(roundToPlaces(1.005, 2), 1);
});

test('ordinary values round the ordinary way', () => {
  assert.equal(roundToPlaces(340.26, 1), 340.3);
  assert.equal(roundToPlaces(340.24, 1), 340.2);
  assert.equal(roundToPlaces(-3.6712345678901235, 6), -3.671235);
  assert.equal(roundToPlaces(57.11812345678901, 6), 57.118123);
});

test('an integer is returned unchanged', () => {
  assert.equal(roundToPlaces(340, 1), 340);
  assert.equal(roundToPlaces(1778914991000, 6), 1778914991000);
  assert.equal(roundToPlaces(0, 6), 0);
});

test('a value below half a unit in the last place becomes zero', () => {
  assert.equal(roundToPlaces(1e-7, 6), 0);
  assert.equal(roundToPlaces(4e-7, 6), 0);
  assert.equal(roundToPlaces(6e-7, 6), 0.000001);
});

test('a subnormal is handled without an exponent underflow', () => {
  assert.equal(roundToPlaces(5e-324, 6), 0);
});

test('a non-finite value cannot be rounded', () => {
  assert.throws(() => roundToPlaces(NaN, 6), RangeError);
  assert.throws(() => roundToPlaces(Infinity, 1), RangeError);
});

test('rounding to 6 places then again is stable', () => {
  for (const v of [-3.6712345678901235, 57.11812345678901, 340.25, 0.0000005]) {
    const once = roundToPlaces(v, 6);
    assert.equal(roundToPlaces(once, 6), once, String(v));
  }
});

// --- red-proof ---------------------------------------------------------------
//
// The corpus reaches the tie rule in two tiers only — `write/emit-boundary` and
// `numbers/` — because every other tier encodes what it decoded, which rounds
// values that already satisfy the rule. These guards are local and are the
// fastest thing that can fail, so they have to have been seen to fail.

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const ROUND_SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'src', 'round.js');

/** Load a copy of round.js with one line replaced. */
async function loadMutated(from, to) {
  const source = fs.readFileSync(ROUND_SRC, 'utf8');
  assert.ok(source.includes(from),
    `mutation target not found: ${JSON.stringify(from)} — the red-proof would prove nothing`);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'vnx-round-'));
  const file = path.join(dir, 'round.js');
  fs.writeFileSync(file, source.replace(from, to));
  return import(pathToFileURL(file).href);
}

const HALF_TO_EVEN =
  '  else if (twiceRemainder === denominator && q % 2n === 1n) q += 1n; // half to even';

test('RED PROOF: flipping the tie to away-from-zero breaks the tie guard', async () => {
  const mutated = await loadMutated(
    HALF_TO_EVEN,
    '  else if (twiceRemainder === denominator) q += 1n; // half away from zero',
  );
  // The values the guard above pins, under the wrong rule.
  assert.equal(mutated.roundToPlaces(340.25, 1), 340.3);
  assert.equal(mutated.roundToPlaces(-340.25, 1), -340.3);
  // ... and the shipped rule still says otherwise, so the assertions
  // discriminate rather than being true of both.
  assert.equal(roundToPlaces(340.25, 1), 340.2);
  assert.equal(roundToPlaces(-340.25, 1), -340.2);
  // A non-tie must be unaffected, or the mutation would be proving something
  // broader than the tie direction.
  assert.equal(mutated.roundToPlaces(0.15, 1), roundToPlaces(0.15, 1));
  assert.equal(mutated.roundToPlaces(340.26, 1), roundToPlaces(340.26, 1));
});

test('RED PROOF: dropping the tie branch entirely truncates a tie', async () => {
  const mutated = await loadMutated(HALF_TO_EVEN, '  // no tie handling at all');
  assert.equal(mutated.roundToPlaces(340.75, 1), 340.7);
  assert.equal(roundToPlaces(340.75, 1), 340.8);
});

test('the smallest real six-place tie rounds to even', () => {
  // 0.0078125 is 1/128 and is exact, so this is a genuine six-place tie.
  assert.equal(roundToPlaces(0.0078125, 6), 0.007812);   // 2 is even: down
  assert.equal(roundToPlaces(-0.0078125, 6), -0.007812);
  assert.equal(roundToPlaces(0.0234375, 6), 0.023438);   // 3/128; 8 is even: up
});

test('0.0000005 is not a six-place tie, and is not even close to being one', () => {
  // The obvious six-place tie test exercises a NON-tie: 0.0000005 is not a
  // binary64 value, and the nearest double is 4.99999999999999977e-7, BELOW the
  // midpoint. It rounds DOWN under every rule, and a suite that used it would
  // pass whichever way the tie branch was written. Frozen so nobody mistakes it
  // for the tie case.
  assert.notEqual((0.0000005).toPrecision(21), '5.00000000000000000000e-7');
  assert.equal(roundToPlaces(0.0000005, 6), 0);
  assert.equal(roundToPlaces(0.0000005, 6), roundToPlaces(0.0000004, 6));
});

test('rounding creates no negative zero on the wire', () => {
  // SPEC.md 4.1: rounding CREATES negative zeros that were never in the model.
  // A writer collapsing negative zero only across the values it was HANDED
  // would carry one out for a value it produced itself. `JSON.stringify` spells
  // -0 as `0`, so the document cannot show it: the number tier compares BIT
  // PATTERNS and is where it becomes visible.
  assert.ok(!Object.is(roundToPlaces(-0.04, 1), -0));
  assert.ok(!Object.is(roundToPlaces(-0.00000004, 6), -0));
  assert.ok(!Object.is(roundToPlaces(-0, 6), -0));
});

test('the tie rule is separated from all five wrong ones, and it takes both signs of value', () => {
  // Measured 2026-09-10 against the five rules an implementation actually
  // reaches for. Each of the two fixture values catches a different pair, and
  // only both together identify half-to-even:
  //
  //   value    rule   half-odd  away-0  toward-0  half-up  half-down
  //   340.25   340.2  340.3     340.3   340.2     340.3    340.2
  //   340.75   340.8  340.7     340.8   340.7     340.8    340.7
  //
  // half-up is not exotic: `Math.round(x * 10) / 10` is exactly it, in both
  // JavaScript and Java, because Math.round breaks a tie toward positive
  // infinity in both. That is the trick this rounder exists to avoid.
  assert.equal(roundToPlaces(340.25, 1), 340.2);
  assert.equal(roundToPlaces(340.75, 1), 340.8);
  // Math.round's asymmetry is what makes a positives-only or negatives-only
  // guard unsafe in general, so both signs are pinned even though the two
  // positives happen to be sufficient for these five rules.
  assert.equal(roundToPlaces(-340.25, 1), -340.2);
  assert.equal(roundToPlaces(-340.75, 1), -340.8);
  assert.notEqual(roundToPlaces(340.25, 1), Math.round(340.25 * 10) / 10);
  assert.equal(Math.round(-340.25 * 10) / 10, -340.2, 'and it is RIGHT on this one, by accident');
});

test('toFixed rounds away from zero on BOTH signs', () => {
  // Recorded because I got this wrong in writing and had the measurement in
  // front of me: `toFixed` strips the sign and rounds the magnitude, so it is
  // symmetric. The asymmetric function is `Math.round`, above. A reason
  // reasoned from prose is not a measurement, even when the conclusion it
  // supports is correct.
  assert.equal((340.25).toFixed(1), '340.3');
  assert.equal((-340.25).toFixed(1), '-340.3');
  assert.equal((2.5).toFixed(0), '3');
  assert.equal((-2.5).toFixed(0), '-3');
});

// Decimal rounding on the write path (SPEC.md 4.5.4).

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

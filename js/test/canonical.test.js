// Canonical form: key order, number rendering, array layout, encoding
// (SPEC.md 4.5).

import test from 'node:test';
import assert from 'node:assert/strict';

import { compareCodePoints } from '../src/codepoint.js';
import { renderNumber } from '../src/number.js';
import { renderCanonical, renderString } from '../src/render.js';

test('the comparator orders by code point where the default sort does not', () => {
  // U+1F600 GRINNING FACE is above U+FFFD REPLACEMENT CHARACTER by code point,
  // and BELOW it by UTF-16 code unit, because its first unit is the high
  // surrogate U+D83D. This is the disagreement SPEC.md 4.5.1 calls out, and it
  // is reachable because a data record body may carry any key.
  const keys = ['\u{1F600}', '�'];
  assert.deepEqual([...keys].sort(), ['\u{1F600}', '�']);
  assert.deepEqual([...keys].sort(compareCodePoints), ['�', '\u{1F600}']);
});

test('the comparator agrees with the default sort on ASCII', () => {
  const keys = ['pips', 'from', 'to', 'data', '@type', 'notes', 'nodes'];
  assert.deepEqual([...keys].sort(compareCodePoints), [...keys].sort());
});

test('a shorter key sorts before its own prefix extension', () => {
  assert.ok(compareCodePoints('a', 'ab') < 0);
  assert.ok(compareCodePoints('nodes', 'notes') < 0);
});

test('numbers render with Number::toString semantics', () => {
  assert.equal(renderNumber(340), '340');
  assert.equal(renderNumber(0.000001), '0.000001');
  assert.equal(renderNumber(1e-7), '1e-7');
  assert.equal(renderNumber(1778914991000), '1778914991000');
  assert.equal(renderNumber(1e21), '1e+21');
  assert.equal(renderNumber(-3.6712345678901235), '-3.6712345678901235');
});

test('negative zero renders as 0', () => {
  assert.equal(renderNumber(-0), '0');
  assert.equal(renderCanonical({ a: -0 }), '{\n  "a": 0\n}\n');
});

test('a non-finite number cannot be rendered', () => {
  assert.throws(() => renderNumber(NaN), RangeError);
  assert.throws(() => renderNumber(Infinity), RangeError);
});

test('keys sort in two groups: scalars, then containers', () => {
  const out = renderCanonical({ zebra: 1, arcs: [], nodes: {}, alpha: 'x' });
  assert.equal(out, [
    '{',
    '  "alpha": "x",',
    '  "zebra": 1,',
    '  "arcs": [],',
    '  "nodes": {}',
    '}',
    '',
  ].join('\n'));
});

test('classification is by the value in this object, not by the key name', () => {
  // The same key name lands in a different group in a different object. This
  // is what accept/shared-turnaround-handle.vnx spells out on the wire, whose
  // second link is `arc, to, from` because `from` holds an inline position.
  const a = renderCanonical({ to: 'node-1', from: [1, 2], arc: 'spur' });
  assert.equal(a, '{\n  "arc": "spur",\n  "to": "node-1",\n  "from": [1, 2]\n}\n');
  const b = renderCanonical({ to: 'node-1', from: 'node-2', arc: 'spur' });
  assert.equal(b, '{\n  "arc": "spur",\n  "from": "node-2",\n  "to": "node-1"\n}\n');
});

test('null counts as a number for the inline-array rule', () => {
  assert.equal(renderCanonical([1, null]), '[1, null]\n');
  assert.equal(renderCanonical([1, 'x']), '[\n  1,\n  "x"\n]\n');
});

test('empty containers go on one line', () => {
  assert.equal(renderCanonical({ a: [], b: {} }), '{\n  "a": [],\n  "b": {}\n}\n');
});

test('arrays are never reordered', () => {
  assert.equal(renderCanonical([3, 1, 2]), '[3, 1, 2]\n');
});

test('a string escapes only what RFC 8259 requires', () => {
  assert.equal(renderString('a"b\\c'), '"a\\"b\\\\c"');
  assert.equal(renderString('\n\t\r\b\f'), '"\\n\\t\\r\\b\\f"');
  assert.equal(renderString('\u0001'), '"\\u0001"');
  // Raw, deliberately: SPEC.md 4.5.5 names these five as characters Go escapes
  // by default and must not.
  assert.equal(renderString('<>&\u2028\u2029'), '"<>&\u2028\u2029"');
  assert.equal(renderString('Cairn Gorm — 1245 m'), '"Cairn Gorm — 1245 m"');
});

test('no character above U+007F is written as an escape', () => {
  assert.equal(renderString('\u{1F600}'), '"\u{1F600}"');
});

test('output ends in exactly one newline', () => {
  const out = renderCanonical({ vnx: '0.1.0' });
  assert.ok(out.endsWith('}\n'));
  assert.ok(!out.endsWith('\n\n'));
});

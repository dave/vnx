// `encode` refuses a value JSON cannot carry rather than letting
// `JSON.stringify` drop it or write it as null.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import { decodeText } from '../src/decode.js';
import { encode } from '../src/index.js';

const base = () => decodeText(JSON.stringify({
  vnx: '0.1.0',
  waypoints: [{
    at: [1, 2],
    style: { color: '#123456' },
    data: [{ '@type': 'com.example.t', value: 1 }],
  }],
  'com.example.v': { nested: [1] },
})).document;

// Each place a caller-built value can reach the writer: an application key's
// value (top level, nested in an object, nested in an array), a record body,
// and a style.
const sites = {
  'an application key value': (d, v) => { d.appKeys[0].value = v; },
  'inside an application key object': (d, v) => { d.appKeys[0].value.x = v; },
  'inside an application key array': (d, v) => { d.appKeys[0].value.nested.push(v); },
  'a record body': (d, v) => { d.waypoints[0].data[0].value = v; },
  'a style': (d, v) => { d.waypoints[0].style.color = v; },
};

const unwritable = {
  undefined,
  'a function': () => 1,
  'a symbol': Symbol('s'),
  'a bigint': 1n,
  NaN,
  Infinity,
  '-Infinity': -Infinity,
};

for (const [site, put] of Object.entries(sites)) {
  for (const [what, value] of Object.entries(unwritable)) {
    test(`encode refuses ${what} in ${site}`, () => {
      const d = base();
      put(d, value);
      assert.throws(() => encode(d), /vnx: .*(JSON cannot carry|not finite)/);
    });
  }
}

test('a null application key value is written, not refused', () => {
  const d = base();
  d.appKeys[0].value = null;
  assert.equal(JSON.parse(encode(d))['com.example.v'], null);
});

test('a legal document encodes to the same tree it was read from', () => {
  const text = readFileSync(new URL('../../corpus/accept/application-keys.vnx', import.meta.url), 'utf8');
  const out = encode(decodeText(text).document);
  assert.equal(out, JSON.stringify(JSON.parse(out)));
  assert.deepEqual(JSON.parse(out)['com.example.nested'], { 'a.b': { 'c.d': 1 } });
  assert.equal(JSON.parse(out)['org.example.cleared'], null);
});

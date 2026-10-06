// Application keys (SPEC.md 3.9): top-level keys containing a full stop, kept
// verbatim, never reported, and refused by the encoder when misnamed.

import test from 'node:test';
import assert from 'node:assert/strict';

import { decodeText } from '../src/decode.js';
import { encode, AppKey, Document } from '../src/index.js';

test('application keys are kept verbatim, sorted by key, and not reported', () => {
  const text = `{"vnx":"0.9.0",
    "org.example.z": {"anything": {"at": ["all"]}},
    "app.viatrail.rulesets": [{"rules": [{"op": "equals"}]}],
    "com.example.null": null,
    "com.example.empty": [],
    "com.example.string": "text"}`;
  const { document, diagnostics } = decodeText(text);
  assert.deepEqual(diagnostics, []);
  const tree = JSON.parse(text);
  assert.deepEqual(
    document.appKeys.map((k) => k.key),
    ['app.viatrail.rulesets', 'com.example.empty', 'com.example.null', 'com.example.string', 'org.example.z'],
  );
  for (const k of document.appKeys) assert.deepEqual(k.value, tree[k.key], k.key);
});

test('a null or empty application key survives a read and a write', () => {
  const text = '{"vnx":"0.9.0","com.example.null":null,"com.example.a":[],"com.example.o":{}}';
  const out = JSON.parse(encode(decodeText(text).document));
  assert.deepEqual(out, JSON.parse(text));
});

test('a dotted key below the top level is an unknown key', () => {
  const { diagnostics } = decodeText('{"vnx":"0.9.0","rulesets":[],"waypoints":[{"at":[1,2],"app.viatrail.x":1}]}');
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]).sort(), [
    ['/rulesets', 'unknown-key'],
    ['/waypoints/0/app.viatrail.x', 'unknown-key'],
  ]);
});

test('encode refuses an application key it cannot write', () => {
  for (const [keys, want] of [
    [[new AppKey('rulesets', [])], /no full stop/],
    [[new AppKey('folders', [])], /no full stop/],
    [[new AppKey('', null)], /no full stop/],
    [[new AppKey('a.b', 1), new AppKey('a.b', 2)], /twice/],
  ]) {
    const d = new Document();
    d.vnx = '0.9.0';
    d.appKeys = keys;
    assert.throws(() => encode(d), want);
  }
});

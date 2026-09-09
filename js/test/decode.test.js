// Reading: refusals, diagnostics, and the model state the corpus depends on.

import test from 'node:test';
import assert from 'node:assert/strict';

import { decodeText } from '../src/decode.js';
import { VnxRefusal } from '../src/errors.js';
import { rerender } from '../src/index.js';

const doc = (extra) => JSON.stringify({ vnx: '0.1.0', ...extra });

function refusal(text) {
  try {
    decodeText(text);
  } catch (err) {
    assert.ok(err instanceof VnxRefusal, `threw ${err}`);
    return err;
  }
  assert.fail('the document was read; it must be refused');
}

// --- the four refusal causes of SPEC.md 6.2 ---------------------------------

test('a top level that is not an object is refused', () => {
  assert.equal(refusal('[]').cause_code, 'not-a-vnx-document');
  assert.equal(refusal('"0.1.0"').cause_code, 'not-a-vnx-document');
  assert.equal(refusal('null').cause_code, 'not-a-vnx-document');
});

test('a document with no vnx key is refused', () => {
  assert.equal(refusal('{"waypoints": []}').cause_code, 'not-a-vnx-document');
});

test('a version that is not three digit-runs is refused, and the refusal names the shape', () => {
  for (const v of ['0.1', '0.1.0.0', '0.1.x', '0.1.+1', '', 'v0.1.0']) {
    const err = refusal(JSON.stringify({ vnx: v }));
    assert.equal(err.cause_code, 'version-shape', v);
    assert.ok(err.message.includes('MAJOR.MINOR.PATCH'), err.message);
  }
});

test('a non-string vnx value is refused rather than coerced', () => {
  assert.equal(refusal('{"vnx": 0.1}').cause_code, 'version-shape');
  assert.equal(refusal('{"vnx": null}').cause_code, 'version-shape');
});

test('a MAJOR or MINOR above the reader is refused; PATCH never is', () => {
  assert.equal(refusal(doc0('0.2.0').text).cause_code, 'version-too-new');
  assert.equal(refusal(doc0('1.0.0').text).cause_code, 'version-too-new');
  for (const v of ['0.1.99', '0.0.9', '0.1.0']) {
    assert.equal(decodeText(doc0(v).text).document.vnx, v);
  }
});

function doc0(v) {
  return { text: JSON.stringify({ vnx: v }) };
}

test('leading zeros are a legal spelling and compare numerically', () => {
  assert.equal(decodeText('{"vnx": "00.01.0000"}').document.vnx, '00.01.0000');
});

test('a NaN or Infinity literal is refused as malformed JSON', () => {
  assert.equal(refusal('{"vnx": "0.1.0", "a": NaN}').cause_code, 'malformed-json');
  assert.equal(refusal('{"vnx": "0.1.0", "a": Infinity}').cause_code, 'malformed-json');
});

test('a numeric literal that overflows to Infinity is refused as not finite', () => {
  // `1e400` is well-formed JSON and parses to Infinity. SPEC.md 4.2 forbids a
  // non-finite number throughout a document; the corpus has no case for this
  // spelling, so the guard is proved here.
  const err = refusal('{"vnx": "0.1.0", "a": 1e400}');
  assert.equal(err.cause_code, 'non-finite-number');
  assert.ok(err.message.includes('not finite'), err.message);
});

test('a version above the reader names the version needed', () => {
  const err = refusal('{"vnx": "9.9.9"}');
  assert.ok(err.message.includes('9.9.9'), err.message);
  assert.ok(err.message.includes('a newer reader is needed'), err.message);
});

// --- position state ---------------------------------------------------------

test('a position keeps its arity, including a trailing null', () => {
  const { document } = decodeText(doc({ waypoints: [{ at: [1, 2, null] }] }));
  assert.equal(document.waypoints[0].at.arity, 3);
  assert.equal(document.waypoints[0].at.ele, null);
  assert.equal(document.waypoints[0].at.time, undefined);
});

test('a re-render reproduces the spelling the document carried', () => {
  const text = '{\n  "vnx": "0.1.0",\n  "waypoints": [\n    {\n      "at": [1, 2, null]\n    }\n  ]\n}\n';
  assert.equal(rerender(decodeText(text).document), text);
});

test('an interior absence keeps its null', () => {
  const { document } = decodeText(doc({
    tracks: [{ segments: [{ pips: [[1, 2, null, 5]] }] }],
  }));
  const pip = document.tracks[0].segments[0].pips[0];
  assert.equal(pip.ele, null);
  assert.equal(pip.time, 5);
});

test('a channel beyond those the reader knows survives a re-render', () => {
  // SPEC.md 4.1 says a reader ignores channels beyond those it knows so that a
  // future channel is additive. Ignoring is not discarding: the position is
  // kept whole, so the unknown channel comes back out.
  const text = '{\n  "vnx": "0.1.0",\n  "waypoints": [\n    {\n      "at": [1, 2, 3, 4, 5]\n    }\n  ]\n}\n';
  assert.equal(rerender(decodeText(text).document), text);
});

test('an unusable position drops its element and diagnoses', () => {
  const { document, diagnostics } = decodeText(doc({ waypoints: [{ at: [1] }] }));
  assert.equal(document.waypoints.length, 0);
  assert.equal(diagnostics.length, 1);
  assert.equal(diagnostics[0].path, '/waypoints/0/at');
});

test('a pip that will not parse is dropped and the rest of the list survives', () => {
  const { document, diagnostics } = decodeText(doc({
    tracks: [{ segments: [{ pips: [[1, 2], ['x', 2], [3, 4]] }] }],
  }));
  assert.equal(document.tracks[0].segments[0].pips.length, 2);
  assert.equal(diagnostics[0].path, '/tracks/0/segments/0/pips/1');
});

// --- SPEC.md 6.2 ------------------------------------------------------------

test('an unrecognised key is diagnosed, skipped, and the item kept', () => {
  const { document, diagnostics } = decodeText(doc({ waypoints: [{ at: [1, 2], nmae: 'x' }] }));
  assert.equal(document.waypoints.length, 1);
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]), [['/waypoints/0/nmae', 'unknown-key']]);
});

test('a JSON Pointer token escapes ~ and /', () => {
  const { diagnostics } = decodeText(doc({ waypoints: [{ at: [1, 2], 'a/b~c': 1 }] }));
  assert.equal(diagnostics[0].path, '/waypoints/0/a~1b~0c');
});

test('a wrong-typed OPTIONAL key drops the field and keeps the item', () => {
  const { document, diagnostics } = decodeText(doc({ waypoints: [{ at: [1, 2], name: 7 }] }));
  assert.equal(document.waypoints.length, 1);
  assert.equal(document.waypoints[0].name, undefined);
  assert.equal(diagnostics[0].code, 'wrong-type-optional');
});

test('a wrong-typed REQUIRED key drops the item', () => {
  const { document, diagnostics } = decodeText(doc({ waypoints: [{ at: 'somewhere' }] }));
  assert.equal(document.waypoints.length, 0);
  assert.equal(diagnostics.length, 1);
});

test('a data record with no @type is dropped; the item survives', () => {
  const { document, diagnostics } = decodeText(doc({
    waypoints: [{ at: [1, 2], data: [{ value: 1 }, { '@type': 'com.example.x' }] }],
  }));
  assert.equal(document.waypoints[0].data.length, 1);
  assert.equal(diagnostics[0].code, 'record-type-missing');
});

test('a record body is never inspected, whatever its type says', () => {
  // SPEC.md 5.1: a record body is opaque apart from `@type`. A codec has no
  // vocabulary layer, so `size` here is neither validated nor dropped even
  // though SPEC.md 5.2 names an enumeration for it.
  const { document, diagnostics } = decodeText(doc({
    networks: [{ points: [{ at: [1, 2], data: [{ '@type': 'app.viatrail.settlement', size: 'metropolis' }] }] }],
  }));
  assert.deepEqual(diagnostics, []);
  assert.equal(document.networks[0].points[0].data[0].size, 'metropolis');
});

test('an importance outside 1 to 5 is dropped and the item kept', () => {
  const { document, diagnostics } = decodeText(doc({
    waypoints: [{ at: [1, 2], style: { importance: 9, color: '#fff' } }],
  }));
  assert.equal(diagnostics[0].code, 'importance-out-of-range');
  assert.deepEqual(document.waypoints[0].style, { color: '#fff' });
});

test('an unrecognised style key is carried without a diagnostic', () => {
  const { document, diagnostics } = decodeText(doc({
    waypoints: [{ at: [1, 2], style: { 'halo-width': 3 } }],
  }));
  assert.deepEqual(diagnostics, []);
  assert.deepEqual(document.waypoints[0].style, { 'halo-width': 3 });
});

test('a folder naming an absent folder puts the item at the top level', () => {
  const { document, diagnostics } = decodeText(doc({ waypoints: [{ at: [1, 2], folder: 'ghost' }] }));
  assert.equal(document.waypoints.length, 1);
  assert.equal(document.waypoints[0].folder, undefined);
  assert.equal(diagnostics[0].code, 'folder-absent');
});

test('a folder with no id is dropped and its contents survive at the top level', () => {
  const { document, diagnostics } = decodeText(doc({
    folders: [{ name: 'No id' }],
    waypoints: [{ at: [1, 2], folder: 'anything' }],
  }));
  assert.equal(document.folders.length, 0);
  assert.equal(document.waypoints.length, 1);
  assert.deepEqual(diagnostics.map((d) => d.code).sort(), ['folder-absent', 'missing-required-key']);
});

test('a ring that does not close drops the item', () => {
  const { document, diagnostics } = decodeText(doc({
    areas: [{ polygon: [[1, 2], [3, 4], [5, 6], [7, 8]] }],
  }));
  assert.equal(document.areas.length, 0);
  assert.equal(diagnostics[0].code, 'polygon-not-closed');
});

test('a ring of fewer than four positions drops the item', () => {
  const { document, diagnostics } = decodeText(doc({
    areas: [{ polygon: [[1, 2], [3, 4], [1, 2]] }],
  }));
  assert.equal(document.areas.length, 0);
  assert.equal(diagnostics[0].code, 'polygon-too-short');
});

test('an arc naming an absent node keeps the arc', () => {
  const { document, diagnostics } = decodeText(doc({
    networks: [{ nodes: { a: [1, 2] }, arcs: { x: { from: 'a', to: 'ghost', pips: [[1, 2], [3, 4]] } } }],
  }));
  assert.equal(document.networks[0].arcs.size, 1);
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]),
    [['/networks/0/arcs/x/to', 'arc-node-absent']]);
});

test('a link on an arc with no vertices is dropped rather than measured', () => {
  const { document, diagnostics } = decodeText(doc({
    networks: [{
      nodes: { a: [1, 2] },
      arcs: { x: { from: 'a', to: 'a', pips: [] } },
      chains: [{ links: [{ arc: 'x', from: 'a', to: [1, 2] }] }],
    }],
  }));
  assert.equal(document.networks[0].chains[0].links.length, 0);
  assert.ok(diagnostics.some((d) => d.code === 'inline-endpoint-empty-arc'));
});

test('a record-level timestamp with an offset drops the field and keeps the item', () => {
  const { document, diagnostics } = decodeText(doc({
    tracks: [{ segments: [], started: '2026-05-12T08:03:11+01:00' }],
  }));
  assert.equal(document.tracks.length, 1);
  assert.equal(document.tracks[0].started, undefined);
  assert.equal(diagnostics[0].code, 'timestamp-not-utc');
});

test('fractional seconds are accepted on a record-level timestamp', () => {
  const { diagnostics } = decodeText(doc({
    tracks: [{ segments: [], started: '2026-05-12T08:03:11.250Z' }],
  }));
  assert.deepEqual(diagnostics, []);
});

// --- moving time ------------------------------------------------------------

test('moving time sums per segment, and is unknown rather than zero', () => {
  const { document } = decodeText(doc({
    tracks: [
      { name: 'paused', segments: [{ pips: [[1, 2, 0, 1000], [1, 2, 0, 4000]] }, { pips: [[1, 2, 0, 9000], [1, 2, 0, 10000]] }] },
      { name: 'timeless', segments: [{ pips: [[1, 2], [3, 4]] }] },
    ],
  }));
  assert.equal(document.tracks[0].movingTimeMs(), 4000);
  assert.equal(document.tracks[1].movingTimeMs(), null);
});

test('a track with no segments has an unknown moving time', () => {
  const { document } = decodeText(doc({ tracks: [{ segments: [] }] }));
  assert.equal(document.tracks[0].movingTimeMs(), null);
});

// --- absence is not a default ----------------------------------------------

test('an absent label-visible is neither true nor false', () => {
  const { document } = decodeText(doc({ waypoints: [{ at: [1, 2], style: {} }] }));
  assert.equal('label-visible' in document.waypoints[0].style, false);
});

test('an absent collection stays absent and does not become empty', () => {
  const { document } = decodeText('{"vnx": "0.1.0"}');
  assert.equal(document.waypoints, undefined);
  assert.equal(rerender(document), '{\n  "vnx": "0.1.0"\n}\n');
});

test('an empty collection is distinct from an absent one', () => {
  const { document } = decodeText('{"vnx": "0.1.0", "waypoints": []}');
  assert.deepEqual(document.waypoints, []);
  assert.equal(rerender(document), '{\n  "vnx": "0.1.0",\n  "waypoints": []\n}\n');
});

// --- continuity: SPEC.md 6.2 -----------------------------------------------

const chainDoc = (nodes, links) => doc({
  networks: [{
    nodes,
    arcs: { a: { from: Object.keys(nodes)[0], to: Object.keys(nodes)[0], pips: Object.values(nodes) } },
    chains: [{ links: links.map((l) => ({ arc: 'a', ...l })) }],
  }],
});

test('two DISTINCT node keys at the same position meet', () => {
  // A node key is resolved to its position before comparing. Comparing the
  // keys alone would report this continuous walk as broken, which is the
  // failure the rule exists to prevent. Nothing in the corpus covers it.
  const { diagnostics } = decodeText(chainDoc(
    { a: [1, 2], b: [3, 4], c: [3, 4] },
    [{ from: 'a', to: 'b' }, { from: 'c', to: 'a' }],
  ));
  assert.deepEqual(diagnostics, []);
});

test('two node keys at different positions do not meet', () => {
  const { diagnostics } = decodeText(chainDoc(
    { a: [1, 2], b: [3, 4], c: [5, 6] },
    [{ from: 'a', to: 'b' }, { from: 'c', to: 'a' }],
  ));
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]),
    [['/networks/0/chains/0/links/1', 'chain-discontinuity']]);
});

test('a node meets an inline endpoint on lon and lat, whatever its elevation', () => {
  // A node carries an elevation and an inline endpoint never does, so a
  // comparison over all channels would report every such junction as broken.
  const { diagnostics } = decodeText(chainDoc(
    { a: [1, 2, 900], b: [3, 4, 950] },
    [{ from: 'a', to: [3, 4] }, { from: 'b', to: 'a' }],
  ));
  assert.deepEqual(diagnostics, []);
});

test('a link naming an absent node keeps the link and takes that end out of the comparison', () => {
  const { document, diagnostics } = decodeText(chainDoc(
    { a: [1, 2], b: [3, 4] },
    [{ from: 'a', to: 'ghost' }, { from: 'b', to: 'a' }],
  ));
  assert.equal(document.networks[0].chains[0].links.length, 2);
  assert.deepEqual(diagnostics.map((d) => d.code), ['link-node-absent']);
});

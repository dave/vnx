// The emit boundary (SPEC.md 4.1): what `encode` does to a model on the way out.

import test from 'node:test';
import assert from 'node:assert/strict';

import { decodeText } from '../src/decode.js';
import { encode } from '../src/index.js';
import { decodeContainer, encodeContainer, writeContainer } from '../src/vnz.js';
import { writeArchive } from '../src/zip.js';

const doc = (extra) => JSON.stringify({ vnx: '0.1.0', ...extra });

/** The `at` of the first waypoint, as the encoded document spells it. */
function encodedAt(document) {
  return JSON.parse(encode(document)).waypoints[0].at;
}

test('encoding rounds lon and lat to 6 places and elevation to 1', () => {
  const { document } = decodeText(doc({
    waypoints: [{ at: [-3.6712345678901235, 57.11812345678901, 340.26] }],
  }));
  assert.deepEqual(encodedAt(document), [-3.671235, 57.118123, 340.3]);
});

test('encoding emits the shortest spelling of a position', () => {
  const { document } = decodeText(doc({ waypoints: [{ at: [1, 2, null] }] }));
  assert.deepEqual(encodedAt(document), [1, 2]);
});

test('shortening stops at an interior absence', () => {
  // Dropping an interior null would move the time into the elevation channel.
  const { document } = decodeText(doc({
    tracks: [{ segments: [{ pips: [[1, 2, null, 5], [1, 2, 3, null]] }] }],
  }));
  const pips = JSON.parse(encode(document)).tracks[0].segments[0].pips;
  assert.deepEqual(pips, [[1, 2, null, 5], [1, 2, 3]]);
});

test('a channel beyond those this version names is neither rounded nor dropped', () => {
  // SPEC.md 4.1: a reader ignores channels beyond those it knows so that a
  // future channel is additive. Ignoring is not discarding.
  const { document } = decodeText(doc({ waypoints: [{ at: [1, 2, 3, 4, 5.55555] }] }));
  assert.deepEqual(encodedAt(document), [1, 2, 3, 4, 5.55555]);
});

test('a writer emits hidden only when it is true', () => {
  const { document } = decodeText(doc({
    networks: [{ hidden: false, name: 'a' }, { hidden: true, name: 'b' }],
  }));
  const networks = JSON.parse(encode(document)).networks;
  assert.equal('hidden' in networks[0], false);
  assert.equal(networks[1].hidden, true);
});

test('a re-encoded document carries the version it declared, not the reader own', () => {
  const { document } = decodeText('{"vnx": "0.1.99"}');
  assert.equal(JSON.parse(encode(document)).vnx, '0.1.99');
});

test('the output is compact: no space argument reaches JSON.stringify', () => {
  // SPEC.md 8.1 leaves whitespace to the writer, and a document is mostly
  // coordinates. Indentation would be pure size for no reader.
  const out = encode(decodeText(doc({ waypoints: [{ at: [1, 2] }] })).document);
  assert.equal(out, '{"vnx":"0.1.0","waypoints":[{"at":[1,2]}]}');
});

test('rounding happens in the emit path, never in the model', () => {
  // SPEC.md 4.1: endpoint comparison and ring winding compare positions by full
  // value, and rounding ahead of them changes the geometry. Encoding must not
  // mutate what the model holds.
  const { document } = decodeText(doc({ waypoints: [{ at: [-3.6712345678901235, 57.118] }] }));
  encode(document);
  assert.equal(document.waypoints[0].at.lon, -3.6712345678901235);
});

test('a writer creates no negative zero from a value that had none', () => {
  // SPEC.md 4.1: rounding -0.04 to one place, or -4e-8 to six, produces a zero
  // with the sign bit set in most languages. Neither value carries a negative
  // zero into the writer; both could carry one out of it. `JSON.stringify`
  // spells -0 as `0`, so the guard that matters is on the VALUE.
  const { document } = decodeText(doc({
    waypoints: [{ at: [-0.00000004, -0.00000004, -0.04] }],
  }));
  for (const channel of encodedAt(document)) assert.ok(!Object.is(channel, -0));
  assert.equal(encode(document).includes('-0'), false);
});

test('a container round-trips its document and its attachments', () => {
  const text = doc({ waypoints: [{ at: [-3.6712345678901235, 57.118, null] }] });
  const packed = encodeContainer(text, [['a/photo.txt', Buffer.from('x')]]);
  const container = decodeContainer(packed);

  const again = decodeContainer(writeContainer(container));
  // The document went through the encoder, so it is rounded and shortened.
  assert.equal(again.document.waypoints[0].at.lon, -3.671235);
  assert.equal(again.document.waypoints[0].at.arity, 2);
  assert.deepEqual([...again.attachments.keys()], ['a/photo.txt']);
  assert.equal(again.attachments.get('a/photo.txt').toString('utf8'), 'x');
});

test('repackaging keeps the document entry name it was read under', () => {
  const packed = writeArchive([{ name: 'some-export.vnx', data: Buffer.from(doc({ waypoints: [] })) }]);
  const container = decodeContainer(packed);
  assert.equal(decodeContainer(writeContainer(container)).documentEntry, 'some-export.vnx');
});

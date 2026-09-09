// The WRITE path (SPEC.md 4.1, 4.5.4), and its difference from RE-RENDER.

import test from 'node:test';
import assert from 'node:assert/strict';

import { decodeText } from '../src/decode.js';
import { rerender, write, canonicalise } from '../src/index.js';
import { decodeContainer, encodeContainer, rerenderContainer, writeContainer } from '../src/vnz.js';
import { writeArchive } from '../src/zip.js';

const doc = (extra) => JSON.stringify({ vnx: '0.1.0', ...extra });

test('a writer rounds lon and lat to 6 places and elevation to 1', () => {
  const { document } = decodeText(doc({
    waypoints: [{ at: [-3.6712345678901235, 57.11812345678901, 340.26] }],
  }));
  assert.match(write(document), /\[-3\.671235, 57\.118123, 340\.3\]/);
});

test('a re-render rounds nothing', () => {
  const { document } = decodeText(doc({
    waypoints: [{ at: [-3.6712345678901235, 57.11812345678901, 340.26] }],
  }));
  assert.match(rerender(document), /\[-3\.6712345678901235, 57\.11812345678901, 340\.26\]/);
});

test('a writer emits the shortest spelling; a re-render does not', () => {
  const { document } = decodeText(doc({ waypoints: [{ at: [1, 2, null] }] }));
  assert.match(write(document), /"at": \[1, 2\]/);
  assert.match(rerender(document), /"at": \[1, 2, null\]/);
});

test('shortening stops at an interior absence', () => {
  const { document } = decodeText(doc({
    tracks: [{ segments: [{ pips: [[1, 2, null, 5], [1, 2, 3, null]] }] }],
  }));
  const out = write(document);
  assert.match(out, /\[1, 2, null, 5\]/);
  assert.match(out, /\[1, 2, 3\]/);
});

test('a writer emits hidden only when it is true; a re-render carries what it read', () => {
  const { document } = decodeText(doc({
    networks: [{ hidden: false, name: 'a' }, { hidden: true, name: 'b' }],
  }));
  assert.equal(write(document).includes('"hidden": false'), false);
  assert.equal(write(document).includes('"hidden": true'), true);
  assert.equal(rerender(document).includes('"hidden": false'), true);
});

test('a re-encoded document carries the version it declared, not the reader own', () => {
  const { document } = decodeText('{"vnx": "0.1.99"}');
  assert.match(write(document), /"vnx": "0\.1\.99"/);
});

test('canonicalise is decode-then-re-render, and is idempotent', () => {
  const messy = '{"waypoints":[{"at":[340.0, 1e-06],"name":"x"}],"vnx":"0.1.0"}';
  const once = canonicalise(messy).text;
  assert.equal(canonicalise(once).text, once);
  assert.match(once, /\[340, 0\.000001\]/);
});

test('rounding happens in the emit path, never in the model', () => {
  // SPEC.md 4.5.4: endpoint comparison and ring winding compare positions by
  // full value, and rounding ahead of them changes the geometry. Writing must
  // not mutate what the model holds.
  const { document } = decodeText(doc({ waypoints: [{ at: [-3.6712345678901235, 57.118] }] }));
  write(document);
  assert.equal(document.waypoints[0].at.lon, -3.6712345678901235);
});

test('a writer creates no negative zero from a value that had none', () => {
  // SPEC.md 4.5.4: rounding -0.04 to one place, or -4e-8 to six, produces a
  // zero with the sign bit set in most languages. Neither value carries a
  // negative zero into the writer; both could carry one out of it.
  const { document } = decodeText(doc({
    waypoints: [{ at: [-0.00000004, -0.00000004, -0.04] }],
  }));
  assert.match(write(document), /"at": \[0, 0, 0\]/);
  assert.equal(write(document).includes('-0'), false);
});

test('a container has the same two emit paths as a document', () => {
  // SPEC.md 4.5.4: repackaging a container unchanged must not silently round
  // it, so the container path splits the same way.
  const text = doc({ waypoints: [{ at: [-3.6712345678901235, 57.118, null] }] });
  const packed = encodeContainer(text, [['a/photo.txt', Buffer.from('x')]]);
  const container = decodeContainer(packed);

  const again = decodeContainer(rerenderContainer(container));
  assert.equal(again.document.waypoints[0].at.lon, -3.6712345678901235);
  assert.equal(again.document.waypoints[0].at.arity, 3);
  assert.deepEqual([...again.attachments.keys()], ['a/photo.txt']);

  const written = decodeContainer(writeContainer(container));
  assert.equal(written.document.waypoints[0].at.lon, -3.671235);
  assert.equal(written.document.waypoints[0].at.arity, 2);
  assert.deepEqual([...written.attachments.keys()], ['a/photo.txt']);
});

test('repackaging keeps the document entry name it was read under', () => {
  const packed = writeArchive([{ name: 'some-export.vnx', data: Buffer.from(doc({ waypoints: [] })) }]);
  const container = decodeContainer(packed);
  assert.equal(decodeContainer(rerenderContainer(container)).documentEntry, 'some-export.vnx');
});

// The emit boundary (SPEC.md 4.1): what `encode` does to a model on the way out.

import test from 'node:test';
import assert from 'node:assert/strict';

import { decodeText } from '../src/decode.js';
import { encode } from '../src/index.js';
import { decodeContainer, encodeContainer, writeContainer } from '../src/vnz.js';
import { writeArchive } from '../src/zip.js';

const doc = (extra) => JSON.stringify({ vnx: '0.10.0', ...extra });

/** The `at` of the first waypoint, as the encoded document spells it. */
function encodedAt(document) {
  return JSON.parse(encode(document)).waypoints[0].at;
}

test('a coordinate is written at the precision the model holds', () => {
  // SPEC.md 4.1 does not constrain a writer's precision, and the six-places
  // paragraph there is advice with no requirement in it. A writer that rounded
  // would still be conforming; this one does not, so a document that arrives at
  // full binary64 precision leaves at full binary64 precision.
  const { document } = decodeText(
    doc({
      waypoints: [{ at: [-3.6712345678901235, 57.11812345678901, 340.26] }],
    }),
  );
  assert.deepEqual(encodedAt(document), [-3.6712345678901235, 57.11812345678901, 340.26]);
  assert.ok(encode(document).includes('-3.6712345678901235'));
});

test('encoding emits the shortest spelling of a position', () => {
  const { document } = decodeText(doc({ waypoints: [{ at: [1, 2, null] }] }));
  assert.deepEqual(encodedAt(document), [1, 2]);
});

test('shortening stops at an interior absence', () => {
  // Dropping an interior null would move the time into the elevation channel.
  const { document } = decodeText(
    doc({
      tracks: [
        {
          segments: [
            {
              pips: [
                [1, 2, null, 5],
                [1, 2, 3, null],
              ],
            },
          ],
        },
      ],
    }),
  );
  const pips = JSON.parse(encode(document)).tracks[0].segments[0].pips;
  assert.deepEqual(pips, [
    [1, 2, null, 5],
    [1, 2, 3],
  ]);
});

test('a channel beyond those this version names is not dropped', () => {
  // SPEC.md 4.1: a reader ignores channels beyond those it knows so that a
  // future channel is additive. Ignoring is not discarding.
  const { document } = decodeText(doc({ waypoints: [{ at: [1, 2, 3, 4, 5.55555] }] }));
  assert.deepEqual(encodedAt(document), [1, 2, 3, 4, 5.55555]);
});

test('a writer emits hidden only when it is true', () => {
  const { document } = decodeText(
    doc({
      networks: [
        { hidden: false, name: 'a' },
        { hidden: true, name: 'b' },
      ],
    }),
  );
  const networks = JSON.parse(encode(document)).networks;
  assert.equal('hidden' in networks[0], false);
  assert.equal(networks[1].hidden, true);
});

test('a re-encoded document carries the version it declared, not the reader own', () => {
  const { document } = decodeText('{"vnx": "0.10.99"}');
  assert.equal(JSON.parse(encode(document)).vnx, '0.10.99');
});

test('the output is compact: no space argument reaches JSON.stringify', () => {
  // SPEC.md 8.1 leaves whitespace to the writer, and a document is mostly
  // coordinates. Indentation would be pure size for no reader.
  const out = encode(decodeText(doc({ waypoints: [{ at: [1, 2] }] })).document);
  assert.equal(out, '{"vnx":"0.10.0","waypoints":[{"at":[1,2]}]}');
});

test('encoding does not mutate the model', () => {
  // Endpoint comparison and ring winding compare positions by full value, so a
  // writer that wrote back into the model would change the geometry rather than
  // its spelling. The shortening is on a copy.
  const { document } = decodeText(doc({ waypoints: [{ at: [-3.6712345678901235, 57.118, null] }] }));
  encode(document);
  assert.equal(document.waypoints[0].at.lon, -3.6712345678901235);
  assert.equal(document.waypoints[0].at.arity, 3);
});

test('a writer invents no negative zero', () => {
  // The writer touches no value, so a value that carried no negative zero in
  // cannot carry one out. `JSON.stringify` spells -0 as `0`, so the guard that
  // matters is on the VALUE.
  const { document } = decodeText(
    doc({
      waypoints: [{ at: [-0.00000004, -0.00000004, -0.04] }],
    }),
  );
  for (const channel of encodedAt(document)) assert.ok(!Object.is(channel, -0));
});

test('a container round-trips its document and its attachments', () => {
  const text = doc({ waypoints: [{ at: [-3.6712345678901235, 57.118, null] }] });
  const packed = encodeContainer(text, [['a/photo.txt', Buffer.from('x')]]);
  const container = decodeContainer(packed);

  const again = decodeContainer(writeContainer(container));
  // The document went through the encoder, so its position is shortened — and
  // its coordinate is unchanged, which is the other half of the same claim.
  assert.equal(again.document.waypoints[0].at.lon, -3.6712345678901235);
  assert.equal(again.document.waypoints[0].at.arity, 2);
  assert.deepEqual([...again.attachments.keys()], ['a/photo.txt']);
  assert.equal(again.attachments.get('a/photo.txt').toString('utf8'), 'x');
});

test('repackaging keeps the document entry name it was read under', () => {
  const packed = writeArchive([{ name: 'some-export.vnx', data: Buffer.from(doc({ waypoints: [] })) }]);
  const container = decodeContainer(packed);
  assert.equal(decodeContainer(writeContainer(container)).documentEntry, 'some-export.vnx');
});

test('every item but a folder keeps its pinned through a write and a read', () => {
  // SPEC.md 3.8 and 4.3: a record-level timestamp, kept verbatim, a value that
  // names no calendar instant included.
  const nodes = { n1: [1, 1], n2: [1, 2] };
  const arcs = { a1: { from: 'n1', to: 'n2', pips: [] } };
  const walk = [{ arc: 'a1', from: 'n1', to: 'n2' }];
  const ring = [
    [1, 1],
    [2, 1],
    [2, 2],
  ];
  const source = doc({
    networks: [
      {
        name: 'network',
        pinned: '2026-10-20T08:15:00Z',
        nodes,
        arcs,
        highlights: [{ pinned: '2026-10-21T09:00:00.250Z', walk }],
        points: [{ name: 'point', pinned: '2026-10-22T10:30:45.5Z', at: [1, 1.5] }],
        areas: [{ name: 'network area', pinned: '2026-10-23T11:00:00Z', polygon: ring }],
      },
    ],
    plans: [{ name: 'plan', pinned: '2026-10-24T12:00:00Z', nodes, arcs, walk }],
    tracks: [{ name: 'track', pinned: '2026-10-25T13:00:00.001Z', segments: [{ pips: [[1, 1]] }] }],
    waypoints: [
      { name: 'waypoint', pinned: '2026-10-26T14:00:00Z', at: [3, 3] },
      { name: 'unpinned', at: [4, 4] },
    ],
    areas: [{ name: 'area', pinned: '2026-13-45T99:99:99Z', polygon: ring }],
  });
  const pinned = (d) => {
    const out = {};
    for (const n of d.networks) {
      out[n.name] = n.pinned;
      n.highlights.forEach((h, i) => (out[`highlight ${i}`] = h.pinned));
      for (const i of [...n.points, ...n.areas]) out[i.name] = i.pinned;
    }
    for (const i of [...d.plans, ...d.tracks, ...d.waypoints, ...d.areas]) out[i.name] = i.pinned;
    return out;
  };
  const first = decodeText(source);
  assert.deepEqual(first.diagnostics, []);
  const second = decodeText(encode(first.document));
  assert.deepEqual(second.diagnostics, []);
  const want = {
    network: '2026-10-20T08:15:00Z',
    'highlight 0': '2026-10-21T09:00:00.250Z',
    point: '2026-10-22T10:30:45.5Z',
    'network area': '2026-10-23T11:00:00Z',
    plan: '2026-10-24T12:00:00Z',
    track: '2026-10-25T13:00:00.001Z',
    waypoint: '2026-10-26T14:00:00Z',
    unpinned: undefined,
    area: '2026-13-45T99:99:99Z',
  };
  assert.deepEqual(pinned(first.document), want);
  assert.deepEqual(pinned(second.document), want);
});

test('a writer emits pinned only when the item is pinned', () => {
  // SPEC.md 3.8: absence means not pinned.
  const { document } = decodeText(doc({ waypoints: [{ name: 'a', at: [1, 1] }] }));
  assert.equal('pinned' in JSON.parse(encode(document)).waypoints[0], false);
});

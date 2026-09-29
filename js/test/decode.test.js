// Reading: refusals, diagnostics, and the model state the corpus depends on.

import test from 'node:test';
import assert from 'node:assert/strict';

import { decodeText } from '../src/decode.js';
import { VnxRefusal } from '../src/errors.js';
import { encode } from '../src/index.js';

const doc = (extra) => JSON.stringify({ vnx: '0.2.0', ...extra });

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
  assert.equal(refusal('"0.2.0"').cause_code, 'not-a-vnx-document');
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

test("a MAJOR or MINOR outside the reader's series is refused; PATCH never is", () => {
  // SPEC.md 2.2: through 0.x each MINOR is its own series, and "a lower minor
  // is refused for the same reason as a higher one". Both directions are here
  // because a reader written as "greater than" passes the first two lines and
  // reads 0.1.9, which is the defect this pair exists to catch.
  assert.equal(refusal(doc0('0.3.0').text).cause_code, 'version-too-new');
  assert.equal(refusal(doc0('1.0.0').text).cause_code, 'version-too-new');
  assert.equal(refusal(doc0('0.1.9').text).cause_code, 'version-too-old');
  assert.equal(refusal(doc0('0.0.9').text).cause_code, 'version-too-old');
  for (const v of ['0.2.99', '0.2.0']) {
    assert.equal(decodeText(doc0(v).text).document.vnx, v);
  }
});

test("a refusal below the reader's series names the version and the series", () => {
  // SPEC.md 2.2 requires the version; the corpus additionally expects the
  // document's own series, so the user is told which reader they need.
  const err = refusal(doc0('0.1.9').text);
  assert.ok(err.message.includes('0.1.9'), err.message);
  assert.ok(err.message.includes('0.1 series'), err.message);
});

function doc0(v) {
  return { text: JSON.stringify({ vnx: v }) };
}

test('leading zeros are a legal spelling and compare numerically', () => {
  assert.equal(decodeText('{"vnx": "00.02.0000"}').document.vnx, '00.02.0000');
});

test('a NaN or Infinity literal is refused as malformed JSON', () => {
  assert.equal(refusal('{"vnx": "0.2.0", "a": NaN}').cause_code, 'malformed-json');
  assert.equal(refusal('{"vnx": "0.2.0", "a": Infinity}').cause_code, 'malformed-json');
});

test('a numeric literal that overflows to Infinity is refused as not finite', () => {
  // `1e400` is well-formed JSON and parses to Infinity. SPEC.md 4.2 forbids a
  // non-finite number throughout a document; the corpus has no case for this
  // spelling, so the guard is proved here.
  const err = refusal('{"vnx": "0.2.0", "a": 1e400}');
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

test('a trailing null survives into the model and is shortened on the way out', () => {
  // The model keeps the arity the document spelled; SPEC.md 4.1 puts the
  // shortest spelling at the emit boundary, so the two answers differ and both
  // are right.
  const { document } = decodeText('{"vnx":"0.2.0","waypoints":[{"at":[1,2,null]}]}');
  assert.equal(document.waypoints[0].at.arity, 3);
  assert.equal(encode(document), '{"vnx":"0.2.0","waypoints":[{"at":[1,2]}]}');
});

test('an interior absence keeps its null', () => {
  const { document } = decodeText(
    doc({
      tracks: [{ segments: [{ pips: [[1, 2, null, 5]] }] }],
    }),
  );
  const pip = document.tracks[0].segments[0].pips[0];
  assert.equal(pip.ele, null);
  assert.equal(pip.time, 5);
});

test('a channel beyond those the reader knows survives the round trip', () => {
  // SPEC.md 4.1 says a reader ignores channels beyond those it knows so that a
  // future channel is additive. Ignoring is not discarding: the position is
  // kept whole, so the unknown channel comes back out.
  const text = '{"vnx":"0.2.0","waypoints":[{"at":[1,2,3,4,5]}]}';
  assert.equal(encode(decodeText(text).document), text);
});

test('an unusable position drops its element and diagnoses', () => {
  const { document, diagnostics } = decodeText(doc({ waypoints: [{ at: [1] }] }));
  assert.equal(document.waypoints.length, 0);
  assert.equal(diagnostics.length, 1);
  assert.equal(diagnostics[0].path, '/waypoints/0/at');
});

test('a pip that will not parse drops the SEGMENT, not the pip', () => {
  // SPEC.md 6.2: geometry is continuous and structure is not. Dropping the pip
  // would leave a shorter, plausible, wrong line with nothing to indicate it;
  // dropping the segment moves the loss somewhere a consumer can see.
  const { document, diagnostics } = decodeText(
    doc({
      tracks: [
        {
          name: 't',
          segments: [
            {
              pips: [
                [1, 2, 0, 1000],
                ['x', 2],
                [3, 4, 0, 9000],
              ],
            },
            {
              pips: [
                [1, 2, 0, 1000],
                [3, 4, 0, 4000],
              ],
            },
          ],
        },
      ],
    }),
  );
  assert.equal(document.tracks.length, 1, 'the track survives');
  assert.equal(document.tracks[0].segments.length, 1, 'the bad segment is gone');
  // Moving time is the only thing that separates the two readings: dropping the
  // pip would keep segment 0 and answer 11000.
  assert.equal(document.tracks[0].movingTimeMs(), 3000);
  assert.deepEqual(
    diagnostics.map((d) => [d.path, d.code]),
    [['/tracks/0/segments/0/pips/1', 'position-unusable']],
  );
});

test('a pip that will not parse drops the ARC, and the highlight walking it then goes too', () => {
  // Two diagnostics, one per loss: the first says the arc is gone, the second
  // that the highlight is (SPEC.md 6.2).
  const { document, diagnostics } = decodeText(
    doc({
      networks: [
        {
          nodes: { n1: [1, 2], n2: [3, 4] },
          arcs: {
            broken: {
              from: 'n1',
              to: 'n2',
              pips: [
                [1, 2],
                [null, 3],
                [3, 4],
              ],
            },
          },
          highlights: [{ walk: [{ arc: 'broken', from: 'n1', to: 'n2' }] }],
        },
      ],
    }),
  );
  assert.equal(document.networks[0].arcs.size, 0, 'the arc is gone');
  assert.equal(document.networks[0].highlights.length, 0, 'and the highlight with it');
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]).sort(), [
    ['/networks/0/arcs/broken/pips/1', 'position-unusable'],
    ['/networks/0/highlights/0/walk/0', 'pass-arc-absent'],
  ]);
});

// --- SPEC.md 6.2 ------------------------------------------------------------

test('an unrecognised key is diagnosed, skipped, and the item kept', () => {
  const { document, diagnostics } = decodeText(doc({ waypoints: [{ at: [1, 2], nmae: 'x' }] }));
  assert.equal(document.waypoints.length, 1);
  assert.deepEqual(
    diagnostics.map((d) => [d.path, d.code]),
    [['/waypoints/0/nmae', 'unknown-key']],
  );
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
  const { document, diagnostics } = decodeText(
    doc({
      waypoints: [{ at: [1, 2], data: [{ value: 1 }, { '@type': 'com.example.x' }] }],
    }),
  );
  assert.equal(document.waypoints[0].data.length, 1);
  assert.deepEqual(
    diagnostics.map((d) => [d.path, d.code]),
    [['/waypoints/0/data/0', 'wrong-type-required']],
  );
});

test('a record body is never inspected, whatever its type says', () => {
  // SPEC.md 5.1: a record body is opaque apart from `@type`. A codec has no
  // vocabulary layer, so `size` here is neither validated nor dropped.
  const { document, diagnostics } = decodeText(
    doc({
      networks: [{ points: [{ at: [1, 2], data: [{ '@type': 'app.viatrail.settlement', size: 'metropolis' }] }] }],
    }),
  );
  assert.deepEqual(diagnostics, []);
  assert.equal(document.networks[0].points[0].data[0].size, 'metropolis');
});

test('an importance outside 1 to 5 is dropped and the item kept', () => {
  const { document, diagnostics } = decodeText(
    doc({
      waypoints: [{ at: [1, 2], style: { importance: 9, color: '#fff' } }],
    }),
  );
  // SPEC.md 6.2: a value outside a stated range is an unusable value in an
  // optional key.
  assert.deepEqual(
    diagnostics.map((d) => [d.path, d.code]),
    [['/waypoints/0/style/importance', 'wrong-type-optional']],
  );
  assert.deepEqual(document.waypoints[0].style, { color: '#fff' });
});

test('an unrecognised style key is carried without a diagnostic', () => {
  const { document, diagnostics } = decodeText(
    doc({
      waypoints: [{ at: [1, 2], style: { 'halo-width': 3 } }],
    }),
  );
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
  const { document, diagnostics } = decodeText(
    doc({
      folders: [{ name: 'No id' }],
      waypoints: [{ at: [1, 2], folder: 'anything' }],
    }),
  );
  assert.equal(document.folders.length, 0);
  assert.equal(document.waypoints.length, 1);
  // An absent REQUIRED key takes wrong-type-required, at the key (SPEC.md 6.2).
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]).sort(), [
    ['/folders/0/id', 'wrong-type-required'],
    ['/waypoints/0/folder', 'folder-absent'],
  ]);
});

test('a record-level timestamp with an offset drops the field and keeps the item', () => {
  const { document, diagnostics } = decodeText(
    doc({
      tracks: [{ segments: [], started: '2026-05-12T08:03:11+01:00' }],
    }),
  );
  assert.equal(document.tracks.length, 1);
  assert.equal(document.tracks[0].started, undefined);
  assert.equal(diagnostics[0].code, 'timestamp-not-utc');
});

test('fractional seconds are accepted on a record-level timestamp', () => {
  const { diagnostics } = decodeText(
    doc({
      tracks: [{ segments: [], started: '2026-05-12T08:03:11.250Z' }],
    }),
  );
  assert.deepEqual(diagnostics, []);
});

// --- moving time ------------------------------------------------------------

test('moving time sums per segment, and is unknown rather than zero', () => {
  const { document } = decodeText(
    doc({
      tracks: [
        {
          name: 'paused',
          segments: [
            {
              pips: [
                [1, 2, 0, 1000],
                [1, 2, 0, 4000],
              ],
            },
            {
              pips: [
                [1, 2, 0, 9000],
                [1, 2, 0, 10000],
              ],
            },
          ],
        },
        {
          name: 'timeless',
          segments: [
            {
              pips: [
                [1, 2],
                [3, 4],
              ],
            },
          ],
        },
      ],
    }),
  );
  assert.equal(document.tracks[0].movingTimeMs(), 4000);
  assert.equal(document.tracks[1].movingTimeMs(), null);
});

test('a partially timed track is unknown as a whole, not a partial sum', () => {
  // SPEC.md 3.5, ruled 2026-09-09: the rule that makes zero wrong for an
  // all-untimed track makes a partial sum wrong here. No corpus track is
  // partially timed, so nothing in the corpus can see this.
  const { document } = decodeText(
    doc({
      tracks: [
        {
          name: 'half',
          segments: [
            {
              pips: [
                [1, 2, 0, 1000],
                [1, 2, 0, 4000],
              ],
            },
            {
              pips: [
                [1, 2],
                [3, 4],
              ],
            },
          ],
        },
      ],
    }),
  );
  assert.equal(document.tracks[0].movingTimeMs(), null);
});

test('an untimed segment makes the track unknown wherever it sits', () => {
  const order = (segments) =>
    decodeText(
      doc({
        tracks: [{ name: 't', segments }],
      }),
    ).document.tracks[0].movingTimeMs();
  const timed = {
    pips: [
      [1, 2, 0, 1000],
      [1, 2, 0, 4000],
    ],
  };
  const untimed = {
    pips: [
      [1, 2],
      [3, 4],
    ],
  };
  assert.equal(order([untimed, timed]), null, 'untimed first');
  assert.equal(order([timed, untimed]), null, 'untimed last');
  assert.equal(order([timed, untimed, timed]), null, 'untimed in the middle');
  assert.equal(order([timed, timed]), 6000, 'all timed');
});

test('an EMPTY segment does not make a track unknown', () => {
  // A segment with no pips has no span to measure, so it is not an unmeasured
  // span: it contributes nothing and the rest of the track is still known.
  const { document } = decodeText(
    doc({
      tracks: [
        {
          name: 't',
          segments: [
            { pips: [] },
            {
              pips: [
                [1, 2, 0, 1000],
                [1, 2, 0, 4000],
              ],
            },
          ],
        },
      ],
    }),
  );
  assert.equal(document.tracks[0].movingTimeMs(), 3000);
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
  const { document } = decodeText('{"vnx": "0.2.0"}');
  assert.equal(document.waypoints, undefined);
  assert.equal(encode(document), '{"vnx":"0.2.0"}');
});

test('an empty collection is distinct from an absent one', () => {
  const { document } = decodeText('{"vnx": "0.2.0", "waypoints": []}');
  assert.deepEqual(document.waypoints, []);
  assert.equal(encode(document), '{"vnx":"0.2.0","waypoints":[]}');
});

test('moving time reads the FIRST and LAST pip, not the first and last TIMED pip', () => {
  // SPEC.md 3.5 is derivable only when every segment carries a time on its
  // first and last pip. A segment whose interior pips are timed and whose ends
  // are not is UNMEASURABLE, not a shorter span: the walk began before the
  // first timestamp and ended after the last, by an unknown amount. A reader
  // that reaches inward for the first timed pip reports a number narrower than
  // the truth, with nothing to say so.
  const mk = (pips) =>
    decodeText(doc({ tracks: [{ name: 't', segments: [{ pips }] }] })).document.tracks[0].movingTimeMs();
  assert.equal(
    mk([
      [1, 2],
      [1, 2, 0, 3000],
      [1, 2, 0, 5000],
      [3, 4],
    ]),
    null,
  );
  assert.equal(
    mk([
      [1, 2, 0, 1000],
      [1, 2, 0, null],
    ]),
    null,
    'an explicit null time is untimed',
  );
  assert.equal(
    mk([
      [1, 2, 0, 1000],
      [1, 2, 0, 3000],
      [1, 2, 0, 4000],
    ]),
    3000,
  );
});

test('an untimed INTERIOR pip does not make a segment unmeasurable', () => {
  // Only the ends are read, so a gap in the middle is spanned rather than
  // fatal. This is the boundary of the rule above and is easy to over-apply.
  const { document } = decodeText(
    doc({
      tracks: [
        {
          name: 't',
          segments: [
            {
              pips: [
                [1, 2, 0, 1000],
                [1, 2],
                [1, 2, 0, 4000],
              ],
            },
          ],
        },
      ],
    }),
  );
  assert.equal(document.tracks[0].movingTimeMs(), 3000);
});

test('a wrong-typed position distinguishes a lost field from a lost item', () => {
  // SPEC.md 6.2 gives two remedies, and the CODE is what says which happened —
  // otherwise a consumer has to re-derive required-ness from 3.10's table to
  // find out whether the rest of the item survived.
  const { document, diagnostics } = decodeText(
    doc({
      waypoints: [
        { name: 'kept', at: [1, 2], hidden: 'yes' },
        { name: 'dropped', at: 'beside the burn' },
      ],
    }),
  );
  assert.equal(document.waypoints.length, 1);
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]).sort(), [
    ['/waypoints/0/hidden', 'wrong-type-optional'],
    ['/waypoints/1/at', 'wrong-type-required'],
  ]);
});

test('a position that is an ELEMENT keeps position-unusable even though its container goes', () => {
  // The code distinguishes a WRONG-TYPED KEY from an unusable position, and
  // that boundary is unchanged by the container rule: `at` is a key, a pip is
  // an element. What changed is which thing is dropped, not which fact is
  // reported — the code names the fault, the remedy is SPEC.md 6.2's.
  const { document, diagnostics } = decodeText(
    doc({
      tracks: [{ name: 't', segments: [{ pips: [[1, 2], 'nope', [3, 4]] }] }],
    }),
  );
  assert.equal(document.tracks[0].segments.length, 0);
  assert.deepEqual(
    diagnostics.map((d) => [d.path, d.code]),
    [['/tracks/0/segments/0/pips/1', 'position-unusable']],
  );
});

// Walks, arcs and rings (SPEC.md 3.3, 3.4, 3.7 and 6.2): the reader's remedies,
// the encoder's refusals, and the line a walk draws.

import test from 'node:test';
import assert from 'node:assert/strict';

import { decodeText } from '../src/decode.js';
import { encode, walkLegs, walkLine } from '../src/index.js';
import { nearestOnSegment, EARTH_RADIUS_M } from '../src/geo.js';

const doc = (extra) => JSON.stringify({ vnx: '0.2.0', ...extra });

// a --ab-- b --bc-- c, and a --ad-- d. Node `b` has an elevation.
const NODES = { a: [0, 0], b: [0.001, 0, 100], c: [0.002, 0], d: [0, 0.001] };
const ARCS = {
  ab: { from: 'a', to: 'b', pips: [] },
  bc: { from: 'b', to: 'c', pips: [[0.0015, 0.0001, 90]] },
  ad: { from: 'a', to: 'd', pips: [] },
};

/** Read one highlight over the fixed graph. */
function readWalk(walk, extra = {}) {
  return decodeText(doc({ networks: [{ nodes: NODES, arcs: ARCS, highlights: [{ walk, ...extra }] }] }));
}

const pairs = (diagnostics) => diagnostics.map((d) => [d.path, d.code]);
const W = '/networks/0/highlights/0/walk';

// --- valid walks ------------------------------------------------------------

test('a node-to-node walk is read, and a pass without from or to is derived', () => {
  const { document, diagnostics } = readWalk([
    { arc: 'ab', from: 'a' },
    { arc: 'bc', to: 'c' },
  ]);
  assert.deepEqual(diagnostics, []);
  assert.equal(document.networks[0].highlights[0].walk.length, 2);
});

test('an empty walk is valid and covers nothing', () => {
  const { document, diagnostics } = readWalk([]);
  assert.deepEqual(diagnostics, []);
  assert.deepEqual(document.networks[0].highlights[0].walk, []);
});

test('a pass with a turn and no `to` leaves by the node it entered by', () => {
  // Out along ab, back to a, then on along ad. Without the turn the walk would
  // be at b and ad would not join.
  const turned = readWalk([
    { arc: 'ab', from: 'a', turn: [0.0005, 0] },
    { arc: 'ad', to: 'd' },
  ]);
  assert.deepEqual(turned.diagnostics, []);
  const straight = readWalk([
    { arc: 'ab', from: 'a' },
    { arc: 'ad', to: 'd' },
  ]);
  assert.deepEqual(pairs(straight.diagnostics), [[`${W}/1`, 'walk-not-connected']]);
});

test('a position `from` requires a `to`, and a pass whose entry and exit are one point is legal', () => {
  const { diagnostics } = readWalk([{ arc: 'ab', from: [0.0005, 0], to: [0.0005, 0] }]);
  assert.deepEqual(diagnostics, []);
});

test('"pinned": false is read as absent, and pinned true is kept', () => {
  const { document, diagnostics } = readWalk([
    { arc: 'ab', from: 'a', pinned: false },
    { arc: 'bc', to: 'c', pinned: true },
  ]);
  assert.deepEqual(diagnostics, []);
  const walk = document.networks[0].highlights[0].walk;
  assert.equal(walk[0].pinned, undefined);
  assert.equal(walk[1].pinned, true);
  assert.deepEqual(JSON.parse(encode(document)).networks[0].highlights[0].walk, [
    { arc: 'ab', from: 'a' },
    { arc: 'bc', to: 'c', pinned: true },
  ]);
});

test('a wrong-typed `pinned` drops the field and keeps the highlight', () => {
  const { document, diagnostics } = readWalk([{ arc: 'ab', from: 'a', to: 'b', pinned: 'yes' }]);
  assert.deepEqual(pairs(diagnostics), [[`${W}/0/pinned`, 'wrong-type-optional']]);
  assert.equal(document.networks[0].highlights[0].walk[0].pinned, undefined);
});

// --- SPEC.md 6.2: each rule, at its path -------------------------------------

test('a walk that breaks the rule for `from` and `to` is dropped, named at the right place', () => {
  const cases = [
    [[{ arc: 'ab', to: 'b' }], `${W}/0`],
    [[{ arc: 'ab', from: 'a' }], `${W}/0`],
    [
      [
        { arc: 'ab', from: 'a' },
        { arc: 'bc', from: 'b', to: 'c' },
      ],
      `${W}/1/from`,
    ],
    [
      [
        { arc: 'ab', from: 'a', to: 'b' },
        { arc: 'bc', to: 'c' },
      ],
      `${W}/0/to`,
    ],
    [
      [
        { arc: 'ab', from: [0.0005, 0] },
        { arc: 'bc', to: 'c' },
      ],
      `${W}/0`,
    ],
  ];
  for (const [walk, path] of cases) {
    const { document, diagnostics } = readWalk(walk);
    assert.deepEqual(pairs(diagnostics), [[path, 'walk-spelling']], JSON.stringify(walk));
    assert.equal(document.networks[0].highlights.length, 0);
  }
});

test('a walk that is not connected is dropped, named at the right place', () => {
  const cases = [
    // A `from` node that is not an end of the arc.
    [[{ arc: 'ab', from: 'c', to: 'b' }], `${W}/0/from`],
    // A pass that does not touch the node the walk is at.
    [
      [
        { arc: 'ad', from: 'd' },
        { arc: 'bc', to: 'c' },
      ],
      `${W}/1`,
    ],
    // A `to` node that is not an end of the arc.
    [[{ arc: 'ab', from: 'a', to: 'd' }], `${W}/0/to`],
    // A `to` position on a pass other than the last.
    [
      [
        { arc: 'ab', from: [0.0005, 0], to: [0.0006, 0] },
        { arc: 'bc', to: 'c' },
      ],
      `${W}/0/to`,
    ],
  ];
  for (const [walk, path] of cases) {
    const { document, diagnostics } = readWalk(walk);
    assert.deepEqual(pairs(diagnostics), [[path, 'walk-not-connected']], JSON.stringify(walk));
    assert.equal(document.networks[0].highlights.length, 0);
  }
});

test('a pass carrying both `turn` and `pinned` drops the highlight', () => {
  const { document, diagnostics } = readWalk([{ arc: 'ab', from: 'a', to: 'a', turn: [0.0005, 0], pinned: true }]);
  assert.deepEqual(pairs(diagnostics), [[`${W}/0`, 'pinned-turn']]);
  assert.equal(document.networks[0].highlights.length, 0);
});

test('a pass that is not an object, or names no arc, drops the highlight', () => {
  for (const pass of [
    7,
    [],
    { from: 'a', to: 'b' },
    { arc: 3, from: 'a', to: 'b' },
    { arc: 'ghost', from: 'a', to: 'b' },
  ]) {
    const { document, diagnostics } = readWalk([pass]);
    assert.deepEqual(pairs(diagnostics), [[`${W}/0`, 'pass-arc-absent']], JSON.stringify(pass));
    assert.equal(document.networks[0].highlights.length, 0);
  }
});

test('a `from`, `to` or `turn` that will not parse drops the highlight, named at the key', () => {
  const cases = [
    [[{ arc: 'ab', from: [1], to: 'b' }], `${W}/0/from`],
    [[{ arc: 'ab', from: 'a', to: 7 }], `${W}/0/to`],
    [[{ arc: 'ab', from: 'a', to: 'a', turn: 'b' }], `${W}/0/turn`],
  ];
  for (const [walk, path] of cases) {
    const { document, diagnostics } = readWalk(walk);
    assert.deepEqual(pairs(diagnostics), [[path, 'position-unusable']], JSON.stringify(walk));
    assert.equal(document.networks[0].highlights.length, 0);
  }
});

test('only the FIRST problem in a walk is reported', () => {
  // Pass 0 breaks the spelling rule and pass 1 names an absent arc. The walk
  // is checked pass by pass, so only pass 0 is named.
  const { diagnostics } = readWalk([{ arc: 'ab' }, { arc: 'ghost', to: 'b' }]);
  assert.deepEqual(pairs(diagnostics), [[`${W}/0`, 'walk-spelling']]);
});

test('within a pass, the checks run in the order SPEC.md 6.2 lists', () => {
  // An absent arc is check 1: nothing else about the pass is examined, so its
  // unknown key is not reported.
  assert.deepEqual(pairs(readWalk([{ arc: 'ghost', from: 'a', to: 'b', nmae: 1 }]).diagnostics), [
    [`${W}/0`, 'pass-arc-absent'],
  ]);
  // An unknown key is check 2 and does not drop the highlight, so it is reported
  // beside the later fault.
  assert.deepEqual(pairs(readWalk([{ arc: 'ab', to: 'b', nmae: 1 }]).diagnostics).sort(), [
    [`${W}/0`, 'walk-spelling'],
    [`${W}/0/nmae`, 'unknown-key'],
  ]);
  // A position that will not parse (check 4) is found before pin and turn (6).
  assert.deepEqual(pairs(readWalk([{ arc: 'ab', from: 'a', to: 'a', turn: 'x', pinned: true }]).diagnostics), [
    [`${W}/0/turn`, 'position-unusable'],
  ]);
  // Pin and turn (6) before the spelling rule (7).
  assert.deepEqual(pairs(readWalk([{ arc: 'ab', turn: [0.0005, 0], pinned: true }]).diagnostics), [
    [`${W}/0`, 'pinned-turn'],
  ]);
  // The spelling rule (7) before connection (8).
  assert.deepEqual(pairs(readWalk([{ arc: 'ab', from: 'c' }]).diagnostics), [[`${W}/0`, 'walk-spelling']]);
});

test('a highlight dropped for its walk still reports the diagnostics of its other keys', () => {
  const { diagnostics } = readWalk([{ arc: 'ghost', from: 'a', to: 'b' }], { hidden: 'no' });
  assert.deepEqual(pairs(diagnostics).sort(), [
    ['/networks/0/highlights/0/hidden', 'wrong-type-optional'],
    [`${W}/0`, 'pass-arc-absent'],
  ]);
});

test('a highlight with no `walk`, or a `walk` that is not an array, is dropped', () => {
  for (const highlight of [{ name: 'x' }, { walk: {} }]) {
    const { document, diagnostics } = decodeText(
      doc({ networks: [{ nodes: NODES, arcs: ARCS, highlights: [highlight] }] }),
    );
    assert.deepEqual(pairs(diagnostics), [[W, 'wrong-type-required']]);
    assert.equal(document.networks[0].highlights.length, 0);
  }
});

// --- channels ---------------------------------------------------------------

test('an elevation or a time on a walk position is dropped, and the pass kept', () => {
  const { document, diagnostics } = readWalk([{ arc: 'ab', from: [0.0002, 0, 50], to: [0.0005, 0, null, 1000] }]);
  assert.deepEqual(pairs(diagnostics).sort(), [
    [`${W}/0/from`, 'extra-channel'],
    [`${W}/0/to`, 'extra-channel'],
  ]);
  const pass = document.networks[0].highlights[0].walk[0];
  assert.deepEqual(pass.from.position.channels, [0.0002, 0]);
  assert.deepEqual(pass.to.position.channels, [0.0005, 0]);
});

test('a null elevation on a walk position is not an extra channel', () => {
  const { diagnostics } = readWalk([{ arc: 'ab', from: [0.0002, 0, null], to: 'b' }]);
  assert.deepEqual(diagnostics, []);
});

test('a time on a node is dropped with extra-channel, and the node and its elevation kept', () => {
  const { document, diagnostics } = decodeText(
    doc({ networks: [{ nodes: { a: [1, 2, 300, 5000], b: [1, 2, null, null] } }] }),
  );
  assert.deepEqual(pairs(diagnostics), [['/networks/0/nodes/a', 'extra-channel']]);
  assert.deepEqual(document.networks[0].nodes.get('a').channels, [1, 2, 300]);
});

test('a time on arc pips is reported ONCE for the arc, and every time is dropped', () => {
  const { document, diagnostics } = decodeText(
    doc({
      networks: [
        {
          nodes: { a: [0, 0], b: [1, 0] },
          arcs: {
            ab: {
              from: 'a',
              to: 'b',
              pips: [
                [0.2, 0, 5, 100],
                [0.4, 0],
                [0.6, 0, null, 300],
              ],
            },
          },
        },
      ],
    }),
  );
  assert.deepEqual(pairs(diagnostics), [['/networks/0/arcs/ab/pips', 'arc-pip-time']]);
  assert.deepEqual(
    document.networks[0].arcs.get('ab').pips.map((p) => p.channels),
    [
      [0.2, 0, 5],
      [0.4, 0],
      [0.6, 0, null],
    ],
  );
});

// --- arcs -------------------------------------------------------------------

test('a self-loop arc is dropped, and each highlight walking it goes with its own diagnostic', () => {
  const { document, diagnostics } = decodeText(
    doc({
      networks: [
        {
          nodes: NODES,
          arcs: { ...ARCS, loop: { from: 'b', to: 'b', pips: [[0.001, 0.001]] } },
          highlights: [
            {
              walk: [
                { arc: 'ab', from: 'a' },
                { arc: 'loop', to: 'b' },
              ],
            },
            { walk: [{ arc: 'ab', from: 'a', to: 'b' }] },
          ],
        },
      ],
    }),
  );
  assert.deepEqual(pairs(diagnostics).sort(), [
    ['/networks/0/arcs/loop', 'self-loop-arc'],
    ['/networks/0/highlights/0/walk/1', 'pass-arc-absent'],
  ]);
  assert.equal(document.networks[0].arcs.size, 3);
  assert.equal(document.networks[0].highlights.length, 1, 'only the highlight that walks the loop goes');
});

test('an arc naming absent nodes is dropped, with one diagnostic per absent end', () => {
  const { document, diagnostics } = decodeText(
    doc({
      networks: [
        {
          nodes: { a: [0, 0] },
          arcs: { x: { from: 'ghost', to: 'phantom', pips: [] }, y: { from: 'a', to: 'ghost', pips: [] } },
        },
      ],
    }),
  );
  assert.deepEqual(pairs(diagnostics).sort(), [
    ['/networks/0/arcs/x/from', 'arc-node-absent'],
    ['/networks/0/arcs/x/to', 'arc-node-absent'],
    ['/networks/0/arcs/y/to', 'arc-node-absent'],
  ]);
  assert.equal(document.networks[0].arcs.size, 0);
});

test('a node that will not parse takes its arcs, and they take their highlights', () => {
  const { document, diagnostics } = readWalkWithNodes({ ...NODES, b: 'here' });
  assert.deepEqual(pairs(diagnostics).sort(), [
    ['/networks/0/arcs/ab/to', 'arc-node-absent'],
    ['/networks/0/arcs/bc/from', 'arc-node-absent'],
    ['/networks/0/highlights/0/walk/0', 'pass-arc-absent'],
    ['/networks/0/nodes/b', 'position-unusable'],
  ]);
  assert.equal(document.networks[0].highlights.length, 0);
});

function readWalkWithNodes(nodes) {
  return decodeText(
    doc({ networks: [{ nodes, arcs: ARCS, highlights: [{ walk: [{ arc: 'ab', from: 'a', to: 'b' }] }] }] }),
  );
}

test('an arc missing a required key is dropped, named at the key', () => {
  const { diagnostics } = decodeText(
    doc({
      networks: [{ nodes: NODES, arcs: { x: { from: 'a', pips: [] }, y: { from: 'a', to: 'b' } } }],
    }),
  );
  assert.deepEqual(pairs(diagnostics).sort(), [
    ['/networks/0/arcs/x/to', 'wrong-type-required'],
    ['/networks/0/arcs/y/pips', 'wrong-type-required'],
  ]);
});

// --- routes -----------------------------------------------------------------

const route = (walk, extra = {}) =>
  decodeText(
    doc({
      routes: [{ nodes: NODES, arcs: ARCS, walk, ...extra }],
    }),
  );

test('a route owns its graph and walks it node to node', () => {
  const { document, diagnostics } = route([
    { arc: 'ab', from: 'b' },
    { arc: 'ad', to: 'd' },
  ]);
  assert.deepEqual(diagnostics, []);
  const r = document.routes[0];
  assert.equal(r.nodes.size, 4);
  assert.equal(r.arcs.size, 3);
  assert.equal(r.walk.length, 2);
});

test('a position, a turn or a pin in a route drops the route, named at the key', () => {
  const cases = [
    [[{ arc: 'ab', from: [0.0005, 0], to: 'b' }], '/routes/0/walk/0/from'],
    [[{ arc: 'ab', from: 'a', to: [0.0005, 0] }], '/routes/0/walk/0/to'],
    [[{ arc: 'ab', from: 'a', to: 'a', turn: [0.0005, 0] }], '/routes/0/walk/0/turn'],
    [[{ arc: 'ab', from: 'a', to: 'b', pinned: true }], '/routes/0/walk/0/pinned'],
  ];
  for (const [walk, path] of cases) {
    const { document, diagnostics } = route(walk);
    assert.deepEqual(pairs(diagnostics), [[path, 'route-pin-or-position']], JSON.stringify(walk));
    assert.equal(document.routes.length, 0);
  }
});

test('where a highlight would be dropped, the route is', () => {
  const { document, diagnostics } = route([
    { arc: 'ab', from: 'a' },
    { arc: 'bc', from: 'b', to: 'c' },
  ]);
  assert.deepEqual(pairs(diagnostics), [['/routes/0/walk/1/from', 'walk-spelling']]);
  assert.equal(document.routes.length, 0);
});

test('a route with no `walk` is dropped', () => {
  const { document, diagnostics } = decodeText(doc({ routes: [{ nodes: NODES, arcs: ARCS }] }));
  assert.deepEqual(pairs(diagnostics), [['/routes/0/walk', 'wrong-type-required']]);
  assert.equal(document.routes.length, 0);
});

test('a route arc that is a self-loop takes the route with it', () => {
  const { document, diagnostics } = decodeText(
    doc({
      routes: [
        {
          nodes: NODES,
          arcs: { loop: { from: 'a', to: 'a', pips: [[1, 1]] } },
          walk: [{ arc: 'loop', from: 'a', to: 'a' }],
        },
      ],
    }),
  );
  assert.deepEqual(pairs(diagnostics).sort(), [
    ['/routes/0/arcs/loop', 'self-loop-arc'],
    ['/routes/0/walk/0', 'pass-arc-absent'],
  ]);
  assert.equal(document.routes.length, 0);
});

// --- rings ------------------------------------------------------------------

const area = (polygon) => decodeText(doc({ areas: [{ polygon }] }));

test('an open ring of three distinct positions is read as written', () => {
  const { document, diagnostics } = area([
    [0, 0],
    [1, 0],
    [1, 1],
  ]);
  assert.deepEqual(diagnostics, []);
  assert.equal(document.areas[0].polygon.length, 3);
});

test('a closed ring loses its last position only, and the area is kept', () => {
  const { document, diagnostics } = area([
    [0, 0, 5],
    [1, 0],
    [1, 1],
    [0, 0, 9],
  ]);
  // Equal in longitude and latitude is a repeat, whatever the elevation.
  assert.deepEqual(pairs(diagnostics), [['/areas/0/polygon/3', 'ring-closed']]);
  assert.deepEqual(
    document.areas[0].polygon.map((p) => p.channels),
    [
      [0, 0, 5],
      [1, 0],
      [1, 1],
    ],
  );
});

test('a ring of fewer than three distinct positions drops the area', () => {
  for (const ring of [
    [],
    [[0, 0]],
    [
      [0, 0],
      [1, 0],
    ],
    [
      [0, 0],
      [1, 0],
      [0, 0, 7],
      [1, 0],
    ],
  ]) {
    const { document, diagnostics } = area(ring);
    assert.deepEqual(pairs(diagnostics), [['/areas/0/polygon', 'ring-too-few-points']], JSON.stringify(ring));
    assert.equal(document.areas.length, 0);
  }
});

test('a closed ring with too few distinct positions takes both diagnostics', () => {
  const { document, diagnostics } = area([
    [0, 0],
    [1, 0],
    [0, 0],
  ]);
  assert.deepEqual(pairs(diagnostics).sort(), [
    ['/areas/0/polygon', 'ring-too-few-points'],
    ['/areas/0/polygon/2', 'ring-closed'],
  ]);
  assert.equal(document.areas.length, 0);
});

test('only the last position is dropped, even where the one before it repeats too', () => {
  const { document, diagnostics } = area([
    [0, 0],
    [1, 0],
    [1, 1],
    [0, 0],
    [0, 0],
  ]);
  assert.deepEqual(pairs(diagnostics), [['/areas/0/polygon/4', 'ring-closed']]);
  assert.equal(document.areas[0].polygon.length, 4);
});

// --- the positions index ------------------------------------------------------

test('the positions index holds walk positions and route nodes, and no node key', () => {
  const { positions } = decodeText(
    doc({
      networks: [
        {
          nodes: NODES,
          arcs: ARCS,
          highlights: [{ walk: [{ arc: 'ab', from: [0.0002, 0], to: 'b', turn: [0.0007, 0] }] }],
        },
      ],
      routes: [{ nodes: NODES, arcs: ARCS, walk: [{ arc: 'ab', from: 'a', to: 'b' }] }],
    }),
  );
  assert.equal(positions.get('/networks/0/highlights/0/walk/0/from').lon, 0.0002);
  assert.equal(positions.get('/networks/0/highlights/0/walk/0/turn').lon, 0.0007);
  assert.equal(positions.has('/networks/0/highlights/0/walk/0/to'), false);
  assert.equal(positions.get('/routes/0/nodes/b').lon, 0.001);
  assert.equal(positions.get('/routes/0/arcs/bc/pips/0').lon, 0.0015);
});

// --- the encoder refuses what its reader would drop -------------------------

function readBack(extra) {
  const { document, diagnostics } = decodeText(doc(extra));
  assert.deepEqual(diagnostics, []);
  return document;
}

const GRAPH = () =>
  readBack({
    networks: [{ nodes: NODES, arcs: ARCS, highlights: [{ name: 'c', walk: [{ arc: 'ab', from: 'a', to: 'b' }] }] }],
    routes: [{ name: 'r', nodes: NODES, arcs: ARCS, walk: [{ arc: 'ab', from: 'a', to: 'b' }] }],
    areas: [
      {
        name: 'z',
        polygon: [
          [0, 0],
          [1, 0],
          [1, 1],
        ],
      },
    ],
  });

test('a legal walk, route and ring are written back as read', () => {
  const text = doc({
    networks: [
      {
        nodes: NODES,
        arcs: ARCS,
        highlights: [
          { walk: [{ arc: 'ab', from: [0.0002, 0], to: [0.0003, 0] }] },
          {
            walk: [
              { arc: 'ab', from: 'a', turn: [0.0005, 0] },
              { arc: 'ad', to: 'd', pinned: true },
            ],
          },
        ],
      },
    ],
    routes: [
      {
        nodes: NODES,
        arcs: ARCS,
        walk: [
          { arc: 'bc', from: 'c' },
          { arc: 'ab', to: 'a' },
        ],
      },
    ],
    areas: [
      {
        polygon: [
          [0, 0],
          [1, 0],
          [1, 1],
        ],
      },
    ],
  });
  assert.deepEqual(JSON.parse(encode(readBack(JSON.parse(text)))), JSON.parse(text));
});

const refusals = {
  'a closed ring': (d) => {
    d.areas[0].polygon.push(d.areas[0].polygon[0]);
  },
  'a ring of two distinct positions': (d) => {
    d.areas[0].polygon.pop();
  },
  'a self-loop arc': (d) => {
    d.networks[0].arcs.get('ad').to = 'a';
  },
  'an arc naming an absent node': (d) => {
    d.routes[0].arcs.get('ad').to = 'ghost';
  },
  'a walk naming an absent arc': (d) => {
    d.networks[0].highlights[0].walk[0].arc = 'ghost';
  },
  'a walk missing its last `to`': (d) => {
    d.networks[0].highlights[0].walk[0].to = undefined;
  },
  'a walk that does not join': (d) => {
    d.networks[0].highlights[0].walk[0].to.node = 'd';
  },
  'a pass with turn and pin': (d) => {
    const p = d.networks[0].highlights[0].walk[0];
    p.turn = d.networks[0].nodes.get('a');
    p.pinned = true;
  },
  'a turn in a route': (d) => {
    d.routes[0].walk[0].turn = d.routes[0].nodes.get('c');
  },
  'a time on a node': (d) => {
    d.networks[0].nodes.get('a').channels.push(null, 5);
  },
  'a time on an arc pip': (d) => {
    d.networks[0].arcs.get('bc').pips[0].channels.push(7);
  },
  'an elevation on a walk position': (d) => {
    const n = d.networks[0];
    n.highlights[0].walk[0].to = { kind: 'position', position: n.nodes.get('b') };
  },
};

for (const [what, spoil] of Object.entries(refusals)) {
  test(`encode refuses ${what}`, () => {
    const d = GRAPH();
    assert.doesNotThrow(() => encode(d), 'the unspoiled document must encode, or this proves nothing');
    spoil(d);
    assert.throws(() => encode(d), /^Error: vnx: /);
  });
}

// --- the line a walk draws --------------------------------------------------

const lonLat = (line) => line.map((p) => [p.lon, p.lat]);

/** An interpolated point is arithmetic, so it is compared to within 1e-15 degrees. */
function assertLine(got, want) {
  assert.equal(got.length, want.length, JSON.stringify(lonLat(got)));
  got.forEach((p, i) => {
    assert.ok(
      Math.abs(p.lon - want[i][0]) < 1e-15 && Math.abs(p.lat - want[i][1]) < 1e-15,
      `${JSON.stringify(lonLat(got))} is not ${JSON.stringify(want)}`,
    );
  });
}

function graph(walk) {
  const d = readBack({ networks: [{ nodes: NODES, arcs: ARCS, highlights: [{ walk }] }] });
  const n = d.networks[0];
  return [n.nodes, n.arcs, n.highlights[0].walk];
}

test('a pass walking its arc from `to` to `from` is drawn reversed', () => {
  const legs = walkLegs(
    ...graph([
      { arc: 'bc', from: 'c' },
      { arc: 'ab', to: 'a' },
    ]),
  );
  assert.deepEqual(legs.map(lonLat), [
    [
      [0.002, 0],
      [0.0015, 0.0001],
      [0.001, 0],
    ],
    [
      [0.001, 0],
      [0, 0],
    ],
  ]);
});

test("a node end is the arc's own end, and keeps its elevation", () => {
  const [leg] = walkLegs(...graph([{ arc: 'ab', from: 'a', to: 'b' }]));
  assert.deepEqual(leg[1].channels, [0.001, 0, 100]);
});

test('a position is cut at the nearest point, at any distance, with no elevation', () => {
  // 111 m north of the line, and still a point on the arc: SPEC.md 3.3.3 sets
  // no distance limit.
  const [leg] = walkLegs(...graph([{ arc: 'ab', from: [0.0004, 0.001], to: 'b' }]));
  assertLine(leg, [
    [0.0004, 0],
    [0.001, 0],
  ]);
  assert.equal(leg[0].ele, undefined);
});

test('a turn is drawn out and back', () => {
  const walk = [
    { arc: 'ab', from: 'a', turn: [0.0007, 0] },
    { arc: 'ad', to: 'd' },
  ];
  const legs = walkLegs(...graph(walk));
  assertLine(legs[0], [
    [0, 0],
    [0.0007, 0],
    [0, 0],
  ]);
  assertLine(legs[1], [
    [0, 0],
    [0, 0.001],
  ]);
  // Joined, the shared point where one leg ends and the next begins is written once.
  assertLine(walkLine(...graph(walk)), [
    [0, 0],
    [0.0007, 0],
    [0, 0],
    [0, 0.001],
  ]);
});

test('where two segments are equally near, the earlier one wins', () => {
  // The arc runs out from `a` to (1, 0) and back along the same line to `d`,
  // which sits where `a` does, so the position is exactly as near to both
  // segments. The earlier one leaves the pip on the leg; the later one would
  // not.
  const d = readBack({
    networks: [
      {
        nodes: { a: [0, 0], d: [0, 0] },
        arcs: { v: { from: 'a', to: 'd', pips: [[1, 0]] } },
        highlights: [{ walk: [{ arc: 'v', from: [0.5, 0.001], to: 'd' }] }],
      },
    ],
  });
  const n = d.networks[0];
  const [leg] = walkLegs(n.nodes, n.arcs, n.highlights[0].walk);
  assertLine(leg, [
    [0.5, 0],
    [1, 0],
    [0, 0],
  ]);
});

test('a walk that does not resolve is an error, never a guess', () => {
  const [nodes, arcs, walk] = graph([
    { arc: 'ab', from: 'a' },
    { arc: 'bc', to: 'c' },
  ]);
  walk[1].arc = 'ad';
  assert.throws(() => walkLegs(nodes, arcs, walk), /walk-not-connected/);
  walk[1].arc = 'ghost';
  assert.throws(() => walkLegs(nodes, arcs, walk), /pass-arc-absent/);
});

test("the distance uses the cosine of the POSITION's latitude, for every vertex", () => {
  // SPEC.md 3.3.3 states the model exactly. A position 0.01 degrees west of a
  // north-south segment that starts 0.001 degrees south of it: the cosine of
  // the segment start's latitude would read about 2 cm further.
  const at = (lon, lat) => ({ lon, lat });
  const { metres } = nearestOnSegment(at(0, 60.001), at(0.01, 60), at(0.01, 61));
  const want = 0.01 * (Math.PI / 180) * EARTH_RADIUS_M * Math.cos(60.001 * (Math.PI / 180));
  assert.ok(Math.abs(metres - want) < 1e-6, `${metres} m, want ${want} m`);
});

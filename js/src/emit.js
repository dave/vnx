// Model -> JSON text.
//
// The model becomes a plain JSON value and `JSON.stringify` writes it. The
// bytes of a document are not constrained (SPEC.md 8.1): key order, number
// spelling and whitespace are the writer's own business, so this seat spells a
// document the way its own runtime does.
//
// This is the EMIT BOUNDARY, and the one rule SPEC.md 4.1 puts here is applied
// on the way out: the shortest spelling of a position.
//
// The writer refuses what its own reader would drop (SPEC.md 3.3, 3.4, 3.7):
// an arc that is a self-loop or names an absent node, a walk that breaks a rule,
// a time on a node or an arc pip, an elevation or a time on a walk position, and
// a ring that is closed or holds fewer than three distinct positions. It does
// not repair any of them: opening a closed ring, say, would be the writer
// guessing what its caller meant, and that is the caller's to fix.
//
// ⚠ A COORDINATE PASSES THROUGH UNCHANGED. SPEC.md 4.1 does not constrain a
// writer's precision — the six-places paragraph there is advice, mirroring
// [RFC 7946] 11.2, and carries no requirement. This seat writes the binary64
// value the model holds. Rounding is a producer's size decision and belongs
// somewhere this library's write path does not reach.

import { Position } from './model.js';
import { checkArcs, checkWalk } from './walk.js';
import { distinctLonLat } from './decode.js';

/**
 * Encode a document as VNX text.
 *
 * Throws an `Error` for a value JSON cannot carry — `undefined`, a function, a
 * symbol, a bigint, or a number that is not finite — wherever it sits,
 * including inside an application key's value, a record body or a style.
 * `JSON.stringify` alone would drop the first three from an object, write them
 * as `null` in an array, and write `NaN` and `Infinity` as `null`: a document
 * silently different from the model. A `null` is a value and is written.
 *
 * Throws an `Error` too for a document its own reader would repair or drop:
 * see the list at the top of this file.
 * @param {import('./model.js').Document} doc
 * @returns {string}
 */
export function encode(doc) {
  return JSON.stringify(emitDocument(doc), refuseUnwritable);
}

// The replacer sees every value `JSON.stringify` is about to write, after
// `toJSON`. Model fields left unset never reach it: `put` omits them.
function refuseUnwritable(key, value) {
  const where = key === '' ? 'the document' : `key ${JSON.stringify(key)}`;
  switch (typeof value) {
    case 'undefined':
    case 'function':
    case 'symbol':
    case 'bigint':
      throw new Error(`vnx: ${where} holds a ${typeof value}, which JSON cannot carry`);
    case 'number':
      if (!Number.isFinite(value)) {
        throw new Error(`vnx: ${where} holds ${value}, which is not finite (SPEC.md 4.2)`);
      }
      break;
    default:
      break;
  }
  return value;
}

function emitPosition(position) {
  const out = position.channels.slice();
  // Shortest spelling: drop trailing `null` channels down to arity 2. An
  // INTERIOR absence keeps its null, or dropping it would move the time into
  // the elevation channel.
  let n = out.length;
  while (n > 2 && out[n - 1] === null) n--;
  return out.slice(0, n);
}

function put(o, k, v) {
  if (v !== undefined) o[k] = v;
}

function putHidden(o, v) {
  // SPEC.md 3.8: a writer emits the key only when its value is true.
  if (v === true) o.hidden = true;
}

function emitPips(pips) {
  return pips.map((p) => emitPosition(p));
}

/**
 * A node or an arc pip: longitude, latitude and optionally elevation, and no
 * time (SPEC.md 3.3.1, 3.3.2).
 */
function emitVertex(position, what) {
  if (position.channels.slice(3).some((c) => c !== null)) {
    throw new Error(`vnx: ${what} carries a channel past elevation, and a node or an arc pip carries no time`);
  }
  return emitPosition(new Position(position.channels.slice(0, 3)));
}

/** A walk position: longitude and latitude only (SPEC.md 3.3.3). */
function emitLonLat(position, what) {
  if (position.channels.slice(2).some((c) => c !== null)) {
    throw new Error(`vnx: ${what} carries a channel past latitude, and a walk position carries none`);
  }
  return [position.lon, position.lat];
}

/**
 * @param {import('./model.js').Document} doc
 * @returns {object} a plain JSON value
 */
export function emitDocument(doc) {
  const o = {};
  // SPEC.md 2.2: a reader that re-encodes a document writes the version the
  // DOCUMENT declared, not the reader's own.
  o.vnx = doc.vnx;
  put(o, 'generator', doc.generator);
  put(o, 'created', doc.created);
  if (doc.folders !== undefined) o.folders = doc.folders.map((f) => emitFolder(f));
  if (doc.networks !== undefined) o.networks = doc.networks.map((n) => emitNetwork(n));
  if (doc.plans !== undefined) o.plans = doc.plans.map((r) => emitPlan(r));
  if (doc.tracks !== undefined) o.tracks = doc.tracks.map((t) => emitTrack(t));
  if (doc.waypoints !== undefined) o.waypoints = doc.waypoints.map((w) => emitPointLike(w));
  if (doc.areas !== undefined) o.areas = doc.areas.map((a) => emitArea(a));
  emitAppKeys(o, doc.appKeys);
  return o;
}

/**
 * The application keys (SPEC.md 3.9), each value written exactly as held, so a
 * `null` is written as `null` and an empty value as empty.
 *
 * ⚠ A KEY WITH NO FULL STOP IS REFUSED, NOT WRITTEN: a reader would take it for
 * an unrecognised key, or for one of the format's own. A key held twice is
 * refused too, because a JSON object keeps only one of them.
 */
function emitAppKeys(o, appKeys) {
  const seen = new Set();
  for (const { key, value } of appKeys) {
    if (!key.includes('.')) {
      throw new Error(`vnx: application key ${JSON.stringify(key)} contains no full stop (SPEC.md 3.9)`);
    }
    if (seen.has(key)) {
      throw new Error(`vnx: application key ${JSON.stringify(key)} appears twice`);
    }
    seen.add(key);
    o[key] = value;
  }
}

function emitFolder(f) {
  const o = { id: f.id };
  put(o, 'parent', f.parent);
  put(o, 'name', f.name);
  put(o, 'notes', f.notes);
  putHidden(o, f.hidden);
  put(o, 'data', f.data);
  return o;
}

function emitNodes(o, nodes, owner) {
  if (nodes === undefined) return;
  const out = {};
  for (const [k, p] of nodes) out[k] = emitVertex(p, `${owner} node ${JSON.stringify(k)}`);
  o.nodes = out;
}

function emitArcs(o, nodes, arcs, owner) {
  const fault = checkArcs(nodes, arcs);
  if (fault !== null) throw new Error(`vnx: ${owner}: ${fault}`);
  if (arcs === undefined) return;
  const out = {};
  for (const [k, a] of arcs) {
    const ao = { from: a.from, to: a.to };
    put(ao, 'data', a.data);
    ao.pips = a.pips.map((p, i) => emitVertex(p, `${owner} arc ${JSON.stringify(k)} pip ${i}`));
    out[k] = ao;
  }
  o.arcs = out;
}

/**
 * A walk, checked first against its owner's arcs: a writer never emits a walk
 * its own reader would drop. `from` and `to` are written only where the model
 * holds them, and `turn` and `pin` only where the pass has one.
 */
function emitWalk(walk, arcs, plan, owner) {
  const fault = checkWalk(walk, arcs, plan);
  if (fault !== null) throw new Error(`vnx: ${owner}: ${fault.toError().message.replace(/^vnx: /, '')}`);
  return walk.map((p, i) => {
    const po = { arc: p.arc };
    for (const key of ['from', 'to']) {
      const e = p[key];
      if (e === undefined) continue;
      po[key] = e.kind === 'node' ? e.node : emitLonLat(e.position, `${owner} pass ${i} \`${key}\``);
    }
    if (p.turn !== undefined) po.turn = emitLonLat(p.turn, `${owner} pass ${i} \`turn\``);
    if (p.pin !== undefined) po.pin = emitLonLat(p.pin, `${owner} pass ${i} \`pin\``);
    return po;
  });
}

function emitNetwork(n) {
  const o = {};
  put(o, 'name', n.name);
  put(o, 'notes', n.notes);
  put(o, 'folder', n.folder);
  putHidden(o, n.hidden);
  put(o, 'pinned', n.pinned);
  const owner = `network ${JSON.stringify(n.name ?? '')}`;
  emitNodes(o, n.nodes, owner);
  emitArcs(o, n.nodes, n.arcs, owner);
  if (n.highlights !== undefined)
    o.highlights = n.highlights.map((c, i) => emitHighlight(c, n.arcs, `${owner} highlight ${i}`));
  if (n.points !== undefined) o.points = n.points.map((p) => emitPointLike(p));
  if (n.areas !== undefined) o.areas = n.areas.map((a) => emitArea(a));
  put(o, 'data', n.data);
  return o;
}

function emitHighlight(c, arcs, owner) {
  const o = {};
  putHidden(o, c.hidden);
  put(o, 'pinned', c.pinned);
  put(o, 'data', c.data);
  o.walk = emitWalk(c.walk, arcs, false, owner);
  return o;
}

function emitPlan(r) {
  const o = {};
  put(o, 'name', r.name);
  put(o, 'notes', r.notes);
  put(o, 'folder', r.folder);
  putHidden(o, r.hidden);
  put(o, 'pinned', r.pinned);
  put(o, 'style', r.style);
  put(o, 'data', r.data);
  const owner = `plan ${JSON.stringify(r.name ?? '')}`;
  emitNodes(o, r.nodes, owner);
  emitArcs(o, r.nodes, r.arcs, owner);
  o.walk = emitWalk(r.walk, r.arcs, true, owner);
  return o;
}

function emitTrack(t) {
  const o = {};
  put(o, 'name', t.name);
  put(o, 'notes', t.notes);
  put(o, 'folder', t.folder);
  put(o, 'started', t.started);
  put(o, 'ended', t.ended);
  putHidden(o, t.hidden);
  put(o, 'pinned', t.pinned);
  put(o, 'style', t.style);
  put(o, 'data', t.data);
  o.segments = t.segments.map((s) => ({ pips: emitPips(s.pips) }));
  return o;
}

function emitPointLike(w) {
  const o = {};
  put(o, 'name', w.name);
  put(o, 'notes', w.notes);
  put(o, 'folder', w.folder);
  putHidden(o, w.hidden);
  put(o, 'pinned', w.pinned);
  o.at = emitPosition(w.at);
  put(o, 'style', w.style);
  put(o, 'data', w.data);
  return o;
}

function emitArea(a) {
  const o = {};
  put(o, 'name', a.name);
  put(o, 'notes', a.notes);
  put(o, 'folder', a.folder);
  putHidden(o, a.hidden);
  put(o, 'pinned', a.pinned);
  checkRing(a);
  o.polygon = a.polygon.map((p) => emitPosition(p));
  put(o, 'style', a.style);
  put(o, 'data', a.data);
  return o;
}

/**
 * A writer emits a ring open, with at least three distinct positions
 * (SPEC.md 3.7).
 */
function checkRing(a) {
  const ring = a.polygon;
  const what = a.name === undefined ? 'an area' : `area ${JSON.stringify(a.name)}`;
  const n = ring.length;
  if (n >= 2 && ring[0].sameCoordinates(ring[n - 1])) {
    throw new Error(`vnx: ${what}: a ring is written open, and this one's last position repeats its first`);
  }
  const distinct = distinctLonLat(ring);
  if (distinct < 3) {
    throw new Error(`vnx: ${what}: a ring needs at least 3 distinct positions, this one has ${distinct}`);
  }
}

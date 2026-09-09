// Model -> JSON value, in one of the two modes SPEC.md 8.1 distinguishes.
//
//   RE-RENDER  turn a document a reader was handed back into that document,
//              applying neither rounding nor shortening, so arity and precision
//              survive. This is what the accept tier requires.
//   WRITE      turn a model into a document, applying the emit-boundary rules:
//              round coordinates (4.5.4) and emit the shortest spelling of a
//              position (4.1).
//
// Rounding MUST happen here and MUST NOT happen in the model (SPEC.md 4.5.4):
// comparing positions by full value is what endpoint snapping and ring winding
// rely on, and rounding ahead of them changes the geometry rather than its
// spelling.

import { roundToPlaces } from './round.js';

export const RERENDER = 'rerender';
export const WRITE = 'write';

const LONLAT_PLACES = 6;
const ELEVATION_PLACES = 1;

function round(v, places) {
  if (v === null) return null;
  // See round.js: the tie direction is not stated by SPEC.md 4.5.4 and the
  // target languages disagree about it.
  return roundToPlaces(v, places);
}

function emitPosition(position, mode) {
  const c = position.channels;
  if (mode === RERENDER) return c.slice();
  const out = c.slice();
  out[0] = round(out[0], LONLAT_PLACES);
  out[1] = round(out[1], LONLAT_PLACES);
  if (out.length > 2) out[2] = round(out[2], ELEVATION_PLACES);
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

function putHidden(o, v, mode) {
  if (v === undefined) return;
  // SPEC.md 3.8: a writer emits the key only when its value is true. A
  // re-render reproduces what the document carried, whatever it carried.
  if (mode === WRITE && v !== true) return;
  o.hidden = v;
}

function emitPips(pips, mode) {
  return pips.map((p) => emitPosition(p, mode));
}

function emitEndpoint(end, mode) {
  return end.kind === 'node' ? end.node : emitPosition(end.position, mode);
}

/**
 * @param {import('./model.js').Document} doc
 * @param {'rerender'|'write'} mode
 * @returns {object} a plain JSON value ready for renderCanonical
 */
export function emitDocument(doc, mode = RERENDER) {
  const o = {};
  // SPEC.md 2.2: a reader that re-encodes a document writes the version the
  // DOCUMENT declared, not the reader's own.
  o.vnx = doc.vnx;
  put(o, 'profile', doc.profile);
  put(o, 'generator', doc.generator);
  put(o, 'created', doc.created);
  if (doc.folders !== undefined) o.folders = doc.folders.map((f) => emitFolder(f, mode));
  if (doc.networks !== undefined) o.networks = doc.networks.map((n) => emitNetwork(n, mode));
  if (doc.routes !== undefined) o.routes = doc.routes.map((r) => emitRoute(r, mode));
  if (doc.tracks !== undefined) o.tracks = doc.tracks.map((t) => emitTrack(t, mode));
  if (doc.waypoints !== undefined) o.waypoints = doc.waypoints.map((w) => emitPointLike(w, mode));
  if (doc.areas !== undefined) o.areas = doc.areas.map((a) => emitArea(a, mode));
  if (doc.rulesets !== undefined) o.rulesets = doc.rulesets;
  return o;
}

function emitFolder(f, mode) {
  const o = { id: f.id };
  put(o, 'parent', f.parent);
  put(o, 'name', f.name);
  put(o, 'notes', f.notes);
  putHidden(o, f.hidden, mode);
  put(o, 'data', f.data);
  return o;
}

function emitNetwork(n, mode) {
  const o = {};
  put(o, 'name', n.name);
  put(o, 'notes', n.notes);
  put(o, 'folder', n.folder);
  putHidden(o, n.hidden, mode);
  if (n.nodes !== undefined) {
    const nodes = {};
    for (const [k, p] of n.nodes) nodes[k] = emitPosition(p, mode);
    o.nodes = nodes;
  }
  if (n.arcs !== undefined) {
    const arcs = {};
    for (const [k, a] of n.arcs) {
      const ao = { from: a.from, to: a.to };
      put(ao, 'data', a.data);
      ao.pips = emitPips(a.pips, mode);
      arcs[k] = ao;
    }
    o.arcs = arcs;
  }
  if (n.chains !== undefined) o.chains = n.chains.map((c) => emitChain(c, mode));
  if (n.points !== undefined) o.points = n.points.map((p) => emitPointLike(p, mode));
  if (n.areas !== undefined) o.areas = n.areas.map((a) => emitArea(a, mode));
  put(o, 'data', n.data);
  return o;
}

function emitChain(c, mode) {
  const o = {};
  put(o, 'name', c.name);
  put(o, 'notes', c.notes);
  putHidden(o, c.hidden, mode);
  put(o, 'style', c.style);
  put(o, 'data', c.data);
  o.links = c.links.map((l) => ({
    arc: l.arc,
    from: emitEndpoint(l.from, mode),
    to: emitEndpoint(l.to, mode),
  }));
  return o;
}

function emitRoute(r, mode) {
  const o = {};
  put(o, 'name', r.name);
  put(o, 'notes', r.notes);
  put(o, 'folder', r.folder);
  putHidden(o, r.hidden, mode);
  put(o, 'style', r.style);
  put(o, 'data', r.data);
  o.arcs = r.arcs.map((a) => {
    const ao = {};
    put(ao, 'data', a.data);
    ao.pips = emitPips(a.pips, mode);
    return ao;
  });
  return o;
}

function emitTrack(t, mode) {
  const o = {};
  put(o, 'name', t.name);
  put(o, 'notes', t.notes);
  put(o, 'folder', t.folder);
  put(o, 'started', t.started);
  put(o, 'ended', t.ended);
  putHidden(o, t.hidden, mode);
  put(o, 'style', t.style);
  put(o, 'data', t.data);
  o.segments = t.segments.map((s) => ({ pips: emitPips(s.pips, mode) }));
  return o;
}

function emitPointLike(w, mode) {
  const o = {};
  put(o, 'name', w.name);
  put(o, 'notes', w.notes);
  put(o, 'folder', w.folder);
  putHidden(o, w.hidden, mode);
  o.at = emitPosition(w.at, mode);
  put(o, 'style', w.style);
  put(o, 'data', w.data);
  return o;
}

function emitArea(a, mode) {
  const o = {};
  put(o, 'name', a.name);
  put(o, 'notes', a.notes);
  put(o, 'folder', a.folder);
  putHidden(o, a.hidden, mode);
  o.polygon = a.polygon.map((p) => emitPosition(p, mode));
  put(o, 'style', a.style);
  put(o, 'data', a.data);
  return o;
}

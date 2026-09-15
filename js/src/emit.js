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
// ⚠ A COORDINATE PASSES THROUGH UNCHANGED. SPEC.md 4.1 does not constrain a
// writer's precision — the six-places paragraph there is advice, mirroring
// [RFC 7946] 11.2, and carries no requirement. This seat writes the binary64
// value the model holds. Rounding is a producer's size decision and belongs
// somewhere this library's write path does not reach.

/**
 * Encode a document as VNX text.
 * @param {import('./model.js').Document} doc
 * @returns {string}
 */
export function encode(doc) {
  return JSON.stringify(emitDocument(doc));
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

function emitEndpoint(end) {
  return end.kind === 'node' ? end.node : emitPosition(end.position);
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
  if (doc.routes !== undefined) o.routes = doc.routes.map((r) => emitRoute(r));
  if (doc.tracks !== undefined) o.tracks = doc.tracks.map((t) => emitTrack(t));
  if (doc.waypoints !== undefined) o.waypoints = doc.waypoints.map((w) => emitPointLike(w));
  if (doc.areas !== undefined) o.areas = doc.areas.map((a) => emitArea(a));
  if (doc.rulesets !== undefined) o.rulesets = doc.rulesets;
  return o;
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

function emitNetwork(n) {
  const o = {};
  put(o, 'name', n.name);
  put(o, 'notes', n.notes);
  put(o, 'folder', n.folder);
  putHidden(o, n.hidden);
  if (n.nodes !== undefined) {
    const nodes = {};
    for (const [k, p] of n.nodes) nodes[k] = emitPosition(p);
    o.nodes = nodes;
  }
  if (n.arcs !== undefined) {
    const arcs = {};
    for (const [k, a] of n.arcs) {
      const ao = { from: a.from, to: a.to };
      put(ao, 'data', a.data);
      ao.pips = emitPips(a.pips);
      arcs[k] = ao;
    }
    o.arcs = arcs;
  }
  if (n.chains !== undefined) o.chains = n.chains.map((c) => emitChain(c));
  if (n.points !== undefined) o.points = n.points.map((p) => emitPointLike(p));
  if (n.areas !== undefined) o.areas = n.areas.map((a) => emitArea(a));
  put(o, 'data', n.data);
  return o;
}

function emitChain(c) {
  const o = {};
  put(o, 'name', c.name);
  put(o, 'notes', c.notes);
  putHidden(o, c.hidden);
  put(o, 'style', c.style);
  put(o, 'data', c.data);
  o.links = c.links.map((l) => ({
    arc: l.arc,
    from: emitEndpoint(l.from),
    to: emitEndpoint(l.to),
  }));
  return o;
}

function emitRoute(r) {
  const o = {};
  put(o, 'name', r.name);
  put(o, 'notes', r.notes);
  put(o, 'folder', r.folder);
  putHidden(o, r.hidden);
  put(o, 'style', r.style);
  put(o, 'data', r.data);
  o.arcs = r.arcs.map((a) => {
    const ao = {};
    put(ao, 'data', a.data);
    ao.pips = emitPips(a.pips);
    return ao;
  });
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
  o.polygon = a.polygon.map((p) => emitPosition(p));
  put(o, 'style', a.style);
  put(o, 'data', a.data);
  return o;
}

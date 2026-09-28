// JSON-tree comparison, which is what the corpus compares now that the bytes of
// a document are not constrained (SPEC.md 8.1).
//
// ⚠ IT IS NEVER MODEL-VERSUS-MODEL, and that is the finding the accept tier
// rests on. A reader that discarded every data record, every application key,
// every style and every `notes` produces a model that compares equal to itself, and
// passes an accept tier built that way green — that is the whole extensibility
// mechanism, all styling and all user prose. Comparing the tree parsed from the
// ORIGINAL FILE against the tree parsed from encode(decode(file)) is what proves
// a reader kept what it read. test/redproof.test.js freezes the proof.
//
// ⚠ THE TWO TIERS NORMALISE DIFFERENTLY, AND MUST. The accept tier compares a
// document against a re-encoding of itself, so SPEC.md 4.1's emit-boundary rule
// — the shortest spelling of a position — is applied to both sides; otherwise
// the tier would require the file to have been written by this library. The
// write tier compares one writer's output against another's, where that rule is
// the very one under test, so normalising it there would make the tier prove
// nothing.
//
// ⚠ PRECISION IS NEVER NORMALISED, on either tier. SPEC.md 4.1 does not
// constrain a writer's precision and a reader MUST NOT round, so a comparison
// that rounded would hide a reader that had. That is what makes
// `accept/full-precision-coordinate.vnx` an instrument rather than a formality.
//
// SPEC.md 8.1 permits EXACTLY TWO normalisations and no others: the shortest
// spelling of a position, and an absent container reading the same as an empty
// one.

/**
 * Parse JSON to a plain tree. Numbers land as doubles, so the comparison is
 * between values rather than between spellings (SPEC.md 4.2).
 * @param {string} text
 * @returns {unknown}
 */
export function parseTree(text) {
  return JSON.parse(text);
}

/**
 * Normalise a decoded VNX document.
 *
 * It is SCHEMA-AWARE rather than a heuristic over shapes, because the two
 * regions the format declares opaque — a data record's body and a style — may
 * hold arrays of numbers that are not positions and must not be shortened, and
 * may hold empty containers a third party means to keep.
 *
 * @param {unknown} v
 * @param {{emitBoundary?: boolean}} opt
 */
export function normaliseDocument(v, opt = {}) {
  if (!isObject(v)) return v;
  const out = { ...v };
  normItems(out, 'folders', opt, normFolder);
  normItems(out, 'waypoints', opt, normWaypoint);
  normItems(out, 'areas', opt, normArea);
  normItems(out, 'tracks', opt, normTrack);
  normItems(out, 'routes', opt, normRoute);
  normItems(out, 'networks', opt, normNetwork);
  // Application keys are not touched: SPEC.md 3.9 exempts them from 8.1's
  // absent-equals-empty normalisation, so an empty or null value is compared
  // as written and a key dropped for holding one is a difference.
  return out;
}

function normFolder(o) {
  dropEmptyContainer(o, 'data');
}

function normWaypoint(o, opt) {
  if ('at' in o) o.at = normPosition(o.at, opt);
  dropEmptyContainer(o, 'style');
  dropEmptyContainer(o, 'data');
}

function normArea(o, opt) {
  normPositionList(o, 'polygon', opt);
  dropEmptyContainer(o, 'style');
  dropEmptyContainer(o, 'data');
}

function normTrack(o, opt) {
  normItems(o, 'segments', opt, (s, inner) => normPositionList(s, 'pips', inner));
  dropEmptyContainer(o, 'style');
  dropEmptyContainer(o, 'data');
}

function normRoute(o, opt) {
  normItems(o, 'arcs', opt, normArc);
  dropEmptyContainer(o, 'style');
  dropEmptyContainer(o, 'data');
}

function normArc(o, opt) {
  normPositionList(o, 'pips', opt);
  dropEmptyContainer(o, 'data');
}

function normNetwork(o, opt) {
  if (isObject(o.nodes)) {
    const nodes = { ...o.nodes };
    for (const k of Object.keys(nodes)) nodes[k] = normPosition(nodes[k], opt);
    o.nodes = nodes;
  }
  if (isObject(o.arcs)) {
    const arcs = { ...o.arcs };
    for (const k of Object.keys(arcs)) {
      if (!isObject(arcs[k])) continue;
      const a = { ...arcs[k] };
      normArc(a, opt);
      arcs[k] = a;
    }
    o.arcs = arcs;
  }
  normItems(o, 'chains', opt, normChain);
  normItems(o, 'points', opt, normWaypoint);
  normItems(o, 'areas', opt, normArea);
  dropEmptyContainer(o, 'nodes');
  dropEmptyContainer(o, 'arcs');
  dropEmptyContainer(o, 'data');
}

function normChain(o, opt) {
  normItems(o, 'links', opt, (l, inner) => {
    // An endpoint is a node key or an inline position (SPEC.md 3.3.3). Only the
    // second is a position, and normPosition leaves a string alone.
    for (const side of ['from', 'to']) {
      if (side in l) l[side] = normPosition(l[side], inner);
    }
  });
  dropEmptyContainer(o, 'style');
  dropEmptyContainer(o, 'data');
}

/** Normalise an array of objects under `key`, dropping the key when it is empty. */
function normItems(o, key, opt, each) {
  const arr = o[key];
  if (!Array.isArray(arr)) return;
  if (arr.length === 0) {
    delete o[key];
    return;
  }
  o[key] = arr.map((item) => {
    if (!isObject(item)) return item;
    const copy = { ...item };
    each(copy, opt);
    return copy;
  });
}

function normPositionList(o, key, opt) {
  const arr = o[key];
  if (!Array.isArray(arr)) return;
  if (arr.length === 0) {
    delete o[key];
    return;
  }
  o[key] = arr.map((p) => normPosition(p, opt));
}

/**
 * The first permitted normalisation: on a position, trailing `null` channels are
 * dropped. It is an emit-boundary rule, so it is applied for the accept tier
 * only — see the note at the top. A channel's VALUE is never touched.
 *
 * ⚠ A CHANNEL THIS VERSION DOES NOT NAME IS NOT NORMALISED AWAY. SPEC.md 4.1's
 * fifth-channel preservation rule is gone, so a fixture asking a writer to
 * reproduce one would be asking for a rule the format does not state — the
 * fixture is what changes, not this comparison.
 */
function normPosition(v, opt) {
  if (!Array.isArray(v)) return v;
  const out = v.slice();
  if (!opt.emitBoundary) return out;
  let n = out.length;
  while (n > 2 && out[n - 1] === null) n--;
  return out.slice(0, n);
}

/**
 * The second permitted normalisation, SPEC.md 3.1: an absent container and an
 * empty one carry the same content and a reader MUST treat them alike, so the
 * comparison does too. That keeps a writer that omits an empty key conforming.
 */
function dropEmptyContainer(o, key) {
  const v = o[key];
  if (Array.isArray(v) && v.length === 0) delete o[key];
  else if (isObject(v) && Object.keys(v).length === 0) delete o[key];
}

function isObject(v) {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

/**
 * Where two trees first differ, in JSON Pointer form, or '' when they are equal.
 *
 * It reports a PATH rather than a boolean because a failing corpus case has to
 * say which rule broke to be actionable.
 */
export function treeDiff(got, want, path = '') {
  const where = path === '' ? 'the document' : path;
  if (isObject(want)) {
    if (!isObject(got)) return `${where}: got ${describe(got)}, want an object`;
    for (const k of Object.keys(want).sort()) {
      if (!(k in got)) return `${where}: key ${JSON.stringify(k)} is missing`;
      const d = treeDiff(got[k], want[k], `${path}/${escapePointer(k)}`);
      if (d !== '') return d;
    }
    const extra = Object.keys(got).filter((k) => !(k in want)).sort();
    if (extra.length > 0) return `${where}: unexpected key(s) ${extra.join(', ')}`;
    return '';
  }
  if (Array.isArray(want)) {
    if (!Array.isArray(got)) return `${where}: got ${describe(got)}, want an array`;
    if (got.length !== want.length) {
      return `${where}: got ${got.length} element(s), want ${want.length}`;
    }
    for (let i = 0; i < want.length; i++) {
      const d = treeDiff(got[i], want[i], `${path}/${i}`);
      if (d !== '') return d;
    }
    return '';
  }
  // A number compares by VALUE, so -0 and 0 are equal here, as they are in
  // JavaScript. `!==` is right for every scalar JSON carries.
  if (got !== want) return `${where}: got ${describe(got)}, want ${describe(want)}`;
  return '';
}

function escapePointer(k) {
  return k.replace(/~/g, '~0').replace(/\//g, '~1');
}

function describe(v) {
  if (v === null) return 'null';
  if (Array.isArray(v)) return 'an array';
  if (typeof v === 'object') return 'an object';
  return JSON.stringify(v);
}

/**
 * Compare two documents as normalised JSON trees.
 * @returns {string} the first difference, or '' when they agree
 */
export function compareDocuments(gotText, wantText, opt = {}) {
  const g = normaliseDocument(parseTree(gotText), opt);
  const w = normaliseDocument(parseTree(wantText), opt);
  return treeDiff(g, w);
}

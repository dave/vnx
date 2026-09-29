// Reading a VNX document (SPEC.md 3, 4 and 6).
//
// A reader is tolerant of damage and silent about nothing: an unusable value is
// dropped with a diagnostic and reading continues, and only the four causes of
// SPEC.md 6.2 refuse the document outright.
//
// There are no fallbacks here. A malformed value never becomes a default; it is
// dropped, and the diagnostic names what and where.

import { VnxRefusal, Diagnostic } from './errors.js';
import {
  Position, Folder, Network, Arc, Chain, Pass, Endpoint,
  Route, Track, Segment, Waypoint, Area, Document, AppKey,
} from './model.js';
import { WalkCursor } from './walk.js';

/** The MAJOR.MINOR this reader implements (SPEC.md 2.2). */
export const READER_MAJOR = 0;
export const READER_MINOR = 2;

const DROP = Symbol('drop');

// SPEC.md 4.3: ISO 8601 in UTC, literal `Z`, OPTIONAL fractional seconds.
const TIMESTAMP = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/;

// SPEC.md 2.2: three runs of decimal digits separated by U+002E FULL STOP. No
// bound on length, no rule against a leading zero.
const VERSION = /^(\d+)\.(\d+)\.(\d+)$/;

/** JSON Pointer token escaping (RFC 6901). */
function esc(token) {
  return String(token).replace(/~/g, '~0').replace(/\//g, '~1');
}

class Ctx {
  constructor() {
    /** @type {Diagnostic[]} */ this.diagnostics = [];
    // Positions are indexed AFTER decoding, by walking the model — see
    // `indexPositions`. Registering them during the read would index them by
    // their WIRE path, and a dropped element shifts every later index.

    /** @type {WeakMap<object, string>} model item -> wire pointer */
    this.paths = new WeakMap();
  }
  diag(path, message, code) {
    this.diagnostics.push(new Diagnostic(path, message, code));
  }
  position(path, position) {
    return position;
  }
}

/**
 * Parse JSON text, refusing what SPEC.md 6.2 says to refuse at this stage.
 *
 * Two distinct causes reach here. `NaN` and `Infinity` LITERALS are not JSON at
 * all, so a conforming parser refuses the text. A finite-looking literal that
 * overflows the double range — `1e400` — parses cleanly and arrives as
 * `Infinity`, and SPEC.md 4.2 refuses that separately.
 */
function parseJson(text) {
  let value;
  try {
    value = JSON.parse(text, function (key, v) {
      if (typeof v === 'number' && !Number.isFinite(v)) {
        throw new VnxRefusal(
          'non-finite-number',
          `a number in the document is not finite (SPEC.md 4.2), at ${key === '' ? 'the top level' : `key ${JSON.stringify(key)}`}`,
        );
      }
      return v;
    });
  } catch (err) {
    if (err instanceof VnxRefusal) throw err;
    throw new VnxRefusal('malformed-json', `malformed JSON: ${err.message}`, { cause: err });
  }
  return value;
}

/**
 * Decode VNX document text.
 * @param {string} text
 * @returns {{document: Document, diagnostics: Diagnostic[], positions: Map<string, Position>}}
 * @throws {VnxRefusal}
 */
export function decodeText(text) {
  return decodeValue(parseJson(text));
}

/**
 * Decode an already-parsed JSON value.
 * @param {unknown} value
 */
export function decodeValue(value) {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    throw new VnxRefusal(
      'not-a-vnx-document',
      'not a VNX document: the top level must be a JSON object carrying a "vnx" key',
    );
  }
  if (!Object.prototype.hasOwnProperty.call(value, 'vnx')) {
    throw new VnxRefusal(
      'not-a-vnx-document',
      'not a VNX document: no "vnx" key at the top level',
    );
  }
  const declared = value.vnx;
  if (typeof declared !== 'string' || !VERSION.test(declared)) {
    throw new VnxRefusal(
      'version-shape',
      `"vnx" is ${JSON.stringify(declared)}, which is not a version: expected MAJOR.MINOR.PATCH, three runs of decimal digits separated by full stops`,
    );
  }
  const [, majorText, minorText] = VERSION.exec(declared);
  const major = Number(majorText);
  const minor = Number(minorText);
  // SPEC.md 2.2: through 0.x each MINOR is its own series, so the question is
  // whether the document is in THIS reader's series, not whether it is newer
  // than it. A lower minor is refused for the same reason as a higher one: a
  // 0.2 reader has no rules for 0.1, exactly as it has none for 0.3. PATCH
  // never refuses, at any version, because a patch increment is additive.
  if (major !== READER_MAJOR || minor !== READER_MINOR) {
    const newer = major > READER_MAJOR || (major === READER_MAJOR && minor > READER_MINOR);
    // Both refusals name the version, which SPEC.md 2.2 requires; they differ
    // in what the user is told to do about it, which is the only reason a
    // reader distinguishes the two directions at all.
    if (newer) {
      throw new VnxRefusal(
        'version-too-new',
        `the document declares version ${declared}; this reader implements ${READER_MAJOR}.${READER_MINOR}.x, so a newer reader is needed`,
      );
    }
    throw new VnxRefusal(
      'version-too-old',
      `the document declares version ${declared}; this reader implements ${READER_MAJOR}.${READER_MINOR}.x and does not support the ${major}.${minor} series`,
    );
  }

  const ctx = new Ctx();
  const doc = readDocument(value, ctx);
  resolveReferences(doc, ctx);
  return { document: doc, diagnostics: ctx.diagnostics, positions: indexPositions(doc) };
}

// --- field helpers ----------------------------------------------------------

/**
 * Split an object's keys into the ones this format defines and the ones it does
 * not, diagnosing the latter (SPEC.md 6.2). An unrecognised key is skipped and
 * the item is KEPT: unrecognised is not unusable.
 */
function known(obj, path, required, optional, ctx, alsoKnown = () => false) {
  const out = Object.create(null);
  for (const k of Object.keys(obj)) {
    if (required.includes(k) || optional.includes(k) || alsoKnown(k)) out[k] = obj[k];
    else ctx.diag(`${path}/${esc(k)}`, `unknown key "${k}"`, 'unknown-key');
  }
  return out;
}

function typeName(v) {
  if (v === null) return 'null';
  if (Array.isArray(v)) return 'an array';
  switch (typeof v) {
    case 'string': return 'a string';
    case 'number': return 'a number';
    case 'boolean': return 'a boolean';
    default: return 'an object';
  }
}

/**
 * A value of the wrong JSON type is an unusable value (SPEC.md 6.2). Where the
 * key is OPTIONAL the field is dropped and the item kept; where it is REQUIRED
 * the item goes, because the object cannot exist without it. An absent
 * REQUIRED key takes the same code as a wrong-typed one, at the key's path.
 */
function field(k, path, kv, want, check, required, ctx) {
  const v = kv[k];
  if (v === undefined) {
    if (required) {
      ctx.diag(`${path}/${esc(k)}`, `missing required key "${k}"`, 'wrong-type-required');
      return DROP;
    }
    return undefined;
  }
  if (!check(v)) {
    const p = `${path}/${esc(k)}`;
    ctx.diag(p, `"${k}" must be ${want}, not ${typeName(v)}`,
      required ? 'wrong-type-required' : 'wrong-type-optional');
    return required ? DROP : undefined;
  }
  return v;
}

const isString = (v) => typeof v === 'string';
const isBoolean = (v) => typeof v === 'boolean';
const isArray = (v) => Array.isArray(v);
const isPlainObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);

const str = (k, path, kv, req, ctx) => field(k, path, kv, 'a string', isString, req, ctx);
const bool = (k, path, kv, req, ctx) => field(k, path, kv, 'a boolean', isBoolean, req, ctx);
const arr = (k, path, kv, req, ctx) => field(k, path, kv, 'an array', isArray, req, ctx);
const objField = (k, path, kv, req, ctx) => field(k, path, kv, 'an object', isPlainObject, req, ctx);

/** A record-level timestamp (SPEC.md 4.3). */
function timestamp(k, path, kv, ctx) {
  const v = str(k, path, kv, false, ctx);
  if (v === undefined) return undefined;
  if (!TIMESTAMP.test(v)) {
    ctx.diag(`${path}/${esc(k)}`, `"${k}" must be UTC with a Z suffix`, 'timestamp-not-utc');
    return undefined;
  }
  return v;
}

/**
 * A `style` object is opaque (SPEC.md 3.10): a reader carries any key it does
 * not define through a read-modify-write without reporting it.
 *
 * `importance` is the one style value this format constrains normatively
 * (SPEC.md 3.6). Out of range it is an unusable value in an optional key, so it
 * is dropped and the item kept. Nothing else in a style object is inspected.
 */
function style(path, kv, ctx) {
  const s = objField('style', path, kv, false, ctx);
  if (s === undefined) return undefined;
  if (!Object.prototype.hasOwnProperty.call(s, 'importance')) return s;
  const imp = s.importance;
  if (typeof imp === 'number' && Number.isInteger(imp) && imp >= 1 && imp <= 5) return s;
  // SPEC.md 6.2: a value outside a stated range is an unusable value in an
  // optional key, so it takes that code.
  ctx.diag(
    `${path}/style/importance`,
    '"importance" must be an integer from 1 to 5',
    'wrong-type-optional',
  );
  const copy = { ...s };
  delete copy.importance;
  return copy;
}

/**
 * A `data` bag (SPEC.md 5). Bodies are opaque apart from `@type`: nothing here
 * inspects a record's fields, and a record of an unrecognised type is inert on
 * any host. A record with no `@type`, or one that is not a string, is dropped.
 */
function data(path, kv, ctx) {
  const a = arr('data', path, kv, false, ctx);
  if (a === undefined) return undefined;
  const out = [];
  a.forEach((rec, i) => {
    const p = `${path}/data/${i}`;
    if (!isPlainObject(rec)) {
      ctx.diag(p, `a data record must be an object, not ${typeName(rec)}`, 'record-unusable');
      return;
    }
    if (typeof rec['@type'] !== 'string') {
      ctx.diag(p, 'a data record must carry a string "@type"', 'wrong-type-required');
      return;
    }
    out.push(rec);
  });
  return out;
}

/**
 * A position (SPEC.md 4.1). The channel array is kept WHOLE, at the arity the
 * document carried. A reader MUST NOT round — decoding preserves the binary64
 * value the document states — and MUST NOT shorten, which is an emit rule.
 * A node, an arc pip and a walk position then keep only the channels SPEC.md
 * 3.3 lets them carry, and their callers report what they drop.
 */
/**
 * @param {string} [code] the diagnostic code to report a failure under.
 *
 * Defaults to `position-unusable`, which is the right fact for a position that
 * is an ELEMENT of something — a pip, a ring vertex, a node in a map — where
 * dropping it leaves the containing item alive.
 *
 * Where the position is the value of a REQUIRED KEY, the caller passes
 * `wrong-type-required` instead, because the two codes differ in whether the
 * rest of the item survives (SPEC.md 6.2). A consumer told only that a type was
 * wrong would otherwise have to re-derive required-ness from 3.10's table to
 * find out which happened, which is the lookup a code exists to save.
 */
function position(v, path, ctx, code = 'position-unusable', consequence = 'so it is dropped') {
  const bad = (why) => {
    ctx.diag(path, `${why}, ${consequence}`, code);
    return DROP;
  };
  if (!Array.isArray(v)) return bad(`a position must be an array, not ${typeName(v)}`);
  if (v.length < 2) return bad(`found ${v.length} channel(s), a position needs at least lon and lat`);
  if (typeof v[0] !== 'number') return bad(`channel 0 is ${typeName(v[0])}, expected a number`);
  if (typeof v[1] !== 'number') return bad(`channel 1 is ${typeName(v[1])}, expected a number`);
  for (let i = 2; i < v.length; i++) {
    if (v[i] !== null && typeof v[i] !== 'number') {
      return bad(`channel ${i} is ${typeName(v[i])}, expected a number or null`);
    }
  }
  return new Position(v.slice());
}

/**
 * A track segment's ordered pip list. A pip that will not parse drops the
 * SEGMENT holding it, not the pip (SPEC.md 6.2).
 *
 * Geometry is continuous and structure is not. A missing vertex leaves a
 * shorter, plausible, wrong line with nothing to indicate it, while a missing
 * segment is a gap SPEC.md 3.5 already makes a pause.
 */
function pips(kv, path, ctx) {
  const a = arr('pips', path, kv, true, ctx);
  if (a === DROP) return DROP;
  const out = [];
  for (const [i, v] of a.entries()) {
    const p = position(v, `${path}/pips/${i}`, ctx, 'position-unusable',
      'so the segment holding it is dropped');
    if (p === DROP) return DROP;
    out.push(p);
  }
  return out;
}

/** True when a position carries a time: channel 3 is present and not null. */
const timed = (p) => p.channels.length > 3 && p.channels[3] !== null;

/**
 * A node or an arc pip carries longitude, latitude and optionally elevation,
 * and no time (SPEC.md 3.3.1, 3.3.2). The caller has diagnosed any time; this
 * keeps the first three channels and drops the rest.
 */
const vertex = (p) => new Position(p.channels.slice(0, 3));

// --- structure --------------------------------------------------------------

const DOC_OPTIONAL = ['generator', 'created', 'folders', 'networks', 'routes',
  'tracks', 'waypoints', 'areas'];

/**
 * SPEC.md 3.9: a TOP-LEVEL key containing a full stop is an application key.
 * No key the format defines contains one. The test is applied to the document
 * only; every other object keeps its fixed key list, so a dotted key on an item
 * is reported as unknown.
 */
export function isAppKey(k) {
  return k.includes('.');
}

function readDocument(raw, ctx) {
  const kv = known(raw, '', ['vnx'], DOC_OPTIONAL, ctx, isAppKey);
  const doc = new Document();
  doc.vnx = kv.vnx;
  doc.generator = str('generator', '', kv, false, ctx);
  doc.created = timestamp('created', '', kv, ctx);
  doc.folders = collection('folders', kv, ctx, readFolder);
  doc.networks = collection('networks', kv, ctx, readNetwork);
  doc.routes = collection('routes', kv, ctx, readRoute);
  doc.tracks = collection('tracks', kv, ctx, readTrack);
  doc.waypoints = collection('waypoints', kv, ctx, (r, p, c) => readPointLike(r, p, c, true));
  doc.areas = collection('areas', kv, ctx, (r, p, c) => readArea(r, p, c, true));
  doc.appKeys = readAppKeys(kv);
  return doc;
}

/**
 * Every application key, sorted by key. Each value is kept as parsed — `null`
 * and an empty value included — and never inspected (SPEC.md 3.9), so this
 * raises no diagnostic. Sorted rather than in document order to match the Go
 * reference, whose JSON decoder has lost the order by the time it sees keys.
 */
function readAppKeys(kv) {
  const out = [];
  for (const k of Object.keys(kv)) {
    if (isAppKey(k)) out.push(new AppKey(k, kv[k]));
  }
  // By UTF-8 bytes, as Go's sort.Strings orders them. `<` on strings compares
  // UTF-16 code units, which disagrees above U+FFFF.
  out.sort((a, b) => Buffer.compare(Buffer.from(a.key, 'utf8'), Buffer.from(b.key, 'utf8')));
  return out;
}

function collection(key, kv, ctx, read) {
  const a = arr(key, '', kv, false, ctx);
  if (a === undefined) return undefined;
  const out = [];
  a.forEach((raw, i) => {
    const path = `/${key}/${i}`;
    if (!isPlainObject(raw)) {
      ctx.diag(path, `must be an object, not ${typeName(raw)}`, 'item-unusable');
      return;
    }
    const item = read(raw, path, ctx);
    if (item !== DROP) {
      ctx.paths.set(item, path);
      out.push(item);
    }
  });
  return out;
}

function readFolder(raw, path, ctx) {
  const kv = known(raw, path, ['id'], ['parent', 'name', 'notes', 'hidden', 'data'], ctx);
  const f = new Folder();
  f.id = str('id', path, kv, true, ctx);
  if (f.id === DROP) return DROP;
  f.parent = str('parent', path, kv, false, ctx);
  f.name = str('name', path, kv, false, ctx);
  f.notes = str('notes', path, kv, false, ctx);
  f.hidden = bool('hidden', path, kv, false, ctx);
  f.data = data(path, kv, ctx);
  return f;
}

function readNetwork(raw, path, ctx) {
  const kv = known(raw, path, [],
    ['name', 'notes', 'folder', 'hidden', 'nodes', 'arcs', 'chains', 'points', 'areas', 'data'], ctx);
  const n = new Network();
  n.name = str('name', path, kv, false, ctx);
  n.notes = str('notes', path, kv, false, ctx);
  n.folder = str('folder', path, kv, false, ctx);
  n.hidden = bool('hidden', path, kv, false, ctx);
  n.data = data(path, kv, ctx);
  // Nodes, then arcs, then walks: a walk is checked against the arcs that
  // survived, and an arc against the nodes that survived (SPEC.md 6.2).
  n.nodes = readNodes(kv, path, ctx);
  n.arcs = readArcs(kv, path, n.nodes, ctx);
  n.chains = subCollection(kv, path, 'chains', ctx, (r, p, c) => readChain(r, p, c, n.arcs));
  n.points = subCollection(kv, path, 'points', ctx, (r, p, c) => readPointLike(r, p, c, false));
  n.areas = subCollection(kv, path, 'areas', ctx, (r, p, c) => readArea(r, p, c, false));
  return n;
}

/**
 * The `nodes` of a network or a route. A node that will not parse is dropped,
 * and an arc naming it is then dropped under `arc-node-absent`. A time on a
 * node is dropped with `extra-channel`, and the node kept (SPEC.md 3.3.1).
 */
function readNodes(kv, path, ctx) {
  const nodes = objField('nodes', path, kv, false, ctx);
  if (nodes === undefined) return undefined;
  const out = new Map();
  for (const key of Object.keys(nodes)) {
    const p = `${path}/nodes/${esc(key)}`;
    const v = position(nodes[key], p, ctx, 'position-unusable', 'so the node is dropped');
    if (v === DROP) continue;
    if (timed(v)) {
      ctx.diag(p, 'a node carries no time, so the time channel is dropped and the node kept', 'extra-channel');
    }
    out.set(key, vertex(v));
  }
  return out;
}

/**
 * The `arcs` of a network or a route (SPEC.md 3.3.2).
 *
 * ⚠ AN ARC THAT IS DROPPED TAKES EVERY CHAIN WALKING IT WITH IT, but not here:
 * the chain goes when its walk is read, under `pass-arc-absent`, with a
 * diagnostic of its own (SPEC.md 6.2).
 */
function readArcs(kv, path, nodes, ctx) {
  const arcs = objField('arcs', path, kv, false, ctx);
  if (arcs === undefined) return undefined;
  const out = new Map();
  for (const key of Object.keys(arcs)) {
    const p = `${path}/arcs/${esc(key)}`;
    const raw = arcs[key];
    if (!isPlainObject(raw)) {
      ctx.diag(p, `an arc must be an object, not ${typeName(raw)}`, 'item-unusable');
      continue;
    }
    const a = readArc(raw, p, nodes, ctx);
    if (a !== DROP) out.set(key, a);
  }
  return out;
}

function readArc(raw, p, nodes, ctx) {
  const akv = known(raw, p, ['from', 'to', 'pips'], ['data'], ctx);
  const a = new Arc();
  a.from = str('from', p, akv, true, ctx);
  if (a.from === DROP) return DROP;
  a.to = str('to', p, akv, true, ctx);
  if (a.to === DROP) return DROP;
  const list = arr('pips', p, akv, true, ctx);
  if (list === DROP) return DROP;
  const read = [];
  for (const [i, v] of list.entries()) {
    const pip = position(v, `${p}/pips/${i}`, ctx, 'position-unusable', 'so the arc is dropped');
    if (pip === DROP) return DROP;
    read.push(pip);
  }
  // One diagnostic for the arc, however many pips carried a time (SPEC.md 6.3).
  if (read.some(timed)) {
    ctx.diag(`${p}/pips`,
      'an arc pip carries no time, so every time channel on this arc is dropped and its pips kept', 'arc-pip-time');
  }
  a.pips = read.map(vertex);
  a.data = data(p, akv, ctx);
  if (a.from === a.to) {
    ctx.diag(p, `the arc runs from node ${JSON.stringify(a.from)} to itself, and a self-loop cannot be walked, so the arc is dropped`,
      'self-loop-arc');
    return DROP;
  }
  let absent = false;
  for (const which of ['from', 'to']) {
    if (nodes !== undefined && nodes.has(a[which])) continue;
    ctx.diag(`${p}/${which}`,
      `"${which}" names node ${JSON.stringify(a[which])}, which is not here, so the arc is dropped`, 'arc-node-absent');
    absent = true;
  }
  return absent ? DROP : a;
}

function subCollection(kv, path, key, ctx, read) {
  const a = arr(key, path, kv, false, ctx);
  if (a === undefined) return undefined;
  const out = [];
  a.forEach((raw, i) => {
    const p = `${path}/${key}/${i}`;
    if (!isPlainObject(raw)) {
      ctx.diag(p, `must be an object, not ${typeName(raw)}`, 'item-unusable');
      return;
    }
    const item = read(raw, p, ctx);
    if (item !== DROP) {
      ctx.paths.set(item, p);
      out.push(item);
    }
  });
  return out;
}

function readChain(raw, path, ctx, arcs) {
  const kv = known(raw, path, ['walk'], ['name', 'notes', 'hidden', 'style', 'data'], ctx);
  const c = new Chain();
  c.name = str('name', path, kv, false, ctx);
  c.notes = str('notes', path, kv, false, ctx);
  c.hidden = bool('hidden', path, kv, false, ctx);
  c.style = style(path, kv, ctx);
  c.data = data(path, kv, ctx);
  c.walk = readWalk(kv, path, arcs, false, ctx);
  return c.walk === DROP ? DROP : c;
}

/**
 * A chain's or a route's REQUIRED `walk`, checked pass by pass in the order
 * SPEC.md 6.2 lists. Returns DROP when the owner is to be dropped: a missing or
 * wrong-typed `walk`, or the first pass that breaks a rule, which is reported
 * alone — nothing after it in the walk is examined.
 *
 * ⚠ A WALK IS USED WHOLE OR NOT AT ALL. Dropping a pass would hand the caller a
 * different walk that reads as valid — two arcs joined that do not meet, a
 * direction reversed by a lost turn, a start moved to a node.
 */
function readWalk(kv, owner, arcs, route, ctx) {
  const list = arr('walk', owner, kv, true, ctx);
  if (list === DROP) return DROP;
  const what = route ? 'route' : 'chain';
  const cursor = new WalkCursor(list.length - 1, route);
  const walk = [];
  for (const [i, raw] of list.entries()) {
    const at = `${owner}/walk/${i}`;
    const drop = (key, code, message) => {
      ctx.diag(key === '' ? at : `${at}/${key}`, `${message}, so the ${what} is dropped`, code);
      return DROP;
    };
    // 1. The pass is an object naming an arc of its owner.
    if (!isPlainObject(raw)) return drop('', 'pass-arc-absent', `a pass must be an object, not ${typeName(raw)}`);
    const arc = typeof raw.arc === 'string' ? arcs?.get(raw.arc) : undefined;
    if (arc === undefined) {
      return drop('', 'pass-arc-absent', `the pass names arc ${JSON.stringify(raw.arc ?? null)}, which is not here`);
    }
    const p = new Pass();
    p.arc = raw.arc;
    // 2. Unrecognised keys. 3. `pinned`, where `false` is read as absent.
    const pkv = known(raw, at, ['arc'], ['from', 'to', 'turn', 'pinned'], ctx);
    if (bool('pinned', at, pkv, false, ctx) === true) p.pinned = true;
    // 4. `from`, `to` and `turn`, in that order, each parse.
    for (const k of ['from', 'to', 'turn']) {
      const v = pkv[k];
      if (v === undefined) continue;
      if (typeof v === 'string' && k !== 'turn') {
        p[k] = new Endpoint('node', v);
        continue;
      }
      const pos = position(v, `${at}/${k}`, ctx, 'position-unusable', `so the ${what} is dropped`);
      if (pos === DROP) return DROP;
      const [, , ele, time] = pos.channels;
      if ((ele !== undefined && ele !== null) || (time !== undefined && time !== null)) {
        ctx.diag(`${at}/${k}`,
          'a walk position carries longitude and latitude only, so its other channels are dropped', 'extra-channel');
      }
      const lonLat = new Position(pos.channels.slice(0, 2));
      p[k] = k === 'turn' ? lonLat : new Endpoint('position', lonLat);
    }
    // 5 to 8: a route's rule, pin and turn, spelling, connection.
    const step = cursor.step(i, p, arc);
    if (step.fault) return drop(step.fault.key, step.fault.code, step.fault.message);
    walk.push(p);
  }
  return walk;
}

/**
 * A route owns its graph (SPEC.md 3.4), and is its own chain: wherever a chain
 * would be dropped, the whole route is.
 */
function readRoute(raw, path, ctx) {
  const kv = known(raw, path, ['walk'],
    ['name', 'notes', 'folder', 'hidden', 'style', 'nodes', 'arcs', 'data'], ctx);
  const r = new Route();
  r.name = str('name', path, kv, false, ctx);
  r.notes = str('notes', path, kv, false, ctx);
  r.folder = str('folder', path, kv, false, ctx);
  r.hidden = bool('hidden', path, kv, false, ctx);
  r.style = style(path, kv, ctx);
  r.data = data(path, kv, ctx);
  r.nodes = readNodes(kv, path, ctx);
  r.arcs = readArcs(kv, path, r.nodes, ctx);
  r.walk = readWalk(kv, path, r.arcs, true, ctx);
  return r.walk === DROP ? DROP : r;
}

function readTrack(raw, path, ctx) {
  const kv = known(raw, path, ['segments'],
    ['name', 'notes', 'folder', 'hidden', 'style', 'started', 'ended', 'data'], ctx);
  const t = new Track();
  const segments = arr('segments', path, kv, true, ctx);
  if (segments === DROP) return DROP;
  t.segments = [];
  segments.forEach((raw2, i) => {
    const p = `${path}/segments/${i}`;
    if (!isPlainObject(raw2)) {
      ctx.diag(p, `a segment must be an object, not ${typeName(raw2)}`, 'item-unusable');
      return;
    }
    const skv = known(raw2, p, ['pips'], [], ctx);
    const s = new Segment();
    s.pips = pips(skv, p, ctx);
    if (s.pips === DROP) return;
    ctx.paths.set(s, p);
    t.segments.push(s);
  });
  t.name = str('name', path, kv, false, ctx);
  t.notes = str('notes', path, kv, false, ctx);
  t.folder = str('folder', path, kv, false, ctx);
  t.hidden = bool('hidden', path, kv, false, ctx);
  t.style = style(path, kv, ctx);
  t.started = timestamp('started', path, kv, ctx);
  t.ended = timestamp('ended', path, kv, ctx);
  t.data = data(path, kv, ctx);
  return t;
}

/** A top-level waypoint and a network's point are one shape (SPEC.md 3.6). */
function readPointLike(raw, path, ctx, topLevel) {
  const optional = ['name', 'notes', 'hidden', 'style', 'data'];
  if (topLevel) optional.push('folder');
  const kv = known(raw, path, ['at'], optional, ctx);
  const w = new Waypoint();
  if (kv.at === undefined) {
    ctx.diag(`${path}/at`, 'missing required key "at"', 'wrong-type-required');
    return DROP;
  }
  const at = position(kv.at, `${path}/at`, ctx, 'wrong-type-required', 'so the item is dropped');
  if (at === DROP) return DROP;
  w.at = at;
  w.name = str('name', path, kv, false, ctx);
  w.notes = str('notes', path, kv, false, ctx);
  if (topLevel) w.folder = str('folder', path, kv, false, ctx);
  w.hidden = bool('hidden', path, kv, false, ctx);
  w.style = style(path, kv, ctx);
  w.data = data(path, kv, ctx);
  return w;
}

/**
 * An area carries ONE open ring of at least three distinct positions
 * (SPEC.md 3.7).
 *
 * A nested or additional ring drops the area: keeping the outer ring would
 * draw an area whose hole had vanished, with nothing to say so.
 *
 * ⚠ A CLOSED RING IS REPAIRED, NOT DROPPED. Its last position repeating its
 * first is one fact written twice, so only that position goes, with a
 * diagnostic. The distinct positions are counted after that, so [A, B, A] is
 * two positions and the area goes.
 */
function readArea(raw, path, ctx, topLevel) {
  const optional = ['name', 'notes', 'hidden', 'style', 'data'];
  if (topLevel) optional.push('folder');
  const kv = known(raw, path, ['polygon'], optional, ctx);
  const ring = arr('polygon', path, kv, true, ctx);
  if (ring === DROP) return DROP;
  const p = `${path}/polygon`;
  if (ring.some((el) => Array.isArray(el) && el.some(Array.isArray))) {
    ctx.diag(p, 'a polygon carries one ring; this one has nested rings', 'polygon-nested-rings');
    return DROP;
  }
  const vertices = [];
  for (let i = 0; i < ring.length; i++) {
    const v = position(ring[i], `${p}/${i}`, ctx, 'position-unusable', 'so the area is dropped');
    if (v === DROP) return DROP;
    vertices.push(v);
  }
  const n = vertices.length;
  if (n >= 2 && vertices[0].sameCoordinates(vertices[n - 1])) {
    ctx.diag(`${p}/${n - 1}`, 'a ring is stored open, and this last position repeats the first, so it is dropped',
      'ring-closed');
    vertices.pop();
  }
  const distinct = distinctLonLat(vertices);
  if (distinct < 3) {
    ctx.diag(p, `the ring has ${distinct} distinct position(s) and needs at least 3, so the area is dropped`,
      'ring-too-few-points');
    return DROP;
  }
  const a = new Area();
  a.polygon = vertices;
  a.name = str('name', path, kv, false, ctx);
  a.notes = str('notes', path, kv, false, ctx);
  if (topLevel) a.folder = str('folder', path, kv, false, ctx);
  a.hidden = bool('hidden', path, kv, false, ctx);
  a.style = style(path, kv, ctx);
  a.data = data(path, kv, ctx);
  return a;
}

/**
 * The number of positions that differ in longitude or latitude; no other
 * channel takes part (SPEC.md 3.7). `String` spells 0 and -0 alike, and they
 * are one value.
 */
export function distinctLonLat(ring) {
  return new Set(ring.map((v) => `${v.lon},${v.lat}`)).size;
}

// --- cross-references -------------------------------------------------------

function resolveReferences(doc, ctx) {
  const folders = new Set((doc.folders ?? []).map((f) => f.id));

  // SPEC.md 6.2: a `folder` naming a folder that is not present is not an
  // unusable value. The key is treated as absent, the item sits at the top
  // level, and the loss is diagnosed. Dropping the item would lose content over
  // a lost grouping.
  const placed = [
    ...(doc.folders ?? []).map((f) => [f, 'parent']),
    ...(doc.networks ?? []).map((n) => [n, 'folder']),
    ...(doc.routes ?? []).map((r) => [r, 'folder']),
    ...(doc.tracks ?? []).map((t) => [t, 'folder']),
    ...(doc.waypoints ?? []).map((w) => [w, 'folder']),
    ...(doc.areas ?? []).map((a) => [a, 'folder']),
  ];
  for (const [item, key] of placed) {
    const ref = item[key];
    if (ref === undefined || folders.has(ref)) continue;
    ctx.diag(`${ctx.paths.get(item)}/${key}`,
      `"${key}" names ${JSON.stringify(ref)}, which is not a folder in this document`,
      key === 'parent' ? 'parent-folder-absent' : 'folder-absent');
    item[key] = undefined;
  }

}

// --- position index ---------------------------------------------------------

/**
 * Every position in the decoded document, by JSON Pointer.
 *
 * The pointers use the WIRE's key names — `at`, `pips`, `polygon` — and the
 * MODEL's indices. That distinction is load-bearing and easy to get backwards:
 * an element dropped under SPEC.md 6.2 shifts every later index, so after a
 * reader drops the pip at wire index 1 the model's `/pips/1` is the wire's pip
 * 2. `corpus/README.md` requires the pointer to be resolved against the decoded
 * model, and this builds the index from the model for that reason.
 *
 * A DIAGNOSTIC's path is the other convention — it names where the fault was in
 * the document the reader was handed, which is the wire index. So the same
 * pointer string can mean two different elements in the two fields of one
 * sidecar, and does in `diagnose/position-unusable`.
 *
 * @param {Document} doc
 * @returns {Map<string, Position>}
 */
export function indexPositions(doc) {
  const index = new Map();
  const put = (path, p) => {
    if (p !== undefined) index.set(path, p);
  };
  const pips = (path, list) => (list ?? []).forEach((p, i) => put(`${path}/pips/${i}`, p));
  const ring = (path, list) => (list ?? []).forEach((p, i) => put(`${path}/polygon/${i}`, p));

  (doc.waypoints ?? []).forEach((w, i) => put(`/waypoints/${i}/at`, w.at));
  (doc.areas ?? []).forEach((a, i) => ring(`/areas/${i}`, a.polygon));
  // Only a POSITION in a walk is a position. A node key is a reference, and is
  // addressed at the node.
  const walk = (path, passes) => (passes ?? []).forEach((p, k) => {
    for (const which of ['from', 'to']) {
      if (p[which] !== undefined && p[which].kind === 'position') put(`${path}/${k}/${which}`, p[which].position);
    }
    put(`${path}/${k}/turn`, p.turn);
  });
  const graph = (base, owner) => {
    for (const [key, p] of owner.nodes ?? []) put(`${base}/nodes/${esc(key)}`, p);
    for (const [key, arc] of owner.arcs ?? []) pips(`${base}/arcs/${esc(key)}`, arc.pips);
  };
  (doc.routes ?? []).forEach((r, i) => graph(`/routes/${i}`, r));
  (doc.tracks ?? []).forEach((t, i) => {
    (t.segments ?? []).forEach((seg, j) => pips(`/tracks/${i}/segments/${j}`, seg.pips));
  });
  (doc.networks ?? []).forEach((n, i) => {
    const base = `/networks/${i}`;
    graph(base, n);
    (n.chains ?? []).forEach((c, j) => walk(`${base}/chains/${j}/walk`, c.walk));
    (n.points ?? []).forEach((pt, j) => put(`${base}/points/${j}/at`, pt.at));
    (n.areas ?? []).forEach((a, j) => ring(`${base}/areas/${j}`, a.polygon));
  });
  return index;
}

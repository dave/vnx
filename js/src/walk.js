// A walk (SPEC.md 3.3.3): the rules that span more than one pass, and the line
// a walk draws.
//
// ⚠ A PASS CANNOT BE CHECKED FROM ITS OWN KEYS. Its entry is where the previous
// pass left, and where the previous pass left depends on whether it had a `to`
// and a `turn`. So a cursor carries the node the walk is at from one pass to
// the next, and the same stepping serves the reader's check, the encoder's
// check and `walkLegs` — three callers, one answer.

import { Endpoint, Position } from './model.js';
import { nearestOnLine } from './geo.js';

/**
 * The first rule a pass breaks: `key` is `''` when the diagnostic names the
 * pass itself, and otherwise the pass key it names (SPEC.md 6.3).
 */
export class WalkFault {
  constructor(pass, key, code, message) {
    this.pass = pass;
    this.key = key;
    this.code = code;
    this.message = message;
  }
  toError() {
    const at = this.key === '' ? `pass ${this.pass}` : `pass ${this.pass} \`${this.key}\``;
    return new Error(`vnx: ${at}: ${this.message} (${this.code})`);
  }
}

export class WalkCursor {
  /**
   * @param {number} last the index of the walk's last pass
   * @param {boolean} route a route's walk runs node to node (SPEC.md 3.4)
   */
  constructor(last, route) {
    this.last = last;
    this.route = route;
    /** @type {string|undefined} the node the walk is at; undefined before the first pass */
    this.at = undefined;
  }

  /**
   * Check pass `i` against the rules of SPEC.md 3.3.3 that span more than the
   * pass's own values — a route's rule, valid-walk rules 2 to 6 — in the order
   * SPEC.md 6.2 lists them, and move the cursor to where the pass leaves.
   *
   * @param {number} i
   * @param {import('./model.js').Pass} p
   * @param {import('./model.js').Arc} arc the arc `p` names
   * @returns {{entry: Endpoint, exit: Endpoint} | {fault: WalkFault}}
   */
  step(i, p, arc) {
    const fault = (key, code, message) => ({ fault: new WalkFault(i, key, code, message) });
    const isPosition = (e) => e !== undefined && e.kind === 'position';
    if (this.route) {
      if (isPosition(p.from))
        return fault(
          'from',
          'route-pin-or-position',
          "a route's walk runs node to node, and this `from` is a position",
        );
      if (isPosition(p.to))
        return fault('to', 'route-pin-or-position', "a route's walk runs node to node, and this `to` is a position");
      if (p.turn !== undefined) return fault('turn', 'route-pin-or-position', "a route's walk carries no turn");
      if (p.pin !== undefined) return fault('pin', 'route-pin-or-position', "a route's walk carries no pin");
    }
    if (p.turn !== undefined && p.pin !== undefined) {
      return fault('', 'pinned-turn', 'a pass with a turn carries no pin: the turn already fixes it');
    }
    // The rule for `from` and `to`: first `from`, then `to`.
    if (i === 0 && p.from === undefined) return fault('', 'walk-spelling', 'the first pass must carry `from`');
    if (i > 0 && p.from !== undefined) return fault('from', 'walk-spelling', 'only the first pass carries `from`');
    const wantTo = i === this.last || isPosition(p.from);
    if (wantTo && p.to === undefined) {
      return fault(
        '',
        'walk-spelling',
        i === this.last ? 'the last pass must carry `to`' : 'a pass whose `from` is a position must carry `to`',
      );
    }
    if (!wantTo && p.to !== undefined) {
      return fault(
        'to',
        'walk-spelling',
        '`to` appears only on the last pass, or on a pass whose `from` is a position',
      );
    }
    // Connection.
    const isEnd = (node) => node === arc.from || node === arc.to;
    let entry;
    if (p.from !== undefined) {
      if (p.from.kind === 'node' && !isEnd(p.from.node)) {
        return fault(
          'from',
          'walk-not-connected',
          `\`from\` node ${JSON.stringify(p.from.node)} is not an end of arc ${JSON.stringify(p.arc)}`,
        );
      }
      entry = p.from;
    } else {
      if (!isEnd(this.at)) {
        return fault(
          '',
          'walk-not-connected',
          `the walk is at node ${JSON.stringify(this.at)}, which is not an end of arc ${JSON.stringify(p.arc)}`,
        );
      }
      entry = new Endpoint('node', this.at);
    }
    let exit;
    if (p.to !== undefined && p.to.kind === 'node' && !isEnd(p.to.node)) {
      return fault(
        'to',
        'walk-not-connected',
        `\`to\` node ${JSON.stringify(p.to.node)} is not an end of arc ${JSON.stringify(p.arc)}`,
      );
    } else if (isPosition(p.to) && i !== this.last) {
      return fault('to', 'walk-not-connected', 'only the last pass leaves at a position');
    } else if (p.to !== undefined) {
      exit = p.to;
    } else if (p.turn !== undefined) {
      // The entry is a node here: a position `from` requires a `to`, which the
      // spelling check above has already enforced.
      exit = new Endpoint('node', entry.node);
    } else if (entry.node === arc.from) {
      exit = new Endpoint('node', arc.to);
    } else {
      exit = new Endpoint('node', arc.from);
    }
    this.at = exit.node;
    return { entry, exit };
  }
}

/**
 * The encoder's check on a walk: every pass names an arc of its owner, each
 * endpoint is exactly one of a node key and a position, and the walk follows
 * SPEC.md 3.3.3. A writer never emits a walk its own reader would drop.
 *
 * @param {import('./model.js').Pass[]} walk
 * @param {Map<string, import('./model.js').Arc>|undefined} arcs
 * @param {boolean} route
 * @returns {WalkFault|null}
 */
export function checkWalk(walk, arcs, route) {
  const cursor = new WalkCursor(walk.length - 1, route);
  for (let i = 0; i < walk.length; i++) {
    const p = walk[i];
    const arc = arcs?.get(p.arc);
    if (arc === undefined) {
      return new WalkFault(i, '', 'pass-arc-absent', `the pass names arc ${JSON.stringify(p.arc)}, which is not here`);
    }
    for (const key of ['from', 'to']) {
      const e = p[key];
      if (e === undefined) continue;
      const ok =
        (e.kind === 'node' && typeof e.node === 'string') || (e.kind === 'position' && e.position instanceof Position);
      if (!ok) {
        return new WalkFault(
          i,
          key,
          'position-unusable',
          'an endpoint must be exactly one of a node key and a position',
        );
      }
    }
    const step = cursor.step(i, p, arc);
    if (step.fault) return step.fault;
  }
  return null;
}

/**
 * The encoder's check on an owner's arcs: two different nodes, both present.
 * Returns an English reason naming the arc, or null.
 *
 * @param {Map<string, import('./model.js').Position>|undefined} nodes
 * @param {Map<string, import('./model.js').Arc>|undefined} arcs
 */
export function checkArcs(nodes, arcs) {
  for (const [id, a] of arcs ?? []) {
    if (typeof a.from !== 'string' || typeof a.to !== 'string') {
      return `arc ${JSON.stringify(id)} must name both its nodes`;
    }
    if (a.from === a.to) {
      return `arc ${JSON.stringify(id)} runs from node ${JSON.stringify(a.from)} to itself, and a self-loop cannot be written`;
    }
    for (const n of [a.from, a.to]) {
      if (!(nodes ?? new Map()).has(n)) {
        return `arc ${JSON.stringify(id)} names node ${JSON.stringify(n)}, which is not in its owner's nodes`;
      }
    }
  }
  return null;
}

/**
 * The whole line of an arc (SPEC.md 3.3.2): its `from` node's position, its
 * interior pips, and its `to` node's position.
 *
 * @param {Map<string, Position>|undefined} nodes
 * @param {import('./model.js').Arc} arc
 * @returns {Position[]}
 * @throws {Error} the arc names a node its owner does not have
 */
export function arcLine(nodes, arc) {
  const from = nodes?.get(arc.from);
  const to = nodes?.get(arc.to);
  for (const [key, node] of [
    [arc.from, from],
    [arc.to, to],
  ]) {
    if (node === undefined)
      throw new Error(`vnx: the arc names node ${JSON.stringify(key)}, which is not in its owner`);
  }
  return [from, ...arc.pips, to];
}

/**
 * The line each pass of a walk covers, one leg per pass, in walk order
 * (SPEC.md 3.3.3): from the pass's entry, out to its turn where it has one, and
 * on to its exit, along its arc's line whatever order those points lie in. A
 * leg runs the way the walk goes, so a pass that walks its arc from `to` to
 * `from` is reversed.
 *
 * ⚠ A NODE END IS THE ARC'S OWN END, NOT A PROJECTION. A position is cut at the
 * nearest point of the line, at any distance.
 *
 * ⚠ A WALK THAT DOES NOT RESOLVE IS AN ERROR, never a guess. Drawing "the whole
 * arc forwards" would put a line on the map that nobody walked.
 *
 * An interpolated point carries no elevation and no time: absent is absent. A
 * cut that lands exactly on a vertex copies that vertex whole.
 *
 * @param {Map<string, Position>|undefined} nodes
 * @param {Map<string, import('./model.js').Arc>|undefined} arcs
 * @param {import('./model.js').Pass[]} walk
 * @returns {Position[][]}
 * @throws {Error} the walk does not resolve
 */
export function walkLegs(nodes, arcs, walk) {
  const cursor = new WalkCursor(walk.length - 1, false);
  const legs = [];
  for (let i = 0; i < walk.length; i++) {
    const p = walk[i];
    const arc = arcs?.get(p.arc);
    if (arc === undefined) {
      throw new WalkFault(
        i,
        '',
        'pass-arc-absent',
        `the pass names arc ${JSON.stringify(p.arc)}, which is not here`,
      ).toError();
    }
    const step = cursor.step(i, p, arc);
    if (step.fault) throw step.fault.toError();
    const line = arcLine(nodes, arc);
    const cutAt = (e) => {
      if (e.kind === 'position') return nearestOnLine(line, e.position);
      if (e.node === arc.from) return { seg: 0, t: 0 };
      return { seg: line.length - 2, t: 1 };
    };
    const entry = cutAt(step.entry);
    const exit = cutAt(step.exit);
    if (p.turn === undefined) {
      legs.push(between(line, entry, exit));
      continue;
    }
    const turn = nearestOnLine(line, p.turn);
    legs.push([...between(line, entry, turn), ...between(line, turn, exit).slice(1)]);
  }
  return legs;
}

/**
 * The walk's legs joined into one line: where a leg starts at the point the
 * previous one ended, the shared point is written once.
 *
 * @returns {Position[]}
 * @throws {Error} the walk does not resolve
 */
export function walkLine(nodes, arcs, walk) {
  const out = [];
  for (let leg of walkLegs(nodes, arcs, walk)) {
    const last = out[out.length - 1];
    if (last !== undefined && leg.length > 0 && last.sameCoordinates(leg[0])) leg = leg.slice(1);
    out.push(...leg);
  }
  return out;
}

const before = (a, b) => (a.seg !== b.seg ? a.seg < b.seg : a.t < b.t);

function pointAt(line, c) {
  const a = line[c.seg];
  const b = line[c.seg + 1];
  if (c.t === 0) return a;
  if (c.t === 1) return b;
  return new Position([a.lon + (b.lon - a.lon) * c.t, a.lat + (b.lat - a.lat) * c.t]);
}

/** The piece of `line` from cut `a` to cut `b`, reversed when `b` comes first. */
function between(line, a, b) {
  if (before(b, a)) return between(line, b, a).reverse();
  const out = [pointAt(line, a)];
  for (let i = a.seg + 1; i <= b.seg; i++) out.push(line[i]);
  out.push(pointAt(line, b));
  return out;
}

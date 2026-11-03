// The decoded model.
//
// Two properties of this model are load-bearing for SPEC.md 8.1's accept tier
// and are easy to lose:
//
//   * A position keeps its ARITY, including a trailing `null` (SPEC.md 4.1).
//     An `ele` field that is merely optional is the model that cannot re-render
//     `accept/absent-elevation-and-time.vnx`.
//   * Opaque values — data record bodies, `style` objects, the values of
//     application keys — are kept as the parsed JSON value, whole. Nothing
//     here inspects them.
//
// Absent is `undefined` throughout, and is never a default (SPEC.md 3.1).

/** A position: an array of fixed positional channels (SPEC.md 4.1). */
export class Position {
  /** @param {Array<number|null>} channels */
  constructor(channels) {
    /** @type {Array<number|null>} */
    this.channels = channels;
  }
  get lon() {
    return this.channels[0];
  }
  get lat() {
    return this.channels[1];
  }
  /** @returns {number|null|undefined} elevation in metres */
  get ele() {
    return this.channels.length > 2 ? this.channels[2] : undefined;
  }
  /** @returns {number|null|undefined} Unix milliseconds */
  get time() {
    return this.channels.length > 3 ? this.channels[3] : undefined;
  }
  get arity() {
    return this.channels.length;
  }
  /** Value equality over lon and lat only (SPEC.md 3.7: two positions are distinct when they differ in either). */
  sameCoordinates(other) {
    return this.lon === other.lon && this.lat === other.lat;
  }
  /** Value equality over every channel. */
  equals(other) {
    if (this.channels.length !== other.channels.length) return false;
    return this.channels.every((c, i) => c === other.channels[i]);
  }
}

export class Folder {
  constructor() {
    /** @type {string} */ this.id = undefined;
    /** @type {string|undefined} */ this.parent = undefined;
    /** @type {string|undefined} */ this.name = undefined;
    /** @type {string|undefined} */ this.notes = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

/**
 * An arc of a network or a plan (SPEC.md 3.3.2). `pips` holds the INTERIOR
 * points of the line only: the line runs from the `from` node's position,
 * through `pips`, to the `to` node's. An arc may have no pips. A pip carries no
 * time, and `from` and `to` name two different nodes of the arc's owner.
 */
export class Arc {
  constructor() {
    /** @type {string} */ this.from = undefined;
    /** @type {string} */ this.to = undefined;
    /** @type {Position[]} */ this.pips = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

/**
 * Where a pass enters or leaves its arc (SPEC.md 3.3.3): a node key, or a
 * position on the arc. A position carries longitude and latitude only.
 */
export class Endpoint {
  /**
   * @param {'node'|'position'} kind
   * @param {string|Position} value
   */
  constructor(kind, value) {
    this.kind = kind;
    /** @type {string|undefined} */ this.node = kind === 'node' ? value : undefined;
    /** @type {Position|undefined} */ this.position = kind === 'position' ? value : undefined;
  }
}

/**
 * One entry of a walk (SPEC.md 3.3.3): the arc it covers, and where on it the
 * walk enters, turns and leaves.
 *
 * `from` and `to` are present only where the rule for `from` and `to` puts
 * them: on the first pass, on the last pass, and on a pass whose `from` is a
 * position. Everywhere else the entry is the node the walk is at, and the exit
 * is derived. An absent `from` or `to` is that absence, not "unknown".
 *
 * A pass is pinned when it has a `pin`: the user fixed it with a handle at that
 * position. The pin leaves what the pass covers unchanged.
 */
export class Pass {
  constructor() {
    /** @type {string} */ this.arc = undefined;
    /** @type {Endpoint|undefined} */ this.from = undefined;
    /** @type {Endpoint|undefined} */ this.to = undefined;
    /** @type {Position|undefined} */ this.turn = undefined;
    /** @type {Position|undefined} */ this.pin = undefined;
  }
}

/**
 * A lightweight container for data objects along a walk of its network's arcs
 * (SPEC.md 3.3.3). It has no name or style of its own. Its geometry is by
 * reference: two highlights that cross one trail name the same arc.
 */
export class Highlight {
  constructor() {
    /** @type {Pass[]} */ this.walk = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
    /** @type {string|undefined} */ this.pinned = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

export class Waypoint {
  constructor() {
    /** @type {Position} */ this.at = undefined;
    /** @type {string|undefined} */ this.name = undefined;
    /** @type {string|undefined} */ this.notes = undefined;
    /** @type {string|undefined} */ this.folder = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
    /** @type {string|undefined} */ this.pinned = undefined;
    /** @type {object|undefined} */ this.style = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

export class Area {
  constructor() {
    /** @type {Position[]} */ this.polygon = undefined;
    /** @type {string|undefined} */ this.name = undefined;
    /** @type {string|undefined} */ this.notes = undefined;
    /** @type {string|undefined} */ this.folder = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
    /** @type {string|undefined} */ this.pinned = undefined;
    /** @type {object|undefined} */ this.style = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

export class Network {
  constructor() {
    /** @type {string|undefined} */ this.name = undefined;
    /** @type {string|undefined} */ this.notes = undefined;
    /** @type {string|undefined} */ this.folder = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
    /** @type {string|undefined} */ this.pinned = undefined;
    /** @type {Map<string, Position>|undefined} */ this.nodes = undefined;
    /** @type {Map<string, Arc>|undefined} */ this.arcs = undefined;
    /** @type {Highlight[]|undefined} */ this.highlights = undefined;
    /** @type {Waypoint[]|undefined} */ this.points = undefined;
    /** @type {Area[]|undefined} */ this.areas = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

/**
 * A top-level plan (SPEC.md 3.4). It owns its graph: `nodes` and `arcs` of a
 * network's shape, and one walk over them. The walk runs node to node, with no
 * position, no `turn` and no `pin`.
 */
export class Plan {
  constructor() {
    /** @type {Pass[]} */ this.walk = undefined;
    /** @type {Map<string, Position>|undefined} */ this.nodes = undefined;
    /** @type {Map<string, Arc>|undefined} */ this.arcs = undefined;
    /** @type {string|undefined} */ this.name = undefined;
    /** @type {string|undefined} */ this.notes = undefined;
    /** @type {string|undefined} */ this.folder = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
    /** @type {string|undefined} */ this.pinned = undefined;
    /** @type {object|undefined} */ this.style = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

export class Segment {
  constructor() {
    /** @type {Position[]} */ this.pips = undefined;
  }
}

export class Track {
  constructor() {
    /** @type {Segment[]} */ this.segments = undefined;
    /** @type {string|undefined} */ this.name = undefined;
    /** @type {string|undefined} */ this.notes = undefined;
    /** @type {string|undefined} */ this.folder = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
    /** @type {string|undefined} */ this.pinned = undefined;
    /** @type {object|undefined} */ this.style = undefined;
    /** @type {string|undefined} */ this.started = undefined;
    /** @type {string|undefined} */ this.ended = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }

  /**
   * Moving time in milliseconds, or `null` for unknown (SPEC.md 3.5).
   *
   * The sum over segments of the difference between the last and the first pip
   * time in that segment — deliberately NOT the wall-clock span, which pauses
   * lengthen.
   *
   * Unknown is not zero, and unknown is ALL-OR-NOTHING. A track any segment of
   * which cannot be measured is unknown as a whole: summing only the segments
   * that carry times reports a number for a span the reader cannot measure,
   * which is the shape SPEC.md 3.5 forbids when it rules out zero for a track
   * with no times at all. The rule that makes zero wrong there makes a partial
   * sum wrong here — it is a fallback that hides missing data behind a
   * plausible number, and a user comparing two tracks gets a wrong answer with
   * nothing to say so. Ruled 2026-09-09.
   *
   * @returns {number|null}
   */
  movingTimeMs() {
    const segments = this.segments ?? [];
    let total = 0;
    let measured = 0;
    for (const segment of segments) {
      const pips = segment.pips ?? [];
      if (pips.length === 0) continue;
      const first = pips[0].time;
      const last = pips[pips.length - 1].time;
      if (typeof first !== 'number' || typeof last !== 'number') return null;
      measured++;
      total += last - first;
    }
    return measured === 0 ? null : total;
  }
}

export class Document {
  constructor() {
    /** @type {string} the version the document declared (SPEC.md 2.2) */
    this.vnx = undefined;
    /** @type {string|undefined} */ this.generator = undefined;
    /** @type {string|undefined} */ this.created = undefined;
    /** @type {Folder[]|undefined} */ this.folders = undefined;
    /** @type {Network[]|undefined} */ this.networks = undefined;
    /** @type {Plan[]|undefined} */ this.plans = undefined;
    /** @type {Track[]|undefined} */ this.tracks = undefined;
    /** @type {Waypoint[]|undefined} */ this.waypoints = undefined;
    /** @type {Area[]|undefined} */ this.areas = undefined;
    /**
     * The application keys (SPEC.md 3.9), sorted by key on decode. A list of
     * pairs rather than an object, so a key's presence never depends on its
     * value: a `null` value is a present key.
     * @type {AppKey[]}
     */
    this.appKeys = [];
  }
}

/** One application key: a top-level key containing a full stop (SPEC.md 3.9). */
export class AppKey {
  /**
   * @param {string} key
   * @param {unknown} value the parsed JSON value, `null` included; opaque
   */
  constructor(key, value) {
    this.key = key;
    this.value = value;
  }
}

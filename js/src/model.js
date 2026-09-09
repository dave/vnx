// The decoded model.
//
// Two properties of this model are load-bearing for SPEC.md 8.1's accept tier
// and are easy to lose:
//
//   * A position keeps its ARITY, including a trailing `null` (SPEC.md 4.1).
//     An `ele` field that is merely optional is the model that cannot re-render
//     `accept/absent-elevation-and-time.vnx`.
//   * Opaque values — data record bodies, `style` objects, `rulesets` — are
//     kept as the parsed JSON value, whole. Nothing here inspects them.
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
  /** Value equality over lon and lat only; see decode.js `endpointsMeet`. */
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

export class NetworkArc {
  constructor() {
    /** @type {string} */ this.from = undefined;
    /** @type {string} */ this.to = undefined;
    /** @type {Position[]} */ this.pips = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

/** A link endpoint: a node key, or an inline position (SPEC.md 3.3.3). */
export class Endpoint {
  /** @param {'node'|'inline'} kind */
  constructor(kind, value) {
    this.kind = kind;
    /** @type {string|undefined} */ this.node = kind === 'node' ? value : undefined;
    /** @type {Position|undefined} */ this.position = kind === 'inline' ? value : undefined;
  }
}

export class Link {
  constructor() {
    /** @type {string} */ this.arc = undefined;
    /** @type {Endpoint} */ this.from = undefined;
    /** @type {Endpoint} */ this.to = undefined;
  }
}

export class Chain {
  constructor() {
    /** @type {Link[]} */ this.links = undefined;
    /** @type {string|undefined} */ this.name = undefined;
    /** @type {string|undefined} */ this.notes = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
    /** @type {object|undefined} */ this.style = undefined;
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
    /** @type {Map<string, Position>|undefined} */ this.nodes = undefined;
    /** @type {Map<string, NetworkArc>|undefined} */ this.arcs = undefined;
    /** @type {Chain[]|undefined} */ this.chains = undefined;
    /** @type {Waypoint[]|undefined} */ this.points = undefined;
    /** @type {Area[]|undefined} */ this.areas = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

export class RouteArc {
  constructor() {
    /** @type {Position[]} */ this.pips = undefined;
    /** @type {object[]|undefined} */ this.data = undefined;
  }
}

export class Route {
  constructor() {
    /** @type {RouteArc[]} */ this.arcs = undefined;
    /** @type {string|undefined} */ this.name = undefined;
    /** @type {string|undefined} */ this.notes = undefined;
    /** @type {string|undefined} */ this.folder = undefined;
    /** @type {boolean|undefined} */ this.hidden = undefined;
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
   * lengthen. Unknown is not zero: a track carrying no pip times is
   * indistinguishable from a track on which nobody moved, and the reader must
   * be able to say which it is holding.
   *
   * @returns {number|null}
   */
  movingTimeMs() {
    let total = 0;
    let known = false;
    for (const segment of this.segments ?? []) {
      const pips = segment.pips ?? [];
      if (pips.length === 0) continue;
      const first = pips[0].time;
      const last = pips[pips.length - 1].time;
      if (typeof first !== 'number' || typeof last !== 'number') continue;
      known = true;
      total += last - first;
    }
    return known ? total : null;
  }
}

export class Document {
  constructor() {
    /** @type {string} the version the document declared (SPEC.md 2.2) */
    this.vnx = undefined;
    /** @type {string|undefined} */ this.profile = undefined;
    /** @type {string|undefined} */ this.generator = undefined;
    /** @type {string|undefined} */ this.created = undefined;
    /** @type {Folder[]|undefined} */ this.folders = undefined;
    /** @type {Network[]|undefined} */ this.networks = undefined;
    /** @type {Route[]|undefined} */ this.routes = undefined;
    /** @type {Track[]|undefined} */ this.tracks = undefined;
    /** @type {Waypoint[]|undefined} */ this.waypoints = undefined;
    /** @type {Area[]|undefined} */ this.areas = undefined;
    /** @type {unknown[]|undefined} opaque; preserved, never interpreted (3.9) */
    this.rulesets = undefined;
  }
}

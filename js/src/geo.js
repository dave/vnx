// The nearest point of an arc's line to a position (SPEC.md 3.3.3).
//
// A position in a walk's `from`, `to` or `turn` identifies the point of its
// arc's line nearest to it, at ANY distance. Two readers must identify the same
// point, so the distance is defined exactly: every coordinate is projected to
// metres by a local equirectangular map about the latitude of the POSITION,
// held constant for every vertex, which keeps the map affine. Taking the cosine
// from each segment's own start instead is a different model and cuts a walk
// somewhere else.

export const EARTH_RADIUS_M = 6371008.8;

const DEG = Math.PI / 180;

/**
 * The point of `line` nearest to `point`: the index of the segment it lies on,
 * and how far along that segment (0 to 1). Where two segments are equally
 * near, the earlier one wins — `<`, not `<=`, is that rule.
 *
 * @param {import('./model.js').Position[]} line at least two vertices
 * @param {import('./model.js').Position} point
 * @returns {{seg: number, t: number}}
 */
export function nearestOnLine(line, point) {
  if (line.length < 2) throw new Error(`vnx: a line needs at least two vertices, this one has ${line.length}`);
  let best = null;
  let bestDist = Infinity;
  for (let i = 0; i + 1 < line.length; i++) {
    const { t, metres } = nearestOnSegment(point, line[i], line[i + 1]);
    if (metres < bestDist) {
      bestDist = metres;
      best = { seg: i, t };
    }
  }
  return best;
}

/**
 * Project `p` onto the segment `a`–`b` and return how far along it lands
 * (0 to 1) and how far away it is, in metres.
 */
export function nearestOnSegment(p, a, b) {
  const k = EARTH_RADIUS_M * Math.cos(p.lat * DEG);
  const px = (p.lon - a.lon) * DEG * k;
  const py = (p.lat - a.lat) * DEG * EARTH_RADIUS_M;
  const bx = (b.lon - a.lon) * DEG * k;
  const by = (b.lat - a.lat) * DEG * EARTH_RADIUS_M;
  const lenSq = bx * bx + by * by;
  if (lenSq === 0) return { t: 0, metres: Math.hypot(px, py) };
  const t = Math.max(0, Math.min(1, (px * bx + py * by) / lenSq));
  return { t, metres: Math.hypot(px - t * bx, py - t * by) };
}

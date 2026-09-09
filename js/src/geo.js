// The projection tolerance model of SPEC.md 3.3.3.
//
// The model is stated in the spec because the CHOICE decides which documents
// are legal, not because the arithmetic is delicate: four plausible models read
// one endpoint of the worked example at 19.81 m, 20.34 m, 25.52 m and 36.47 m.
// The cosine term is taken from the latitude of the INLINE POSITION and held
// constant for every vertex, which keeps the map affine.

export const EARTH_RADIUS_M = 6371008.8;
export const PROJECTION_TOLERANCE_M = 50;

const DEG = Math.PI / 180;

/**
 * Least distance in metres from `point` to the polyline `pips`.
 *
 * @param {import('./model.js').Position} point
 * @param {import('./model.js').Position[]} pips
 * @returns {number|null} null when the polyline has no vertices, which has no
 *   distance: the caller must diagnose rather than invent a number.
 */
export function distanceToPolylineM(point, pips) {
  if (pips.length === 0) return null;
  const k = EARTH_RADIUS_M * Math.cos(point.lat * DEG);
  const project = (p) => [p.lon * DEG * k, p.lat * DEG * EARTH_RADIUS_M];
  const [px, py] = project(point);
  if (pips.length === 1) {
    const [ax, ay] = project(pips[0]);
    return Math.hypot(px - ax, py - ay);
  }
  let best = Infinity;
  let [ax, ay] = project(pips[0]);
  for (let i = 1; i < pips.length; i++) {
    const [bx, by] = project(pips[i]);
    const d = distanceToSegment(px, py, ax, ay, bx, by);
    if (d < best) best = d;
    ax = bx;
    ay = by;
  }
  return best;
}

function distanceToSegment(px, py, ax, ay, bx, by) {
  const vx = bx - ax;
  const vy = by - ay;
  const len2 = vx * vx + vy * vy;
  if (len2 === 0) return Math.hypot(px - ax, py - ay);
  let t = ((px - ax) * vx + (py - ay) * vy) / len2;
  if (t < 0) t = 0;
  else if (t > 1) t = 1;
  return Math.hypot(px - (ax + t * vx), py - (ay + t * vy));
}

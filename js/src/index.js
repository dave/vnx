// The VNX codec: read and write `.vnx` and `.vnz`, produce canonical form, and
// report diagnostics. See ../README.md for what it does not do.

export { VnxRefusal, Diagnostic, DIAGNOSTIC_CODES, PINNED_CODES, REFUSAL_SUBSTRINGS } from './errors.js';
export {
  Position, Folder, Network, NetworkArc, Chain, Link, Endpoint,
  Route, RouteArc, Track, Segment, Waypoint, Area, Document,
} from './model.js';
export { decodeText, decodeValue, READER_MAJOR, READER_MINOR } from './decode.js';
export { renderCanonical } from './render.js';
export { emitDocument, RERENDER, WRITE } from './emit.js';
export { compareCodePoints } from './codepoint.js';
export { renderNumber } from './number.js';
export { distanceToPolylineM, PROJECTION_TOLERANCE_M, EARTH_RADIUS_M } from './geo.js';
export { roundToPlaces } from './round.js';
export { decodeContainer, encodeContainer, rerenderContainer, writeContainer } from './vnz.js';
export { rerender, write } from './emitpaths.js';

import { decodeText } from './decode.js';
import { rerender } from './emitpaths.js';

/**
 * Canonicalise document text: decode it, then re-render it. Precision and
 * position arity survive, because canonical form constrains key order and
 * number spelling, not precision (SPEC.md 4.5.4).
 * @param {string} text
 * @returns {{text: string, diagnostics: import('./errors.js').Diagnostic[]}}
 */
export function canonicalise(text) {
  const { document, diagnostics } = decodeText(text);
  return { text: rerender(document), diagnostics };
}

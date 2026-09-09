// The VNX codec: read and write `.vnx` and `.vnz`, produce canonical form, and
// report diagnostics. See ../README.md for what it does not do.

export { VnxRefusal, Diagnostic, CAUSE_SUBSTRINGS } from './errors.js';
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
export { decodeContainer, encodeContainer } from './vnz.js';

import { decodeText } from './decode.js';
import { renderCanonical } from './render.js';
import { emitDocument, RERENDER, WRITE } from './emit.js';

/**
 * RE-RENDER: turn a document a reader was handed back into that document,
 * applying neither rounding nor shortening (SPEC.md 8.1). This is the operation
 * the accept tier requires, and the one `canonicalise` below is built from.
 * @param {import('./model.js').Document} doc
 * @returns {string}
 */
export function rerender(doc) {
  return renderCanonical(emitDocument(doc, RERENDER));
}

/**
 * WRITE: turn a model into a document, applying the emit-boundary rules —
 * coordinates rounded to 6 places and elevations to 1 (SPEC.md 4.5.4), and the
 * shortest spelling of each position (SPEC.md 4.1).
 * @param {import('./model.js').Document} doc
 * @returns {string}
 */
export function write(doc) {
  return renderCanonical(emitDocument(doc, WRITE));
}

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

// The two emit paths a conforming library offers, named distinctly (SPEC.md
// 4.5.4). They live here rather than in index.js so that vnz.js can reach them
// without an import cycle.

import { renderCanonical } from './render.js';
import { emitDocument, RERENDER, WRITE } from './emit.js';

/**
 * RE-RENDER: turn a document a reader was handed back into that document,
 * applying neither rounding nor shortening, so arity and precision survive.
 * This is the operation the accept tier requires (SPEC.md 8.1).
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
 *
 * The negative-zero rule of SPEC.md 4.5.3 is applied AFTER rounding, by the
 * renderer: rounding CREATES negative zeros that were never in the model, and a
 * writer collapsing only the ones it was handed would emit `-0`.
 * @param {import('./model.js').Document} doc
 * @returns {string}
 */
export function write(doc) {
  return renderCanonical(emitDocument(doc, WRITE));
}

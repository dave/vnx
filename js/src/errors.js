// Errors and diagnostics.
//
// A diagnostic carries a `code` — a short stable token naming which diagnostic
// it is (SPEC.md 6.3) — alongside its English `message`. The code is what a
// conformance case compares; the English MUST NOT be compared, because a corpus
// that matched substrings of prose across implementations would be pinning
// translations of a rule rather than the rule.
//
// A refusal still carries only English, matched as a substring. That is a known
// defect and typed errors are their own piece of work; `REFUSAL_SUBSTRINGS`
// below is the migration map, kept honest by a test.

/** A document (or container) the reader refuses. SPEC.md 6.2 and 7. */
export class VnxRefusal extends Error {
  /**
   * @param {string} cause stable cause code
   * @param {string} message English text; MUST contain the corpus substring
   * @param {object} [options]
   */
  constructor(cause, message, options = {}) {
    super(message, options);
    this.name = 'VnxRefusal';
    this.cause_code = cause;
    /** JSON Pointer to the offending element, where one is known. */
    this.path = options.path ?? null;
  }
}

/** One thing the reader could not use as written. SPEC.md 6.3. */
export class Diagnostic {
  /**
   * @param {string} path JSON Pointer (RFC 6901)
   * @param {string} message short description in English, advisory only
   * @param {string} code stable token naming which diagnostic this is
   */
  constructor(path, message, code) {
    this.path = path;
    this.message = message;
    this.code = code;
  }
  toJSON() {
    return { path: this.path, code: this.code, message: this.message };
  }
}

/**
 * Every diagnostic code this library emits.
 *
 * SPEC.md 6.3 lets a reader leave `code` absent on any diagnostic no corpus
 * case pins. This library emits one for all of them: an absent code is only
 * ever less useful to a caller, and the register below is what stops a typo in
 * a call site inventing a code nobody declared.
 */
export const DIAGNOSTIC_CODES = Object.freeze([
  // pinned by the corpus (see PINNED_CODES)
  'chain-discontinuity',
  'endpoint-off-arc',
  'link-arc-absent',
  'polygon-nested-rings',
  'position-unusable',
  'terminal-pip-off-node',
  'timestamp-not-utc',
  'unknown-key',
  'wrong-type-optional',
  'wrong-type-required',
  // not pinned: this library's own tokens, and not wire commitments
  'arc-node-absent',
  'attachment-compression',
  'attachment-unreadable',
  'folder-absent',
  'importance-out-of-range',
  'inline-endpoint-empty-arc',
  'item-unusable',
  'link-node-absent',
  'missing-required-key',
  'parent-folder-absent',
  'polygon-not-closed',
  'polygon-too-short',
  'record-type-missing',
  'record-unusable',
]);

/**
 * The codes the conformance corpus pins today.
 *
 * The set grows whenever a diagnose case is added. `test/runner.test.js`
 * compares it against the corpus in BOTH directions, so it cannot drift: a case
 * pinning a code absent from here fails, and a code here that no case pins fails
 * too.
 */
export const PINNED_CODES = Object.freeze([
  'chain-discontinuity',
  'endpoint-off-arc',
  'link-arc-absent',
  'polygon-nested-rings',
  'position-unusable',
  'terminal-pip-off-node',
  'timestamp-not-utc',
  'unknown-key',
  'wrong-type-optional',
  'wrong-type-required',
]);

/**
 * The substring the corpus expects for each refusal cause. Refusals are still
 * compared as English; this is the whole of the migration when they gain codes.
 *
 * `non-finite-number` has no corpus case, and the gap is the clearest argument
 * for typed refusals. The corpus expects `malformed JSON` for a `NaN` or
 * `Infinity` LITERAL, which only a STRICT parser says: measured 2026-09-09,
 *
 *   literal        Go encoding/json      Node JSON.parse   Python json.loads
 *   NaN            invalid character     throws            accepts as nan
 *   Infinity       invalid character     throws            accepts as inf
 *   1e400          cannot unmarshal      Infinity          accepts as inf
 *
 * so the expected substring describes a route to the refusal and not the rule.
 * A permissive parser reaches SPEC.md 4.2 and refuses for the right reason in
 * different words.
 *
 * `1e400` is the case this library has to catch itself: it is well-formed JSON
 * and `JSON.parse` yields `Infinity` without complaint, so the reviver in
 * decode.js is the only thing between it and a coordinate of Infinity.
 */
export const REFUSAL_SUBSTRINGS = Object.freeze({
  'malformed-json': 'malformed JSON',
  'non-finite-number': 'is not finite',
  'not-a-vnx-document': 'not a VNX document',
  'version-shape': 'MAJOR.MINOR.PATCH',
  'version-too-new': 'a newer reader is needed',
  'zip-duplicate-entry': 'duplicate entry',
  'zip-path-escapes': 'escapes the archive',
  'zip-malformed': 'malformed archive',
  'zip-no-document': 'no VNX document',
  'zip-root-compression': 'compression method',
});

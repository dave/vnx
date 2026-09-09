// Errors and diagnostics.
//
// Every refusal and every diagnostic carries a stable machine-readable `cause`
// alongside its English `message`. The conformance corpus matches English
// substrings today (SPEC.md 8.1); when it grows reason codes, the mapping in
// `CAUSE_SUBSTRINGS` below is the whole of the migration.

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
   * @param {string} message short English description
   * @param {string} cause stable cause code
   */
  constructor(path, message, cause) {
    this.path = path;
    this.message = message;
    this.cause = cause;
  }
  toJSON() {
    return { path: this.path, message: this.message };
  }
}

// The substring the corpus currently expects for each cause. Refusal causes are
// checked against a case's `refusal`; diagnostic causes against a `message`.
// This table exists so that a corpus migration to reason codes is mechanical:
// nothing else in this library depends on the English wording.
export const CAUSE_SUBSTRINGS = {
  // refusals
  'malformed-json': 'malformed JSON',
  // A `NaN` or `Infinity` LITERAL is not JSON and is refused as malformed. A
  // literal that overflows the double range (`1e400`) parses and arrives as
  // Infinity; SPEC.md 4.2 refuses it separately and the corpus has no case.
  'non-finite-number': 'is not finite',
  'not-a-vnx-document': 'not a VNX document',
  'version-shape': 'MAJOR.MINOR.PATCH',
  'version-too-new': 'a newer reader is needed',
  'zip-duplicate-entry': 'duplicate entry',
  'zip-path-escapes': 'escapes the archive',
  'zip-malformed': 'malformed archive',
  'zip-no-document': 'no VNX document',
  'zip-root-compression': 'compression method',
  // diagnostics
  'unknown-key': 'unknown key',
  'link-arc-absent': 'not in this network',
  'chain-not-connected': 'not one connected walk',
  'polygon-nested-rings': 'nested rings',
  'inline-endpoint-off-arc': 'beyond the 50 m tolerance',
  'timestamp-not-utc': 'must be UTC with a Z suffix',
};

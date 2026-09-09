// The VNZ container (SPEC.md 7).
//
// A container is refused for causes of its own, separate from a document's: a
// duplicate entry name, an attachment path that escapes the archive, and an
// unreadable ROOT DOCUMENT. A fault in an attachment is not among them — a
// whole library is not refused over one photograph.

import { VnxRefusal } from './errors.js';
import { Diagnostic } from './errors.js';
import { decodeText } from './decode.js';
import { compareCodePoints } from './codepoint.js';
import { rerender, write } from './emitpaths.js';
import { readEntries, readEntryData, writeArchive, ZipError, METHOD_DEFLATE, METHOD_STORE } from './zip.js';

const SUPPORTED = new Set([METHOD_STORE, METHOD_DEFLATE]);

/**
 * @typedef {{document: import('./model.js').Document,
 *            diagnostics: Diagnostic[],
 *            positions: Map<string, import('./model.js').Position>,
 *            documentEntry: string,
 *            attachments: Map<string, Buffer>}} Container
 */

/**
 * @param {Buffer} buf
 * @returns {Container}
 */
export function decodeContainer(buf) {
  let entries;
  try {
    entries = readEntries(buf);
  } catch (err) {
    if (err instanceof ZipError) throw new VnxRefusal('zip-malformed', `malformed archive: ${err.message}`, { cause: err });
    throw err;
  }

  // SPEC.md 7: duplicate entry names are refused, compared case-insensitively.
  // Language runtimes resolve a repeated name differently, so one archive would
  // otherwise yield different documents in different languages.
  const seen = new Map();
  for (const e of entries) {
    const key = e.name.toLowerCase();
    if (seen.has(key)) {
      throw new VnxRefusal('zip-duplicate-entry',
        `duplicate entry name ${JSON.stringify(e.name)} in the archive`, { path: e.name });
    }
    seen.set(key, e);
  }

  for (const e of entries) {
    const fault = pathFault(e.name);
    if (fault !== null) {
      throw new VnxRefusal('zip-path-escapes',
        `entry ${JSON.stringify(e.name)} escapes the archive: ${fault}`, { path: e.name });
    }
  }

  const files = entries.filter((e) => !e.name.endsWith('/'));

  // Root document: prefer an entry named `doc.vnx` at the archive root, matched
  // on the FULL entry name, case-insensitively. Where there is none, take the
  // first entry with a `.vnx` extension in archive order.
  let root = files.find((e) => e.name.toLowerCase() === 'doc.vnx');
  if (root === undefined) root = files.find((e) => e.name.toLowerCase().endsWith('.vnx'));
  if (root === undefined) {
    throw new VnxRefusal('zip-no-document', 'the archive holds no VNX document: no entry has a .vnx extension');
  }
  if (!SUPPORTED.has(root.method)) {
    throw new VnxRefusal('zip-root-compression',
      `the root document uses compression method ${root.method}; only store and deflate are available`,
      { path: root.name });
  }

  let text;
  try {
    text = readEntryData(buf, root).toString('utf8');
  } catch (err) {
    if (err instanceof ZipError) {
      throw new VnxRefusal('zip-malformed', `malformed archive: ${err.message}`, { cause: err });
    }
    throw err;
  }
  const result = decodeText(text);

  const attachments = new Map();
  for (const e of files) {
    if (e === root) continue;
    if (!SUPPORTED.has(e.method)) {
      // An attachment the reader cannot decompress is dropped with a
      // diagnostic; the document is still returned.
      result.diagnostics.push(new Diagnostic(
        `/attachments/${e.name}`,
        `attachment uses compression method ${e.method}; only store and deflate are available`,
        'attachment-compression',
      ));
      continue;
    }
    try {
      attachments.set(e.name, readEntryData(buf, e));
    } catch (err) {
      if (!(err instanceof ZipError)) throw err;
      result.diagnostics.push(new Diagnostic(
        `/attachments/${e.name}`, `attachment could not be read: ${err.message}`, 'attachment-unreadable',
      ));
    }
  }

  return { ...result, documentEntry: root.name, attachments };
}

/**
 * SPEC.md 7's path rules, in the order they are applied.
 *
 * The BACKSLASH check stands alone and fires FIRST, so `..\\x` is refused for the
 * simple reason rather than the subtle one and "is a backslash a separator while
 * hunting for the segment" never has to be answered. Ruled 2026-09-09; the
 * refusal is deliberately stricter than [APPNOTE], which permits the character,
 * because no VNX archive has a legitimate use for it in an entry name.
 *
 * `..` is then a path SEGMENT, not a substring: `a..b/photo.jpg` is a legal name
 * and escapes nothing.
 *
 * @returns {string|null} the fault, or null when the path is fine
 */
function pathFault(name) {
  if (name.includes('\\')) return 'the name carries a backslash';
  if (name.startsWith('/')) return 'the path is absolute';
  if (/^[A-Za-z]:/.test(name)) return 'the path carries a drive letter';
  for (const segment of name.split('/')) {
    if (segment === '..') return 'the path has a `..` segment';
  }
  return null;
}

/**
 * RE-RENDER a decoded container: the document is reproduced as it was handed to
 * the reader, and the attachments and the document's entry name are carried
 * through unchanged.
 *
 * The container emit path splits the same way the document one does (SPEC.md
 * 4.5.4), so repackaging a container cannot silently round it.
 *
 * @param {Container} container from decodeContainer
 * @returns {Buffer}
 */
export function rerenderContainer(container) {
  return packContainer(container, rerender(container.document));
}

/**
 * WRITE a decoded container: the document goes through the writer, so its
 * coordinates are rounded and its positions shortened.
 *
 * @param {Container} container
 * @returns {Buffer}
 */
export function writeContainer(container) {
  return packContainer(container, write(container.document));
}

function packContainer(container, documentText) {
  const entry = container.documentEntry ?? 'doc.vnx';
  const attachments = [];
  for (const [name, data] of container.attachments ?? []) {
    if (name === entry) continue;
    attachments.push({ name, data });
  }
  return writeArchive([{ name: entry, data: Buffer.from(documentText, 'utf8') }, ...sortEntries(attachments)]);
}

/**
 * SPEC.md 7.1: the root document first, then attachments sorted by entry name
 * by Unicode CODE POINT.
 *
 * Caller order is not a property of the content — two exporters over one
 * library, one iterating a hash map and one a sorted list, would otherwise emit
 * different archives while satisfying every other field in the table.
 *
 * The comparator is required and is not decoration. `Array.prototype.sort()`
 * orders by UTF-16 code unit, which disagrees with code point above U+FFFF, and
 * an attachment name may carry any character. This is the same divergence
 * SPEC.md 4.5.1 calls out for object keys, in a place the spec does not repeat
 * the warning.
 */
function sortEntries(files) {
  return [...files].sort((a, b) => compareCodePoints(a.name, b.name));
}

/**
 * Build a container from document TEXT the caller has already produced, so the
 * caller has chosen which emit path it wanted. The document goes in as
 * `doc.vnx`.
 *
 * @param {string} documentText canonical or written VNX text
 * @param {Iterable<[string, Buffer]>} [attachments]
 * @returns {Buffer}
 */
export function encodeContainer(documentText, attachments = []) {
  const used = new Set(['doc.vnx']);
  const files = [];
  for (const [name, data] of attachments) {
    const fault = pathFault(name);
    if (fault !== null) throw new VnxRefusal('zip-path-escapes', `attachment ${JSON.stringify(name)} escapes the archive: ${fault}`);
    const key = name.toLowerCase();
    if (used.has(key)) throw new VnxRefusal('zip-duplicate-entry', `duplicate entry name ${JSON.stringify(name)}`);
    used.add(key);
    files.push({ name, data });
  }
  return writeArchive([
    { name: 'doc.vnx', data: Buffer.from(documentText, 'utf8') },
    ...sortEntries(files),
  ]);
}

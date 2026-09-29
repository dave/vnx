// The VNZ container (SPEC.md 7).
//
// A container is refused for causes of its own, separate from a document's: a
// duplicate entry name, an attachment path that escapes the archive, and an
// unreadable ROOT DOCUMENT. A fault in an attachment is not among them — a
// whole library is not refused over one photograph.

import { VnxRefusal } from './errors.js';
import { Diagnostic } from './errors.js';
import { decodeText } from './decode.js';
import { encode } from './emit.js';
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
    if (err instanceof ZipError)
      throw new VnxRefusal('zip-malformed', `malformed archive: ${err.message}`, { cause: err });
    throw err;
  }

  // SPEC.md 7: duplicate entry names are refused, compared case-insensitively.
  // Language runtimes resolve a repeated name differently, so one archive would
  // otherwise yield different documents in different languages.
  const seen = new Map();
  for (const e of entries) {
    const key = e.name.toLowerCase();
    if (seen.has(key)) {
      throw new VnxRefusal('zip-duplicate-entry', `duplicate entry name ${JSON.stringify(e.name)} in the archive`, {
        path: e.name,
      });
    }
    seen.set(key, e);
  }

  for (const e of entries) {
    const fault = pathFault(e.name);
    if (fault !== null) {
      throw new VnxRefusal('zip-path-escapes', `entry ${JSON.stringify(e.name)} escapes the archive: ${fault}`, {
        path: e.name,
      });
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
    throw new VnxRefusal(
      'zip-root-compression',
      `the root document uses compression method ${root.method}; only store and deflate are available`,
      { path: root.name },
    );
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
      result.diagnostics.push(
        new Diagnostic(
          entryPointer(e.name),
          `attachment uses compression method ${e.method}; only store and deflate are available`,
          'attachment-unreadable',
        ),
      );
      continue;
    }
    try {
      attachments.set(e.name, readEntryData(buf, e));
    } catch (err) {
      if (!(err instanceof ZipError)) throw err;
      result.diagnostics.push(
        new Diagnostic(entryPointer(e.name), `attachment could not be read: ${err.message}`, 'attachment-unreadable'),
      );
    }
  }

  return { ...result, documentEntry: root.name, attachments };
}

/**
 * SPEC.md 6.3's `#/entries/` pointer space, which is where a diagnostic about
 * an archive ENTRY lives: an archive entry is not part of the document, so no
 * pointer into the document can locate one.
 *
 * ⚠ THE MEMBER NAME IS ONE RFC 6901 REFERENCE TOKEN, and the escaping ORDER
 * decides the answer: `~` becomes `~0` BEFORE `/` becomes `~1`. Reversed, the
 * second pass escapes the `~1` the first pass just wrote, so `photos/a~b.jpg`
 * comes out `photos~01a~0b.jpg`. An attachment name is the one place in this
 * format where a member name routinely holds a slash.
 */
function entryPointer(name) {
  return `#/entries/${name.replace(/~/g, '~0').replace(/\//g, '~1')}`;
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
 * Write a decoded container back out: the document goes through the encoder, so
 * its positions are written in their shortest spelling, and the attachments and
 * the document's entry name are carried through unchanged.
 *
 * @param {Container} container from decodeContainer
 * @returns {Buffer}
 */
export function writeContainer(container) {
  return packContainer(container, encode(container.document));
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
 * The root document first, then attachments sorted by entry name in UTF-8 byte
 * order, which is the order of their code points.
 *
 * SPEC.md 7.1 does not constrain entry order past the root document. Sorting is
 * this library's own choice, so that two exporters over it — one iterating a
 * hash map, one a sorted list — write the same archive.
 *
 * ⚠ `Array.prototype.sort()` WILL NOT DO. It orders strings by UTF-16 code
 * unit, which disagrees with code-point order above U+FFFF, and an attachment
 * name may carry any character. Comparing the UTF-8 bytes gives code-point
 * order directly.
 */
function sortEntries(files) {
  return [...files].sort((a, b) => Buffer.from(a.name, 'utf8').compare(Buffer.from(b.name, 'utf8')));
}

/**
 * Build a container from document TEXT the caller has already produced. The
 * document goes in as `doc.vnx`.
 *
 * @param {string} documentText VNX document text
 * @param {Iterable<[string, Buffer]>} [attachments]
 * @returns {Buffer}
 */
export function encodeContainer(documentText, attachments = []) {
  const used = new Set(['doc.vnx']);
  const files = [];
  for (const [name, data] of attachments) {
    const fault = pathFault(name);
    if (fault !== null)
      throw new VnxRefusal('zip-path-escapes', `attachment ${JSON.stringify(name)} escapes the archive: ${fault}`);
    const key = name.toLowerCase();
    if (used.has(key)) throw new VnxRefusal('zip-duplicate-entry', `duplicate entry name ${JSON.stringify(name)}`);
    used.add(key);
    files.push({ name, data });
  }
  return writeArchive([{ name: 'doc.vnx', data: Buffer.from(documentText, 'utf8') }, ...sortEntries(files)]);
}

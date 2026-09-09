// A ZIP reader and writer, written rather than imported.
//
// SPEC.md 7 restricts compression to STORED and DEFLATE, which Node's `zlib`
// covers, so the format needs no third-party archive library and this file is
// the whole of the container mechanics. ZIP64 must be READ; a writer MAY emit a
// plain archive, and this one does.

import zlib from 'node:zlib';

const SIG_LOCAL = 0x04034b50;
const SIG_CENTRAL = 0x02014b50;
const SIG_EOCD = 0x06054b50;
const SIG_ZIP64_EOCD = 0x06064b50;
const SIG_ZIP64_LOCATOR = 0x07064b50;

export const METHOD_STORE = 0;
export const METHOD_DEFLATE = 8;

const MAX32 = 0xffffffff;
const MAX16 = 0xffff;

export class ZipError extends Error {
  constructor(message) {
    super(message);
    this.name = 'ZipError';
  }
}

const CRC_TABLE = (() => {
  const t = new Int32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c;
  }
  return t;
})();

export function crc32(buf) {
  let c = ~0;
  for (let i = 0; i < buf.length; i++) c = CRC_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8);
  return (~c) >>> 0;
}

/**
 * @typedef {{name: string, method: number, compressedSize: number,
 *            uncompressedSize: number, crc: number, flags: number,
 *            localOffset: number}} ZipEntry
 */

/**
 * Read an archive's central directory, in archive order.
 * @param {Buffer} buf
 * @returns {ZipEntry[]}
 */
export function readEntries(buf) {
  const eocd = findEocd(buf);
  let entryCount = buf.readUInt16LE(eocd + 10);
  let cdSize = buf.readUInt32LE(eocd + 12);
  let cdOffset = buf.readUInt32LE(eocd + 16);

  if (entryCount === MAX16 || cdSize === MAX32 || cdOffset === MAX32) {
    const z = findZip64(buf, eocd);
    entryCount = readU64(buf, z + 32);
    cdSize = readU64(buf, z + 40);
    cdOffset = readU64(buf, z + 48);
  }

  const entries = [];
  let p = cdOffset;
  for (let i = 0; i < entryCount; i++) {
    if (p + 46 > buf.length || buf.readUInt32LE(p) !== SIG_CENTRAL) {
      throw new ZipError(`central directory entry ${i} is not where the archive says it is`);
    }
    const flags = buf.readUInt16LE(p + 8);
    const method = buf.readUInt16LE(p + 10);
    const crc = buf.readUInt32LE(p + 16);
    let compressedSize = buf.readUInt32LE(p + 20);
    let uncompressedSize = buf.readUInt32LE(p + 24);
    const nameLen = buf.readUInt16LE(p + 28);
    const extraLen = buf.readUInt16LE(p + 30);
    const commentLen = buf.readUInt16LE(p + 32);
    let localOffset = buf.readUInt32LE(p + 42);
    const name = buf.subarray(p + 46, p + 46 + nameLen).toString('utf8');
    const extra = buf.subarray(p + 46 + nameLen, p + 46 + nameLen + extraLen);

    if (uncompressedSize === MAX32 || compressedSize === MAX32 || localOffset === MAX32) {
      const z = findZip64Extra(extra);
      if (z === null) throw new ZipError(`entry ${JSON.stringify(name)} needs a ZIP64 extra field and has none`);
      let q = 0;
      if (uncompressedSize === MAX32) { uncompressedSize = readU64(z, q); q += 8; }
      if (compressedSize === MAX32) { compressedSize = readU64(z, q); q += 8; }
      if (localOffset === MAX32) { localOffset = readU64(z, q); q += 8; }
    }

    entries.push({ name, method, crc, compressedSize, uncompressedSize, flags, localOffset });
    p += 46 + nameLen + extraLen + commentLen;
  }
  return entries;
}

/**
 * Decompress one entry's bytes.
 * @param {Buffer} buf
 * @param {ZipEntry} entry
 * @returns {Buffer}
 */
export function readEntryData(buf, entry) {
  const p = entry.localOffset;
  if (p + 30 > buf.length || buf.readUInt32LE(p) !== SIG_LOCAL) {
    throw new ZipError(`entry ${JSON.stringify(entry.name)} has no local header`);
  }
  // The local header's own name and extra lengths are authoritative for where
  // the data starts; the central directory's extra field is a different field.
  const nameLen = buf.readUInt16LE(p + 26);
  const extraLen = buf.readUInt16LE(p + 28);
  const start = p + 30 + nameLen + extraLen;
  const raw = buf.subarray(start, start + entry.compressedSize);
  let out;
  if (entry.method === METHOD_STORE) out = Buffer.from(raw);
  else if (entry.method === METHOD_DEFLATE) out = zlib.inflateRawSync(raw);
  else throw new ZipError(`entry ${JSON.stringify(entry.name)} uses compression method ${entry.method}`);
  if (crc32(out) !== entry.crc) {
    throw new ZipError(`entry ${JSON.stringify(entry.name)} fails its CRC check`);
  }
  return out;
}

function findEocd(buf) {
  const max = Math.min(buf.length, 22 + MAX16);
  for (let i = buf.length - 22; i >= buf.length - max; i--) {
    if (i < 0) break;
    if (buf.readUInt32LE(i) === SIG_EOCD) return i;
  }
  throw new ZipError('not a ZIP archive: no end-of-central-directory record');
}

function findZip64(buf, eocd) {
  const loc = eocd - 20;
  if (loc < 0 || buf.readUInt32LE(loc) !== SIG_ZIP64_LOCATOR) {
    throw new ZipError('the archive needs ZIP64 and carries no ZIP64 locator');
  }
  const z = readU64(buf, loc + 8);
  if (buf.readUInt32LE(z) !== SIG_ZIP64_EOCD) {
    throw new ZipError('the ZIP64 locator does not point at a ZIP64 end-of-central-directory record');
  }
  return z;
}

function findZip64Extra(extra) {
  let p = 0;
  while (p + 4 <= extra.length) {
    const id = extra.readUInt16LE(p);
    const size = extra.readUInt16LE(p + 2);
    if (id === 0x0001) return extra.subarray(p + 4, p + 4 + size);
    p += 4 + size;
  }
  return null;
}

function readU64(buf, off) {
  const v = buf.readBigUInt64LE(off);
  if (v > BigInt(Number.MAX_SAFE_INTEGER)) throw new ZipError('a ZIP64 field exceeds 2^53');
  return Number(v);
}

// --- writing ----------------------------------------------------------------

/**
 * Write a plain (non-ZIP64) archive with the UTF-8 filename flag set.
 * @param {Array<{name: string, data: Buffer, method?: number}>} files
 * @returns {Buffer}
 */
export function writeArchive(files) {
  const locals = [];
  const centrals = [];
  let offset = 0;
  for (const f of files) {
    const name = Buffer.from(f.name, 'utf8');
    const method = f.method ?? METHOD_DEFLATE;
    const body = method === METHOD_STORE ? f.data : zlib.deflateRawSync(f.data, { level: 9 });
    const crc = crc32(f.data);

    const local = Buffer.alloc(30);
    local.writeUInt32LE(SIG_LOCAL, 0);
    local.writeUInt16LE(20, 4); // version needed
    local.writeUInt16LE(0x0800, 6); // UTF-8 filename flag
    local.writeUInt16LE(method, 8);
    local.writeUInt16LE(0, 10); // mod time
    local.writeUInt16LE(0x0021, 12); // mod date: 1980-01-01
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(body.length, 18);
    local.writeUInt32LE(f.data.length, 22);
    local.writeUInt16LE(name.length, 26);
    local.writeUInt16LE(0, 28);
    locals.push(local, name, body);

    const central = Buffer.alloc(46);
    central.writeUInt32LE(SIG_CENTRAL, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(0x0800, 8);
    central.writeUInt16LE(method, 10);
    central.writeUInt16LE(0, 12);
    central.writeUInt16LE(0x0021, 14);
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(body.length, 20);
    central.writeUInt32LE(f.data.length, 24);
    central.writeUInt16LE(name.length, 28);
    central.writeUInt32LE(offset, 42);
    centrals.push(central, name);

    offset += local.length + name.length + body.length;
  }
  const localBytes = Buffer.concat(locals);
  const centralBytes = Buffer.concat(centrals);
  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(SIG_EOCD, 0);
  eocd.writeUInt16LE(files.length, 8);
  eocd.writeUInt16LE(files.length, 10);
  eocd.writeUInt32LE(centralBytes.length, 12);
  eocd.writeUInt32LE(localBytes.length, 16);
  return Buffer.concat([localBytes, centralBytes, eocd]);
}

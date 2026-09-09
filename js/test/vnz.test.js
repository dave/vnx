// The container (SPEC.md 7), and the ZIP mechanics under it.

import test from 'node:test';
import assert from 'node:assert/strict';
import zlib from 'node:zlib';

import { decodeContainer, encodeContainer } from '../src/vnz.js';
import { readEntries, readEntryData, writeArchive, crc32, METHOD_STORE } from '../src/zip.js';
import { VnxRefusal } from '../src/errors.js';

const DOC = '{\n  "vnx": "0.1.0",\n  "waypoints": [\n    {\n      "at": [-3.6712, 57.118]\n    }\n  ]\n}\n';

function refusal(fn) {
  try {
    fn();
  } catch (err) {
    assert.ok(err instanceof VnxRefusal, `threw ${err}`);
    return err;
  }
  assert.fail('the archive was read; it must be refused');
}

test('a container round-trips through this library', () => {
  const buf = encodeContainer(DOC, [['attachments/photo.txt', Buffer.from('hello')]]);
  const c = decodeContainer(buf);
  assert.equal(c.documentEntry, 'doc.vnx');
  assert.deepEqual([...c.attachments.keys()], ['attachments/photo.txt']);
  assert.equal(c.attachments.get('attachments/photo.txt').toString(), 'hello');
  assert.equal(c.document.waypoints.length, 1);
  assert.deepEqual(c.diagnostics, []);
});

test('stored entries are read as well as deflated ones', () => {
  const buf = writeArchive([{ name: 'doc.vnx', data: Buffer.from(DOC), method: METHOD_STORE }]);
  assert.equal(decodeContainer(buf).document.vnx, '0.1.0');
});

test('the root document is matched on the FULL entry name, not the base name', () => {
  // A nested `doc.vnx` must not win over the first `.vnx` at the root. This is
  // the defect SPEC.md 7 warns about and the corpus does not cover.
  const buf = writeArchive([
    { name: 'export.vnx', data: Buffer.from(DOC) },
    { name: 'nested/doc.vnx', data: Buffer.from('{"vnx": "0.1.0", "waypoints": []}') },
  ]);
  const c = decodeContainer(buf);
  assert.equal(c.documentEntry, 'export.vnx');
  assert.equal(c.document.waypoints.length, 1);
});

test('the root document match is case-insensitive', () => {
  const buf = writeArchive([
    { name: 'aaa.vnx', data: Buffer.from('{"vnx": "0.1.0", "waypoints": []}') },
    { name: 'DOC.VNX', data: Buffer.from(DOC) },
  ]);
  assert.equal(decodeContainer(buf).documentEntry, 'DOC.VNX');
});

test('the fallback takes the FIRST .vnx entry in archive order', () => {
  const buf = writeArchive([
    { name: 'a.vnx', data: Buffer.from(DOC) },
    { name: 'b.vnx', data: Buffer.from('{"vnx": "0.1.0", "waypoints": []}') },
  ]);
  assert.equal(decodeContainer(buf).documentEntry, 'a.vnx');
});

test('duplicate entry names are refused, compared case-insensitively', () => {
  const buf = writeArchive([
    { name: 'doc.vnx', data: Buffer.from(DOC) },
    { name: 'DOC.VNX', data: Buffer.from(DOC) },
  ]);
  const err = refusal(() => decodeContainer(buf));
  assert.equal(err.cause_code, 'zip-duplicate-entry');
});

test('a path that escapes the archive is refused', () => {
  for (const name of ['../etc/passwd', 'a/../../b', '/etc/passwd', 'C:/windows', '..\\\\x']) {
    const buf = writeArchive([
      { name: 'doc.vnx', data: Buffer.from(DOC) },
      { name, data: Buffer.from('x') },
    ]);
    assert.equal(refusal(() => decodeContainer(buf)).cause_code, 'zip-path-escapes', name);
  }
});

test('`..` is a segment, not a substring', () => {
  // SPEC.md 7 names this explicitly: `a..b/photo.jpg` is a legal name and
  // escapes nothing. Nothing in the corpus covers it.
  const buf = writeArchive([
    { name: 'doc.vnx', data: Buffer.from(DOC) },
    { name: 'a..b/photo.jpg', data: Buffer.from('x') },
  ]);
  assert.deepEqual([...decodeContainer(buf).attachments.keys()], ['a..b/photo.jpg']);
});

test('an archive with no .vnx entry is refused', () => {
  const buf = writeArchive([{ name: 'readme.txt', data: Buffer.from('x') }]);
  assert.equal(refusal(() => decodeContainer(buf)).cause_code, 'zip-no-document');
});

test('an unreadable ROOT DOCUMENT refuses the archive', () => {
  const buf = mutateMethod(writeArchive([{ name: 'doc.vnx', data: Buffer.from(DOC) }]), 'doc.vnx', 12);
  assert.equal(refusal(() => decodeContainer(buf)).cause_code, 'zip-root-compression');
});

test('an unreadable ATTACHMENT is dropped with a diagnostic; the document survives', () => {
  // A whole library is not refused over one photograph (SPEC.md 7).
  const buf = mutateMethod(writeArchive([
    { name: 'doc.vnx', data: Buffer.from(DOC) },
    { name: 'photo.bin', data: Buffer.from('x'.repeat(64)) },
  ]), 'photo.bin', 12);
  const c = decodeContainer(buf);
  assert.equal(c.document.waypoints.length, 1);
  assert.deepEqual([...c.attachments.keys()], []);
  assert.equal(c.diagnostics[0].code, 'attachment-compression');
});

test('a corrupt entry fails its CRC rather than yielding wrong bytes', () => {
  const buf = writeArchive([{ name: 'doc.vnx', data: Buffer.from(DOC), method: METHOD_STORE }]);
  const i = buf.indexOf(Buffer.from('"0.1.0"'));
  buf[i + 1] = '9'.charCodeAt(0);
  assert.equal(refusal(() => decodeContainer(buf)).cause_code, 'zip-malformed');
});

test('a ZIP64 archive is readable', () => {
  const buf = zip64Archive('doc.vnx', Buffer.from(DOC));
  const entries = readEntries(buf);
  assert.equal(entries.length, 1);
  assert.equal(readEntryData(buf, entries[0]).toString(), DOC);
  assert.equal(decodeContainer(buf).document.vnx, '0.1.0');
});

test('crc32 matches the known value for the ZIP test vector', () => {
  assert.equal(crc32(Buffer.from('123456789')), 0xcbf43926);
});

/** Rewrite one entry's compression method in both headers. */
function mutateMethod(buf, name, method) {
  const out = Buffer.from(buf);
  for (const entry of readEntries(out)) {
    if (entry.name !== name) continue;
    out.writeUInt16LE(method, entry.localOffset + 8);
  }
  // and in the central directory
  let p = out.indexOf(Buffer.from([0x50, 0x4b, 0x01, 0x02]));
  while (p !== -1) {
    const nameLen = out.readUInt16LE(p + 28);
    if (out.subarray(p + 46, p + 46 + nameLen).toString() === name) {
      out.writeUInt16LE(method, p + 10);
      break;
    }
    p = out.indexOf(Buffer.from([0x50, 0x4b, 0x01, 0x02]), p + 1);
  }
  return out;
}

/**
 * A minimal ZIP64 archive: the sizes and the central-directory offset are all
 * written as the 0xFFFFFFFF escape, so a reader that ignores the ZIP64 records
 * cannot read it at all.
 */
function zip64Archive(name, data) {
  const nameBuf = Buffer.from(name, 'utf8');
  const body = zlib.deflateRawSync(data);
  const crc = crc32(data);

  const localExtra = Buffer.alloc(20);
  localExtra.writeUInt16LE(0x0001, 0);
  localExtra.writeUInt16LE(16, 2);
  localExtra.writeBigUInt64LE(BigInt(data.length), 4);
  localExtra.writeBigUInt64LE(BigInt(body.length), 12);

  const local = Buffer.alloc(30);
  local.writeUInt32LE(0x04034b50, 0);
  local.writeUInt16LE(45, 4);
  local.writeUInt16LE(0x0800, 6);
  local.writeUInt16LE(8, 8);
  local.writeUInt16LE(0x0021, 12);
  local.writeUInt32LE(crc, 14);
  local.writeUInt32LE(0xffffffff, 18);
  local.writeUInt32LE(0xffffffff, 22);
  local.writeUInt16LE(nameBuf.length, 26);
  local.writeUInt16LE(localExtra.length, 28);
  const localBytes = Buffer.concat([local, nameBuf, localExtra, body]);

  const centralExtra = Buffer.alloc(28);
  centralExtra.writeUInt16LE(0x0001, 0);
  centralExtra.writeUInt16LE(24, 2);
  centralExtra.writeBigUInt64LE(BigInt(data.length), 4);
  centralExtra.writeBigUInt64LE(BigInt(body.length), 12);
  centralExtra.writeBigUInt64LE(0n, 20);

  const central = Buffer.alloc(46);
  central.writeUInt32LE(0x02014b50, 0);
  central.writeUInt16LE(45, 4);
  central.writeUInt16LE(45, 6);
  central.writeUInt16LE(0x0800, 8);
  central.writeUInt16LE(8, 10);
  central.writeUInt16LE(0x0021, 14);
  central.writeUInt32LE(crc, 16);
  central.writeUInt32LE(0xffffffff, 20);
  central.writeUInt32LE(0xffffffff, 24);
  central.writeUInt16LE(nameBuf.length, 28);
  central.writeUInt16LE(centralExtra.length, 30);
  central.writeUInt32LE(0xffffffff, 42);
  const centralBytes = Buffer.concat([central, nameBuf, centralExtra]);

  const z64 = Buffer.alloc(56);
  z64.writeUInt32LE(0x06064b50, 0);
  z64.writeBigUInt64LE(44n, 4);
  z64.writeUInt16LE(45, 12);
  z64.writeUInt16LE(45, 14);
  z64.writeBigUInt64LE(1n, 24);
  z64.writeBigUInt64LE(1n, 32);
  z64.writeBigUInt64LE(BigInt(centralBytes.length), 40);
  z64.writeBigUInt64LE(BigInt(localBytes.length), 48);

  const locator = Buffer.alloc(20);
  locator.writeUInt32LE(0x07064b50, 0);
  locator.writeBigUInt64LE(BigInt(localBytes.length + centralBytes.length), 8);
  locator.writeUInt32LE(1, 16);

  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0);
  eocd.writeUInt16LE(0xffff, 8);
  eocd.writeUInt16LE(0xffff, 10);
  eocd.writeUInt32LE(0xffffffff, 12);
  eocd.writeUInt32LE(0xffffffff, 16);

  return Buffer.concat([localBytes, centralBytes, z64, locator, eocd]);
}

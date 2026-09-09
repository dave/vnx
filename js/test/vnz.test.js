// The container (SPEC.md 7), and the ZIP mechanics under it.

import test from 'node:test';
import assert from 'node:assert/strict';
import zlib from 'node:zlib';

import { decodeContainer, encodeContainer } from '../src/vnz.js';
import { readEntries, readEntryData, writeArchive, crc32, METHOD_STORE, METHOD_DEFLATE } from '../src/zip.js';
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
  for (const name of ['../etc/passwd', 'a/../../b', '/etc/passwd', 'C:/windows']) {
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

test('a backslash anywhere in an entry name is refused, on its own', () => {
  // Ruled 2026-09-09, stricter than APPNOTE, which permits the character. The
  // check stands alone and fires BEFORE the `..` segment hunt, so `..\x` is
  // refused for the simple reason and the question "is a backslash a separator
  // while hunting for the segment" never arises.
  for (const name of ['..\\x', 'weird\\name.jpg', 'sub\\dir\\photo.jpg', '\\abs.jpg']) {
    const buf = writeArchive([
      { name: 'doc.vnx', data: Buffer.from(DOC) },
      { name, data: Buffer.from('x') },
    ]);
    const err = refusal(() => decodeContainer(buf));
    assert.equal(err.cause_code, 'zip-path-escapes', name);
    assert.match(err.message, /backslash/, name);
  }
});

test('a legal `a..b/` name is still accepted after the backslash rule', () => {
  // The two rules must not collapse into one: `a..b/photo.jpg` is named legal by
  // SPEC.md 7 and carries no backslash.
  const buf = writeArchive([
    { name: 'doc.vnx', data: Buffer.from(DOC) },
    { name: 'a..b/photo.jpg', data: Buffer.from('x') },
  ]);
  assert.deepEqual([...decodeContainer(buf).attachments.keys()], ['a..b/photo.jpg']);
});

test('the writer refuses to pack a name it would refuse to read', () => {
  assert.throws(() => encodeContainer(DOC, [['weird\\name.jpg', Buffer.from('x')]]),
    /backslash/);
});

test('the writer STORES every entry, and the reader still inflates', () => {
  // Ruled 2026-09-09: deflate has no canonical output, so a deflated archive is
  // byte-reproducible only by the language that wrote it. Required of what you
  // write, never of what you read — the four committed .vnz fixtures are all
  // deflate and are what tests the reader side.
  const buf = encodeContainer(DOC, [['attachments/photo.txt', Buffer.from('not a photo\n')]]);
  for (const e of readEntries(buf)) {
    assert.equal(e.method, METHOD_STORE, e.name);
    assert.equal(e.flags, 0x0800, `${e.name}: UTF-8 flag only, no data descriptor`);
  }
  // and it still round-trips
  assert.equal(decodeContainer(buf).attachments.get('attachments/photo.txt').toString(), 'not a photo\n');
});

test('a writer does not defer sizes it already knows', () => {
  // Bit 3 (the data descriptor) moves the CRC and both sizes out of the local
  // header. A writer that knows them has no reason to defer them, and the extra
  // 16 bytes per entry are a byte difference carrying no information. A reader
  // must still accept it: the committed fixtures set 0x808.
  const buf = encodeContainer(DOC);
  const flags = buf.readUInt16LE(6);
  assert.equal(flags & 0x08, 0, 'bit 3 must not be set');
  assert.notEqual(buf.readUInt32LE(14), 0, 'CRC is in the local header');
  assert.notEqual(buf.readUInt32LE(18), 0, 'compressed size is in the local header');
  assert.notEqual(buf.readUInt32LE(22), 0, 'uncompressed size is in the local header');
});

// --- raw byte parsing ---------------------------------------------------------
//
// A ZIP LIBRARY CAN NEVER BE THE INSTRUMENT FOR A LOCAL-HEADER RULE, because
// every ZIP library resolves from the central directory on purpose. `readEntries`
// is one, so every assertion above that goes through it is an assertion about
// the central directory and is blind to a local header that disagrees.
//
// That is not hypothetical: the reference implementation was found on 2026-09-09
// emitting `flags=0x0008` in its local headers with correct values in its central
// directory, and its own gap-pinning test could not see it, because the test read
// its output back through `archive/zip`.
//
// These walk the bytes.

const SIG_LOCAL = 0x04034b50;
const SIG_CENTRAL = 0x02014b50;

/**
 * Walk the LOCAL headers, forward-only, exactly as a reader that cannot seek
 * would have to.
 *
 * It can only be done while bit 3 is clear. With a data descriptor the sizes in
 * the local header are zero, so there is nothing to advance by and a walker has
 * to scan for the next signature — which for a STORED entry is ambiguous,
 * because the payload can contain `PK\x03\x04`. This helper therefore embodies
 * the argument for the rule it checks: if it cannot walk, the archive is the
 * shape SPEC.md 7.1 forbids.
 */
function localHeaders(buf) {
  const out = [];
  let p = 0;
  while (p + 30 <= buf.length && buf.readUInt32LE(p) === SIG_LOCAL) {
    const flags = buf.readUInt16LE(p + 6);
    if (flags & 0x08) {
      throw new Error(
        `entry ${out.length} sets bit 3, so its local header carries no sizes and this ` +
        'archive cannot be walked forward-only',
      );
    }
    const nameLen = buf.readUInt16LE(p + 26);
    const extraLen = buf.readUInt16LE(p + 28);
    out.push({
      versionNeeded: buf.readUInt16LE(p + 4),
      flags,
      method: buf.readUInt16LE(p + 8),
      dosTime: buf.readUInt16LE(p + 10),
      dosDate: buf.readUInt16LE(p + 12),
      crc: buf.readUInt32LE(p + 14),
      compressedSize: buf.readUInt32LE(p + 18),
      uncompressedSize: buf.readUInt32LE(p + 22),
      extraLen,
      name: buf.subarray(p + 30, p + 30 + nameLen).toString('utf8'),
    });
    p += 30 + nameLen + extraLen + buf.readUInt32LE(p + 18);
  }
  return out;
}

/** Walk the CENTRAL directory, by scanning for its signature. */
function centralHeaders(buf) {
  const out = [];
  let p = buf.indexOf(Buffer.from([0x50, 0x4b, 0x01, 0x02]));
  while (p !== -1 && buf.readUInt32LE(p) === SIG_CENTRAL) {
    const nameLen = buf.readUInt16LE(p + 28);
    const extraLen = buf.readUInt16LE(p + 30);
    const commentLen = buf.readUInt16LE(p + 32);
    out.push({
      versionMadeBy: buf.readUInt16LE(p + 4),
      versionNeeded: buf.readUInt16LE(p + 6),
      flags: buf.readUInt16LE(p + 8),
      method: buf.readUInt16LE(p + 10),
      dosTime: buf.readUInt16LE(p + 12),
      dosDate: buf.readUInt16LE(p + 14),
      crc: buf.readUInt32LE(p + 16),
      compressedSize: buf.readUInt32LE(p + 20),
      uncompressedSize: buf.readUInt32LE(p + 24),
      extraLen,
      name: buf.subarray(p + 46, p + 46 + nameLen).toString('utf8'),
    });
    p += 46 + nameLen + extraLen + commentLen;
    if (p + 4 > buf.length || buf.readUInt32LE(p) !== SIG_CENTRAL) break;
  }
  return out;
}

const ARCHIVE = () => encodeContainer(DOC, [['attachments/photo.txt', Buffer.from('not a photo\n')]]);

test('RAW: every local header carries SPEC.md 7.1 fields', () => {
  for (const h of localHeaders(ARCHIVE())) {
    assert.equal(h.method, 0, `${h.name}: stored`);
    assert.equal(h.flags, 0x0800, `${h.name}: UTF-8 name flag only, bit 3 clear`);
    assert.equal(h.versionNeeded, 10, `${h.name}: 2.0 is the DEFLATE minimum and is not needed`);
    assert.equal(h.extraLen, 0, `${h.name}: no extra field`);
    assert.notEqual(h.crc, 0, `${h.name}: CRC is present, not deferred`);
    assert.equal(h.compressedSize, h.uncompressedSize, `${h.name}: stored, so the sizes match`);
    assert.notEqual(h.uncompressedSize, 0, `${h.name}: size is present, not deferred`);
  }
});

test('RAW: the local and central headers agree with each other', () => {
  // The assertion nobody had. A reader resolving from the central directory
  // cannot make it, and it is the one that would have caught the reference
  // implementation writing zeroes into its local headers.
  const buf = ARCHIVE();
  const local = localHeaders(buf);
  const central = centralHeaders(buf);
  assert.equal(local.length, central.length, 'entry counts');
  assert.ok(local.length >= 2, 'the fixture must have more than one entry');
  for (let i = 0; i < local.length; i++) {
    for (const field of ['name', 'flags', 'method', 'dosTime', 'dosDate', 'crc',
      'compressedSize', 'uncompressedSize', 'versionNeeded']) {
      assert.equal(local[i][field], central[i][field],
        `entry ${i} (${local[i].name}): ${field} differs between local header and central directory`);
    }
  }
});

test('RAW: the archive can be walked forward-only', () => {
  // The property bit 3 destroys, asserted directly rather than inferred from
  // the flag. `localHeaders` throws if it cannot advance.
  const buf = ARCHIVE();
  assert.deepEqual(localHeaders(buf).map((h) => h.name),
    ['doc.vnx', 'attachments/photo.txt']);
});

test('RAW: the walker refuses an archive it cannot walk, so the guard above is real', () => {
  // Red proof for the helper. Set bit 3 in the first local header and the
  // forward-only walk must fail rather than silently returning fewer entries —
  // which is how this check would decay into asserting nothing.
  const buf = Buffer.from(ARCHIVE());
  buf.writeUInt16LE(0x0808, 6);
  assert.throws(() => localHeaders(buf), /cannot be walked forward-only/);
});

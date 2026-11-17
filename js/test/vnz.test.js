// The container (SPEC.md 7), and the ZIP mechanics under it.

import test from 'node:test';
import assert from 'node:assert/strict';
import zlib from 'node:zlib';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
// The REPOSITORY, always. `CORPUS` may be an extract in a temp directory when a
// run is measured against `git archive HEAD`.
const REPO = path.resolve(HERE, '..', '..');
const CORPUS = process.env.VNX_CORPUS ? path.resolve(process.env.VNX_CORPUS) : path.join(REPO, 'corpus');

import { decodeContainer, encodeContainer, writeContainer } from '../src/vnz.js';
import { readEntries, readEntryData, writeArchive, crc32, METHOD_STORE, METHOD_DEFLATE } from '../src/zip.js';
import { VnxRefusal } from '../src/errors.js';

const DOC = '{\n  "vnx": "0.11.0",\n  "points": [\n    {\n      "at": [-3.6712, 57.118]\n    }\n  ]\n}\n';

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
  assert.equal(c.document.points.length, 1);
  assert.deepEqual(c.diagnostics, []);
});

test('stored entries are read as well as deflated ones', () => {
  const buf = writeArchive([{ name: 'doc.vnx', data: Buffer.from(DOC), method: METHOD_STORE }]);
  assert.equal(decodeContainer(buf).document.vnx, '0.11.0');
});

test('the root document is matched on the FULL entry name, not the base name', () => {
  // A nested `doc.vnx` must not win over the first `.vnx` at the root. This is
  // the defect SPEC.md 7 warns about and the corpus does not cover.
  const buf = writeArchive([
    { name: 'export.vnx', data: Buffer.from(DOC) },
    { name: 'nested/doc.vnx', data: Buffer.from('{"vnx": "0.11.0", "points": []}') },
  ]);
  const c = decodeContainer(buf);
  assert.equal(c.documentEntry, 'export.vnx');
  assert.equal(c.document.points.length, 1);
});

test('the root document match is case-insensitive', () => {
  const buf = writeArchive([
    { name: 'aaa.vnx', data: Buffer.from('{"vnx": "0.11.0", "points": []}') },
    { name: 'DOC.VNX', data: Buffer.from(DOC) },
  ]);
  assert.equal(decodeContainer(buf).documentEntry, 'DOC.VNX');
});

test('the fallback takes the FIRST .vnx entry in archive order', () => {
  const buf = writeArchive([
    { name: 'a.vnx', data: Buffer.from(DOC) },
    { name: 'b.vnx', data: Buffer.from('{"vnx": "0.11.0", "points": []}') },
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
  const buf = mutateMethod(
    writeArchive([
      { name: 'doc.vnx', data: Buffer.from(DOC) },
      { name: 'photo.bin', data: Buffer.from('x'.repeat(64)) },
    ]),
    'photo.bin',
    12,
  );
  const c = decodeContainer(buf);
  assert.equal(c.document.points.length, 1);
  assert.deepEqual([...c.attachments.keys()], []);
  assert.equal(c.diagnostics[0].code, 'attachment-unreadable');
  // SPEC.md 6.3's separate pointer space: an archive entry is not in the document.
  assert.equal(c.diagnostics[0].path, '#/entries/photo.bin');
});

test('a corrupt entry fails its CRC rather than yielding wrong bytes', () => {
  const buf = writeArchive([{ name: 'doc.vnx', data: Buffer.from(DOC), method: METHOD_STORE }]);
  const i = buf.indexOf(Buffer.from('"0.11.0"'));
  buf[i + 1] = '9'.charCodeAt(0);
  assert.equal(refusal(() => decodeContainer(buf)).cause_code, 'zip-malformed');
});

test('a ZIP64 archive is readable', () => {
  const buf = zip64Archive('doc.vnx', Buffer.from(DOC));
  const entries = readEntries(buf);
  assert.equal(entries.length, 1);
  assert.equal(readEntryData(buf, entries[0]).toString(), DOC);
  assert.equal(decodeContainer(buf).document.vnx, '0.11.0');
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
  assert.throws(() => encodeContainer(DOC, [['weird\\name.jpg', Buffer.from('x')]]), /backslash/);
});

test('the writer DEFLATES every entry, and the reader still reads stored ones', () => {
  // SPEC.md 7.1 lets a writer choose stored or deflate per entry, and nothing
  // else about the archive is constrained. This one deflates, so a container is
  // smaller than what it holds.
  const buf = encodeContainer(DOC, [['attachments/photo.txt', Buffer.from('not a photo\n')]]);
  for (const e of readEntries(buf)) {
    assert.equal(e.method, METHOD_DEFLATE, e.name);
    assert.equal(e.flags, 0x0800, `${e.name}: UTF-8 flag only, no data descriptor`);
  }
  // and it still round-trips
  assert.equal(decodeContainer(buf).attachments.get('attachments/photo.txt').toString(), 'not a photo\n');
});

test('a container is smaller than the document it holds', () => {
  // The whole point of deflating. Measured over a document large enough that a
  // ZIP header — about 100 bytes an entry — cannot decide the answer: a writer
  // switched back to stored fails this and satisfies every other rule in 7.1.
  const pips = [];
  for (let i = 0; i < 2000; i++) pips.push([-3.671235 + i / 1e6, 57.118123 + i / 1e6, 340.3]);
  const text = JSON.stringify({ vnx: '0.11.0', tracks: [{ segments: [{ pips }] }] });
  const buf = encodeContainer(text);
  assert.ok(
    buf.length < Buffer.byteLength(text, 'utf8') / 2,
    `the archive is ${buf.length} bytes and the document is ${Buffer.byteLength(text, 'utf8')}`,
  );
  assert.equal(decodeContainer(buf).document.tracks[0].segments[0].pips.length, 2000);
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
    assert.equal(h.method, 8, `${h.name}: deflated`);
    assert.equal(h.flags, 0x0800, `${h.name}: UTF-8 name flag only, bit 3 clear`);
    assert.equal(h.versionNeeded, 20, `${h.name}: 2.0 is the DEFLATE minimum`);
    assert.equal(h.extraLen, 0, `${h.name}: no extra field`);
    assert.notEqual(h.crc, 0, `${h.name}: CRC is present, not deferred`);
    assert.notEqual(h.compressedSize, 0, `${h.name}: size is present, not deferred`);
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

  // ORDER, named rather than left to fall out of the field loop below. The two
  // sequences can disagree independently of every field's value: a writer may
  // emit local headers in one order and list them in the central directory in
  // another, and both halves would then be individually correct. It was
  // covered here only because `name` happens to be in that list, so removing
  // `name` as a duplicate of the check elsewhere would have deleted this
  // property with nothing to say so.
  assert.deepEqual(
    local.map((h) => h.name),
    central.map((h) => h.name),
    'the local headers and the central directory list the entries in different orders',
  );

  for (let i = 0; i < local.length; i++) {
    for (const field of [
      'name',
      'flags',
      'method',
      'dosTime',
      'dosDate',
      'crc',
      'compressedSize',
      'uncompressedSize',
      'versionNeeded',
    ]) {
      assert.equal(
        local[i][field],
        central[i][field],
        `entry ${i} (${local[i].name}): ${field} differs between local header and central directory`,
      );
    }
  }
});

test('RAW: the archive can be walked forward-only', () => {
  // The property bit 3 destroys, asserted directly rather than inferred from
  // the flag. `localHeaders` throws if it cannot advance.
  const buf = ARCHIVE();
  assert.deepEqual(
    localHeaders(buf).map((h) => h.name),
    ['doc.vnx', 'attachments/photo.txt'],
  );
});

test('RAW: the walker refuses an archive it cannot walk, so the guard above is real', () => {
  // Red proof for the helper. Set bit 3 in the first local header and the
  // forward-only walk must fail rather than silently returning fewer entries —
  // which is how this check would decay into asserting nothing.
  const buf = Buffer.from(ARCHIVE());
  buf.writeUInt16LE(0x0808, 6);
  assert.throws(() => localHeaders(buf), /cannot be walked forward-only/);
});

test('TWO accept archives are the only coverage of two reader MUSTs, and must not be regenerated', () => {
  // SPEC.md 7 divides the two in as many words: "This section states what a
  // READER accepts. Section 7.1 states what a writer emits." These archives are
  // INPUT, so deflate and a deferred size are not defects in them — they are
  // what §7 line 26 and lines 35-36 require a reader to accept.
  //
  // Regenerating them to satisfy §7.1's writer table would delete both MUSTs
  // with every implementation still passing, because nothing left would ask the
  // question. This pins them so that act cannot be silent.
  //
  // TWO, not four. All four deflate archives carry the same headers, but a
  // REJECT case's expectation is a refusal naming a duplicate entry or an
  // escaping path, and both are decided before anything is decompressed — so a
  // reader with no deflate at all, or one trusting the local header's zeroed
  // sizes, still refuses them correctly. Measured by mutation: removing deflate
  // reddens these two and nothing else, and so does taking sizes from the local
  // header. An earlier version of this test said four, which over-claimed in the
  // same direction as the retracted reading it replaced.
  const coverBothMusts = ['accept/container-root-doc.vnz', 'accept/container-named-document.vnz'];
  const sameHeadersButNotCoverage = ['reject/container-duplicate-entries.vnz', 'reject/container-escapes.vnz'];
  for (const name of [...coverBothMusts, ...sameHeadersButNotCoverage]) {
    const buf = fs.readFileSync(path.join(CORPUS, name));
    assert.equal(buf.readUInt16LE(8), 8, `${name}: deflate`);
    assert.equal(buf.readUInt16LE(6) & 0x08, 0x08, `${name}: the data-descriptor bit`);
    assert.equal(buf.readUInt32LE(14), 0, `${name}: CRC deferred to the descriptor`);
  }
  // The coverage itself: these two must READ, which is what a reject case does
  // not require and therefore does not defend.
  for (const name of coverBothMusts) {
    assert.ok(decodeContainer(fs.readFileSync(path.join(CORPUS, name))).document.vnx, `${name} reads`);
  }
});

test('a corrupt deflate stream is a refusal, not a crash', () => {
  // `zlib` throws its own Error, which escaped every ZipError handler and left
  // the caller entirely — found by a mutation experiment, not by the corpus.
  // A library that crashes on a malformed archive gives a caller nothing to
  // catch and takes down whatever it was iterating.
  const good = readEntries(fs.readFileSync(path.join(CORPUS, 'accept/container-root-doc.vnz')));
  const buf = Buffer.from(fs.readFileSync(path.join(CORPUS, 'accept/container-root-doc.vnz')));
  const entry = good.find((e) => e.name === 'doc.vnx');
  const nameLen = buf.readUInt16LE(entry.localOffset + 26);
  const extraLen = buf.readUInt16LE(entry.localOffset + 28);
  const start = entry.localOffset + 30 + nameLen + extraLen;
  buf.fill(0xff, start, start + 20); // wreck the deflate stream
  const err = refusal(() => decodeContainer(buf));
  assert.equal(err.cause_code, 'zip-malformed');
  assert.match(err.message, /will not inflate|malformed archive/);
});

test('attachments are sorted by entry name, and the caller order is not preserved', () => {
  // ⚠ THIS PINS THIS LIBRARY'S OWN CHOICE, NOT A RULE. SPEC.md 7.1 requires the
  // root document first and leaves every other field of the archive to the
  // writer, so attachment ORDER is not conformance and no corpus case may check
  // it (ruled 2026-09-10). Sorting is kept for run-to-run determinism: caller
  // order is not a property of the content, so two exporters over one library —
  // one iterating a hash map, one a sorted list — would otherwise write
  // different archives from one input.
  //
  // Supplied out of order ON PURPOSE. Two names already in sorted order would
  // be decoration: the case only discriminates when the supplied order and the
  // sorted order differ.
  const buf = encodeContainer(DOC, [
    ['z/last.txt', Buffer.from('z')],
    ['a/first.txt', Buffer.from('a')],
  ]);
  assert.deepEqual(
    readEntries(buf).map((e) => e.name),
    ['doc.vnx', 'a/first.txt', 'z/last.txt'],
  );
});

test('the sort is by CODE POINT, which the default comparator is not', () => {
  // Sorting is this library's own choice, not a rule of SPEC.md 7.1, but the
  // comparator still has to be right. U+1F600 is above U+FFFD by code point and
  // below it by UTF-16 code unit, because its first unit is a high surrogate.
  const names = ['\u{1F600}.txt', '�.txt'];
  const buf = encodeContainer(
    DOC,
    names.map((n) => [n, Buffer.from('x')]),
  );
  const emitted = readEntries(buf)
    .slice(1)
    .map((e) => e.name);
  assert.deepEqual(emitted, ['�.txt', '\u{1F600}.txt']);
  // ... and the default sort would have produced the other order, so this
  // assertion discriminates rather than agreeing with both.
  assert.deepEqual([...names].sort(), ['\u{1F600}.txt', '�.txt']);
});

test('repackaging a container sorts too, so a read-modify-write cannot reorder', () => {
  const messy = writeArchive([
    { name: 'doc.vnx', data: Buffer.from(DOC) },
    { name: 'z/last.txt', data: Buffer.from('z') },
    { name: 'a/first.txt', data: Buffer.from('a') },
  ]);
  const container = decodeContainer(messy);
  assert.deepEqual(
    readEntries(writeContainer(container)).map((e) => e.name),
    ['doc.vnx', 'a/first.txt', 'z/last.txt'],
  );
});

test('RAW: the order check fires when the two sequences disagree', () => {
  // Red proof for the assertion above. Swapping two names in the CENTRAL
  // directory alone leaves every individual field value present somewhere in
  // each header, so a per-entry field comparison that did not include `name`
  // would see nothing wrong. The order check is what catches it.
  const buf = Buffer.from(
    encodeContainer(DOC, [
      ['aaaaaa.txt', Buffer.from('a')],
      ['bbbbbb.txt', Buffer.from('b')],
    ]),
  );
  const a = buf.indexOf(Buffer.from('aaaaaa.txt'), buf.indexOf(Buffer.from([0x50, 0x4b, 0x01, 0x02])));
  const b = buf.indexOf(Buffer.from('bbbbbb.txt'), buf.indexOf(Buffer.from([0x50, 0x4b, 0x01, 0x02])));
  assert.ok(a !== -1 && b !== -1, 'both names appear in the central directory');
  buf.write('bbbbbb.txt', a, 'utf8');
  buf.write('aaaaaa.txt', b, 'utf8');

  const local = localHeaders(buf).map((h) => h.name);
  const central = centralHeaders(buf).map((h) => h.name);
  assert.notDeepEqual(local, central, 'the mutation must actually disagree');
  assert.deepEqual(
    [...local].sort(),
    [...central].sort(),
    'and it must disagree ONLY in order, so a set comparison would miss it',
  );
});

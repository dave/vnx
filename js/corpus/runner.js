// The conformance corpus runner (SPEC.md 8).
//
// Passing every case is the definition of done. A file with no sidecar is a
// FAILURE, not a skip: the file everyone assumes is covered is otherwise
// exactly the one that is not.
//
// The accept tier is a JSON-TREE comparison between the tree parsed from the
// original file and the tree parsed from `encode(decode(file))` (SPEC.md 8.1).
// The bytes of a document are not constrained, so comparing them would require
// every conforming file to have been written by this library.

import fs from 'node:fs';
import path from 'node:path';

import { compareDocuments } from './tree.js';
// The harness's own ZIP reader, for opening a fixture archive and the archive a
// case just wrote. It is not the implementation under test: a write case must
// measure what was WRITTEN, not also how this library reads somebody else's
// archive, which the accept and diagnose tiers already cover.
import { readEntries, readEntryData } from '../src/zip.js';

const COLLECTIONS = ['folders', 'networks', 'routes', 'tracks', 'waypoints', 'areas', 'rulesets'];

export class Case {
  constructor(name) {
    this.name = name;
    this.failures = [];
  }
  check(ok, message) {
    if (!ok) this.failures.push(message);
    return ok;
  }
}

/**
 * Run the whole corpus against one implementation.
 *
 * The implementation is injected rather than imported so that a red-proof can
 * point this runner at a DELIBERATELY BROKEN copy of the library and check that
 * the tier that should catch the defect actually goes red. A guard nobody has
 * seen fail is not a guard.
 *
 * @param {string} CORPUS path to the corpus directory
 * @param {{decodeText: Function, decodeContainer: Function, encode: Function,
 *          encodeContainer: Function, roundToPlaces: Function}} api
 * @returns {Case[]}
 */
export function runCorpus(CORPUS, api) {
  const cases = [];
  for (const kind of ['accept', 'reject', 'diagnose']) {
    const dir = path.join(CORPUS, kind);
    for (const file of fs.readdirSync(dir).sort()) {
      if (!file.endsWith('.vnx') && !file.endsWith('.vnz')) continue;
      cases.push(runSidecarCase(kind, dir, file, api));
    }
  }
  cases.push(...runWriteCases(path.join(CORPUS, 'write'), api));
  cases.push(...runNumberCases(path.join(CORPUS, 'numbers'), api));
  return cases;
}

function runSidecarCase(kind, dir, file, api) {
  const { decodeText, decodeContainer, encode } = api;
  const c = new Case(`${kind}/${file}`);
  const full = path.join(dir, file);
  const sidecarPath = full.replace(/\.vn[xz]$/, '.expect.json');
  if (!fs.existsSync(sidecarPath)) {
    c.failures.push(`no sidecar at ${path.basename(sidecarPath)}: a file with no sidecar is a failure, not a skip`);
    return c;
  }
  const expect = JSON.parse(fs.readFileSync(sidecarPath, 'utf8'));
  c.check(expect.kind === kind, `sidecar kind is ${JSON.stringify(expect.kind)}, directory is ${kind}`);
  c.check(typeof expect.note === 'string' && expect.note.length > 0, 'sidecar carries no note');

  const bytes = fs.readFileSync(full);
  const isContainer = file.endsWith('.vnz');

  let result = null;
  let refusal = null;
  try {
    result = isContainer ? decodeContainer(bytes) : decodeText(bytes.toString('utf8'));
  } catch (err) {
    // Duck-typed on purpose: a red-proof runs a separate copy of the library,
    // whose VnxRefusal is a different class object.
    if (typeof err.cause_code === 'string') {
      refusal = err;
    } else {
      // Anything else is a CRASH, not a refusal, and it fails this case rather
      // than the run. Letting it propagate takes every other case down with it
      // and reports one defect as a total collapse — the same hole this runner
      // had in its format and write tiers, in a third place.
      c.failures.push(`threw ${err && err.name}: ${err && err.message}`);
      return c;
    }
  }

  if (kind === 'reject') {
    if (!c.check(refusal !== null, 'the document was read; it must be refused')) return c;
    c.check(
      refusal.message.includes(expect.refusal),
      `refusal ${JSON.stringify(refusal.message)} does not contain ${JSON.stringify(expect.refusal)}`,
    );
    return c;
  }

  if (!c.check(refusal === null, `refused: ${refusal && refusal.message}`)) return c;

  const expected = expect.diagnostics ?? [];
  matchDiagnostics(c, result.diagnostics, expected);
  checkCounts(c, result.document, expect.counts);
  checkPositions(c, result.positions, expect.positions ?? []);
  checkTracks(c, result.document, expect.tracks);
  if (isContainer) checkAttachments(c, result.attachments, expect.attachments);

  if (kind === 'accept' && !isContainer) {
    // SPEC.md 8.1: decode, ENCODE, and compare the two JSON TREES, with the
    // emit-boundary rules applied to both sides.
    //
    // ⚠ AGAINST THE ORIGINAL FILE, never against another model. A reader that
    // discarded every data record, ruleset, style and `notes` produces a model
    // equal to itself and would pass a model-versus-model tier green.
    let encoded = null;
    try {
      encoded = encode(result.document);
    } catch (err) {
      c.failures.push(`threw rather than encoding: ${err && err.message}`);
    }
    if (encoded !== null) {
      const diff = compareDocuments(encoded, bytes.toString('utf8'), { emitBoundary: true });
      c.check(diff === '', `the re-encoded document is not the one the file carries: ${diff}`);
    }
  }
  return c;
}

/**
 * Diagnostics are compared as a SET (SPEC.md 6.3), by a perfect matching: each
 * expected diagnostic pairs with one DISTINCT reported diagnostic, so a runner
 * cannot let one reported diagnostic satisfy two expectations.
 *
 * The comparison is `path` + `code`. The English is NOT compared and is not
 * read: the sidecar names that field `message_advisory` precisely so that a
 * runner reaching for `message` finds nothing rather than finding a claim.
 */
export function matchDiagnostics(c, reported, expected) {
  for (const e of expected) {
    if (typeof e.code !== 'string' || e.code === '') {
      c.failures.push(`expected diagnostic at ${JSON.stringify(e.path)} carries no code; SPEC.md 6.3 requires one on a pinned diagnostic`);
    }
    if (Object.prototype.hasOwnProperty.call(e, 'message')) {
      c.failures.push(`expected diagnostic at ${JSON.stringify(e.path)} carries a "message" field; the English is "message_advisory" and is not compared`);
    }
  }
  const fits = (r, e) => r.path === e.path && r.code === e.code;
  const pairedTo = new Array(reported.length).fill(-1);
  const augment = (ei, seen) => {
    for (let ri = 0; ri < reported.length; ri++) {
      if (seen.has(ri) || !fits(reported[ri], expected[ei])) continue;
      seen.add(ri);
      if (pairedTo[ri] === -1 || augment(pairedTo[ri], seen)) {
        pairedTo[ri] = ei;
        return true;
      }
    }
    return false;
  };
  const unmatched = [];
  for (let ei = 0; ei < expected.length; ei++) {
    if (!augment(ei, new Set())) unmatched.push(expected[ei]);
  }
  for (const e of unmatched) {
    c.failures.push(`no diagnostic matched {path: ${JSON.stringify(e.path)}, code: ${JSON.stringify(e.code)}}`);
  }
  reported.forEach((r, ri) => {
    if (pairedTo[ri] === -1) {
      c.failures.push(`unexpected diagnostic {path: ${JSON.stringify(r.path)}, code: ${JSON.stringify(r.code)}, message: ${JSON.stringify(r.message)}}`);
    }
  });
}

/**
 * `counts` is exact per collection. A collection the sidecar does not name is
 * asserted EMPTY, so a decoder that invented items somewhere unnamed cannot
 * hide behind an unmentioned key.
 */
function checkCounts(c, doc, counts) {
  if (counts === undefined) return;
  for (const key of COLLECTIONS) {
    const got = (doc[key] ?? []).length;
    const want = counts[key] ?? 0;
    c.check(got === want, `counts.${key}: expected ${want}, read ${got}`);
  }
  for (const key of Object.keys(counts)) {
    c.check(COLLECTIONS.includes(key), `counts names ${JSON.stringify(key)}, which is not a collection`);
  }
}

/**
 * `positions` pins which number reached which channel, resolved against the
 * DECODED MODEL. Resolving against the raw JSON would compare the file to
 * itself and could not see a decoder that swapped two channels.
 */
function checkPositions(c, positions, expected) {
  for (const e of expected) {
    const p = positions.get(e.at);
    if (!c.check(p !== undefined, `no position at ${JSON.stringify(e.at)}`)) continue;
    c.check(p.lon === e.lon, `${e.at}: lon is ${p.lon}, expected ${e.lon}`);
    c.check(p.lat === e.lat, `${e.at}: lat is ${p.lat}, expected ${e.lat}`);
  }
}

function checkTracks(c, doc, expected) {
  if (expected === undefined) return;
  for (const e of expected) {
    const track = (doc.tracks ?? []).find((t) => t.name === e.name);
    if (!c.check(track !== undefined, `no track named ${JSON.stringify(e.name)}`)) continue;
    if (Object.prototype.hasOwnProperty.call(e, 'moving_time_ms')) {
      const got = track.movingTimeMs();
      c.check(got === e.moving_time_ms,
        `${e.name}: moving time is ${got === null ? 'unknown' : got}, expected ${e.moving_time_ms === null ? 'unknown' : e.moving_time_ms}`);
    }
  }
}

function checkAttachments(c, attachments, expected) {
  if (expected === undefined) return;
  const got = [...attachments.keys()].sort();
  const want = [...expected].sort();
  c.check(JSON.stringify(got) === JSON.stringify(want),
    `attachments are ${JSON.stringify(got)}, expected ${JSON.stringify(want)}`);
}

/**
 * The NUMBER tier: `numbers.tsv` fixes the result of rounding a given double to
 * 6 and to 1 decimal places.
 *
 * ⚠ IT TESTS THE ROUNDING OPERATION, NOT A SPELLING. The table's `render`
 * column is not read: how a number is spelled is the writer's own business
 * (SPEC.md 4.2). What is compared is the rounded VALUE, BY BIT PATTERN —
 * comparing it as text would put a rendering rule back in through the side door
 * and would then pass or fail on how this runtime spells a double.
 *
 * A row is a value rather than a document, so there is no sidecar: the answer
 * columns ARE the expectation. It is the only tier that reaches past the emit
 * boundary at value level, and it separates two independent choices the document
 * tiers cannot — the tie direction, and whether rounding is done on the exact
 * decimal expansion or by scaling first.
 *
 * The double arrives as 16 hexadecimal digits of its IEEE 754 encoding. A
 * decimal literal would beg the question the table exists to answer.
 */
function runNumberCases(dir, api) {
  const { roundToPlaces } = api;
  if (!fs.existsSync(dir)) return [];
  const c = new Case('numbers/numbers.tsv');
  const file = path.join(dir, 'numbers.tsv');
  if (!fs.existsSync(file)) {
    c.failures.push('no numbers.tsv in the numbers tier');
    return [c];
  }
  if (typeof roundToPlaces !== 'function') {
    c.failures.push('the implementation exposes no rounder');
    return [c];
  }

  const view = new DataView(new ArrayBuffer(8));
  const toDouble = (hex) => {
    view.setBigUint64(0, BigInt(`0x${hex}`));
    return view.getFloat64(0);
  };
  const bitsOf = (n) => {
    view.setFloat64(0, n);
    return view.getBigUint64(0).toString(16).padStart(16, '0');
  };

  let rows = 0;
  let failedRows = 0;
  const kinds = new Set();
  const sample = [];
  const COLUMNS = ['round6', 'round1'];
  const lines = fs.readFileSync(file, 'utf8').split('\n');
  for (const [index, line] of lines.entries()) {
    if (line === '' || line.startsWith('#')) continue;
    const columns = line.split('\t');
    if (columns.length !== 5) {
      c.failures.push(`line ${index + 1}: expected five tab-separated columns, found ${columns.length}`);
      continue;
    }
    const [bits, kind] = columns;
    const want = [Number(columns[3]), Number(columns[4])];
    rows++;
    kinds.add(kind);
    const v = toDouble(bits);
    const got = [roundToPlaces(v, 6), roundToPlaces(v, 1)];
    // ⚠ BY BIT PATTERN. -0 and 0 are the same value and DIFFERENT bit patterns;
    // SPEC.md 4.1 has rounding normalise the sign, so the table says 0 and a
    // mismatch here is real rather than a spelling.
    if (got.every((g, i) => bitsOf(g) === bitsOf(want[i]))) continue;
    failedRows++;
    // A bounded sample: five thousand identical failures are one defect, and a
    // report nobody reads is not a report. The count above it is the scale.
    if (sample.length < 12) {
      const wrong = COLUMNS.filter((_, i) => bitsOf(got[i]) !== bitsOf(want[i])).join(' and ');
      sample.push(`line ${index + 1}  ${bits} (${kind})  ${wrong}: expected [${want}], produced [${got}]`);
    }
  }
  if (failedRows > 0) {
    c.failures.push(`${failedRows} of ${rows} rows disagree; first ${sample.length}:`, ...sample);
  }
  // A table read as zero rows would satisfy every comparison in it.
  c.check(rows > 100, `only ${rows} rows read from numbers.tsv`);
  c.check(kinds.size >= 5, `only ${kinds.size} kinds present, so the sample is narrower than the table claims`);
  return [c];
}

/**
 * The WRITE tier (SPEC.md 4.1): decode the input, ENCODE, and compare the two
 * documents as JSON trees. This is the only tier that reaches the emit-boundary
 * rules, because every other tier encodes what it decoded — which applies
 * rounding to values that already satisfy it and shortening to positions already
 * short.
 *
 * ⚠ AND SO IT NORMALISES ALMOST NOTHING. The accept tier rounds both sides and
 * drops a trailing `null` from both, because it compares a document against a
 * re-encoding of itself. Doing that here would normalise away the two rules
 * under test. The one normalisation left is the one that is not about the emit
 * boundary at all: SPEC.md 3.1's absent-versus-empty.
 */
function runWriteCases(dir, api) {
  const { decodeText, encode } = api;
  const cases = [];
  if (!fs.existsSync(dir)) return cases;
  if (typeof encode !== 'function') {
    const c = new Case('write/');
    c.failures.push('the implementation exposes no encoder');
    return [c];
  }
  const seen = new Set();
  for (const file of fs.readdirSync(dir).sort()) {
    if (!file.endsWith('.in.vnx')) continue;
    const base = file.slice(0, -'.in.vnx'.length);
    seen.add(`${base}.in.vnx`);
    seen.add(`${base}.written.vnx`);
    const c = new Case(`write/${base}`);
    const manifestPath = path.join(dir, `${base}.attachments.json`);
    if (fs.existsSync(manifestPath)) {
      // A case carrying an attachment manifest writes a CONTAINER, so its
      // expected output is an archive rather than a document — see
      // `corpus/write/README.md`.
      seen.add(`${base}.written.vnz`);
      cases.push(runWriteContainerCase(dir, base, api));
      continue;
    }
    const writtenPath = path.join(dir, `${base}.written.vnx`);
    const sidecarPath = path.join(dir, `${base}.expect.json`);
    if (!fs.existsSync(writtenPath)) {
      c.failures.push(`no ${base}.written.vnx beside the input`);
      cases.push(c);
      continue;
    }
    if (!fs.existsSync(sidecarPath)) {
      c.failures.push(`no ${base}.expect.json: a file with no sidecar is a failure, not a skip`);
      cases.push(c);
      continue;
    }
    const expect = JSON.parse(fs.readFileSync(sidecarPath, 'utf8'));
    c.check(expect.kind === 'write', `sidecar kind is ${JSON.stringify(expect.kind)}, directory is write`);
    c.check(typeof expect.note === 'string' && expect.note.length > 0, 'sidecar carries no note');

    const input = fs.readFileSync(path.join(dir, file), 'utf8');
    const want = fs.readFileSync(writtenPath, 'utf8');
    // Without this a writer that echoed its input would pass every case here.
    c.check(compareDocuments(input, want, {}) !== '',
      'the input is already the written form, so this case cannot fail');

    // A refusal fails THIS case. Letting it propagate would take down every
    // other case in the run, which is a runner that reports one defect as a
    // total collapse.
    try {
      const decoded = decodeText(input);
      c.check(decoded.diagnostics.length === 0,
        `the input must read cleanly; got ${JSON.stringify(decoded.diagnostics.map((d) => d.code))}`);
      const diff = compareDocuments(encode(decoded.document), want, {});
      c.check(diff === '', `the written document is not the one the case expects: ${diff}`);
      // Rounding and shortening are both idempotent, so the written form must be
      // a fixed point. A case that fails this is a defect in the fixture.
      const again = compareDocuments(encode(decodeText(want).document), want, {});
      c.check(again === '', `the expected output is not a fixed point of the writer: ${again}`);
    } catch (err) {
      c.failures.push(`threw rather than writing: ${err && err.message}`);
    }
    cases.push(c);
  }
  for (const file of fs.readdirSync(dir)) {
    if ((!file.endsWith('.vnx') && !file.endsWith('.vnz')) || seen.has(file)) continue;
    const c = new Case(`write/${file}`);
    c.failures.push('not part of an .in.vnx / .written.vnx pair');
    cases.push(c);
  }
  return cases;
}

/**
 * A write case whose output is a CONTAINER (`corpus/write/README.md`). It is the
 * only case proving attachments survive a round trip through an archive.
 *
 * ⚠ THE ARCHIVE'S BYTES ARE NOT COMPARED. SPEC.md 7.1 constrains five rules and
 * leaves every other field of the archive to the writer, so two conforming
 * writers are not expected to agree byte for byte — and DEFLATE has no canonical
 * output, so two zlib builds would not agree either. What is compared is what
 * 7.1 does constrain: the root document's tree, the entry names, the attachment
 * bodies, and the document being first.
 *
 * The attachments come from a manifest rather than from real files beside the
 * sidecar: a name holding U+1F600 is subject to the checkout filesystem's
 * Unicode normalisation and case folding, so a checked-out file could arrive
 * under a name that is not the one the case means and the fixture would test the
 * filesystem instead of the writer.
 *
 * The attachments are fed REVERSED. A writer's output must not depend on the
 * order it was handed, and feeding them in manifest order cannot see one that
 * does.
 */
function runWriteContainerCase(dir, base, api) {
  const { decodeText, decodeContainer, encode, encodeContainer } = api;
  const c = new Case(`write/${base}`);
  if (typeof encodeContainer !== 'function') {
    c.failures.push('the implementation exposes no container WRITE path; SPEC.md 7 requires one');
    return c;
  }
  const writtenPath = path.join(dir, `${base}.written.vnz`);
  const sidecarPath = path.join(dir, `${base}.expect.json`);
  if (!fs.existsSync(writtenPath)) {
    c.failures.push(`the manifest promises ${base}.written.vnz and there is none`);
    return c;
  }
  if (!fs.existsSync(sidecarPath)) {
    c.failures.push(`no ${base}.expect.json: a file with no sidecar is a failure, not a skip`);
    return c;
  }
  const expect = JSON.parse(fs.readFileSync(sidecarPath, 'utf8'));
  c.check(expect.kind === 'write', `sidecar kind is ${JSON.stringify(expect.kind)}, directory is write`);
  c.check(typeof expect.note === 'string' && expect.note.length > 0, 'sidecar carries no note');
  c.check(expect.root === 'doc.vnx', `this runner writes the document as doc.vnx, and the sidecar names ${JSON.stringify(expect.root)}`);

  const input = fs.readFileSync(path.join(dir, `${base}.in.vnx`), 'utf8');
  const wantBytes = fs.readFileSync(writtenPath);
  const manifest = JSON.parse(fs.readFileSync(path.join(dir, `${base}.attachments.json`), 'utf8'));
  const listed = manifest.attachments.map((a) => [a.name, Buffer.from(a.body, 'utf8')]);

  // The expected archive is FIXTURE, and is opened with the harness's own ZIP
  // reader rather than the implementation's, so that a case measures what the
  // implementation WROTE and not also how it reads someone else's archive.
  let wanted;
  try {
    wanted = archiveEntries(wantBytes);
  } catch (err) {
    c.failures.push(`the expected archive will not open: ${err && err.message}`);
    return c;
  }
  if (!c.check(wanted.length > 0, 'the expected archive holds no entries')) return c;
  c.check(wanted[0].name === manifest.root,
    `the expected archive's first entry is ${JSON.stringify(wanted[0].name)}, and SPEC.md 7.1 requires the root document ${JSON.stringify(manifest.root)}`);

  try {
    const decoded = decodeText(input);
    c.check(decoded.diagnostics.length === 0,
      `the input must read cleanly; got ${JSON.stringify(decoded.diagnostics.map((d) => d.code))}`);
    const packed = encodeContainer(encode(decoded.document), [...listed].reverse());
    checkArchive(c, packed, wanted, manifest);
    // This case does NOT check that the archive is smaller than the document it
    // holds. Its document is 109 bytes and a ZIP header is about 100, so the
    // overhead decides the answer whatever the writer does. The compression
    // guard lives in test/vnz.test.js, over a document large enough to see it.

    // And the archive reads back through the implementation's own reader.
    const back = decodeContainer(packed);
    c.check(back.diagnostics.length === 0,
      `the archive this library wrote must read cleanly; got ${JSON.stringify(back.diagnostics.map((d) => d.code))}`);
    c.check(back.attachments.size === listed.length,
      `the archive reads back with ${back.attachments.size} attachments and the manifest lists ${listed.length}`);

    // The written form is a fixed point, reached the way every other write case
    // reaches it: read the expected output back and write it again.
    const again = encodeContainer(encode(back.document), back.attachments);
    checkArchive(c, again, wanted, manifest);
  } catch (err) {
    c.failures.push(`threw rather than writing: ${err && err.message}`);
  }
  return c;
}

/**
 * What SPEC.md 7.1 constrains about an archive: the root document first and
 * under the name the manifest gives, the same entry names, and the same
 * attachment bodies. An attachment is bytes and is compared as bytes — nothing
 * in this format interprets one.
 */
function checkArchive(c, gotBytes, wanted, manifest) {
  let got;
  try {
    got = archiveEntries(gotBytes);
  } catch (err) {
    c.failures.push(`the archive written here will not open: ${err && err.message}`);
    return;
  }
  if (!c.check(got.length === wanted.length,
    `the archive holds ${got.length} entries, want ${wanted.length}`)) return;
  c.check(got[0].name === manifest.root,
    `the first entry is ${JSON.stringify(got[0].name)}; SPEC.md 7.1 requires the root document first`);
  const names = got.map((e) => e.name);
  const wantNames = wanted.map((e) => e.name);
  c.check(JSON.stringify(names) === JSON.stringify(wantNames),
    `the entry names are ${JSON.stringify(names)}, want ${JSON.stringify(wantNames)}`);

  const diff = compareDocuments(got[0].body.toString('utf8'), wanted[0].body.toString('utf8'), {});
  c.check(diff === '', `the archive's root document is not the one the case expects: ${diff}`);

  const bodies = new Map(manifest.attachments.map((a) => [a.name, a.body]));
  for (const e of got.slice(1)) {
    const body = bodies.get(e.name);
    if (body === undefined) {
      c.failures.push(`the archive carries ${JSON.stringify(e.name)}, which the manifest does not name`);
      continue;
    }
    c.check(e.body.toString('utf8') === body,
      `${e.name}: body is ${JSON.stringify(e.body.toString('utf8'))}, want ${JSON.stringify(body)}`);
  }
}

/** Every file entry of an archive, in archive order, read as bytes. */
function archiveEntries(buf) {
  return readEntries(buf)
    .filter((e) => !e.name.endsWith('/'))
    .map((e) => ({ name: e.name, body: readEntryData(buf, e) }));
}

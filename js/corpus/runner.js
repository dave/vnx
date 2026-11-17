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

const COLLECTIONS = ['folders', 'networks', 'plans', 'tracks', 'points', 'areas'];

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
 *          encodeContainer: Function}} api
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
  if (isContainer) checkAttachments(c, result.attachments, expect.attachments, expect.attachment_contents);

  if (kind === 'accept' && !isContainer) {
    // SPEC.md 8.1: decode, ENCODE, and compare the two JSON TREES, with the
    // emit-boundary rule applied to both sides.
    //
    // ⚠ AGAINST THE ORIGINAL FILE, never against another model. A reader that
    // discarded every data record, application key, style and `notes` produces a model
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
      c.failures.push(
        `expected diagnostic at ${JSON.stringify(e.path)} carries no code; SPEC.md 6.3 requires one on a pinned diagnostic`,
      );
    }
    if (Object.prototype.hasOwnProperty.call(e, 'message')) {
      c.failures.push(
        `expected diagnostic at ${JSON.stringify(e.path)} carries a "message" field; the English is "message_advisory" and is not compared`,
      );
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
      c.failures.push(
        `unexpected diagnostic {path: ${JSON.stringify(r.path)}, code: ${JSON.stringify(r.code)}, message: ${JSON.stringify(r.message)}}`,
      );
    }
  });
}

/**
 * `counts` is exact, and covers both the six top-level collections and the
 * NESTED containers inside them, addressed by the same JSON Pointer vocabulary
 * `positions` uses. A bare name is a top-level collection; a name starting with
 * `/` is a container inside one.
 *
 * A collection the sidecar does not name is asserted EMPTY, so a decoder that
 * invented items somewhere unnamed cannot hide behind an unmentioned key — and,
 * in the other direction, a sidecar key this runner cannot produce is an error
 * rather than a silence. Both halves matter: this check used to iterate the keys
 * it produced ITSELF and read `counts[key] ?? 0`, so every nested key a sidecar
 * carried was skipped without a word. A runner that quietly drops an
 * expectation reports conformance it never measured.
 */
function checkCounts(c, doc, counts) {
  if (counts === undefined) return;
  const got = {};
  for (const key of COLLECTIONS) got[key] = (doc[key] ?? []).length;
  for (const [ptr, n] of containerCountsByPointer(doc)) got[ptr] = n;

  for (const [key, want] of Object.entries(counts)) {
    if (!Object.prototype.hasOwnProperty.call(got, key)) {
      c.failures.push(`counts names ${JSON.stringify(key)}, which is not a collection the document carries`);
      continue;
    }
    c.check(got[key] === want, `${key}: got ${got[key]}, want ${want}`);
  }
  // A sidecar MUST name every collection its document carries non-empty. An
  // omitted collection used to mean "unchecked", which let a decoder invent or
  // drop items in it with nothing to say so. Naming a zero stays legal — a
  // diagnose case whose only highlight is dropped pins `highlights: 0` on purpose.
  for (const [key, n] of Object.entries(got)) {
    if (n === 0) continue;
    if (!Object.prototype.hasOwnProperty.call(counts, key)) {
      c.failures.push(
        `the document carries ${n} ${key} and the sidecar does not name that collection; an unnamed collection is unchecked, not empty`,
      );
    }
  }
}

/**
 * The number of children of every NESTED container in the decoded document,
 * keyed by a JSON Pointer in the wire's own key names.
 *
 * ⚠ IT EXISTS BECAUSE `counts` COUNTED ONLY THE SIX TOP-LEVEL ARRAYS. A reader
 * that dropped every plan's arcs, or truncated an arc's pips to the first two,
 * or lost every highlight in a network, changed nothing a sidecar could see: the
 * document still carried one plan, one network, one track. `positions` does
 * not close it either — it pins the VALUE at a few named indices and says
 * nothing about how many there are. Measured on the pre-D2 tree: a reader that
 * dropped a dangling link AND THE REST OF ITS HIGHLIGHT passed the whole corpus
 * green, because the diagnose tier is exempt from the round trip (SPEC.md 8.1)
 * and the diagnose tier is exactly where a reader is supposed to drop things.
 *
 * It is a producer rather than a pointer resolver, for the reason
 * `positionsByPointer` is in the Go reference: one traversal answers every
 * pointer, so a case can pin a container deep inside a network without the
 * harness growing a pointer parser.
 *
 * ⚠ ARC AND NODE CONTAINERS ARE KEYED BY THEIR OWN KEY, NOT BY AN INDEX.
 * `/networks/0/arcs/arc-1/pips` — SPEC.md 3.3 makes `arcs` and `nodes` objects,
 * and an object has no order, so an index there would be this implementation's
 * map iteration order exported as a conformance requirement.
 *
 * @param {import('../src/model.js').Document} doc
 * @returns {Map<string, number>}
 */
function containerCountsByPointer(doc) {
  const out = new Map();
  const graph = (base, owner) => {
    out.set(`${base}/nodes`, (owner.nodes ?? new Map()).size);
    out.set(`${base}/arcs`, (owner.arcs ?? new Map()).size);
    for (const [key, arc] of owner.arcs ?? new Map()) {
      out.set(`${base}/arcs/${key}/pips`, (arc.pips ?? []).length);
    }
  };
  (doc.networks ?? []).forEach((n, i) => {
    graph(`/networks/${i}`, n);
    out.set(`/networks/${i}/highlights`, (n.highlights ?? []).length);
    (n.highlights ?? []).forEach((highlight, j) => {
      out.set(`/networks/${i}/highlights/${j}/walk`, (highlight.walk ?? []).length);
    });
    // ⚠ UNCONDITIONALLY, EMPTY OR NOT, like every other container in this walk.
    // These two once carried a non-empty guard, and it bought nothing: a zero
    // never triggers the completeness rule either way. What it COST was that
    // `/networks/0/points: 0` resolved in a runner emitting unconditionally and
    // was a hard "not a collection the document carries" in one that did not —
    // a sidecar entry legal in one conforming implementation and a failure in
    // another, with nothing in the specification to separate them. Ruled
    // 2026-09-14 across all six runners.
    out.set(`/networks/${i}/points`, (n.points ?? []).length);
    out.set(`/networks/${i}/areas`, (n.areas ?? []).length);
    (n.areas ?? []).forEach((a, j) => {
      out.set(`/networks/${i}/areas/${j}/polygon`, (a.polygon ?? []).length);
    });
  });
  (doc.plans ?? []).forEach((r, i) => {
    graph(`/plans/${i}`, r);
    out.set(`/plans/${i}/walk`, (r.walk ?? []).length);
  });
  (doc.tracks ?? []).forEach((t, i) => {
    out.set(`/tracks/${i}/segments`, (t.segments ?? []).length);
    (t.segments ?? []).forEach((s, j) => {
      out.set(`/tracks/${i}/segments/${j}/pips`, (s.pips ?? []).length);
    });
  });
  (doc.areas ?? []).forEach((a, i) => {
    out.set(`/areas/${i}/polygon`, (a.polygon ?? []).length);
  });
  return out;
}

/**
 * `positions` pins which number reached which channel, resolved against the
 * DECODED MODEL. Resolving against the raw JSON would compare the file to itself
 * and could not see a decoder that swapped two channels.
 *
 * ⚠ THE COMPARISON IS BY BIT PATTERN, NOT BY VALUE, for two reasons.
 *
 * A value comparison cannot see a one-ulp move between the parse and the model,
 * and when it does fail it prints two numbers that look identical, so it reads
 * as a broken test rather than a broken decoder. Both spellings go in the
 * message below for that reason.
 *
 * And `-0 === 0` is true in JavaScript, so a value comparison cannot tell them
 * apart. That matters here more than it looks: `JSON.stringify(-0)` is `"0"` and
 * the accept tier compares trees by value, so NO DOCUMENT TIER can see a
 * negative zero. This bit-pattern check is the corpus's only instrument for one,
 * now that the rounding tier that used to carry a table of literals is gone.
 *
 * ⚠ THE NEGATIVE-ZERO HALF IS LATENT, MEASURED 2026-09-10. Every coordinate any
 * sidecar pins is between -3.74 and -2.99, so no `positions` expectation reaches
 * a zero and a decoder that turned one negative passes this check. The machinery
 * is right and the corpus does not exercise it. A fixture carrying a zero
 * coordinate would close the gap; see test/redproof.test.js, which asserts the
 * absence so it cannot be mistaken for coverage.
 *
 * ⚠ WHAT IT DOES NOT CATCH: a PARSER one ulp out. The sidecar's expected value is
 * read by the same JSON parser as the file, so a parser that lost a bit would
 * lose it on both sides and this check would agree with itself. That is a
 * different test's job — `accept/full-precision-coordinate.vnx` is where the
 * parser is pinned. This one is about anything that moves a value BETWEEN the
 * parse and the model.
 */
function checkPositions(c, positions, expected) {
  for (const e of expected) {
    const p = positions.get(e.at);
    if (!c.check(p !== undefined, `no position at ${JSON.stringify(e.at)}`)) continue;
    for (const channel of ['lon', 'lat']) {
      const got = p[channel];
      const want = e[channel];
      c.check(
        sameBits(got, want),
        `${e.at}: ${channel} is ${got} (${bitsOf(got)}), expected ${want} (${bitsOf(want)})`,
      );
    }
  }
}

const BITS_VIEW = new DataView(new ArrayBuffer(8));

/** The IEEE 754 encoding of a double, as 16 hexadecimal digits. */
function bitsOf(n) {
  if (typeof n !== 'number') return 'not a number';
  BITS_VIEW.setFloat64(0, n);
  return BITS_VIEW.getBigUint64(0).toString(16).padStart(16, '0');
}

function sameBits(a, b) {
  return typeof a === 'number' && typeof b === 'number' && bitsOf(a) === bitsOf(b);
}

function checkTracks(c, doc, expected) {
  if (expected === undefined) return;
  for (const e of expected) {
    const track = (doc.tracks ?? []).find((t) => t.name === e.name);
    if (!c.check(track !== undefined, `no track named ${JSON.stringify(e.name)}`)) continue;
    if (Object.prototype.hasOwnProperty.call(e, 'moving_time_ms')) {
      const got = track.movingTimeMs();
      c.check(
        got === e.moving_time_ms,
        `${e.name}: moving time is ${got === null ? 'unknown' : got}, expected ${e.moving_time_ms === null ? 'unknown' : e.moving_time_ms}`,
      );
    }
  }
}

/**
 * `attachments` is the entry NAMES, `attachment_contents` each entry's bytes as
 * UTF-8 text (corpus/README.md, "The sidecar").
 *
 * Both are compared without regard to order: SPEC.md 7 does not order a
 * reader's attachment list, so a reader that sorts them is as conforming as one
 * that keeps archive order.
 *
 * ⚠ `attachment_contents` is keyed by the entries the reader is expected to
 * RETURN, which is not always every entry in the archive. The archive of
 * diagnose/container-unusable-method.vnz carries an entry compressed by a
 * method SPEC.md 7 lets a reader drop with a diagnostic, and its sidecar lists
 * only the readable one. So this walks the SIDECAR's keys and requires each to
 * be present; the names check above is what catches an extra attachment the
 * reader should have dropped.
 *
 * @param {Map<string, Buffer>} attachments entry name to bytes, as the library returns them
 * @param {string[]|undefined} expected entry names
 * @param {Record<string, string>|undefined} contents entry name to UTF-8 text
 */
function checkAttachments(c, attachments, expected, contents) {
  if (expected !== undefined) {
    const got = [...attachments.keys()].sort();
    const want = [...expected].sort();
    c.check(
      JSON.stringify(got) === JSON.stringify(want),
      `attachments are ${JSON.stringify(got)}, expected ${JSON.stringify(want)}`,
    );
  }
  if (contents === undefined) return;
  for (const [name, want] of Object.entries(contents)) {
    const body = attachments.get(name);
    if (!c.check(body !== undefined, `attachment ${JSON.stringify(name)} has expected content but was not returned`))
      continue;
    // Compared as TEXT, because that is how the sidecar can carry it. An
    // attachment is bytes; a fixture whose body is not valid UTF-8 cannot be
    // pinned this way and must not be given an `attachment_contents` entry.
    const got = Buffer.from(body).toString('utf8');
    c.check(
      got === want,
      `attachment ${JSON.stringify(name)} is ${JSON.stringify(got)}, expected ${JSON.stringify(want)}`,
    );
  }
}

/**
 * The WRITE tier (SPEC.md 4.1): decode the input, ENCODE, and compare the two
 * documents as JSON trees. This is the only tier that reaches the emit-boundary
 * rule, because every other tier encodes what it decoded — which applies
 * shortening to positions that are already short.
 *
 * ⚠ AND SO IT NORMALISES ALMOST NOTHING. The accept tier drops a trailing
 * `null` from both sides, because it compares a document against a re-encoding
 * of itself. Doing that here would normalise away the rule under test. The one
 * normalisation left is the one that is not about the emit boundary at all:
 * SPEC.md 3.1's absent-versus-empty.
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
    c.check(
      compareDocuments(input, want, {}) !== '',
      'the input is already the written form, so this case cannot fail',
    );

    // A refusal fails THIS case. Letting it propagate would take down every
    // other case in the run, which is a runner that reports one defect as a
    // total collapse.
    try {
      const decoded = decodeText(input);
      // A write sidecar's `diagnostics` is what the input's decode must
      // report, compared as the diagnose tier compares it; absent, nothing.
      matchDiagnostics(c, decoded.diagnostics, expect.diagnostics ?? []);
      const diff = compareDocuments(encode(decoded.document), want, {});
      c.check(diff === '', `the written document is not the one the case expects: ${diff}`);
      // Shortening is idempotent, so the written form must be
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
 * The attachments are fed REVERSED. That is now a check on the writer's CONTENT
 * rather than on its order: a writer that dropped or duplicated an attachment
 * when handed one it did not expect is still caught, and the order it lands in
 * is not this case's business.
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
  c.check(
    expect.root === 'doc.vnx',
    `this runner writes the document as doc.vnx, and the sidecar names ${JSON.stringify(expect.root)}`,
  );

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
  c.check(
    wanted[0].name === manifest.root,
    `the expected archive's first entry is ${JSON.stringify(wanted[0].name)}, and SPEC.md 7.1 requires the root document ${JSON.stringify(manifest.root)}`,
  );

  try {
    const decoded = decodeText(input);
    matchDiagnostics(c, decoded.diagnostics, expect.diagnostics ?? []);
    const packed = encodeContainer(encode(decoded.document), [...listed].reverse());
    checkArchive(c, packed, wanted, manifest);
    // This case does NOT check that the archive is smaller than the document it
    // holds. Its document is 109 bytes and a ZIP header is about 100, so the
    // overhead decides the answer whatever the writer does. The compression
    // guard lives in test/vnz.test.js, over a document large enough to see it.

    // And the archive reads back through the implementation's own reader.
    const back = decodeContainer(packed);
    c.check(
      back.diagnostics.length === 0,
      `the archive this library wrote must read cleanly; got ${JSON.stringify(back.diagnostics.map((d) => d.code))}`,
    );
    c.check(
      back.attachments.size === listed.length,
      `the archive reads back with ${back.attachments.size} attachments and the manifest lists ${listed.length}`,
    );

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
 * What SPEC.md 7.1 constrains about an archive, and only that: the root document
 * first and under the name the manifest gives, the same set of entry names, and
 * the same attachment bodies. An attachment is bytes and is compared as bytes —
 * nothing in this format interprets one.
 *
 * ⚠ ATTACHMENT ORDER IS NOT COMPARED. §7.1 has five rules and then says every
 * other field of the archive is the writer's own business, so the attachments
 * are an order-insensitive COLLECTION here. Ruled 2026-09-10. A library may sort
 * its own output for determinism — this one does — but that is its choice, not
 * conformance, and a case that checked it would enforce a rule the specification
 * explicitly disclaims.
 */
function checkArchive(c, gotBytes, wanted, manifest) {
  let got;
  try {
    got = archiveEntries(gotBytes);
  } catch (err) {
    c.failures.push(`the archive written here will not open: ${err && err.message}`);
    return;
  }
  if (!c.check(got.length === wanted.length, `the archive holds ${got.length} entries, want ${wanted.length}`)) return;
  c.check(
    got[0].name === manifest.root,
    `the first entry is ${JSON.stringify(got[0].name)}; SPEC.md 7.1 requires the root document first`,
  );
  const names = got
    .slice(1)
    .map((e) => e.name)
    .sort();
  const wantNames = wanted
    .slice(1)
    .map((e) => e.name)
    .sort();
  c.check(
    JSON.stringify(names) === JSON.stringify(wantNames),
    `the attachment names are ${JSON.stringify(names)}, want ${JSON.stringify(wantNames)}`,
  );

  const diff = compareDocuments(got[0].body.toString('utf8'), wanted[0].body.toString('utf8'), {});
  c.check(diff === '', `the archive's root document is not the one the case expects: ${diff}`);

  const bodies = new Map(manifest.attachments.map((a) => [a.name, a.body]));
  for (const e of got.slice(1)) {
    const body = bodies.get(e.name);
    if (body === undefined) {
      c.failures.push(`the archive carries ${JSON.stringify(e.name)}, which the manifest does not name`);
      continue;
    }
    c.check(
      e.body.toString('utf8') === body,
      `${e.name}: body is ${JSON.stringify(e.body.toString('utf8'))}, want ${JSON.stringify(body)}`,
    );
  }
}

/** Every file entry of an archive, in archive order, read as bytes. */
function archiveEntries(buf) {
  return readEntries(buf)
    .filter((e) => !e.name.endsWith('/'))
    .map((e) => ({ name: e.name, body: readEntryData(buf, e) }));
}

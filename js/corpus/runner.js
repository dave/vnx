// The conformance corpus runner (SPEC.md 8).
//
// Passing every case is the definition of done. A file with no sidecar is a
// FAILURE, not a skip: the file everyone assumes is covered is otherwise
// exactly the one that is not.
//
// The runner never uses the writer. The accept tier is
// `re-render(decode(file)) == file`, and a writer rounds.

import fs from 'node:fs';
import path from 'node:path';

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
 * @param {{decodeText: Function, decodeContainer: Function, rerender: Function}} api
 * @returns {Case[]}
 */
export function runCorpus(CORPUS, api) {
  const { decodeText, decodeContainer, rerender } = api;
  const cases = [];
  for (const kind of ['accept', 'reject', 'diagnose']) {
    const dir = path.join(CORPUS, kind);
    for (const file of fs.readdirSync(dir).sort()) {
      if (!file.endsWith('.vnx') && !file.endsWith('.vnz')) continue;
      cases.push(runSidecarCase(kind, dir, file, api));
    }
  }
  cases.push(...runFormatCases(path.join(CORPUS, 'format'), api));
  cases.push(...runWriteCases(path.join(CORPUS, 'write'), api));
  return cases;
}

function runSidecarCase(kind, dir, file, api) {
  const { decodeText, decodeContainer, rerender } = api;
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
    if (typeof err.cause_code !== 'string') throw err;
    refusal = err;
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
    // SPEC.md 8.1: decode, RE-RENDER, and require the original bytes back.
    const again = rerender(result.document);
    c.check(again === bytes.toString('utf8'), firstDifference(bytes.toString('utf8'), again));
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

function runFormatCases(dir, api) {
  const { decodeText, rerender } = api;
  const cases = [];
  for (const file of fs.readdirSync(dir).sort()) {
    if (!file.endsWith('.in.vnx')) continue;
    const base = file.slice(0, -'.in.vnx'.length);
    const c = new Case(`format/${base}`);
    const inputPath = path.join(dir, file);
    const canonicalPath = path.join(dir, `${base}.canonical.vnx`);
    if (!fs.existsSync(canonicalPath)) {
      c.failures.push(`no ${base}.canonical.vnx beside the input`);
      cases.push(c);
      continue;
    }
    const input = fs.readFileSync(inputPath, 'utf8');
    const want = fs.readFileSync(canonicalPath, 'utf8');
    // SPEC.md 8.1: a runner MUST assert that the input is not already
    // canonical. Without it a formatter that echoed its input would pass.
    c.check(input !== want, 'the input is already canonical, so this case cannot fail');
    try {
      const got = rerender(decodeText(input).document);
      c.check(got === want, firstDifference(want, got));
      // The canonical file must be a fixed point of the formatter.
      const again = rerender(decodeText(want).document);
      c.check(again === want, `re-rendering the canonical file changed it: ${firstDifference(want, again)}`);
    } catch (err) {
      c.failures.push(`threw rather than formatting: ${err && err.message}`);
    }
    cases.push(c);
  }
  // Every .vnx in format/ belongs to a pair.
  for (const file of fs.readdirSync(dir)) {
    if (!file.endsWith('.vnx')) continue;
    if (file.endsWith('.in.vnx') || file.endsWith('.canonical.vnx')) continue;
    const c = new Case(`format/${file}`);
    c.failures.push('not part of an .in.vnx / .canonical.vnx pair');
    cases.push(c);
  }
  return cases;
}

/**
 * The WRITE tier (SPEC.md 4.5.4): decode the input, call the WRITER, and require
 * the expected bytes. This is the only tier that applies the emit-boundary
 * rules, because every other tier re-renders.
 */
function runWriteCases(dir, api) {
  const { decodeText, write } = api;
  const cases = [];
  if (!fs.existsSync(dir)) return cases;
  if (typeof write !== 'function') {
    const c = new Case('write/');
    c.failures.push('the implementation exposes no WRITE path; SPEC.md 4.5.4 requires both');
    return [c];
  }
  const seen = new Set();
  for (const file of fs.readdirSync(dir).sort()) {
    if (!file.endsWith('.in.vnx')) continue;
    const base = file.slice(0, -'.in.vnx'.length);
    seen.add(`${base}.in.vnx`);
    seen.add(`${base}.written.vnx`);
    const c = new Case(`write/${base}`);
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
    c.check(input !== want, 'the input is already the written form, so this case cannot fail');

    // A refusal fails THIS case. Letting it propagate would take down every
    // other case in the run, which is a runner that reports one defect as a
    // total collapse.
    try {
      const decoded = decodeText(input);
      c.check(decoded.diagnostics.length === 0,
        `the input must read cleanly; got ${JSON.stringify(decoded.diagnostics.map((d) => d.code))}`);
      const got = write(decoded.document);
      c.check(got === want, firstDifference(want, got));
      // Rounding and shortening are both idempotent, so the written form must be
      // a fixed point. A case that fails this is a defect in the fixture.
      const again = write(decodeText(want).document);
      c.check(again === want, `writing the written form changed it: ${firstDifference(want, again)}`);
    } catch (err) {
      c.failures.push(`threw rather than writing: ${err && err.message}`);
    }
    cases.push(c);
  }
  for (const file of fs.readdirSync(dir)) {
    if (!file.endsWith('.vnx') || seen.has(file)) continue;
    const c = new Case(`write/${file}`);
    c.failures.push('not part of an .in.vnx / .written.vnx pair');
    cases.push(c);
  }
  return cases;
}

function firstDifference(want, got) {
  if (want === got) return 'identical';
  const a = want.split('\n');
  const b = got.split('\n');
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    if (a[i] !== b[i]) {
      return `line ${i + 1}: expected ${JSON.stringify(a[i] ?? '<end of file>')}, produced ${JSON.stringify(b[i] ?? '<end of file>')}`;
    }
  }
  return 'differ only in trailing bytes';
}

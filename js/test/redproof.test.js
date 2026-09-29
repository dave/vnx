// Red-proof for the corpus runner.
//
// The corpus went green on the first run, which is exactly when a runner that
// measures nothing is indistinguishable from one that works. Each case below
// breaks the library in one specific way and asserts that the tier which is
// supposed to catch that defect goes red — and that the tiers which are not
// supposed to catch it stay green, so a mutation is not merely setting the
// whole suite on fire.
//
// Every mutation asserts its own search string was FOUND. A sed that matches
// nothing is a mutation that never happened, and would make this whole file a
// test of nothing.

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { runCorpus } from '../corpus/runner.js';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const SRC = path.resolve(HERE, '..', 'src');
// Overridable so the same red-proofs can be run against a `git archive HEAD`
// extract as a second, separately labelled measurement: `corpus/` is a shared
// tree and is routinely mid-edit.
const CORPUS = process.env.VNX_CORPUS
  ? path.resolve(process.env.VNX_CORPUS)
  : path.resolve(HERE, '..', '..', 'corpus');

/**
 * Assert a fixture is the shape a red-proof depends on, so that running against
 * an older corpus fails by naming the reason instead of by a bare inequality.
 */
function requireFixture(relative, predicate, why) {
  const raw = JSON.parse(fs.readFileSync(path.join(CORPUS, relative), 'utf8'));
  assert.ok(predicate(raw), `${relative} in ${CORPUS} is not the fixture this red-proof needs: ${why}`);
}

let counter = 0;

/** A chain over a fixed three-node line, a --ab-- b --bc-- c. */
function chainDoc(walk) {
  return JSON.stringify({
    vnx: '0.2.0',
    networks: [{
      nodes: { a: [0, 0], b: [1, 0], c: [2, 0] },
      arcs: { ab: { from: 'a', to: 'b', pips: [] }, bc: { from: 'b', to: 'c', pips: [] } },
      chains: [{ walk }],
    }],
  });
}

/** A mutated copy of the library, for a check the corpus cannot make. */
async function mutatedApi(edits) {
  const dir = mutatedCopy(edits);
  return import(pathToFileURL(path.join(dir, 'index.js')).href);
}

/**
 * @param {Array<{file: string, from: string, to: string}>} edits
 * @returns {Promise<import('../corpus/runner.js').Case[]>}
 */
async function runMutated(edits) {
  const dir = mutatedCopy(edits);
  const url = pathToFileURL(path.join(dir, 'index.js')).href;
  const api = await import(url);
  return runCorpus(CORPUS, {
    decodeText: api.decodeText,
    decodeContainer: api.decodeContainer,
    encodeContainer: api.encodeContainer,
    encode: api.encode,
  });
}

function mutatedCopy(edits) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), `vnx-redproof-${counter++}-`));
  for (const name of fs.readdirSync(SRC)) {
    fs.copyFileSync(path.join(SRC, name), path.join(dir, name));
  }
  for (const edit of edits) {
    const p = path.join(dir, edit.file);
    const before = fs.readFileSync(p, 'utf8');
    assert.ok(
      before.includes(edit.from),
      `mutation target not found in ${edit.file}: ${JSON.stringify(edit.from)} — the red-proof would prove nothing`,
    );
    fs.writeFileSync(p, before.replace(edit.from, edit.to));
  }
  return dir;
}

function allFailing(cases) {
  return cases.filter((c) => c.failures.length > 0).map((c) => c.name).sort();
}

/**
 * The corpus is a shared tree and is routinely mid-edit, so a case can be red
 * for reasons that have nothing to do with the mutation under test. Every
 * assertion here is therefore about the DELTA a mutation causes, measured
 * against a baseline taken from the unmutated library.
 *
 * Asserting the absolute set instead makes one unrelated corpus defect cascade
 * into every red-proof at once — measured 2026-09-09, when a fixture and its
 * sidecar disagreed about a track's name and 29 of these went red without a
 * single mutation being at fault. A red-proof measures what its mutation
 * changed; anything else is not its business.
 */
let baseline = null;

async function baselineFailures() {
  if (baseline === null) baseline = allFailing(await runMutated([]));
  return baseline;
}

async function failingDelta(cases) {
  const before = new Set(await baselineFailures());
  return allFailing(cases).filter((name) => !before.has(name));
}

test('the corpus baseline is recorded, and any pre-existing failure is named', async () => {
  const cases = await runMutated([]);
  assert.ok(cases.length >= 30, `only ${cases.length} cases ran`);
  const red = cases.filter((c) => c.failures.length > 0);
  if (red.length > 0) {
    // Not an assertion failure: the corpus being red is not this library's
    // defect to report, and failing here would hide every mutation result
    // behind it. It is printed so it cannot pass unnoticed either.
    console.error(
      `\n  NOTE: ${red.length} corpus case(s) are red before any mutation. ` +
      'Every red-proof below measures its delta against that baseline.\n' +
      red.map((c) => `    ${c.name}: ${c.failures.join('; ')}`).join('\n') + '\n',
    );
  }
  await baselineFailures();
});

test('transposing lon and lat, invisibly to the tree check, is caught by positions', async () => {
  // The transposition is undone on the way out, so the re-encoded tree still
  // equals the file's. Only `positions`, resolved against the model, can see
  // it — which is the claim corpus/README.md makes for that field.
  const cases = await runMutated([
    {
      file: 'decode.js',
      from: '  return new Position(v.slice());',
      to: '  const s = v.slice(); [s[0], s[1]] = [s[1], s[0]];\n  return new Position(s);',
    },
    {
      file: 'emit.js',
      from: '  const out = position.channels.slice();',
      to: '  const out = position.channels.slice(); [out[0], out[1]] = [out[1], out[0]];',
    },
  ]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('accept/lonlat-transposition.vnx'), red.join(', '));
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
});

test('dropping a TRAILING null in the model is permitted; dropping an INTERIOR one is not', async () => {
  // SPEC.md 8.1 permits exactly two normalisations, and one of them is this:
  // trailing `null` channels are dropped on both sides of the accept-tier
  // comparison. `[lon, lat, null]` and `[lon, lat]` state the same thing
  // (SPEC.md 4.1), so a reader that keeps neither spelling is still conforming
  // and the tier must NOT redden. Recorded, because this used to be caught and
  // the rule is what changed.
  const trailing = await runMutated([{
    file: 'decode.js',
    from: '  return new Position(v.slice());',
    to: '  const s = v.slice();\n  while (s.length > 2 && s[s.length - 1] === null) s.pop();\n  return new Position(s);',
  }]);
  assert.deepEqual(await failingDelta(trailing), []);

  // An INTERIOR absence is a different matter: dropping it moves the time into
  // the elevation channel, and no normalisation touches it. Only
  // `write/emit-boundary` carries one — measured, every `null` in accept/ is a
  // trailing channel — so that one case is the whole of the corpus's coverage
  // for the rule, in both tiers at once.
  const interior = await runMutated([{
    file: 'decode.js',
    from: '  return new Position(v.slice());',
    to: '  return new Position(v.filter((channel) => channel !== null));',
  }]);
  assert.deepEqual(await failingDelta(interior), ['write/emit-boundary']);
});

// ⚠ THE FOUR SPOILED READERS. Each of the next four cases is a reader that
// silently drops one thing it read: every data record, every application key,
// every style, every `notes`. That is the entire extensibility mechanism, all styling
// and all user prose. Every one of them produces a MODEL EQUAL TO ITSELF and
// passes a model-versus-model accept tier green — measured in Python, re-proved
// in Go. Comparing against the tree parsed from the ORIGINAL FILE is what
// catches them, and these four cases are the proof that it does.
test('a reader that discards data records is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  const a = arr(\'data\', path, kv, false, ctx);\n  if (a === undefined) return undefined;',
    to: '  const a = arr(\'data\', path, kv, false, ctx);\n  if (a !== undefined) return undefined;\n  if (a === undefined) return undefined;',
  }]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
  assert.ok(red.includes('accept/unknown-data-on-unexpected-hosts.vnx'), red.join(', '));
});

test('a reader that discards application keys is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  doc.appKeys = readAppKeys(kv);',
    to: '  doc.appKeys = [];',
  }]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
  assert.ok(red.includes('accept/application-keys.vnx'), red.join(', '));
});

// SPEC.md 3.9 exempts an application key from absent-equals-empty. A writer
// that skipped a key whose value is null or empty would drop content, and
// accept/application-keys.vnx carries all three to catch it.
test('a writer that drops null and empty application keys is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'emit.js',
    from: '    o[key] = value;',
    to: '    if (value !== null && !(typeof value === \'object\' && Object.keys(value).length === 0)) o[key] = value;',
  }]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('accept/application-keys.vnx'), red.join(', '));
});

// The rule is TOP-LEVEL ONLY. A reader that recognised a dotted key on every
// object would stop reporting one on an item.
test('a reader that recognises dotted keys below the top level is caught', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '    if (required.includes(k) || optional.includes(k) || alsoKnown(k)) out[k] = obj[k];',
    to: '    if (required.includes(k) || optional.includes(k) || k.includes(\'.\')) out[k] = obj[k];',
  }]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('diagnose/dotted-key-on-item.vnx'), red.join(', '));
});

test('a reader that discards every style is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: "  const s = objField('style', path, kv, false, ctx);\n  if (s === undefined) return undefined;",
    to: "  const s = objField('style', path, kv, false, ctx);\n  return undefined;\n  if (s === undefined) return undefined;",
  }]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
});

test('a reader that discards every notes is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: "const str = (k, path, kv, req, ctx) => field(k, path, kv, 'a string', isString, req, ctx);",
    to: "const str = (k, path, kv, req, ctx) => (k === 'notes' ? undefined : field(k, path, kv, 'a string', isString, req, ctx));",
  }]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
});

test('answering 0 for an unknown moving time is caught', async () => {
  // Two edits, one defect: "sum whatever segments you can, and report the sum".
  // Both are needed because either guard alone still returns unknown — the
  // in-loop return catches a partial track and the `measured === 0` guard
  // catches an entirely untimed one.
  const cases = await runMutated([
    {
      file: 'model.js',
      from: "      if (typeof first !== 'number' || typeof last !== 'number') return null;",
      to: "      if (typeof first !== 'number' || typeof last !== 'number') continue;",
    },
    {
      file: 'model.js',
      from: '    return measured === 0 ? null : total;',
      to: '    return total;',
    },
  ]);
  // Every case whose sidecar carries a `tracks` expectation with an unknown
  // moving time. The list grows as fixtures adopt that field, which is why the
  // assertion is exact: a case that stops appearing here has stopped asserting
  // the rule, and that is worth a failure rather than a shrug.
  assert.deepEqual(await failingDelta(cases), [
    'accept/absent-elevation-and-time.vnx',
    'accept/track-partly-timed.vnx',
    'accept/track-without-times.vnx',
  ]);
  // diagnose/position-unusable.vnx also carries a `tracks` expectation, and is
  // deliberately NOT here: its moving time is a KNOWN 64000, so a reader that
  // answers zero for an unknown one still gets it right. It pins the container
  // rule, not this one.
});

test('rounding in the reader is caught by the full-precision case', async () => {
  // SPEC.md 4.1: a reader MUST NOT round. The rule survived the removal of the
  // WRITER's rounding, and this is the proof that the corpus still reaches it —
  // the accept tier no longer normalises precision on either side, so a reader
  // that rounds shows up in the tree comparison as well as in `positions`.
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  return new Position(v.slice());',
    to: '  const s = v.slice().map((c, i) => (typeof c === \'number\' && i < 2 ? Number(c.toFixed(6)) : c));\n  return new Position(s);',
  }]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('accept/full-precision-coordinate.vnx'), red.join(', '));
});

test('a value moved between the parse and the model is caught, bit-exactly', async () => {
  // `positions` compares BIT PATTERNS. A value comparison cannot see a one-ulp
  // move, and when it fails it prints two numbers that look identical, so it
  // reads as a broken test rather than a broken decoder.
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  return new Position(v.slice());',
    to: "  const s = v.slice();\n  if (typeof s[0] === 'number' && s[0] !== 0) s[0] = nextAfter(s[0]);\n  return new Position(s);\nfunction nextAfter(x) { const dv = new DataView(new ArrayBuffer(8)); dv.setFloat64(0, x); dv.setBigUint64(0, dv.getBigUint64(0) + 1n); return dv.getFloat64(0); }\n",
  }]);
  const red = await failingDelta(cases);
  assert.ok(red.includes('accept/absent-elevation-and-time.vnx'), red.join(', '));
  assert.ok(red.length >= 5, `only ${red.length} case(s) pin a position: ${red.join(', ')}`);
});

test('a coordinate turned into NEGATIVE zero is NOT caught; no fixture pins a zero', async () => {
  // ⚠ THIS RECORDS A GAP, and the gap is worth naming. `JSON.stringify(-0)` is
  // `"0"` and the accept tier compares trees by VALUE, so no document tier can
  // see a negative zero at all. The bit-exact `positions` check is now the only
  // instrument in the corpus that could — the table of literals that used to be
  // the other one went with the writer's rounding rule.
  //
  // It cannot be, yet. Measured 2026-09-10: every coordinate any sidecar pins
  // lies between -3.74 and -2.99, so no `positions` expectation reaches a zero
  // and this decoder passes every tier.
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  return new Position(v.slice());',
    to: '  const s = v.slice();\n  if (s[0] === 0) s[0] = -0;\n  if (s[1] === 0) s[1] = -0;\n  return new Position(s);',
  }]);
  assert.deepEqual(await failingDelta(cases), [],
    'a fixture now pins a zero coordinate; assert the failure instead of the gap');
});

test('accepting a version outside the reader\'s series is caught by the reject tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  if (major !== READER_MAJOR || minor !== READER_MINOR) {',
    to: '  if (false) {',
  }]);
  assert.deepEqual(await failingDelta(cases), [
    'reject/version-too-new.vnx',
    'reject/version-too-old.vnx',
  ]);
});

test('comparing versions as "newer than" rather than "not this series" is caught by one file only', async () => {
  // The historical defect, restored exactly: SPEC.md 2.2 makes each 0.x MINOR
  // its own series, and a "greater than" reader reads a 0.1 document under 0.2
  // rules. reject/version-too-new.vnx does NOT catch it, which is why the pair
  // was incomplete and five of the six implementations shared the shape.
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  if (major !== READER_MAJOR || minor !== READER_MINOR) {',
    to: '  if (major > READER_MAJOR || (major === READER_MAJOR && minor > READER_MINOR)) {',
  }]);
  assert.deepEqual(await failingDelta(cases), ['reject/version-too-old.vnx']);
});

test('refusing a newer PATCH is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  const [, majorText, minorText] = VERSION.exec(declared);',
    to: "  const [, majorText, minorText, patchText] = VERSION.exec(declared);\n  if (Number(patchText) > 0) throw new VnxRefusal('version-too-new', 'a newer reader is needed');",
  }]);
  // write/emit-boundary declares 0.2.99 too, so the same defect reddens both.
  // reject/version-too-old.vnx is red for a third reason: 0.1.9 has a non-zero
  // PATCH, so the mutant refuses it before reaching the series check and says
  // "a newer reader is needed" about a document below the reader. It is refused
  // for the wrong cause, and its sidecar's substring is what notices.
  assert.deepEqual(await failingDelta(cases), [
    'accept/version-newer-patch.vnx',
    'reject/version-too-old.vnx',
    'write/emit-boundary',
  ]);
});

test('coercing a two-component version is caught by the reject tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: 'const VERSION = /^(\\d+)\\.(\\d+)\\.(\\d+)$/;',
    to: 'const VERSION = /^(\\d+)\\.(\\d+)(?:\\.(\\d+))?$/;',
  }]);
  assert.deepEqual(await failingDelta(cases), ['reject/version-two-component.vnx']);
});

test('keeping the outer ring of a nested polygon is caught', async () => {
  const cases = await runMutated([
    {
      file: 'decode.js',
      from: "  const ring = arr('polygon', path, kv, true, ctx);",
      to: "  let ring = arr('polygon', path, kv, true, ctx);",
    },
    {
      file: 'decode.js',
      from: "    ctx.diag(p, 'a polygon carries one ring; this one has nested rings', 'polygon-nested-rings');\n    return DROP;",
      to: '    ring = ring[0];',
    },
  ]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/polygon-second-ring.vnx']);
});

test('refusing a timestamp with an offset, rather than diagnosing it, is caught', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: "    ctx.diag(`${path}/${esc(k)}`, `\"${k}\" must be UTC with a Z suffix`, 'timestamp-not-utc');\n    return undefined;",
    to: "    throw new VnxRefusal('timestamp-not-utc', 'must be UTC with a Z suffix');",
  }]);
  assert.deepEqual(await failingDelta(cases), [
    'diagnose/created-with-offset.vnx',
    'diagnose/timestamp-with-offset.vnx',
  ]);
});

test('skipping an unknown key silently is caught by the diagnose tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: "    else ctx.diag(`${path}/${esc(k)}`, `unknown key \"${k}\"`, 'unknown-key');",
    to: '    else { /* silently skipped */ }',
  }]);
  assert.deepEqual(await failingDelta(cases),
    ['diagnose/dotted-key-on-item.vnx', 'diagnose/unknown-key.vnx']);
});

test('a container matching its root document on the base name is caught', async () => {
  // The root-document match is on the FULL entry name. Matching on the base
  // name alone matches at any depth; letting the last match win compounds it.
  const cases = await runMutated([{
    file: 'vnz.js',
    from: "  let root = files.find((e) => e.name.toLowerCase() === 'doc.vnx');",
    to: "  let root = files.filter((e) => e.name.toLowerCase().split('/').pop() === 'doc.vnx').pop();",
  }]);
  // Nothing in the corpus has a nested doc.vnx, so this mutation is NOT caught.
  // Asserting that fact here keeps the gap visible instead of assumed.
  assert.deepEqual(await failingDelta(cases), [],
    'the corpus now discriminates the base-name match; delete this test and add a real one');
});

test('accepting a duplicate entry name is caught by the reject tier', async () => {
  const cases = await runMutated([{
    file: 'vnz.js',
    from: '    if (seen.has(key)) {',
    to: '    if (false) {',
  }]);
  assert.deepEqual(await failingDelta(cases), [
    'reject/container-duplicate-entries-case.vnz',
    'reject/container-duplicate-entries.vnz',
  ]);
});

test('treating `..` as a substring rather than a segment still passes; the corpus has no case for it', async () => {
  const cases = await runMutated([{
    file: 'vnz.js',
    from: "    if (segment === '..') return 'the path has a `..` segment';",
    to: "    if (segment.includes('..')) return 'the path has a `..` segment';",
  }]);
  // `a..b/photo.jpg` is explicitly legal (SPEC.md 7) and no corpus file holds
  // one, so the over-strict reader passes. Recorded, not papered over.
  assert.deepEqual(await failingDelta(cases), [],
    'the corpus now holds a legal `a..b` entry name; delete this test and add a real one');
});

test('accepting a path that escapes the archive is caught by the reject tier', async () => {
  const cases = await runMutated([{
    file: 'vnz.js',
    from: '    if (fault !== null) {',
    to: '    if (false) {',
  }]);
  assert.deepEqual(await failingDelta(cases), ['reject/container-escapes.vnz']);
});

test('attachment ORDER is not a rule, and attachment CONTENT is', async () => {
  // Ruled 2026-09-10. SPEC.md 7.1 has five rules — the root document named
  // doc.vnx and FIRST, the UTF-8 flag on every entry, names unique
  // case-insensitively, attachment paths relative and safe, every entry stored
  // or deflated — and then says every other field of the archive is the
  // writer's own business. Entry order past the root document is one of those.
  //
  // This library sorts its attachments anyway, for run-to-run determinism. That
  // is its own choice and NOT conformance, so reordering them must stay green:
  // a corpus case that reddened here would enforce a rule the specification
  // explicitly disclaims. The Kotlin seat found the old case doing exactly that.
  const reversed = await runMutated([{
    file: 'vnz.js', from: "  return [...files].sort((a, b) => Buffer.from(a.name, 'utf8').compare(Buffer.from(b.name, 'utf8')));", to: '  return [...files].reverse();',
  }]);
  assert.deepEqual(await failingDelta(reversed), [],
    'the corpus now enforces attachment order, which SPEC.md 7.1 disclaims');

  // The content is a different matter, and the case still measures it. Feeding
  // the attachments reversed is what makes a writer that drops one visible.
  const dropped = await runMutated([{
    file: 'vnz.js', from: "  return [...files].sort((a, b) => Buffer.from(a.name, 'utf8').compare(Buffer.from(b.name, 'utf8')));", to: '  return [...files].slice(1);',
  }]);
  assert.deepEqual(await failingDelta(dropped), ['write/container-cross-language']);

  const corrupted = await runMutated([{
    file: 'vnz.js', from: "  return [...files].sort((a, b) => Buffer.from(a.name, 'utf8').compare(Buffer.from(b.name, 'utf8')));",
    to: "  return [...files].map((f) => ({ ...f, data: Buffer.from('x') }));",
  }]);
  assert.deepEqual(await failingDelta(corrupted), ['write/container-cross-language']);
});

test('an attachment read back as the wrong bytes is caught by the read tiers', async () => {
  // The names alone do not measure this: before the sidecars pinned
  // `attachment_contents`, a reader that returned every entry under the right
  // name with the wrong bytes passed the whole corpus. Proved here so the
  // runner's content check cannot be quietly dropped again.
  const cases = await runMutated([{
    file: 'vnz.js',
    from: '      attachments.set(e.name, readEntryData(buf, e));',
    to: "      attachments.set(e.name, Buffer.from('not the bytes in the archive'));",
  }]);
  // write/container-cross-language is in the list for its own reason: it reads
  // back the archive it just wrote, so corrupting the reader reddens it too.
  assert.deepEqual(await failingDelta(cases), [
    'accept/container-over-declared-size.vnz',
    'accept/container-root-doc.vnz',
    'accept/container-sizes-disagree.vnz',
    'accept/container-zip64.vnz',
    // Corrupt-attachment is here because the mutation hands back bytes for an
    // entry the reader must have dropped, so its empty `attachments` is what
    // the case notices, not its content.
    'diagnose/container-corrupt-attachment.vnz',
    'diagnose/container-unusable-method.vnz',
    'write/container-cross-language',
  ]);
});

test('shortening a position is caught by the write tier', async () => {
  const cases = await runMutated([{
    file: 'emit.js',
    from: '  while (n > 2 && out[n - 1] === null) n--;',
    to: '  // no shortening',
  }]);
  assert.deepEqual(await failingDelta(cases), ['write/emit-boundary']);
});

test('shortening past an interior null is caught by the write tier', async () => {
  // Dropping an interior null would move the time into the elevation channel.
  const cases = await runMutated([{
    file: 'emit.js',
    from: '  while (n > 2 && out[n - 1] === null) n--;',
    to: '  return out.filter((channel) => channel !== null);',
  }]);
  assert.deepEqual(await failingDelta(cases), ['write/emit-boundary']);
});

test('accepting a backslash in an entry name is NOT caught', async () => {
  // Ruled 2026-09-09 and stricter than APPNOTE, but no archive in the corpus
  // carries a backslash, so a reader that permits one passes every tier while
  // accepting a name that traverses on Windows. test/vnz.test.js holds the
  // local guard. Recorded rather than assumed.
  const cases = await runMutated([{
    file: 'vnz.js',
    from: "  if (name.includes('\\\\')) return 'the name carries a backslash';",
    to: '  // backslash permitted',
  }]);
  assert.deepEqual(await failingDelta(cases), [],
    'the corpus now carries a backslash entry name; assert the failure instead');
});

test('summing only the timed segments of a partial track is caught', async () => {
  requireFixture(
    'accept/track-partly-timed.expect.json',
    (d) => d.tracks?.some((t) => t.moving_time_ms === null),
    'it needs a track with one timed segment and one untimed, expecting unknown',
  );
  // Was NOT caught until accept/track-partly-timed.vnx landed on 2026-09-09:
  // every track in the corpus was fully timed or fully untimed, so a partial sum
  // and an all-or-nothing unknown agreed on all of them. Three implementations
  // summed, including the reference one, with every tier green.
  const cases = await runMutated([{
    file: 'model.js',
    from: '      if (typeof first !== \'number\' || typeof last !== \'number\') return null;',
    to: '      if (typeof first !== \'number\' || typeof last !== \'number\') continue;',
  }]);
  assert.deepEqual(await failingDelta(cases), ['accept/track-partly-timed.vnx']);
});

test('a crash inside a tier fails its own case rather than the run', async () => {
  // The third place this runner had the same hole. A refusal was contained, and
  // the format and write tiers were fixed when the write tier landed, but a
  // non-refusal THROW from a sidecar tier still propagated out of runCorpus and
  // took every other case with it — 38 passing cases vanishing behind one
  // defect. Found by a mutation that made zlib throw, not by any fixture.
  const cases = await runMutated([{
    file: 'zip.js',
    from: '      out = zlib.inflateRawSync(raw);',
    to: '      out = zlib.inflateRawSync(raw.subarray(0, 3));',
  }]);
  const red = await failingDelta(cases);
  // Some cases go red, which is the mutation working — and crucially the RUN
  // completed, so the rest of the corpus was still measured.
  assert.ok(red.length > 0, 'the mutation must break something');
  assert.ok(cases.length >= 39, `the run must complete: only ${cases.length} cases reported`);
  // Container cases only: an archive entry is the one thing that inflates. The
  // write tier's container case is one of them, and is named rather than
  // matched on an extension because its case name has none.
  assert.ok(red.every((n) => n.endsWith('.vnz') || n === 'write/container-cross-language'),
    `only container cases: ${red.join(', ')}`);
});

// --- the 0.2 walk, arc and ring rules ----------------------------------------
//
// ⚠ ONE MUTATION PER CHECK. Each rule SPEC.md 6.2 names for a walk, an arc or a
// ring is switched off on its own, and the case that pins its code must go red.
// A code whose case stays green under its own mutation is pinned by nothing.

test('a reader that keeps a pass naming an absent arc is caught', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: "    if (arc === undefined) {\n      return drop('', 'pass-arc-absent',",
    to: "    if (arc === undefined && false) {\n      return drop('', 'pass-arc-absent',",
  }]);
  assert.deepEqual(await failingDelta(cases), [
    // Each of the other three reaches pass-arc-absent through a dropped arc:
    // the cascade SPEC.md 6.2 describes, one case per cause of the drop.
    'diagnose/arc-names-absent-node.vnx',
    'diagnose/pass-names-absent-arc.vnx',
    'diagnose/position-unusable.vnx',
    'diagnose/self-loop-arc.vnx',
  ]);
});

test('a reader with no rule for `from` and `to` is caught by walk-spelling', async () => {
  const cases = await runMutated([
    { file: 'walk.js', from: "    if (i > 0 && p.from !== undefined) return fault('from', 'walk-spelling',", to: "    if (false) return fault('from', 'walk-spelling'," },
  ]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/walk-spelling.vnx']);
});

test('a reader that does not require `to` on the last pass is caught', async () => {
  const edits = [
    { file: 'walk.js', from: '    if (wantTo && p.to === undefined) {', to: '    if (false) {' },
  ];
  const cases = await runMutated(edits);
  // diagnose/walk-last-pass-lacks-to.vnx is the corpus case for this rule, and the only one
  // the mutant reddens. The second half below proves the same mutant on a
  // document built here.
  assert.deepEqual(await failingDelta(cases), ['diagnose/walk-last-pass-lacks-to.vnx']);
  const { diagnostics } = (await mutatedApi(edits)).decodeText(chainDoc([{ arc: 'ab', from: 'a' }]));
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]), [], 'the mutant must stop reporting it');
  const { diagnostics: sound } = (await mutatedApi([])).decodeText(chainDoc([{ arc: 'ab', from: 'a' }]));
  assert.deepEqual(sound.map((d) => [d.path, d.code]), [['/networks/0/chains/0/walk/0', 'walk-spelling']]);
});

test('a reader that allows a `to` the rule forbids is caught', async () => {
  const edits = [
    { file: 'walk.js', from: '    if (!wantTo && p.to !== undefined) {', to: '    if (false) {' },
  ];
  const cases = await runMutated(edits);
  // diagnose/walk-to-forbidden.vnx is the corpus case for this rule, and the only one
  // the mutant reddens. The second half below proves the same mutant on a
  // document built here.
  assert.deepEqual(await failingDelta(cases), ['diagnose/walk-to-forbidden.vnx']);
  const { diagnostics } = (await mutatedApi(edits)).decodeText(chainDoc([{ arc: 'ab', from: 'a', to: 'b' }, { arc: 'bc', to: 'c' }]));
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]), [], 'the mutant must stop reporting it');
  const { diagnostics: sound } = (await mutatedApi([])).decodeText(chainDoc([{ arc: 'ab', from: 'a', to: 'b' }, { arc: 'bc', to: 'c' }]));
  assert.deepEqual(sound.map((d) => [d.path, d.code]), [['/networks/0/chains/0/walk/0/to', 'walk-spelling']]);
});

test('a reader with no continuity check is caught by walk-not-connected', async () => {
  // Walk continuity, valid-walk rule 4 of SPEC.md 3.3.3: each pass after the
  // first enters its arc at the node the walk is at.
  const cases = await runMutated([{ file: 'walk.js', from: '      if (!isEnd(this.at)) {', to: '      if (false) {' }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/walk-not-connected.vnx']);
});

test('a reader that accepts a `from` node off its arc is caught', async () => {
  const edits = [{
    file: 'walk.js',
    from: "      if (p.from.kind === 'node' && !isEnd(p.from.node)) {",
    to: '      if (false) {',
  }];
  const cases = await runMutated(edits);
  // diagnose/walk-from-not-an-end.vnx is the corpus case for this rule, and the only one
  // the mutant reddens. The second half below proves the same mutant on a
  // document built here.
  assert.deepEqual(await failingDelta(cases), ['diagnose/walk-from-not-an-end.vnx']);
  const { diagnostics } = (await mutatedApi(edits)).decodeText(chainDoc([{ arc: 'ab', from: 'c', to: 'b' }]));
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]), [], 'the mutant must stop reporting it');
  const { diagnostics: sound } = (await mutatedApi([])).decodeText(chainDoc([{ arc: 'ab', from: 'c', to: 'b' }]));
  assert.deepEqual(sound.map((d) => [d.path, d.code]), [['/networks/0/chains/0/walk/0/from', 'walk-not-connected']]);
});

test('a reader that accepts a `to` node off its arc is caught', async () => {
  const edits = [{
    file: 'walk.js',
    from: "    if (p.to !== undefined && p.to.kind === 'node' && !isEnd(p.to.node)) {",
    to: '    if (false) {',
  }];
  const cases = await runMutated(edits);
  // diagnose/walk-to-not-an-end.vnx is the corpus case for this rule, and the only one
  // the mutant reddens. The second half below proves the same mutant on a
  // document built here.
  assert.deepEqual(await failingDelta(cases), ['diagnose/walk-to-not-an-end.vnx']);
  const { diagnostics } = (await mutatedApi(edits)).decodeText(chainDoc([{ arc: 'ab', from: 'a', to: 'c' }]));
  assert.deepEqual(diagnostics.map((d) => [d.path, d.code]), [], 'the mutant must stop reporting it');
  const { diagnostics: sound } = (await mutatedApi([])).decodeText(chainDoc([{ arc: 'ab', from: 'a', to: 'c' }]));
  assert.deepEqual(sound.map((d) => [d.path, d.code]), [['/networks/0/chains/0/walk/0/to', 'walk-not-connected']]);
});

test('a reader that forgets a turn sends the walk back the way it came is caught', async () => {
  // Without the turn rule a pass with a turn and no `to` leaves by the far end
  // of its arc, and the next pass no longer joins.
  const cases = await runMutated([{
    file: 'walk.js',
    from: "    } else if (p.turn !== undefined) {\n      // The entry is a node here",
    to: "    } else if (false) {\n      // The entry is a node here",
  }]);
  assert.deepEqual(await failingDelta(cases), ['accept/walk-forms.vnx']);
});

test('a reader that accepts a pinned turn is caught', async () => {
  const cases = await runMutated([{
    file: 'walk.js',
    from: '    if (p.turn !== undefined && p.pinned === true) {',
    to: '    if (false) {',
  }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/pinned-turn.vnx']);
});

test('a reader that lets a route carry a position is caught', async () => {
  const cases = await runMutated([{ file: 'walk.js', from: '    if (this.route) {', to: '    if (false) {' }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/route-pin-or-position.vnx']);
});

test('a reader that keeps a self-loop arc is caught', async () => {
  const cases = await runMutated([{ file: 'decode.js', from: '  if (a.from === a.to) {', to: '  if (false) {' }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/self-loop-arc.vnx']);
});

test('a reader that keeps an arc naming an absent node is caught', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '    if (nodes !== undefined && nodes.has(a[which])) continue;',
    to: '    continue;',
  }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/arc-names-absent-node.vnx', 'diagnose/position-out-of-range.vnx']);
});

test('a reader that reports an arc pip time and keeps it is NOT caught by the corpus', async () => {
  const edits = [{ file: 'decode.js', from: '  a.pips = read.map(vertex);', to: '  a.pips = read;' }];
  const cases = await runMutated(edits);
  // ⚠ NO CORPUS CASE REACHES THIS, measured 2026-09-29. diagnose/arc-pip-time
  // .vnx is exempt from the round trip (SPEC.md 8.1), and `positions` compares
  // longitude and latitude only, so a time left in the model is invisible. The
  // encoder refuses such a pip, which is the second line; the check below is
  // the first.
  assert.deepEqual(await failingDelta(cases), [],
    'the corpus now reaches this rule; assert the failure instead of the gap');
  const text = JSON.stringify({
    vnx: '0.2.0',
    networks: [{ nodes: { a: [0, 0], b: [1, 0] }, arcs: { ab: { from: 'a', to: 'b', pips: [[0.5, 0, 3, 100]] } } }],
  });
  const pip = (api) => api.decodeText(text).document.networks[0].arcs.get('ab').pips[0].channels;
  assert.deepEqual(pip(await mutatedApi(edits)), [0.5, 0, 3, 100], 'the mutant must keep the time');
  assert.deepEqual(pip(await mutatedApi([])), [0.5, 0, 3]);
});

test('a reader that drops an arc pip time silently is caught', async () => {
  const cases = await runMutated([{ file: 'decode.js', from: '  if (read.some(timed)) {', to: '  if (false) {' }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/arc-pip-time.vnx']);
});

test('a reader that drops a node time silently is caught', async () => {
  const cases = await runMutated([{ file: 'decode.js', from: '    if (timed(v)) {', to: '    if (false) {' }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/extra-channel.vnx']);
});

test('a reader that drops a walk position channel silently is caught', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '      if ((ele !== undefined && ele !== null) || (time !== undefined && time !== null)) {',
    to: '      if (false) {',
  }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/extra-channel.vnx']);
});

test('a reader that keeps a closed ring closed is caught', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  if (n >= 2 && vertices[0].sameCoordinates(vertices[n - 1])) {',
    to: '  if (false) {',
  }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/ring-closed.vnx']);
});

test('a reader that keeps a ring of too few distinct positions is caught', async () => {
  const cases = await runMutated([{ file: 'decode.js', from: '  if (distinct < 3) {', to: '  if (false) {' }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/ring-distinct-by-lon-lat.vnx', 'diagnose/ring-too-few-points.vnx']);
});

test('a reader with no coordinate range is caught', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  const range = offTheGlobe(v[0], v[1]);',
    to: '  const range = null;',
  }]);
  assert.deepEqual(await failingDelta(cases), ['diagnose/position-out-of-range.vnx']);
});

test('a writer that closes its rings is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'emit.js',
    from: '  o.polygon = a.polygon.map((p) => emitPosition(p));',
    to: '  o.polygon = [...a.polygon, a.polygon[0]].map((p) => emitPosition(p));\n  if (false) checkRing(a);',
  }, { file: 'emit.js', from: '  checkRing(a);\n  o.polygon', to: '  o.polygon' }]);
  assert.deepEqual(await failingDelta(cases), [
    'accept/example-network.vnx',
    'accept/unknown-data-on-unexpected-hosts.vnx',
    'accept/viatrail-export-gpt-rapids.vnx',
  ]);
});

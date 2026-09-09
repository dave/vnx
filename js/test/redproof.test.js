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

/**
 * @param {Array<{file: string, from: string, to: string}>} edits
 * @returns {Promise<import('../corpus/runner.js').Case[]>}
 */
async function runMutated(edits) {
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
  const url = pathToFileURL(path.join(dir, 'index.js')).href;
  const api = await import(url);
  return runCorpus(CORPUS, {
    decodeText: api.decodeText,
    decodeContainer: api.decodeContainer,
    rerender: api.rerender,
  });
}

function failing(cases) {
  return cases.filter((c) => c.failures.length > 0).map((c) => c.name).sort();
}

test('the unmutated library passes every corpus case', async () => {
  const cases = await runMutated([]);
  assert.deepEqual(failing(cases), []);
  assert.ok(cases.length >= 30, `only ${cases.length} cases ran`);
});

test('transposing lon and lat, invisibly to the byte check, is caught by positions', async () => {
  // The transposition is undone on the way out, so re-render still reproduces
  // the file. Only `positions`, resolved against the model, can see it — which
  // is the claim corpus/README.md makes for that field.
  const cases = await runMutated([
    {
      file: 'decode.js',
      from: '  return ctx.position(path, new Position(v.slice()));',
      to: '  const s = v.slice(); [s[0], s[1]] = [s[1], s[0]];\n  return ctx.position(path, new Position(s));',
    },
    {
      file: 'emit.js',
      from: '  const c = position.channels;',
      to: '  const c = position.channels.slice(); [c[0], c[1]] = [c[1], c[0]];',
    },
  ]);
  const red = failing(cases);
  assert.ok(red.includes('accept/lonlat-transposition.vnx'), red.join(', '));
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
});

test('normalising [lon, lat, null] to [lon, lat] in the model breaks the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  return ctx.position(path, new Position(v.slice()));',
    to: '  const s = v.slice();\n  while (s.length > 2 && s[s.length - 1] === null) s.pop();\n  return ctx.position(path, new Position(s));',
  }]);
  assert.deepEqual(failing(cases), [
    'accept/absent-elevation-and-time.vnx',
    'accept/compute-failed-arc.vnx',
  ]);
});

test('plain lexicographic key order, without the scalar/container split, is caught', async () => {
  const cases = await runMutated([{
    file: 'render.js',
    from: '    (v !== null && typeof v === \'object\' ? containers : scalars).push(k);',
    to: '    scalars.push(k);',
  }]);
  const red = failing(cases);
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
  // The turnaround file is the one where classification-by-value is visible on
  // its own: its second link spells `arc, to, from` because `from` holds an
  // array in that object and `to` holds a string.
  assert.ok(red.includes('accept/shared-turnaround-handle.vnx'), red.join(', '));
});

test('rendering negative zero as -0 is caught by the format tier alone', async () => {
  const cases = await runMutated([{
    file: 'number.js',
    from: "  if (n === 0) return '0'; // collapses -0 as well as 0",
    to: "  if (n === 0) return Object.is(n, -0) ? '-0' : '0';",
  }]);
  assert.deepEqual(failing(cases), ['format/messy-spellings']);
});

test('a reader that discards data records is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  const a = arr(\'data\', path, kv, false, ctx);\n  if (a === undefined) return undefined;',
    to: '  const a = arr(\'data\', path, kv, false, ctx);\n  if (a !== undefined) return undefined;\n  if (a === undefined) return undefined;',
  }]);
  const red = failing(cases);
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
  assert.ok(red.includes('accept/unknown-data-on-unexpected-hosts.vnx'), red.join(', '));
});

test('a reader that discards rulesets is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: "  doc.rulesets = arr('rulesets', '', kv, false, ctx);",
    to: '  doc.rulesets = undefined;',
  }]);
  const red = failing(cases);
  assert.ok(red.includes('accept/example-network.vnx'), red.join(', '));
});

test('answering 0 for an unknown moving time is caught', async () => {
  const cases = await runMutated([{
    file: 'model.js',
    from: '    return known ? total : null;',
    to: '    return total;',
  }]);
  assert.deepEqual(failing(cases), [
    'accept/absent-elevation-and-time.vnx',
    'accept/track-without-times.vnx',
  ]);
});

test('rounding in the reader is caught by the full-precision case', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  return ctx.position(path, new Position(v.slice()));',
    to: '  const s = v.slice().map((c, i) => (typeof c === \'number\' && i < 2 ? Number(c.toFixed(6)) : c));\n  return ctx.position(path, new Position(s));',
  }]);
  const red = failing(cases);
  assert.ok(red.includes('accept/full-precision-coordinate.vnx'), red.join(', '));
});

test('a reader with no continuity check is caught by the diagnose tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '    if (endpointsMeet(a.link.to, b.link.from, nodes)) continue;',
    to: '    if (true) continue;',
  }]);
  assert.deepEqual(failing(cases), ['diagnose/chain-does-not-join.vnx']);
});

test('comparing inline endpoints by identity rather than value is caught by one file only', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  return pa.sameCoordinates(pb);',
    to: '  return pa === pb;',
  }]);
  // accept/example-network.vnx does NOT catch this. Its only chain junction is
  // node key to node key, which SPEC.md 6.2 settles by comparing the KEYS;
  // its inline endpoint is a chain terminus and is never compared to anything.
  // accept/shared-turnaround-handle.vnx is the whole of the corpus's coverage
  // for value equality between inline endpoints.
  assert.deepEqual(failing(cases), ['accept/shared-turnaround-handle.vnx']);
});

test('accepting a MINOR above the reader is caught by the reject tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  if (major > READER_MAJOR || (major === READER_MAJOR && minor > READER_MINOR)) {',
    to: '  if (false) {',
  }]);
  assert.deepEqual(failing(cases), ['reject/version-too-new.vnx']);
});

test('refusing a newer PATCH is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: '  const [, majorText, minorText] = VERSION.exec(declared);',
    to: "  const [, majorText, minorText, patchText] = VERSION.exec(declared);\n  if (Number(patchText) > 0) throw new VnxRefusal('version-too-new', 'a newer reader is needed');",
  }]);
  assert.deepEqual(failing(cases), ['accept/version-newer-patch.vnx']);
});

test('coercing a two-component version is caught by the reject tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: 'const VERSION = /^(\\d+)\\.(\\d+)\\.(\\d+)$/;',
    to: 'const VERSION = /^(\\d+)\\.(\\d+)(?:\\.(\\d+))?$/;',
  }]);
  assert.deepEqual(failing(cases), ['reject/version-two-component.vnx']);
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
  assert.deepEqual(failing(cases), ['diagnose/polygon-second-ring.vnx']);
});

test('comparing links that were not adjacent in the document is caught', async () => {
  requireFixture(
    'diagnose/link-names-absent-arc.vnx',
    (d) => d.networks[0].chains[0].links.length >= 3,
    'it needs three links with the middle one dangling; with two the expectation is guaranteed by arity',
  );
  const cases = await runMutated([{
    file: 'decode.js',
    from: '    if (b.index !== a.index + 1) continue;',
    to: '    if (b.index === -1) continue;',
  }]);
  // Was NOT caught until 2026-09-09: the fixture had two links and its dangling
  // one was the LAST, so no reader could report a break past it and the
  // expectation was guaranteed by arity. It now has three links with the middle
  // one dropped and survivors that do not meet, so the guard is real.
  assert.deepEqual(failing(cases), ['diagnose/link-names-absent-arc.vnx']);
});

test('refusing a timestamp with an offset, rather than diagnosing it, is caught', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: "    ctx.diag(`${path}/${esc(k)}`, `\"${k}\" must be UTC with a Z suffix`, 'timestamp-not-utc');\n    return undefined;",
    to: "    throw new VnxRefusal('timestamp-not-utc', 'must be UTC with a Z suffix');",
  }]);
  assert.deepEqual(failing(cases), ['diagnose/timestamp-with-offset.vnx']);
});

test('skipping an unknown key silently is caught by the diagnose tier', async () => {
  const cases = await runMutated([{
    file: 'decode.js',
    from: "    else ctx.diag(`${path}/${esc(k)}`, `unknown key \"${k}\"`, 'unknown-key');",
    to: '    else { /* silently skipped */ }',
  }]);
  assert.deepEqual(failing(cases), ['diagnose/unknown-key.vnx']);
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
  assert.deepEqual(failing(cases), [],
    'the corpus now discriminates the base-name match; delete this test and add a real one');
});

test('accepting a duplicate entry name is caught by the reject tier', async () => {
  const cases = await runMutated([{
    file: 'vnz.js',
    from: '    if (seen.has(key)) {',
    to: '    if (false) {',
  }]);
  assert.deepEqual(failing(cases), ['reject/container-duplicate-entries.vnz']);
});

test('treating `..` as a substring rather than a segment still passes; the corpus has no case for it', async () => {
  const cases = await runMutated([{
    file: 'vnz.js',
    from: "    if (segment === '..') return 'the path has a `..` segment';",
    to: "    if (segment.includes('..')) return 'the path has a `..` segment';",
  }]);
  // `a..b/photo.jpg` is explicitly legal (SPEC.md 7) and no corpus file holds
  // one, so the over-strict reader passes. Recorded, not papered over.
  assert.deepEqual(failing(cases), [],
    'the corpus now holds a legal `a..b` entry name; delete this test and add a real one');
});

test('accepting a path that escapes the archive is caught by the reject tier', async () => {
  const cases = await runMutated([{
    file: 'vnz.js',
    from: '    if (fault !== null) {',
    to: '    if (false) {',
  }]);
  assert.deepEqual(failing(cases), ['reject/container-escapes.vnz']);
});

test('widening the projection tolerance is caught by the diagnose tier', async () => {
  const cases = await runMutated([{
    file: 'geo.js',
    from: 'export const PROJECTION_TOLERANCE_M = 50;',
    to: 'export const PROJECTION_TOLERANCE_M = 100;',
  }]);
  assert.deepEqual(failing(cases), ['diagnose/inline-endpoint-beyond-tolerance.vnx']);
});

test('narrowing the projection tolerance is caught by the accept tier', async () => {
  const cases = await runMutated([{
    file: 'geo.js',
    from: 'export const PROJECTION_TOLERANCE_M = 50;',
    to: 'export const PROJECTION_TOLERANCE_M = 40;',
  }]);
  assert.deepEqual(failing(cases), ['accept/inline-endpoint-within-tolerance.vnx']);
});

test('taking the cosine of the segment start latitude is caught in BOTH directions', async () => {
  // SPEC.md 3.3.3 requires the cosine of the INLINE POSITION's latitude, held
  // constant for every vertex. The replacement tolerance pair (corpus commit
  // 1cfa4f9) discriminates the alternative in both directions: the ascending
  // arc makes it over-read and refuse a legal document, the descending arc
  // makes it under-read and accept an illegal one.
  const cases = await runMutated([{
    file: 'geo.js',
    from: '  const k = EARTH_RADIUS_M * Math.cos(point.lat * DEG);',
    to: '  const k = EARTH_RADIUS_M * Math.cos(pips[0].lat * DEG);',
  }]);
  assert.deepEqual(failing(cases), [
    'accept/inline-endpoint-within-tolerance.vnx',
    'diagnose/inline-endpoint-beyond-tolerance.vnx',
  ]);
});

test('using Web Mercator for the projection IS caught', async () => {
  const cases = await runMutated([{
    file: 'geo.js',
    from: '  const k = EARTH_RADIUS_M * Math.cos(point.lat * DEG);',
    to: '  const k = EARTH_RADIUS_M;',
  }]);
  const red = failing(cases);
  assert.ok(red.includes('accept/inline-endpoint-within-tolerance.vnx'), red.join(', '));
});

test('nothing in the corpus exercises the WRITE path', async () => {
  // SPEC.md 8.1 requires that a runner not use its writer, and this is the
  // proof that this one does not: breaking rounding and shortening outright
  // leaves every tier green. It is also the reason the tie-direction question
  // in src/round.js cannot be settled by running the corpus.
  const cases = await runMutated([
    {
      file: 'emit.js',
      from: '  return roundToPlaces(v, places);',
      to: '  return 0;',
    },
    {
      file: 'emit.js',
      from: '  while (n > 2 && out[n - 1] === null) n--;',
      to: '  n = 2;',
    },
  ]);
  assert.deepEqual(failing(cases), [],
    'the corpus now reaches the writer; assert the failures instead');
});

test('rounding half away from zero instead of half to even is NOT caught', async () => {
  // Measured 2026-09-09: Go and Python both give 340.2 for 340.25 to one
  // place, JavaScript `toFixed` gives 340.3. Two conforming writers therefore
  // emit different bytes for one model, and no corpus tier can see it.
  const cases = await runMutated([{
    file: 'round.js',
    from: '  else if (twiceRemainder === denominator && q % 2n === 1n) q += 1n; // half to even',
    to: '  else if (twiceRemainder === denominator) q += 1n; // half away from zero',
  }]);
  assert.deepEqual(failing(cases), [],
    'the corpus now covers the rounding tie; assert the failure instead');
});

test('comparing node keys instead of their positions is NOT caught', async () => {
  // SPEC.md 6.2 resolves a node key to its position before comparing, so two
  // DISTINCT keys at one position meet. diagnose/chain-does-not-join.vnx has
  // keys AND positions that differ, so a key-only comparator gives the same
  // verdict on it and passes. test/decode.test.js carries the local guard.
  const cases = await runMutated([{
    file: 'decode.js',
    from: "  if (a.kind === 'node' && b.kind === 'node' && a.node === b.node) return true;",
    to: "  if (a.kind === 'node' && b.kind === 'node') return a.node === b.node;",
  }]);
  assert.deepEqual(failing(cases), [],
    'the corpus now covers two distinct node keys at one position; assert the failure instead');
});

// An away-from-zero renderer for SPEC.md 4.5.3's tie, deciding the tie on the
// double's EXACT value. The naive version — "prefer the larger last digit
// whenever that spelling still parses back to the same double" — is the phantom
// detector this repo keeps warning about: near 3.67 the double spacing is about
// 4.4e-16 while the 17th significant digit steps by 1e-16, so THREE spellings
// round-trip and only one is nearest. That version reddens
// accept/full-precision-coordinate.vnx and accept/number-rendering.vnx as well,
// for a different defect (emitting a non-nearest spelling), and would have
// proved the wrong thing.
const AWAY_FROM_ZERO_TIE = `  const sign = n < 0 ? '-' : '';
  const mag = Math.abs(n);
  const text = String(mag);
  const parts = /^(\\d+)\\.(\\d*)(\\d)$/.exec(text);
  if (parts && parts[3] !== '9') {
    const view = new DataView(new ArrayBuffer(8));
    view.setFloat64(0, mag);
    const hi = view.getUint32(0);
    const lo = view.getUint32(4);
    const biased = (hi >>> 20) & 0x7ff;
    let mantissa = (BigInt(hi & 0xfffff) << 32n) | BigInt(lo);
    let exponent = -1074;
    if (biased !== 0) {
      mantissa |= 1n << 52n;
      exponent = biased - 1075;
    }
    const places = BigInt(parts[2].length + 1);
    const lower = BigInt(parts[1] + parts[2] + parts[3]);
    const midpointTimesTwo = 2n * lower + 1n;
    const shift = BigInt(exponent) + 1n + places;
    const scaled = mantissa * 5n ** places;
    const tie = shift >= 0n
      ? (scaled << shift) === midpointTimesTwo
      : scaled === (midpointTimesTwo << -shift);
    if (tie) return sign + parts[1] + '.' + parts[2] + String(Number(parts[3]) + 1);
  }
  return sign + text;`;

test('rendering a tie away from zero is caught by the formatter tier', async () => {
  // SPEC.md 4.5.3's rendering tie IS reachable by a re-render, because a
  // re-render always re-spells: format/number-tie carries one spelling in and
  // the other out. The rule is free in this seat — V8's Number::toString
  // already rounds to even — and this proves the case would still catch a
  // hand-written renderer HERE, rather than only catching other languages.
  //
  // I had this backwards in a note to the Python seat: I read the case passing
  // as the case not reaching JavaScript. Passing is what a correct library
  // does. Whether a case DISCRIMINATES is a separate question, and this is how
  // it gets answered.
  const cases = await runMutated([{
    file: 'number.js',
    from: '  return String(n);',
    to: AWAY_FROM_ZERO_TIE,
  }]);
  assert.deepEqual(failing(cases), ['format/number-tie']);
});

test('the full-precision coordinates are NOT rendering ties', async () => {
  // Three 17-digit spellings of -3.6712345678901235 round-trip to one double
  // and only one is nearest, so it is a phantom rather than a tie. If it were a
  // tie, the mutation above would have reddened the two accept files that carry
  // it, and the case above could not have attributed its failure to the tie
  // rule. Frozen because this is the distinction the whole section rests on.
  assert.equal(-3.6712345678901235, -3.6712345678901236);
  assert.equal(-3.6712345678901235, -3.6712345678901234);
  assert.equal((-3.6712345678901235).toPrecision(21), '-3.67123456789012347912');
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
  assert.deepEqual(failing(cases), [],
    'the corpus now carries a backslash entry name; assert the failure instead');
});

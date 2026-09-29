// The runner's own instruments.
//
// A set matcher is exactly the shape that decays into "anything passes" with
// nothing going red: every diagnose case in the corpus expects at most one
// diagnostic, so a matcher that checked nothing would keep the whole tier
// green. These cases freeze one instance per way the matcher can be wrong.

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { Case, matchDiagnostics } from '../corpus/runner.js';
import { decodeText } from '../src/decode.js';
import { decodeContainer, encodeContainer } from '../src/vnz.js';
import { encode } from '../src/index.js';
import { runCorpus } from '../corpus/runner.js';
import { DIAGNOSTIC_CODES, PINNED_CODES, REFUSAL_SUBSTRINGS } from '../src/errors.js';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const CORPUS = process.env.VNX_CORPUS
  ? path.resolve(process.env.VNX_CORPUS)
  : path.resolve(HERE, '..', '..', 'corpus');

function match(reported, expected) {
  const c = new Case('x');
  matchDiagnostics(c, reported, expected);
  return c.failures;
}

const D = (p, code) => ({ path: p, code });
const R = (p, code, message = 'anything at all') => ({ path: p, code, message });

test('an exact pair matches', () => {
  assert.deepEqual(match([R('/a', 'unknown-key')], [D('/a', 'unknown-key')]), []);
});

test('the English is not compared', () => {
  // A reader's wording is its own business. Two readers whose prose has nothing
  // in common must both pass.
  assert.deepEqual(match([R('/a', 'unknown-key', 'clé inconnue')], [D('/a', 'unknown-key')]), []);
});

test('order is not significant', () => {
  const reported = [R('/b', 'pass-arc-absent'), R('/a', 'unknown-key')];
  const expected = [D('/a', 'unknown-key'), D('/b', 'pass-arc-absent')];
  assert.deepEqual(match(reported, expected), []);
});

test('a missing diagnostic fails', () => {
  assert.equal(match([], [D('/a', 'unknown-key')]).length, 1);
});

test('an extra diagnostic fails', () => {
  assert.equal(match([R('/a', 'unknown-key'), R('/b', 'pass-arc-absent')], [D('/a', 'unknown-key')]).length, 1);
});

test('a diagnostic at the wrong path fails', () => {
  assert.equal(match([R('/b', 'unknown-key')], [D('/a', 'unknown-key')]).length, 2);
});

test('a diagnostic with the wrong code fails', () => {
  // The whole point of the change: the right place for the wrong reason is
  // still wrong, and matching English could not have told them apart when both
  // readers phrase it loosely.
  assert.equal(match([R('/a', 'ring-closed')], [D('/a', 'polygon-nested-rings')]).length, 2);
});

test('one reported diagnostic cannot satisfy two expectations', () => {
  const reported = [R('/a', 'unknown-key')];
  const expected = [D('/a', 'unknown-key'), D('/a', 'unknown-key')];
  // One expectation is left unpaired; the single report is not free to serve
  // both. (One failure, not two: the report itself was used.)
  assert.equal(match(reported, expected).length, 1);
});

test('two identical expectations are satisfied by two identical reports', () => {
  const reported = [R('/a', 'unknown-key'), R('/a', 'unknown-key')];
  const expected = [D('/a', 'unknown-key'), D('/a', 'unknown-key')];
  assert.deepEqual(match(reported, expected), []);
});

test('an expectation with no code fails rather than matching anything', () => {
  // A sidecar that lost its code must not quietly become a path-only check.
  const failures = match([R('/a', 'unknown-key')], [{ path: '/a' }]);
  assert.ok(failures.some((f) => f.includes('carries no code')), failures.join(' | '));
});

test('an expectation carrying "message" fails; the English field is message_advisory', () => {
  const failures = match([R('/a', 'unknown-key')], [{ path: '/a', code: 'unknown-key', message: 'x' }]);
  assert.ok(failures.some((f) => f.includes('message_advisory')), failures.join(' | '));
});

// --- the registers cannot go stale ------------------------------------------

function corpusPinnedCodes() {
  const codes = new Set();
  for (const kind of ['accept', 'diagnose']) {
    const dir = path.join(CORPUS, kind);
    for (const file of fs.readdirSync(dir)) {
      if (!file.endsWith('.expect.json')) continue;
      const e = JSON.parse(fs.readFileSync(path.join(dir, file), 'utf8'));
      for (const d of e.diagnostics ?? []) if (d.code) codes.add(d.code);
    }
  }
  return codes;
}

test('PINNED_CODES matches what the corpus pins, in both directions', () => {
  // Direction one: a case pinning a code this library does not claim goes red.
  // Direction two: a code claimed here that no case pins goes red. The set
  // grows whenever a diagnose case is added, and this is what notices.
  assert.deepEqual([...corpusPinnedCodes()].sort(), [...PINNED_CODES].sort());
});

test('every pinned code is one this library declares it can emit', () => {
  for (const code of PINNED_CODES) {
    assert.ok(DIAGNOSTIC_CODES.includes(code), `${code} is pinned but not in DIAGNOSTIC_CODES`);
  }
});

test('every code the library actually emits is declared', () => {
  // Catches a typo in a call site inventing a code nobody registered. Run over
  // the whole corpus, which is the widest set of documents to hand.
  const seen = new Set();
  for (const kind of ['accept', 'diagnose']) {
    const dir = path.join(CORPUS, kind);
    for (const file of fs.readdirSync(dir)) {
      if (file.endsWith('.vnx')) {
        for (const d of decodeText(fs.readFileSync(path.join(dir, file), 'utf8')).diagnostics) seen.add(d.code);
      } else if (file.endsWith('.vnz')) {
        for (const d of decodeContainer(fs.readFileSync(path.join(dir, file))).diagnostics) seen.add(d.code);
      }
    }
  }
  for (const code of seen) assert.ok(DIAGNOSTIC_CODES.includes(code), `${code} is emitted but not declared`);
  // ... and the corpus really does reach some of them, or this proves nothing.
  assert.ok(seen.size >= PINNED_CODES.length, `only ${seen.size} codes reached`);
});

test('a diagnostic code is a short kebab-case token', () => {
  for (const code of DIAGNOSTIC_CODES) {
    assert.match(code, /^[a-z][a-z0-9]*(-[a-z0-9]+)*$/, code);
  }
  assert.equal(new Set(DIAGNOSTIC_CODES).size, DIAGNOSTIC_CODES.length, 'duplicate code');
});

test('every refusal substring the corpus expects is in the refusal table', () => {
  // Refusals are still matched as English (a known defect). This keeps the
  // migration map honest until they gain codes of their own.
  const values = new Set(Object.values(REFUSAL_SUBSTRINGS));
  const missing = [];
  const dir = path.join(CORPUS, 'reject');
  for (const file of fs.readdirSync(dir)) {
    if (!file.endsWith('.expect.json')) continue;
    const e = JSON.parse(fs.readFileSync(path.join(dir, file), 'utf8'));
    if (e.refusal !== undefined && !values.has(e.refusal)) missing.push(`${file}: ${e.refusal}`);
  }
  assert.deepEqual(missing, []);
});

test('the whole corpus passes', () => {
  const cases = runCorpus(CORPUS, { decodeText, decodeContainer, encodeContainer, encode });
  const red = cases.filter((c) => c.failures.length > 0);
  // Reported with the failure text rather than as a bare set inequality: when
  // this goes red the question is always "which case, and why", and a corpus
  // this library does not own is mid-edit often enough that the answer is
  // sometimes "not us".
  assert.deepEqual(red.map((c) => `${c.name}: ${c.failures.join('; ')}`), []);
});

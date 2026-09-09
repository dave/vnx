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
import { CAUSE_SUBSTRINGS } from '../src/errors.js';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const CORPUS = path.resolve(HERE, '..', '..', 'corpus');

function match(reported, expected) {
  const c = new Case('x');
  matchDiagnostics(c, reported, expected);
  return c.failures;
}

const D = (p, m) => ({ path: p, message: m });

test('an exact pair matches', () => {
  assert.deepEqual(match([D('/a', 'unknown key "b"')], [D('/a', 'unknown key')]), []);
});

test('order is not significant', () => {
  const reported = [D('/b', 'second'), D('/a', 'first')];
  const expected = [D('/a', 'first'), D('/b', 'second')];
  assert.deepEqual(match(reported, expected), []);
});

test('a missing diagnostic fails', () => {
  assert.equal(match([], [D('/a', 'first')]).length, 1);
});

test('an extra diagnostic fails', () => {
  assert.equal(match([D('/a', 'first'), D('/b', 'second')], [D('/a', 'first')]).length, 1);
});

test('a diagnostic at the wrong path fails', () => {
  assert.equal(match([D('/b', 'first')], [D('/a', 'first')]).length, 2);
});

test('one reported diagnostic cannot satisfy two expectations', () => {
  // This is the property that stops a reader finding half the defects and
  // passing. Both expectations fit the single report as substrings.
  const reported = [D('/a', 'unknown key "b"')];
  const expected = [D('/a', 'unknown key'), D('/a', 'unknown')];
  // One expectation is left unpaired; the single report is not free to serve
  // both. (One failure, not two: the report itself was used.)
  assert.equal(match(reported, expected).length, 1);
});

test('matching prefers an assignment that satisfies every expectation', () => {
  // A greedy matcher would pair the general expectation with the specific
  // report and then fail; the augmenting search must back out of that.
  const reported = [D('/a', 'unknown key "b"'), D('/a', 'unknown key "c" is unusable')];
  const expected = [D('/a', 'unknown key'), D('/a', 'unusable')];
  assert.deepEqual(match(reported, expected), []);
});

test('a message is matched as a substring, a path exactly', () => {
  assert.deepEqual(match([D('/a', 'xx beyond the 50 m tolerance yy')],
    [D('/a', 'beyond the 50 m tolerance')]), []);
  assert.equal(match([D('/aa', 'x')], [D('/a', 'x')]).length, 2);
});

test('every substring the corpus expects is in the cause table', () => {
  // The corpus compares English substrings today. This keeps the migration to
  // reason codes mechanical: if a case starts expecting a phrase no cause
  // claims, this goes red rather than the table going quietly stale.
  const values = new Set(Object.values(CAUSE_SUBSTRINGS));
  const missing = [];
  for (const kind of ['accept', 'reject', 'diagnose']) {
    const dir = path.join(CORPUS, kind);
    for (const file of fs.readdirSync(dir)) {
      if (!file.endsWith('.expect.json')) continue;
      const e = JSON.parse(fs.readFileSync(path.join(dir, file), 'utf8'));
      if (e.refusal !== undefined && !values.has(e.refusal)) missing.push(`${file}: ${e.refusal}`);
      for (const d of e.diagnostics ?? []) {
        if (!values.has(d.message)) missing.push(`${file}: ${d.message}`);
      }
    }
  }
  assert.deepEqual(missing, []);
});

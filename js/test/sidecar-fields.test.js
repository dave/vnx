// The sidecar field guard.
//
// A sidecar field this runner does not read is a field that quietly stops being
// tested: the expectation sits in the corpus looking like coverage and nothing
// compares it. Python's runner and Kotlin's have failed on one since they were
// written; this one did not, and on 2026-09-13 a new `schema` field landed in
// thirty sidecars and passed here in silence while both of those went red.
//
// So the set below is not documentation. It is the claim that every field in
// every sidecar is either read by the runner or knowingly skipped, and the two
// tests are what make the claim checkable.

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const CORPUS = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', '..', 'corpus');

// Fields this runner compares. `kind` and `note` are structural.
const CHECKED = new Set([
  'kind', 'note', 'counts', 'positions', 'tracks',
  'attachments', 'attachment_contents', 'root', 'refusal', 'diagnostics',
]);

// Fields this runner knowingly does not compare, and why.
const UNCHECKED = new Set([
  // A prose record of a limitation (SPEC 6.4), paired with an empty
  // `diagnostics` list which IS checked.
  'cannot_detect',
  // The verdict corpus/schema/vnx-0.1.schema.json reaches on this case. It is
  // about the schema, not about any reader, so no implementation runner
  // compares it — go/internal/vnxschema is the one thing that does, and it
  // fails if a case records no verdict.
  'schema',
]);

function sidecars() {
  const out = [];
  for (const kind of ['accept', 'reject', 'diagnose', 'write']) {
    const dir = path.join(CORPUS, kind);
    if (!fs.existsSync(dir)) continue;
    for (const name of fs.readdirSync(dir).sort()) {
      if (name.endsWith('.expect.json')) out.push([name, JSON.parse(fs.readFileSync(path.join(dir, name), 'utf8'))]);
    }
  }
  return out;
}

test('the runner knows every sidecar field', () => {
  const found = sidecars();
  // The control. A sweep that read no sidecars would report a clean result.
  assert.ok(found.length >= 20, `read only ${found.length} sidecars — dead probe, not a clean sweep`);
  for (const [name, doc] of found) {
    for (const field of Object.keys(doc)) {
      assert.ok(
        CHECKED.has(field) || UNCHECKED.has(field),
        `${name}: this runner does not know '${field}', so the expectation would pass without being compared`,
      );
    }
  }
});

test('no declared field has outlived the sidecars that carried it', () => {
  // A stale entry is worse than a missing one: it makes the mechanism look more
  // thorough than it is, and nothing goes red when the field goes away.
  const used = new Set();
  for (const [, doc] of sidecars()) for (const field of Object.keys(doc)) used.add(field);
  for (const field of [...CHECKED, ...UNCHECKED].sort()) {
    assert.ok(used.has(field), `'${field}' is declared here and no sidecar carries it any more; delete the entry`);
  }
});

#!/usr/bin/env node
// CLI wrapper around the corpus runner.

import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { decodeText } from '../src/decode.js';
import { decodeContainer } from '../src/vnz.js';
import { rerender, write } from '../src/index.js';
import { runCorpus } from './runner.js';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const CORPUS = process.argv[2]
  ? path.resolve(process.argv[2])
  : path.resolve(HERE, '..', '..', 'corpus');

const cases = runCorpus(CORPUS, { decodeText, decodeContainer, rerender, write });

let failed = 0;
for (const c of cases) {
  if (c.failures.length === 0) {
    process.stdout.write(`PASS ${c.name}\n`);
    continue;
  }
  failed++;
  process.stdout.write(`FAIL ${c.name}\n`);
  for (const f of c.failures) process.stdout.write(`       ${f}\n`);
}
process.stdout.write(`\n${cases.length - failed}/${cases.length} cases passed\n`);
// The line above is the report. The exit code is set for scripts that trust it.
process.exitCode = failed === 0 ? 0 : 1;

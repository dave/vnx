// Read a VNX document with the JavaScript implementation and write it back out.
//
// One of six drivers, one per language; scripts/interop.py runs each over each
// other's output. No fallback: a refusal, a diagnostic, or an unwritable path
// exits non-zero with the cause on stderr.

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const { decodeText, encode } = await import(path.join(HERE, '..', '..', '..', 'js', 'src', 'index.js'));

const [, , input, output] = process.argv;
if (!input || !output) {
  process.stderr.write('usage: interop.mjs <in.vnx> <out.vnx>\n');
  process.exit(2);
}

let result;
try {
  result = decodeText(fs.readFileSync(input, 'utf8'));
} catch (e) {
  process.stderr.write(`interop: refused: ${e.message}\n`);
  process.exit(1);
}
for (const d of result.diagnostics) {
  process.stderr.write(`interop: diagnostic ${d.code} at ${d.path}: ${d.message}\n`);
}
if (result.diagnostics.length > 0) process.exit(1);
fs.writeFileSync(output, encode(result.document));

// check-examples.mjs — every complete JSON document in content/*.md (a ```json
// block whose top level has a "vnx" key) must be read by the reference tool
// with zero diagnostics.
//
//   node check-examples.mjs <path to a built vnx binary>
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const bin = process.argv[2];
if (!bin || !fs.existsSync(bin)) {
  console.error(
    'usage: node check-examples.mjs <vnx binary>  (build it with: cd ../go && go build -o /tmp/vnx ./cmd/vnx)',
  );
  process.exit(2);
}
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'vnx-site-'));
let checked = 0,
  failed = 0;
for (const f of fs.readdirSync(path.join(here, 'content')).filter((f) => f.endsWith('.md'))) {
  const text = fs.readFileSync(path.join(here, 'content', f), 'utf8');
  for (const [, body] of text.matchAll(/```json\n([\s\S]*?)```/g)) {
    let doc;
    try {
      doc = JSON.parse(body);
    } catch {
      continue;
    }
    if (!doc || typeof doc !== 'object' || !('vnx' in doc)) continue;
    const file = path.join(tmp, `${f}-${checked}.vnx`);
    fs.writeFileSync(file, body);
    checked++;
    let report;
    try {
      report = JSON.parse(execFileSync(bin, ['validate', '--json', file], { encoding: 'utf8' }));
    } catch (e) {
      console.error(`FAIL ${f}: refused\n${e.stdout || e.message}`);
      failed++;
      continue;
    }
    if (report.diagnostics?.length) {
      console.error(`FAIL ${f}: ${JSON.stringify(report.diagnostics, null, 2)}`);
      failed++;
    } else {
      console.log(`ok   ${f} (document ${checked})`);
    }
  }
}
if (checked === 0) {
  console.error('no example documents found: dead check');
  process.exit(1);
}
console.log(`${checked} documents checked, ${failed} failed`);
process.exit(failed ? 1 : 0);

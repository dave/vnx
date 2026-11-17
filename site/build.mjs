// build.mjs — builds the vnx.tools site into dist/.
//
//   node build.mjs              the site, as Cloudflare serves it (dist/)
//   node build.mjs --preview    a copy with the stylesheet inlined in each page
//                               (dist-preview/), for a preview host whose
//                               security policy blocks a linked stylesheet
//
// The spec page is made from ../SPEC.md at build time, so the site and the
// repository cannot disagree. IMPLEMENTING.md is linked on GitHub, not built. The other pages are in content/. Every page is also written as
// plain Markdown, and llms.txt / llms-full.txt are made from the same sources.

import { marked } from 'marked';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..');
const preview = process.argv.includes('--preview');
const out = path.join(here, preview ? 'dist-preview' : 'dist');
const SITE = 'https://vnx.tools';
const GITHUB = 'https://github.com/dave/vnx';

function fail(msg) {
  console.error(`build: ${msg}`);
  process.exit(1);
}

function read(p) {
  if (!fs.existsSync(p)) fail(`missing source file ${p}`);
  return fs.readFileSync(p, 'utf8');
}

// ---- sources ---------------------------------------------------------------

function frontmatter(text, file) {
  const m = text.match(/^---\n([\s\S]*?)\n---\n/);
  if (!m) fail(`${file} has no front matter`);
  const meta = {};
  for (const line of m[1].split('\n')) {
    const i = line.indexOf(':');
    if (i < 0) fail(`${file}: bad front matter line ${JSON.stringify(line)}`);
    meta[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  }
  for (const k of ['title', 'nav', 'description']) {
    if (!meta[k]) fail(`${file}: front matter has no ${k}`);
  }
  return { meta, body: text.slice(m[0].length) };
}

const specText = read(path.join(repo, 'SPEC.md'));
const specVersion = (specText.match(/^Version (\d+\.\d+\.\d+)$/m) || [])[1];
if (!specVersion) fail("SPEC.md has no 'Version x.y.z' line");

const pages = [
  { slug: 'index', ...frontmatter(read(path.join(here, 'content/index.md')), 'index.md') },
  { slug: 'concepts', ...frontmatter(read(path.join(here, 'content/concepts.md')), 'concepts.md') },
  { slug: 'writing', ...frontmatter(read(path.join(here, 'content/writing.md')), 'writing.md') },
  { slug: 'extensions', ...frontmatter(read(path.join(here, 'content/extensions.md')), 'extensions.md') },
  {
    slug: 'spec',
    meta: {
      title: 'Specification',
      nav: 'Spec',
      description: `The VNX wire format specification, version ${specVersion}. Normative.`,
    },
    body: specText,
    source: 'SPEC.md',
    toc: 3,
  },
];
for (const p of pages) p.toc ??= 2;

// The writing page shows SPEC §6.2's table of diagnostic codes, taken from the
// spec at build time so the two cannot disagree. A missing table is an error.
const codeTable = (() => {
  const m = specText.match(/^\| Construct \| Remedy \| Code \|\n(?:\|.*\|\n)+/m);
  if (!m) fail('SPEC.md has no "| Construct | Remedy | Code |" table');
  // Section numbers in the table are bare ("(3.10)"); make them §-references so they link.
  return m[0].replace(/\((\d+(?:\.\d+)*)\)/g, '(§$1)');
})();
for (const p of pages) {
  if (p.body.includes('<!-- spec:diagnostic-codes -->'))
    p.body = p.body.replace('<!-- spec:diagnostic-codes -->', codeTable);
}
if (!pages.some((p) => p.body.includes('| Construct | Remedy | Code |')))
  fail('no page carries the diagnostic code table');

// ---- links -----------------------------------------------------------------

// A section number such as 3.3.1 becomes the anchor sec-3-3-1, on the spec page.
const secId = (n) => 'sec-' + n.replace(/\./g, '-');

// A repository-relative link becomes a site page or a GitHub URL.
function rewriteHref(href, sourceDir) {
  if (/^(https?:|mailto:|#)/.test(href)) return href;
  const [p, hash] = href.split('#');
  const rel = path.posix.normalize(path.posix.join(sourceDir, p));
  const frag = hash ? '#' + hash : '';
  if (rel === 'SPEC.md') return 'spec.html' + specAnchor(hash);
  if (rel === 'IMPLEMENTING.md') return `${GITHUB}/blob/main/IMPLEMENTING.md${frag}`;
  if (rel === 'README.md') return './';
  if (rel.startsWith('schema/') || rel.endsWith('.html') || rel.endsWith('.txt')) return href;
  const isDir = p.endsWith('/') || !path.posix.extname(rel);
  return `${GITHUB}/${isDir ? 'tree' : 'blob'}/main/${rel.replace(/\/$/, '')}${frag}`;
}

// GitHub anchors in links to SPEC.md (#33-networks) map to our section ids.
function githubSlug(text) {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9 _-]/g, '')
    .replace(/ /g, '-');
}
const specAnchors = new Map();
function specAnchor(hash) {
  if (!hash) return '';
  if (!specAnchors.has(hash)) fail(`a link names SPEC.md#${hash}, which is not a heading of SPEC.md`);
  return '#' + specAnchors.get(hash);
}

// "Section 3.3.1" and "§3.3" in prose link to the spec section.
function linkSections(html, onSpecPage) {
  const target = (n) => (onSpecPage ? '' : 'spec.html') + '#' + secId(n);
  // Only outside tags and outside <code>/<pre>/<a>.
  const parts = html.split(/(<pre[\s\S]*?<\/pre>|<code[\s\S]*?<\/code>|<a [\s\S]*?<\/a>|<[^>]+>)/);
  return parts
    .map((part) => {
      if (part.startsWith('<')) return part;
      return part
        .replace(/\bSections? (\d+(?:\.\d+)*)/g, (m, n) =>
          knownSections.has(n) ? `<a href="${target(n)}">${m}</a>` : m,
        )
        .replace(/§(\d+(?:\.\d+)*)/g, (m, n) => (knownSections.has(n) ? `<a href="${target(n)}">${m}</a>` : m));
    })
    .join('');
}

const knownSections = new Set();
for (const line of specText.split('\n')) {
  const m = line.match(/^#{2,4} (\d+(?:\.\d+)*)\.? /);
  if (m) knownSections.add(m[1]);
}
for (const line of specText.split('\n')) {
  const m = line.match(/^#{1,6} (.+)$/);
  if (m) specAnchors.set(githubSlug(m[1].replace(/`/g, '')), slugify(m[1]));
}
if (knownSections.size < 20) fail(`found only ${knownSections.size} numbered sections in SPEC.md`);

// ---- markdown --------------------------------------------------------------

function slugify(text) {
  const num = text.match(/^(\d+(?:\.\d+)*)\.? /);
  if (num) return secId(num[1]);
  return text
    .toLowerCase()
    .replace(/<[^>]+>/g, '')
    .replace(/&[a-z]+;/g, '')
    .replace(/[^a-z0-9 -]/g, '')
    .trim()
    .replace(/\s+/g, '-');
}

function renderPage(page) {
  const sourceDir = page.source ? path.posix.dirname(page.source) : '.';
  const headings = [];
  const used = new Set();
  const renderer = new marked.Renderer();
  renderer.heading = (text, level, raw) => {
    let id = slugify(raw);
    let n = 2;
    const base = id;
    while (used.has(id)) id = `${base}-${n++}`;
    used.add(id);
    if (level >= 2 && level <= page.toc) headings.push({ id, level, text: raw.replace(/`/g, '') });
    if (level === 1) return `<h1 id="${id}">${text}</h1>\n`;
    return `<h${level} id="${id}"><a class="anchor" href="#${id}" aria-hidden="true">#</a>${text}</h${level}>\n`;
  };
  renderer.link = (href, title, text) => {
    const h = rewriteHref(href, sourceDir);
    const ext = /^https?:/.test(h) ? ' rel="noopener"' : '';
    return `<a href="${h}"${title ? ` title="${title}"` : ''}${ext}>${text}</a>`;
  };
  renderer.table = (header, body) =>
    `<div class="table"><table><thead>${header}</thead><tbody>${body}</tbody></table></div>\n`;
  let html = marked.parse(page.body, { renderer, gfm: true });
  html = linkSections(html, page.slug === 'spec');
  if (page.slug === 'spec') html = html.replace(/<h1 id="[^"]*">[^<]*<\/h1>/, '');
  return { html, headings };
}

// ---- template --------------------------------------------------------------

const css = read(path.join(here, 'style.css'));

function navHtml(current) {
  return pages
    .map((p) => {
      const href = p.slug === 'index' ? './' : `${p.slug}.html`;
      const cur = p.slug === current ? ' aria-current="page"' : '';
      return `<a href="${href}"${cur}>${p.meta.nav}</a>`;
    })
    .join('');
}

function tocHtml(headings) {
  if (headings.length < 3) return '';
  const items = headings.map((h) => `<li class="l${h.level}"><a href="#${h.id}">${h.text}</a></li>`).join('');
  return `<nav class="toc" aria-label="On this page"><p class="toc-title">On this page</p><ol>${items}</ol></nav>`;
}

function esc(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function shell(page, html, headings) {
  const title = page.slug === 'index' ? 'VNX — trail network exchange format' : `${page.meta.title} · VNX`;
  const style = preview ? `<style>\n${css}\n</style>` : `<link rel="stylesheet" href="style.css">`;
  const toc = tocHtml(headings);
  const mdHref = page.slug === 'index' ? 'index.md' : `${page.slug}.md`;
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="claude:viewport-fit-sides" content="cover">
<title>${esc(title)}</title>
<meta name="description" content="${esc(page.meta.description)}">
${
  page.slug === '404'
    ? ''
    : `<link rel="canonical" href="${SITE}/${page.slug === 'index' ? '' : page.slug}">
<link rel="alternate" type="text/markdown" href="${mdHref}" title="This page as Markdown">`
}
<link rel="icon" href="favicon.svg" type="image/svg+xml">
${style}
</head>
<body>
<div class="prerelease" role="note"><div class="wrap"><strong>Pre-release.</strong> VNX ${specVersion} is a draft. The format may change at any time, with no compatibility guarantee.</div></div>
<header class="site"><div class="wrap">
<a class="brand" href="./"><svg viewBox="0 0 40 24" aria-hidden="true"><path d="M4 18 C 12 6, 18 6, 20 12 S 30 20, 36 6"/><circle cx="4" cy="18" r="3"/><circle cx="20" cy="12" r="3"/><circle cx="36" cy="6" r="3"/></svg><span>VNX</span></a>
<nav class="primary" aria-label="Site">${navHtml(page.slug)}<a href="${GITHUB}" rel="noopener">GitHub</a></nav>
</div></header>
<div class="wrap layout${toc ? ' has-toc' : ''}">
${toc}
<main id="main" class="prose">
${page.slug === 'spec' ? `<h1 id="top">VNX Wire Format Specification</h1>\n<p class="lede">Version ${specVersion}. Made from <a href="${GITHUB}/blob/main/SPEC.md" rel="noopener">SPEC.md</a>. Also available as <a href="spec.md">plain Markdown</a>.</p>` : ''}
${html}
</main>
</div>
<footer class="site"><div class="wrap">
<p>VNX ${specVersion} · Source: <a href="${GITHUB}" rel="noopener">github.com/dave/vnx</a> · MIT licence · For agents: <a href="llms.txt">llms.txt</a>, <a href="llms-full.txt">llms-full.txt</a>${page.slug === '404' ? '' : `, <a href="${mdHref}">this page as Markdown</a>`}</p>
</div></footer>
</body>
</html>
`;
}

// ---- write -----------------------------------------------------------------

fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(path.join(out, 'schema'), { recursive: true });

function write(name, content) {
  fs.writeFileSync(path.join(out, name), content);
}

// Markdown for agents: content pages without front matter, with site links made absolute.
function agentMarkdown(page) {
  let md = page.body;
  if (page.source) {
    const dir = path.posix.dirname(page.source);
    md = md.replace(/\]\(([^)\s]+)\)/g, (m, href) => {
      const h = rewriteHref(href, dir);
      return `](${/^https?:|^#/.test(h) ? h : `${SITE}/${h}`})`;
    });
  } else {
    md = md.replace(/\]\(((?!https?:|#)[^)\s]+)\)/g, (m, href) => `](${SITE}/${href})`);
    md = md.replace(/<div class="cards">[\s\S]*?<\/div>\n/g, (block) =>
      block
        .replace(
          /<a class="card" href="([^"]+)"><strong>([^<]+)<\/strong><span>([^<]+)<\/span><\/a>/g,
          `- [$2](${SITE}/$1): $3`,
        )
        .replace(/<\/?div[^>]*>/g, ''),
    );
    md = md.replace(
      /<figure[\s\S]*?<figcaption[^>]*>([\s\S]*?)<\/figcaption>\s*<\/figure>/g,
      (m, cap) =>
        `(Diagram: one network with four nodes and three arcs. Two highlights both walk arc-1. ${cap.replace(/<\/?code>/g, '`')})`,
    );
  }
  return md.trim() + '\n';
}

for (const page of pages) {
  const { html, headings } = renderPage(page);
  const file = `${page.slug}.html`;
  write(file, shell(page, html, headings));
  write(`${page.slug}.md`, agentMarkdown(page));
}

// 404
// The 404 page is served at any depth, so its links are root-absolute.
write(
  '404.html',
  shell(
    { slug: '404', meta: { title: 'Not found', description: 'No page at this address.' } },
    `<h1>Not found</h1><p>There is no page at this address. Go to the <a href="./">home page</a>, or read <a href="llms.txt">llms.txt</a> for a list of every page.</p>`,
    [],
  ).replace(/(href|src)="(?!https?:|\/|#)(?:\.\/)?([^"]*)"/g, '$1="/$2"'),
);

// Static files
write('style.css', css);
fs.copyFileSync(path.join(here, 'static/favicon.svg'), path.join(out, 'favicon.svg'));
const schemaSrc = path.join(repo, 'corpus/schema/vnx-0.11.schema.json');
JSON.parse(read(schemaSrc));
fs.copyFileSync(schemaSrc, path.join(out, 'schema/vnx-0.11.schema.json'));

// llms.txt (https://llmstxt.org)
const byslug = Object.fromEntries(pages.map((p) => [p.slug, p]));
const line = (slug, label) =>
  `- [${label ?? byslug[slug].meta.title}](${SITE}/${slug}.md): ${byslug[slug].meta.description}`;
write(
  'llms.txt',
  `# VNX

> VNX is a JSON interchange format for trail data. It keeps the topology of a trail network: shared nodes and arcs, with named walks (highlights) drawn over them, and not only polylines. Plain documents are .vnx; ZIP archives with attachments are .vnz. Version ${specVersion}, pre-release: the format may change at any time.

Key facts: a position is [lon, lat, ele, time_ms], longitude first. A network owns nodes and arcs keyed by ID; highlights walk those arcs by reference. VNX defines no data types: applications add data records with reverse-DNS @type IDs. Validate with \`go run ./cmd/vnx validate --json <file>\` in the go/ directory of ${GITHUB}.

## Docs

${line('index', 'Overview')}
${line('concepts')}
${line('writing')}
${line('extensions')}

## Reference

${line('spec')}
- [JSON Schema](${SITE}/schema/vnx-0.11.schema.json): the shape of a VNX ${specVersion.split('.').slice(0, 2).join('.')} document. It cannot check graph rules such as walk connectivity.
- [Source repository](${GITHUB}): six libraries (Go, Python, JavaScript, Rust, Kotlin, Swift), the vnx command-line tool, and the conformance corpus.

## Optional

- [Implementing VNX](${GITHUB}/blob/main/IMPLEMENTING.md): for writing a new VNX library in another language.
- [llms-full.txt](${SITE}/llms-full.txt): the overview, concepts, writing guide, extensions page and full specification in one file.
`,
);

const full = ['index', 'concepts', 'writing', 'extensions', 'spec']
  .map((s) => `<!-- ${SITE}/${s === 'index' ? '' : s} -->\n\n${agentMarkdown(byslug[s])}`)
  .join('\n\n---\n\n');
write(
  'llms-full.txt',
  `# VNX: the complete documentation for agents\n\nVersion ${specVersion}. Pre-release: the format may change at any time. Source: ${GITHUB}\n\nThis file holds the overview, concepts, writing guide, extensions page and the full specification. The specification is normative; where another part disagrees with it, the specification decides.\n\n---\n\n${full}`,
);

write('robots.txt', `User-agent: *\nAllow: /\n\nSitemap: ${SITE}/sitemap.xml\n`);
write(
  'sitemap.xml',
  `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${pages
    .map((p) => `  <url><loc>${SITE}/${p.slug === 'index' ? '' : p.slug}</loc></url>`)
    .join('\n')}\n</urlset>\n`,
);

write(
  '_headers',
  `/*
  Content-Security-Policy: default-src 'self'; style-src 'self'; img-src 'self' data:; script-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'
  X-Content-Type-Options: nosniff
  Referrer-Policy: strict-origin-when-cross-origin
  Strict-Transport-Security: max-age=31536000
  Cross-Origin-Opener-Policy: same-origin
  Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=()
/*.md
  Content-Type: text/markdown; charset=utf-8
/*.txt
  Content-Type: text/plain; charset=utf-8
`,
);

console.log(`build: wrote ${out} (${pages.length} pages, VNX ${specVersion})`);

// The preview host serves one HTML page and no other files, so the preview is
// every page in one document: each page is a <section>, its ids are prefixed
// with its slug, and links between pages become #slug links. CSS shows the
// section that holds the link target, and the home page when there is none.
if (preview) {
  const prefixed = (html, slug) =>
    html
      .replace(
        /\b(id|aria-labelledby)="([^"]+)"/g,
        (m, a, v) =>
          `${a}="${v
            .split(' ')
            .map((x) => `${slug}--${x}`)
            .join(' ')}"`,
      )
      .replace(/href="#([^"]+)"/g, `href="#${slug}--$1"`)
      .replace(/href="\.\/"/g, 'href="#index"')
      .replace(/href="([a-z]+)\.html#([^"]+)"/g, 'href="#$1--$2"')
      .replace(/href="([a-z]+)\.html"/g, 'href="#$1"')
      .replace(
        /href="schema\/vnx-0\.10\.schema\.json"/g,
        `href="${GITHUB}/blob/main/corpus/schema/vnx-0.11.schema.json"`,
      );
  const sections = pages
    .map((page) => {
      const { html, headings } = renderPage(page);
      const toc = tocHtml(headings);
      const lede =
        page.slug === 'spec'
          ? `<h1 id="top">VNX Wire Format Specification</h1>\n<p class="lede">Version ${specVersion}. Made from SPEC.md.</p>`
          : '';
      return `<section class="page" id="${page.slug}"><div class="wrap layout${toc ? ' has-toc' : ''}">${prefixed(toc, page.slug)}<main class="prose">${prefixed(lede + html, page.slug)}</main></div></section>`;
    })
    .join('\n');
  const nav =
    pages.map((p) => `<a href="#${p.slug}">${p.meta.nav}</a>`).join('') +
    `<a href="${GITHUB}" rel="noopener">GitHub</a>`;
  const single = `<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="claude:viewport-fit-sides" content="cover">
<title>vnx.tools Preview</title>
<style>
${css}
.page { display: none; }
.page#index { display: block; }
.pages:has(:target) .page { display: none; }
.pages .page:target, .pages .page:has(:target) { display: block; }
.preview-note { font: 0.85rem/1.4 var(--f-ui); color: var(--ink-2); background: var(--paper-2); border-bottom: 1px solid var(--rule); }
.preview-note .wrap { padding-block: 0.5rem; }
</style>
<div class="prerelease" role="note"><div class="wrap"><strong>Pre-release.</strong> VNX ${specVersion} is a draft. The format may change at any time, with no compatibility guarantee.</div></div>
<div class="preview-note"><div class="wrap">Preview of vnx.tools: all pages in one file. The agent files (llms.txt, llms-full.txt, the .md copy of each page) exist on the real site only.</div></div>
<header class="site"><div class="wrap">
<a class="brand" href="#index"><svg viewBox="0 0 40 24" aria-hidden="true"><path d="M4 18 C 12 6, 18 6, 20 12 S 30 20, 36 6"/><circle cx="4" cy="18" r="3"/><circle cx="20" cy="12" r="3"/><circle cx="36" cy="6" r="3"/></svg><span>VNX</span></a>
<nav class="primary" aria-label="Site">${nav}</nav>
</div></header>
<div class="pages">
${sections}
</div>
<footer class="site"><div class="wrap"><p>VNX ${specVersion} · Source: <a href="${GITHUB}" rel="noopener">github.com/dave/vnx</a> · MIT licence</p></div></footer>
`;
  write('_artifact.html', single);
}

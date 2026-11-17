# vnx.tools

The source of [vnx.tools](https://vnx.tools), the site for developers and
agents who read or write VNX.

| Path | What it is |
|---|---|
| `content/` | The hand-written pages: home, concepts, writing guide, extensions. |
| `build.mjs` | Builds `dist/`. The spec page is made from `../SPEC.md`, so it always matches the repository. It also writes each page as Markdown, `llms.txt`, `llms-full.txt`, the JSON Schema copy and `_headers`. |
| `check-examples.mjs` | Checks that every complete example document in `content/` is read by the `vnx` tool with no diagnostics. |
| `style.css`, `static/` | The stylesheet and favicon. |
| `wrangler.jsonc` | The Cloudflare Worker `vnx-tools` that serves `dist/` as static assets. |

```sh
npm install
node build.mjs                                   # writes dist/
(cd ../go && go build -o /tmp/vnx ./cmd/vnx)
node check-examples.mjs /tmp/vnx                 # every example must pass
npm run deploy                                   # build, then wrangler deploy
```

The site serves no scripts and loads nothing from other hosts. `_headers` sets a
content security policy that allows only its own files.

A change to `SPEC.md` reaches the site at the next build. The hand-written
pages cite spec sections as `§3.3`, which the build links to the spec page, and
it stops with an error if a link names a spec heading that does not exist.

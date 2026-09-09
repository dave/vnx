# VNX — Viatrail Network eXchange

VNX is a JSON interchange format that carries trail network topology rather than
just polylines.

This repository is private and the format is at 0.x. There are no external users
yet. During 0.x the minor version is the breaking axis, so nothing here is
stable.

- [`SPEC.md`](SPEC.md) — the format definition.
- [`corpus/`](corpus/) — the conformance corpus, which decides whether an
  implementation is correct.
- [`go/`](go/) — the reference implementation and the `vnx` command-line tool.

## Why it exists

Viatrail's central structure is a network: a graph of nodes and arcs, with named
chains drawn over it as ordered traversals. Two chains that follow the same
trail reference the same arc, so editing that trail once changes every chain
over it. The shared geometry is the meaning, not a storage optimisation.

No interchange format in general use carries that. GPX has tracks and routes as
independent coordinate lists. KML has geometry and styling but no topology.
GeoJSON has features and no relations between them. Exporting a network to any
of the three flattens the graph into unrelated polylines: the drawing survives,
the structure does not.

VNX carries the structure. It is not a GeoJSON extension — a reader that ignored
the extensions would be left holding nothing worth having.

## The corpus decides what conforms

`SPEC.md` describes the format, and `corpus/` decides whether you have
implemented it. A library ships when it passes the corpus. That is the whole
bar, and it is the only defence against independent implementations quietly
disagreeing with each other.

```
corpus/
  accept/     files a reader must read, with expected results
  reject/     files a reader must refuse, with the reason
  diagnose/   files a reader must read while reporting something
  format/     non-canonical input, and the canonical bytes it must render as
```

Each case carries a `.expect.json` sidecar. The sidecar is plain JSON with no
language-specific metadata, but running the corpus is more than parse and
compare: some expectations are model-level, such as a track's moving-time span
or a chain's continuity, so a harness has to implement the format's semantics
and not only its syntax.

See [`corpus/README.md`](corpus/README.md) for the sidecar fields and for what
the corpus cannot check.

## What a library implements

A VNX library reads and writes `.vnx` and `.vnz`, produces canonical form, and
reports diagnostics. That is what the corpus tests and what "a VNX library"
means here.

The GPX, KML and GeoJSON writers under `go/` are conversion tools, not part of
the format. They are deliberately lossy exports for handing a file to another
application, and no library is expected to implement them.

## Reading a document

A document holds folders, networks, routes, tracks, waypoints and areas. A
network owns a graph — `nodes` and `arcs` keyed by identifier — and its `chains`
traverse those arcs by reference, which is the construct the other formats
cannot express. A top-level route owns its arcs outright and needs no graph, so
writing one requires only ordered lists of positions.

Coordinates are `[lon, lat, ele, time_ms]`, longitude first, and channels from
index 2 upward may be `null` or omitted. Reversing longitude and latitude
produces a file that parses perfectly and draws in the wrong hemisphere;
`corpus/accept/lonlat-transposition.vnx` catches it.

## The command-line tool

`go/cmd/vnx` wraps the same library, so the tool cannot drift from what the
reference implementation writes.

| Command | What it does |
|---|---|
| `validate` | reads a document and reports its diagnostics |
| `fmt` | renders a document in canonical form |
| `outline` | prints a document's structure — names, folders, counts, topology — without coordinates |
| `convert` | converts to and from GPX, KML and GeoJSON |

Every command exits non-zero on failure and offers JSON output carrying document
paths, which is what makes it usable from a script or an agent without parsing
English prose. `outline` exists because a day's recorded track holds tens of
thousands of pips: it lets a caller work on a library far larger than it could
otherwise read.

```sh
cd go && go run ./cmd/vnx validate ../corpus/accept/example-network.vnx
cd go && go run ./cmd/vnx fmt --write ../corpus/accept/example-network.vnx
```

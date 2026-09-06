# VNX — Viatrail Network eXchange

A JSON interchange format that carries **trail network topology**, not just
polylines.

⚠ **This repository is PRIVATE and the format is 0.x.** There are no external
users yet, and that is deliberate: during 0.x the minor version is the breaking
axis, and the format is still free to change. Do not treat anything here as
stable.

## Why it exists

Viatrail's headline structure is a **network**: a graph of nodes and arcs, with
named routes drawn over it as ordered traversals. Two routes sharing a trail
share the *same arc* — that shared geometry is the meaning, not a storage trick.
Edit the trail once and every route over it changes.

No interchange format in use carries that. GPX has tracks and routes as
independent coordinate lists. KML has geometry and styling but no topology.
GeoJSON has features and no relations at all. Exporting a network to any of them
flattens the graph into unrelated polylines: the drawing survives, the structure
does not.

VNX exists to carry the structure. It is **not** a GeoJSON extension — a naive
reader that ignored the extensions would be left holding nothing worth having.

## ⚠ The corpus is the definition of conforming, not the prose

`SPEC.md` describes the format. **`corpus/` decides whether you have implemented
it.** A library ships when it passes the corpus; that is the whole bar, and it is
the only real defence against independent implementations quietly disagreeing.

```
corpus/
  accept/     files a reader must read, with expected results
  reject/     files a reader must REFUSE, with the reason
  diagnose/   files a reader must read while REPORTING something
  format/     non-canonical input, and the canonical bytes it must render as
```

Each case carries a `.expect.json` sidecar. The sidecar is plain JSON with no
language-specific metadata — but ⚠ **passing it is not "parse and compare".**
Some expectations are model-level (a track's moving-time span, a chain's walk
validation), so a harness must implement the format's semantics, not merely its
syntax. Do not expect a fifty-line runner.

## What a library is

**The codec**: read and write `.vnx` and `.vnz`, produce canonical form, and
report diagnostics. That is what the corpus tests and what "a VNX library" means.

The GPX, KML and GeoJSON writers in `go/` are **conversion tools**, not part of
the format. They are deliberately lossy exports for handing a file to somebody
else's app, and no library is expected to implement them.

## Reading a file, in one paragraph

A document holds folders, networks, routes, tracks, waypoints and areas. A
network owns a graph — `nodes` and `arcs` keyed by id — and its `chains`
traverse those arcs by reference, which is the construct the other formats
cannot express. A top-level route owns its arcs outright and needs no graph at
all, so writing one requires only ordered lists of positions.

⚠ **Coordinates are `[lon, lat, ele, time_ms]`** — longitude first, and slots
from index 2 up may be `null` or omitted. Getting this backwards produces a file
that parses perfectly and draws in the wrong hemisphere; there is a corpus case
that catches it.

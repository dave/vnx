# VNX — Viatrail Network eXchange

VNX is a JSON interchange format for trail data. It carries the topology of a
trail network — shared geometry, with named walks drawn over it — and not only
polylines.

The format is at major version 0 and is unstable. Through 0.x the minor version
is a breaking axis, and constructs may be added, changed or removed without a
compatibility guarantee.

A small document: two nodes, an arc joining them, and a highlight: a lightweight
container for data objects along a walk of that arc.

```json
{
  "vnx": "0.10.0",
  "networks": [{
    "name": "Lairig Ghru approaches",
    "nodes": {
      "node-1": [-3.6712, 57.118, 340],
      "node-2": [-3.6605, 57.1042, 512.5]
    },
    "arcs": {
      "arc-1": { "from": "node-1", "to": "node-2", "pips": [[-3.6644, 57.1085, 451.9]] }
    },
    "highlights": [{
      "walk": [{ "arc": "arc-1", "from": "node-1", "to": "node-2" }],
      "data": [{ "@type": "com.example.tag", "value": "scramble" }]
    }]
  }]
}
```

A position is `[lon, lat, ele, time_ms]`, **longitude first**. Channels from index
2 upward may be `null` or omitted. Reversing longitude and latitude produces a
file that parses perfectly and draws in the wrong hemisphere, which
[`corpus/accept/lonlat-transposition.vnx`](corpus/accept/lonlat-transposition.vnx)
catches.

A network owns its graph: `nodes` and `arcs` keyed by identifier, with `highlights`
walking those arcs by reference. A highlight has an optional name and no notes or
style of its own: it is a lightweight container for the data records that apply
along its walk. That reference is the construct GPX, KML and
GeoJSON cannot express. A top-level plan owns a graph of the same shape and
carries one walk over it.

The full [`corpus/accept/example-network.vnx`](corpus/accept/example-network.vnx)
shows the rest: folders, plans, tracks, points, areas and styling. To check a
file of your own:

```sh
cd go && go run ./cmd/vnx validate ../corpus/accept/example-network.vnx
```

## Where to go

| Path | What it is |
|---|---|
| [`SPEC.md`](SPEC.md) | The format definition. Normative: every rule is stated here. |
| [`corpus/`](corpus/) | The conformance corpus, which decides whether an implementation is correct. |
| [`go/`](go/) | Go library. The reference implementation, and the home of the `vnx` command-line tool. |
| [`python/`](python/) | Python library. |
| [`js/`](js/) | JavaScript library. |
| [`rust/`](rust/) | Rust library. |
| [`kotlin/`](kotlin/) | Kotlin library. |
| [`swift/`](swift/) | Swift library. |
| [`IMPLEMENTING.md`](IMPLEMENTING.md) | For writing a new implementation in a seventh language. |
| [`site/`](site/) | The source of [vnx.tools](https://vnx.tools), the site for developers and agents. |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | Repository layout, test commands, and the documentation standard. |

Six libraries read and write the format. Each directory's README gives that
language's install line, usage and API.

## Why it exists

Viatrail's central structure is a network: a graph of nodes and arcs, with
highlights drawn over it as ordered traversals, each a lightweight container for
the data that applies along it. Two highlights that follow the same
trail reference the same arc, so editing that trail once changes every highlight
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
  write/      a document, and the document a writer must produce from it
  tools/      the generator for the .vnz archives
```

Each case carries a `.expect.json` sidecar. The sidecar is plain JSON with no
language-specific metadata, but running the corpus is more than parse and
compare: some expectations are model-level, such as a track's moving-time span
or a highlight's continuity, so a harness has to implement the format's semantics
and not only its syntax.

See [`corpus/README.md`](corpus/README.md) for the sidecar fields and for what
the corpus cannot check.

## Not in scope

VNX describes trail data, and a library's job is to read it, write it and report
diagnostics. That is the whole of what the corpus tests and what "a VNX library"
means here.

A document may carry style properties on its features. How those are drawn,
tiled or rendered belongs to the reading application; the format defines the
properties and stops there.

Conversion to GPX, KML and GeoJSON belongs to the `vnx` command-line tool under
[`go/`](go/). Those three formats cannot express a network, so the conversion is
deliberately lossy — a way to hand a file to another application. It is a
property of that tool, not of the format, and a library conforms without it.

## Contributing

[`CONTRIBUTING.md`](CONTRIBUTING.md) has the repository layout, the test command
for each language, and the standard that documentation here is held to.

A change to the format is a change to `SPEC.md` and to `corpus/` together, and
then to the six libraries that have to pass it.

## Licence

MIT. See [`LICENCE`](LICENCE).

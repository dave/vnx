---
title: VNX
nav: Home
description: VNX is a JSON format for trail data that keeps the shape of a trail network, not only its lines.
---

# Trail networks, in a file

VNX is a JSON format for trail data. A GPX or KML file holds lines. A VNX file
also holds how those lines join: a **network** of nodes and arcs, and named
walks drawn over it. Two walks that follow the same trail point at the same
arc, so the file keeps the fact that they share it.

A plain document has the extension `.vnx`. A ZIP archive that holds a document
and its attachments (photos, audio, any other files) has the extension `.vnz`.

```json
{
  "vnx": "0.11.0",
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
      "name": "North approach",
      "walk": [{ "arc": "arc-1", "from": "node-1", "to": "node-2" }],
      "data": [{ "@type": "com.example.tag", "value": "scramble" }]
    }]
  }]
}
```

A position is `[lon, lat, ele, time_ms]`. **Longitude comes first.** A file
with latitude and longitude the wrong way round still parses, and draws in the
wrong part of the world.

## Start here

<div class="cards">

<a class="card" href="concepts.html"><strong>Concepts</strong><span>Networks, arcs, highlights, plans, tracks, folders and data. How each differs from GPX and KML.</span></a>

<a class="card" href="writing.html"><strong>Write a VNX file</strong><span>Step by step, with the rules that catch most writers and a checklist.</span></a>

<a class="card" href="extensions.html"><strong>Extensions</strong><span>Add your own data with reverse-DNS type IDs, application keys and attachment folders.</span></a>

<a class="card" href="spec.html"><strong>Specification</strong><span>The full format definition. Every rule is stated here.</span></a>

</div>

## For agents

Read [`/llms.txt`](llms.txt) first. It lists every page. [`/llms-full.txt`](llms-full.txt)
holds the concepts, the writing guide, the extensions page and the full
specification in one plain Markdown file. The JSON Schema for the document is at
[`/schema/vnx-0.11.schema.json`](schema/vnx-0.11.schema.json). The schema checks
shape only. The specification has rules that a schema cannot state, such as
walk connectivity, and the reference tool checks them:

```sh
git clone https://github.com/dave/vnx
cd vnx/go && go run ./cmd/vnx validate --json path/to/file.vnx
```

## Libraries and tools

The source is at [github.com/dave/vnx](https://github.com/dave/vnx). It holds
six libraries that read and write VNX: Go, Python, JavaScript, Rust, Kotlin and
Swift. The Go module also has the `vnx` command-line tool:

| Command | What it does |
|---|---|
| `vnx validate <file>` | Reads a `.vnx` or `.vnz` file and prints its diagnostics. `--json` for a machine-readable report. |
| `vnx outline <file>` | Prints the structure: names, and the counts of nodes, arcs, highlights, points and areas in each network. |
| `vnx convert --to gpx\|kml\|geojson [--out <out-file>] <file>` | Writes the document as GPX, KML or GeoJSON, to `--out` or to standard output. Put the flags before the file. Those formats cannot hold a network, so the conversion loses the structure, and the tool reports what it could not write. |

A library conforms when it passes the **conformance corpus** in
[`corpus/`](https://github.com/dave/vnx/tree/main/corpus): files that a reader
must accept, refuse, or read with exact diagnostics, and files that a writer
must produce. To write a library in another language, read
[IMPLEMENTING.md](https://github.com/dave/vnx/blob/main/IMPLEMENTING.md) in the repository.

## Why VNX exists

GPX has tracks and routes as separate lists of coordinates. KML has geometry and
style, but no topology. GeoJSON has features with no relations between them.
When you export a trail network to any of the three, the graph becomes a set of
unrelated lines. The drawing survives. The structure does not.

VNX keeps the structure. It is not a GeoJSON extension: a reader that ignored
the parts GeoJSON does not define would have almost nothing left.

VNX is not a backup format. It carries no durable identity for the things in
it, so an export, an edit and an import makes copies, not updates. A VNX file
is a snapshot.

---
title: Concepts
nav: Concepts
description: The VNX model. Networks, nodes, arcs, highlights, plans, tracks, points, areas, folders and data records, and how they differ from GPX and KML.
---

# Concepts

This page explains the VNX model: what each kind of object is, where it can go,
and how it differs from the GPX and KML ideas you may know. The
[specification](spec.html) has the exact rules. Section numbers in brackets,
such as (§3.3), point to it.

## The document

A VNX document is one JSON object. It has a required `vnx` key with the format
version, and up to six collections:

```json
{
  "vnx": "0.11.0",
  "folders":   [],
  "networks":  [],
  "plans":     [],
  "tracks":    [],
  "points":    [],
  "areas":     []
}
```

Every collection is optional, and an empty collection means the same as no
collection. A document does not say which software wrote it or when (§3.1).

A top-level key that contains a full stop, such as `com.example.routes`, is an
**application key**. It adds a kind of content that VNX does not define. See
[Extensions](extensions.html).

## What goes where

| Object | Where it can be | What it holds |
|---|---|---|
| folder | top level | an `id`, a name, and an optional `parent` folder |
| network | top level | `nodes`, `arcs`, `highlights`, `points`, `areas` |
| plan | top level | its own `nodes` and `arcs`, and one `walk` |
| track | top level | `segments` of recorded positions |
| point | top level, or in a network | one position, `at` |
| area | top level, or in a network | one ring of positions, `polygon` |
| highlight | in a network | a `walk` over the network's arcs |
| data record | in the `data` array of any item, or of an arc | a `@type` and any fields |

Every top-level item except a folder can name the folder it is in with a
`folder` key. Points, areas and highlights in a network do not have `folder`,
because the network already holds them (§3.10).

## Networks

A **network** is a graph. It is the central idea of VNX and the thing GPX, KML
and GeoJSON cannot hold.

- A **node** is a position where arcs start and end: a junction, a summit, a
  trail end. Nodes are stored in an object keyed by ID, for example
  `"node-1": [-3.6712, 57.118, 340]` (§3.3.1).
- An **arc** is a line between two different nodes. It names them in `from` and
  `to`. Its `pips` are the points between the two nodes, and do not repeat the
  nodes' positions. An arc with no pips is a straight line (§3.3.2).
- A **highlight** is an ordered walk over arcs of the network. It does not own
  the arcs. It refers to them by ID (§3.3.3).

<figure class="diagram" aria-labelledby="fig-network">
<svg viewBox="0 0 640 250" role="img" aria-labelledby="fig-network-title">
<title id="fig-network-title">One network: four nodes, three arcs, two highlights that share an arc</title>
<g class="d-arc">
<path d="M60 190 C 140 150, 200 170, 260 120"/>
<path d="M260 120 C 330 80, 400 100, 470 70"/>
<path d="M260 120 C 320 160, 400 190, 560 200"/>
</g>
<g class="d-hl d-hl-a"><path d="M60 178 C 140 138, 200 158, 258 108 C 330 68, 400 88, 470 58"/></g>
<g class="d-hl d-hl-b"><path d="M60 202 C 140 162, 200 182, 262 132 C 320 172, 400 202, 560 212"/></g>
<g class="d-node">
<circle cx="60" cy="190" r="7"/><circle cx="260" cy="120" r="7"/><circle cx="470" cy="70" r="7"/><circle cx="560" cy="200" r="7"/>
</g>
<g class="d-label">
<text x="40" y="225">node-1</text><text x="236" y="100">node-2</text><text x="482" y="66">node-3</text><text x="540" y="232">node-4</text>
<text x="150" y="146">arc-1</text><text x="350" y="78">arc-2</text><text x="380" y="168">arc-3</text>
</g>
<g class="d-key">
<rect x="20" y="16" width="14" height="4" class="d-key-a"/><text x="42" y="22">Highlight “North route”: arc-1, then arc-2</text>
<rect x="20" y="36" width="14" height="4" class="d-key-b"/><text x="42" y="42">Highlight “East route”: arc-1, then arc-3</text>
</g>
</svg>
<figcaption id="fig-network">Both highlights walk <code>arc-1</code>. The file stores that line once. Move a pip on <code>arc-1</code> and both highlights follow it.</figcaption>
</figure>

### How this differs from GPX and KML

| Idea | GPX / KML | VNX |
|---|---|---|
| A trail line | each track or LineString has its own copy of the coordinates | an arc, stored once and referred to by ID |
| A junction | no object; two lines happen to have points near each other | a node that arcs start and end at |
| A named route along trails | a separate polyline that copies the coordinates | a highlight: a list of arc IDs, in order |
| Two routes on the same trail | two copies that can drift apart | two highlights that refer to the same arc |
| Information about a stretch of trail | extensions on a whole track, or nothing | data records on a highlight (its walk) or on an arc |

So the shared geometry is the meaning of the file, not a way to make it
smaller. A reader that only wants lines can draw each highlight by following
its arcs. The [writing guide](writing.html) shows how.

### Walks

A **walk** is an ordered array of **passes**. Each pass covers one arc, and the
order is the direction of travel (§3.3.3).

```json
"walk": [
  { "arc": "arc-1", "from": "node-1" },
  { "arc": "arc-2", "to": "node-3" }
]
```

A pass has `arc`, and sometimes these keys:

| Key | Value | Meaning |
|---|---|---|
| `from` | node ID or `[lon, lat]` | where the pass enters its arc |
| `to` | node ID or `[lon, lat]` | where the pass leaves its arc |
| `turn` | `[lon, lat]` | the walk goes out to this point on the arc and comes back |
| `pin` | `[lon, lat]` | the user fixed this pass with a handle at this point |

`from` and `to` appear only where they are needed: the first pass has `from`,
the last pass has `to`, and a pass that starts part way along an arc (a `from`
position) also has `to`. Every other pass starts at the node where the previous
pass ended. It leaves by the other end of its arc, or, if it has a `turn`, by
the node it entered by. A position on an arc means
the nearest point on the arc's line. A walk can start or end part way along an
arc, so a highlight can cover part of a trail.

### Highlights

A **highlight** is a light container for data along a walk. It has a `walk`, an
optional `name`, and `data`. What a
highlight is, other than its name, is what its data records say: a rating, a
warning, a surface type, a tag (§3.3.3).

A highlight in VNX is not a route that a user follows from start to finish
only. It can mark any stretch of the network: "boggy after rain", "the scramble
section", "the best descent".

### Points and areas in a network

A network can also hold `points` and `areas`, with the same shape as
top-level points and areas. Put them in the network when they belong to it, for
example a shelter on the network's trails, or a no-go zone for its routes.

## Plans

A **plan** is a top-level item that owns a small graph of its own (`nodes` and
`arcs`) and carries one `walk` over it. It is for a single route that is not
part of a shared network, such as a planned day out (§3.4).

- Nothing outside the plan can refer to its nodes or arcs.
- Every `from` and `to` in a plan's walk is a node ID. A plan's walk has no
  positions, no `turn` and no `pin`.
- A plan keeps arc boundaries. A plan that is partly drawn by hand and partly
  routed by an engine keeps that split, because each part is its own arc, with
  its own `data`.

## Tracks

A **track** is a recording: what a GPS device logged (§3.5).

```json
{
  "name": "Lairig Ghru, 12 May",
  "started": "2026-05-12T07:03:11Z",
  "ended": "2026-05-12T16:41:02Z",
  "segments": [
    { "pips": [[-3.6521, 57.1, 940, 1778569391000], [-3.651, 57.1012, 951, 1778569451000]] }
  ]
}
```

- `segments` is ordered. The gap between two segments is a pause. It is not
  geometry, and a reader must not draw a line across it.
- The fourth channel of each position is the time in Unix milliseconds.
- Moving time is not stored. A reader adds up, for each segment, the time from
  its first pip to its last. If a segment has no time on its first or last pip,
  the moving time is unknown, not zero.

## Points

A **point** is one position, `at`, with a name and data
(§3.6). It is the same shape at the top level and in a network. A GPX
`<wpt>` is a point.

```json
{
  "name": "Spring",
  "at": [-3.6521, 57.1, 940],
  "data": [{ "@type": "com.example.description", "value": "Reliable after rain." }]
}
```

## Areas

An **area** is one ring of positions, `polygon` (§3.7).

- The ring is open: do not repeat the first position at the end.
- It has at least three different positions.
- Holes and more than one ring cannot be expressed.

## Folders

A **folder** groups top-level items. It is the only top-level object with an
`id`, because it is the only one that other objects refer to (§3.2).

```json
"folders": [
  { "id": "cairngorms", "name": "Cairngorms" },
  { "id": "approaches", "name": "Approaches", "parent": "cairngorms" }
]
```

An item goes in a folder with `"folder": "approaches"`. A folder goes in
another folder with `parent`. Folder ids are unique in a document, and parents
never form a loop (§3.2). Folders can hold networks, plans, tracks,
top-level points, top-level areas, and other folders.

## Data records

Every item, and every arc, can have a `data` array of **records**. A record is a
JSON object with a `@type` and any other fields (§5):

```json
"data": [
  { "@type": "com.example.tag", "value": "day-walk" },
  { "@type": "com.example.surface", "value": "rock", "confidence": 0.8 }
]
```

VNX defines no record types. Applications define them, each under its own
reverse-DNS name. A reader keeps a record of a type it does not know, and does
not report an error for it. See [Extensions](extensions.html).

## Descriptions, look, hidden and pinned

- VNX has no notes or description key. Text about an item is application
  data: write it in a data record of your own type (§3.10, §5).
- VNX defines no style: no colours, icons or line widths. An application that
  wants to carry how its items look writes data records of its own types
  (§3.6). A `style` key is an unknown key.
- `hidden: true` means the user hid the item. If `hidden` is not there, the
  item is visible (§3.8).
- `pinned` is the time when the user pinned the item (§3.8).

## Positions and values

- A position is `[lon, lat, ele, time_ms]`. Longitude is first, as in GeoJSON.
  Longitude and latitude are decimal degrees on WGS 84. Elevation is in metres
  above mean sea level. Time is Unix milliseconds (§4.1).
- Write the shortest form: `[lon, lat]` when there is no elevation and no time.
  Use `null` in the middle only: `[lon, lat, null, 1778569391000]`.
- Nodes and arc pips have no time. Positions in a walk and in an area ring have
  only longitude and latitude.
- Timestamps such as `started` and `pinned` are ISO 8601 in UTC, ending in `Z`
  (§4.3).
- IDs are scoped to the object that holds them. A plan's `node-1` and a
  network's `node-1` are different nodes (§4.4).
- Array order is data. Never reorder `pips`, a walk, a ring or segments.

## What a reader does with a damaged file

A reader is tolerant of damage, and reports all of it (§6.2):

- An unknown key on a VNX object: the reader drops the key, keeps the object,
  and reports a **diagnostic**.
- A value that cannot be used (a bad position, a walk that does not connect): the
  reader drops that element, reports a diagnostic, and continues.
- A reader **refuses** a plain document for only four causes: it is not a JSON
  object or has no `vnx` key; `vnx` is not `MAJOR.MINOR.PATCH`; the version
  series is not supported; or a number is not finite.

Through version 0.x, each minor version is its own series. A 0.11 reader
accepts any 0.11.x file and refuses every other minor series, such as 0.12 (§2.2).

## VNZ archives and attachments

A `.vnz` file is a ZIP archive. It holds the document as `doc.vnx`, first in the
archive, and any other files beside it: photos, audio, documents (§7).

```
doc.vnx
com.example.photo/summit.jpg
com.example.photo/descent.jpg
org.example.audio/ridge-notes.m4a
```

- Put the files of a data type in a folder named exactly after the type's
  `@type` ID. Then files from two applications cannot have the same name
  (§7.2).
- A record names its file by the full entry name, in a field that its type
  defines.
- A reader returns every file in the archive as an attachment, whatever its
  folder or extension, and does not report the ones that no record names.
- Paths are relative, use `/`, and must not contain `..` or `\`. Entries are
  stored or deflated.

## What VNX does not carry

By design, VNX has no cached distances, elevation gain or bounds (derive them
from the geometry), no deleted items, no durable item identity, no map tiles,
and no creation or change times for items (§6.1).

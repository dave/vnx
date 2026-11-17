---
title: Write a VNX file
nav: Write a file
description: A step-by-step guide to writing a valid VNX document, with the rules that catch most writers and a checklist.
---

# Write a VNX file

This guide is for a developer or an agent that must produce a VNX file. Follow
the steps in order. At the end, check the file with the reference tool.

## 1. Choose what each thing is

| You have | Write it as |
|---|---|
| A set of trails that join, with routes or notes along them | a **network**: nodes at junctions and ends, arcs between them, highlights over the arcs |
| One planned route that is not part of a shared network | a **plan** |
| A GPS recording | a **track** |
| One place | a **point**, at the top level, or in a network if it belongs to that network |
| A region | an **area**, at the top level or in a network |
| A grouping | a **folder** |

If you import GPX: a `<trk>` is usually a track, a `<rte>` is usually a plan,
and a `<wpt>` is a point. To make a network from many lines, split each line
at the places where lines meet. Each split point becomes a node and each piece
becomes an arc.

## 2. Start the document

```json
{
  "vnx": "0.10.0",
  "generator": "Example Trails 2.4.1",
  "created": "2026-11-17T09:00:00Z"
}
```

- `vnx` is required: `MAJOR.MINOR.PATCH`, three numbers. Use `0.10.0` or a later
  `0.10.x`.
- `generator` is your software's name and version. A command-line tool names
  itself, not the application whose data it converts.
- `created` is optional, in UTC, ending in `Z`.

## 3. Build a network

Make the nodes first. Then make each arc between two **different** nodes, with
its interior points as `pips`. Then write the highlights.

```json
{
  "vnx": "0.10.0",
  "networks": [
    {
      "name": "Glen Feshie paths",
      "nodes": {
        "carpark": [-3.8571, 57.0623, 330],
        "bridge": [-3.8402, 57.0457, 360],
        "bothy": [-3.8355, 57.0302, 395],
        "plateau": [-3.7865, 57.0498, 905]
      },
      "arcs": {
        "glen-path": {
          "from": "carpark", "to": "bridge",
          "pips": [[-3.851, 57.0571, 338], [-3.8455, 57.0503, 349]]
        },
        "bothy-path": {
          "from": "bridge", "to": "bothy",
          "pips": [[-3.8381, 57.0385, 377]]
        },
        "moss-path": {
          "from": "bridge", "to": "plateau",
          "pips": [[-3.8204, 57.0479, 560], [-3.8011, 57.0491, 780]]
        }
      },
      "highlights": [
        {
          "name": "To the bothy",
          "walk": [
            { "arc": "glen-path", "from": "carpark" },
            { "arc": "bothy-path", "to": "bothy" }
          ],
          "data": [{ "@type": "com.example.grade", "value": "easy" }]
        },
        {
          "name": "Moss path climb",
          "walk": [
            { "arc": "moss-path", "from": "bridge", "to": "plateau" }
          ],
          "data": [{ "@type": "com.example.hazard", "value": "steep" }]
        }
      ],
      "points": [
        { "name": "Bothy", "at": [-3.8355, 57.0302, 395] }
      ]
    }
  ]
}
```

Rules for the graph:

- IDs are object keys. Make them short and readable, from the name where there
  is one (`glen-path`), or sequential (`node-1`, `arc-1`) (§4.4).
- `pips` holds **only** the points between the two nodes. Do not repeat the
  `from` or `to` node's position in `pips`.
- An arc must not start and end at the same node. A loop trail needs at least
  two arcs and a second node.
- Arcs are not directional for walks: a walk can cover an arc in either
  direction.

## 4. Write each walk

A walk lists the arcs in travel order. Use the fewest `from` and `to` keys:

1. The first pass has `from`.
2. The last pass has `to`.
3. A pass whose `from` is a position also has `to`.
4. No other pass has `from` or `to`.

Each pass after the first must start at a node of its arc: the node where the
previous pass left. If two arcs in your walk do not share a node, the walk is
broken, and a reader drops the whole highlight.

To start or end part way along an arc, give a `[lon, lat]` position instead of
a node ID. Positions in a walk have **two** channels only:

```json
"walk": [
  { "arc": "moss-path", "from": [-3.8204, 57.0479], "to": "plateau" }
]
```

To go out along an arc and come back (an out-and-back to a viewpoint), use
`turn`:

```json
"walk": [
  { "arc": "glen-path", "from": "carpark" },
  { "arc": "bothy-path", "turn": [-3.8381, 57.0385] },
  { "arc": "moss-path", "to": "plateau" }
]
```

The second pass enters `bothy-path` at `bridge`, goes to the turn point, and
leaves at `bridge` again. The third pass then starts at `bridge`.

## 5. Draw a highlight as a line

To turn a highlight into coordinates, for a map or for GPX, follow each pass:

1. Get the arc's full line: the `from` node position, then each pip, then the
   `to` node position.
2. Find the entry and the exit points on that line. A node ID is that end of
   the line. A position is the nearest point on the line.
3. Take the part of the line from entry to exit (through the `turn`, if there
   is one), in travel order. If the walk covers the arc from its `to` node to
   its `from` node, reverse the part.
4. Join the parts. Drop the repeated point where two parts meet.

Section 3.3.3 of the specification defines "nearest point" exactly, so that
every reader finds the same point.

## 6. Add plans, tracks, points and areas

```json
{
  "vnx": "0.10.0",
  "folders": [{ "id": "feshie", "name": "Glen Feshie" }],
  "plans": [
    {
      "folder": "feshie",
      "name": "Day 1",
      "nodes": { "a": [-3.8571, 57.0623], "b": [-3.8402, 57.0457] },
      "arcs": { "a-b": { "from": "a", "to": "b", "pips": [[-3.851, 57.0571]] } },
      "walk": [{ "arc": "a-b", "from": "a", "to": "b" }]
    }
  ],
  "tracks": [
    {
      "folder": "feshie",
      "name": "Recorded, 12 May",
      "segments": [
        { "pips": [[-3.8571, 57.0623, 330, 1778569391000], [-3.851, 57.0571, 338, 1778569751000]] },
        { "pips": [[-3.8455, 57.0503, 349, 1778570651000], [-3.8402, 57.0457, 360, 1778571011000]] }
      ]
    }
  ],
  "points": [
    { "folder": "feshie", "name": "Car park", "at": [-3.8571, 57.0623, 330], "style": { "icon": "parking", "importance": 2 } }
  ],
  "areas": [
    { "folder": "feshie", "name": "Estate ground", "polygon": [[-3.87, 57.03], [-3.82, 57.03], [-3.82, 57.07], [-3.87, 57.07]] }
  ]
}
```

- A plan's walk uses node IDs only: no positions, no `turn`, no `pin`.
- A track's segments are separate recordings with pauses between them.
- An area's ring is open: do not repeat the first position at the end. It needs
  three or more different positions.

## 7. Add your own data

Put information about an item in that item's `data` array, as records with a
`@type` you own:

```json
"data": [{ "@type": "com.example.grade", "value": "easy" }]
```

Use a reverse-DNS name from a domain you control, and choose your own field
names. Read [Extensions](extensions.html) before you design a record type.

## 8. Package attachments as a VNZ

If your records refer to files, write a `.vnz` ZIP archive:

1. Write `doc.vnx` as the **first** entry, at the archive root.
2. Put each data type's files in a folder named after its `@type`, for example
   `com.example.photo/summit.jpg`.
3. In the record, give the full entry name in a field your type defines.
4. Set the UTF-8 filename flag on every entry. Use stored or deflate only. Use
   relative paths with `/`, with no `..` and no `\`. Do not repeat a name, even
   with different letter case.

## Mistakes that writers make

| Mistake | Result |
|---|---|
| `[lat, lon]` instead of `[lon, lat]` | The file is valid and draws in the wrong place. Check one point on a map. |
| The arc's end nodes repeated in `pips` | Doubled points at every junction. |
| A walk whose arcs do not connect | The reader drops the highlight. |
| A time or elevation on a walk position | The reader drops the channel and reports a diagnostic. |
| `"hidden": false` | Not an error, but write `hidden` only when it is `true`. |
| A closed area ring | The reader drops the last position and reports a diagnostic. |
| `NaN` or `Infinity` | The whole document is refused. |
| An unknown key, such as `description` on a point | The reader drops the key and reports it. Use `notes`, or a data record. |
| `notes` or `style` on a highlight | Highlights have neither. Use a data record. |
| A cached length or bounds | VNX has no key for them. Readers derive them. |
| Numbers rounded by the reader | Readers must not round. You may round when you write: six decimal places is about 10 cm. |

## Check the file

```sh
git clone https://github.com/dave/vnx
cd vnx/go
go run ./cmd/vnx validate --json /path/to/file.vnx
```

Exit code 0 means the file was read. Read the `diagnostics` array: every entry
has a `code`, a JSON Pointer `path`, and a `message`. A good file has no
diagnostics. Exit code 1 means the file was refused.

The [JSON Schema](schema/vnx-0.10.schema.json) checks the shape of a document.
It cannot check the graph rules, such as walk connectivity, so use the tool as
well.

# VNX Wire Format Specification

Version 0.10.6

## Abstract

VNX (Viatrail Network eXchange) is a JSON interchange format for trail data. It
carries folders, networks, plans, tracks, points and areas. A network is a
graph of nodes and arcs together with highlights, each of which walks those arcs in
order; two highlights that follow the same trail reference the same arc.

This document defines the format. The conformance corpus distributed with this
specification (Section 8) defines whether an implementation conforms.

## Status of this format

The format is at major version 0, and is unstable. Through 0.x the minor
version is a breaking axis, and constructs may be added, changed or removed
without a compatibility guarantee.

## 1. Introduction

### 1.1 Scope

VNX is designed to be implemented as a library, independently, several times
over. Every construct defined here is intended to be inexpensive to implement
without reference to any particular runtime.

VNX is not a backup format. It carries no durable identity for the items it
contains (Section 6.1), so a cycle of export, edit and re-import duplicates
items rather than updating them. A VNX document is a snapshot.

VNX is not a GeoJSON extension or a GeoJSON profile. A reader that discarded
the constructs GeoJSON does not define would be left with unrelated polylines.

### 1.2 Requirements language

The key words "MUST", "MUST NOT", "REQUIRED", "SHOULD", "SHOULD NOT", "MAY" and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC 2119] [RFC 8174] when, and only when, they appear in all capitals.

### 1.3 Conformance

An implementation conforms to this specification when it passes the conformance
corpus described in Section 8. Where this prose and the corpus disagree, the
disagreement is a defect in one of them and is to be reported rather than
resolved by interpretation.

An example in this document is illustrative. It does not define a rule, and a
rule that is not stated in prose is a gap in this specification.

Two documents ship beside this one and neither is normative. `corpus/README.md`
explains how to run the corpus and why particular cases are shaped as they are.
`IMPLEMENTING.md` is a guide to the traps in this specification. Where either
differs from this document, this one decides.

### 1.4 Terminology

This section is informative. Each term is defined by the section it cites, and
a term is listed after every term its definition uses.

- A **document** is a JSON object carrying a `vnx` version key, up to six
  collections and any number of application keys (Section 3.1).
- An **item** is an object that can carry `name`, `hidden` and `data`, and,
  every kind of item but a highlight, `notes` (Section 3.10). An item is
  top-level when it is a member of one of the document's collections; every
  other item sits inside a top-level one.
- A **position** is a JSON array of channels: longitude, latitude, and
  optionally elevation and time (Section 4.1).
- A **pip** is one position in the `pips` array of an arc or of a track segment.
  An arc's pips are its interior points; a segment's pips are all of its points
  (Sections 3.3.2 and 3.5).
- A **folder** is a named grouping with an `id`. A top-level item other than a
  folder places itself in a folder by naming that `id` in its `folder` key; a
  folder places itself in another folder with `parent` (Section 3.2).
- A **network** is a graph of nodes and arcs, together with the highlights that
  traverse those arcs and the points and areas that belong to the network
  (Section 3.3).
- A **node** is a position in a network, keyed by an identifier, at which arcs
  start and end (Section 3.3.1).
- An **arc** is a line keyed by an identifier, running between the two
  different nodes its `from` and `to` name. Its `pips` carry the points between
  those nodes (Section 3.3.2).
- A **pass** is one entry of a walk: the arc it covers, and where on that arc it
  enters, turns and leaves (Section 3.3.3).
- A **walk** is an ordered array of passes forming one connected journey along
  arcs; the order is the direction of travel (Section 3.3.3).
- A **highlight** is a lightweight container for data objects along a walk of
  its network's arcs: an item carrying a walk and an optional name, with no
  notes or style of its own. A highlight does not own the arcs it walks
  (Section 3.3.3).
- A **plan** is a top-level item that owns its nodes and arcs and carries one
  walk over them (Section 3.4).
- A **segment** is an ordered `pips` array within a track; the interval between
  two segments is a pause (Section 3.5).
- A **track** is a top-level item carrying an ordered array of segments
  (Section 3.5).
- A **point** is an item carrying one position, `at`, either at the top level or
  within a network (Section 3.6).
- An **area** is an item carrying one ring of positions, `polygon`, either at
  the top level or within a network (Section 3.7).
- An **application key** is a top-level key of a document that contains a full
  stop. Its name is in reverse-DNS form and its value is opaque (Section 3.9).
- A **data record** is one object in the `data` array of an item or of an arc,
  identified by its `@type` and otherwise opaque (Section 5).
- A **container** is a ZIP archive, the VNZ file, carrying one document and its
  attachments (Section 7).
- An **attachment** is any entry of a container other than the root document
  (Section 7).
- A **diagnostic** is a report that one element of a document, or one entry of
  a container, could not be used as written; it is returned alongside the
  document (Section 6.3).
- A **refusal** is the outcome in which a reader returns no document; it is
  reported on a channel separate from diagnostics (Sections 6.2 and 6.3).

### 1.5 Example

This section is informative; Section 1.3 says of an example that it defines no
rule. The document below is complete and is read with no diagnostics.

```json
{
  "vnx": "0.10.0",
  "generator": "example 1.0",
  "created": "2026-08-31T06:45:12Z",
  "folders": [
    { "id": "cairngorms", "name": "Cairngorms" }
  ],
  "networks": [
    {
      "folder": "cairngorms",
      "name": "Lairig Ghru approaches",
      "nodes": {
        "node-1": [-3.6712, 57.118, 340],
        "node-2": [-3.6605, 57.1042, 512.5]
      },
      "arcs": {
        "arc-1": {
          "from": "node-1",
          "to": "node-2",
          "pips": [
            [-3.669, 57.1131, 388.2]
          ]
        }
      },
      "highlights": [
        {
          "walk": [
            { "arc": "arc-1", "from": "node-1", "to": "node-2" }
          ],
          "data": [
            { "@type": "com.example.tag", "value": "scramble" }
          ]
        }
      ]
    }
  ],
  "points": [
    {
      "folder": "cairngorms",
      "name": "Car park",
      "at": [-3.675, 57.1201, 320],
      "data": [
        { "@type": "com.example.tag", "value": "parking" }
      ]
    }
  ]
}
```

`vnx` is the format version (Section 2.2); `generator` and `created` are
informational (Section 3.1). The one folder, `cairngorms`, is named by the
`folder` key of the network and of the point (Section 3.2). The network
holds two nodes, one arc from `node-1` to `node-2` with one interior pip, and
one highlight whose walk is a single pass over that arc from `node-1` to `node-2`,
carrying one data record along that walk (Section 3.3). The point carries its position in
`at` and one data record of type `com.example.tag` (Sections 3.6 and 5).

## 2. Document identification

### 2.1 Names and media types

| Property | Value |
|---|---|
| Format name | Viatrail Network eXchange |
| Plain document | `.vnx`, `application/vnd.viatrail.vnx+json` |
| Container | `.vnz`, `application/vnd.viatrail.vnz+zip` |
| Version key | `vnx` |
| Current version | `0.10.6` |

The media types use the vendor tree and the `+json` and `+zip` structured
suffixes of [RFC 6839], so that generic tooling can process a VNX document
without knowledge of this format.

### 2.2 Version

A document MUST carry a `vnx` key at the top level whose value is a version
string. The position of that key within the top-level object is not
significant; see Section 4.4.

The value is `MAJOR.MINOR.PATCH`: three components, each one or more decimal
digits, separated by U+002E, full stop. All three components are REQUIRED.

Each component is one or more decimal digits, with no bound on length and no
rule against a leading zero: `00.010.0000` is a legal spelling of 0.10.0, and a
reader compares the numeric values rather than the text.

A version string that is not three digit-runs separated by full stops MUST be
refused, and MUST NOT be coerced. `"0.10"`, `"0.10.0.0"`, `"0.10.x"` and `"0.10.+1"`
are all refused. A two-component version is not a spelling of a three-component
one. The refusal MUST name the expected shape.

Which regime applies is selected by the document's MAJOR component: 0.x
documents are read under the rules for 0.x below, and 1.0-and-later documents
under the rules from 1.0. Within the selected regime, every comparison is
against the reader's own version.

A reader MAY support more than one MAJOR series, and one that supports several
applies each document's own regime. A reader that does not support a document's
MAJOR series MUST refuse it, naming the version, rather than read it under the
rules of a series it does support.

A reader MUST compare the MAJOR and MINOR components of the document against
its own supported version. The PATCH component MUST NOT be used to refuse a
document at any version, because a patch increment is additive by definition;
constructs introduced by a patch reach a reader through the unknown-key
diagnostics of Section 6.2.

Through 0.x:

- Each MINOR is its own series. A reader MAY support several 0.x minor series,
  and MUST refuse a document in a series it does not support, naming the
  version.
- A reader MUST accept a document in a series it supports, whatever the PATCH
  component. A 0.10.0 reader accepts 0.10.3. It refuses 0.11.0, and it refuses
  0.8.4 unless it declares support for the 0.8 series. A lower minor is refused
  for the same reason as a higher one.

A reader of this version supports the 0.10 series only.

From version 1.0 onward, minor versions are additive: a reader MUST accept any
minor version at or above its own and MUST ignore constructs it does not
recognise, and only a MAJOR increment is a refusal. The rule for PATCH does not
change.

A reader that re-encodes a document MUST write the version the document
declared, not the reader's own. A 0.10.0 reader that reads a 0.10.3 document and
writes it back writes `0.10.3`.

## 3. Document structure

Section 3.10 is the normative key set. The JSON below is illustrative, as
Section 1.3 says of every example.

### 3.1 Envelope

A VNX document is a JSON object [RFC 8259].

```json
{
  "vnx": "0.10.0",
  "generator": "Example Trails 2.4.1",
  "created": "2026-08-31T06:45:12Z",

  "folders":   [ ],
  "networks":  [ ],
  "plans":     [ ],
  "tracks":    [ ],
  "points":    [ ],
  "areas":     [ ],

  "com.example.routes": [ ]
}
```

Every key is OPTIONAL except the ones its object requires, which Section 3.10
lists. A reader MUST accept the absence of every other key.

Absence means "not known". It MUST NOT be read as a default value, with three
exceptions stated where they arise: `label-visible`, where absence means Auto
(Section 3.6), `hidden`, where absence means visible, and `pinned`, where
absence means not pinned (Section 3.8).

All six collections are OPTIONAL. An absent collection and an empty one carry
the same content, and a reader MUST treat them alike when reading. That holds
wherever a container appears in this format, except an application key and
anything inside its value (Section 3.9): an empty `data` bag, an empty `highlights`
array and an empty `nodes` object each carry the same content as the key being
absent.

`created` and `generator` are informational. `created` is a timestamp
(Section 4.3).

`generator` names the software that wrote the document.

- An application writes its own name and version, for example
  `"Example Trails 2.4.1"`.
- A development or command-line tool names itself, and MUST NOT name itself as
  the application.

`generator` carries the application version. The format version is the value of
`vnx` (Section 2.2). The two are unrelated.

### 3.2 Folders

```json
{ "id": "cairngorms", "name": "Cairngorms", "parent": "approaches" }
```

`parent` is absent at the root. Folders are the only top-level items that carry
an `id`, because they are the only top-level items referenced from elsewhere.

A top-level item other than a folder MAY carry a `folder` key naming the folder
that contains it.

### 3.3 Networks

A network owns a graph. Its `nodes` and `arcs` are objects rather than arrays,
because walks reference their entries by key.

```json
{
  "folder": "approaches",
  "name": "Lairig Ghru approaches",
  "notes": "...",
  "nodes": { "node-1": [-3.6521, 57.1000, 940], "node-2": [-3.6402, 57.0931, 811] },
  "arcs":  { "arc-1": { "from": "node-1", "to": "node-2", "pips": [ ], "data": [ ] } },
  "highlights": [ ],
  "points": [ ],
  "areas":  [ ],
  "data":   [ ]
}
```

#### 3.3.1 Nodes

A node is a position (Section 4.1) keyed by an identifier. Node identifiers are
scoped to the containing network (Section 4.4).

A node carries longitude, latitude and optionally elevation. A node carries no
time. A reader that meets a time on a node drops the time channel, keeps the
node and records a diagnostic (Section 6.2).

#### 3.3.2 Arcs

An arc carries `from` and `to`, each naming a node of the same network, and
`pips`, an array of positions.

An arc's line runs from the position of its `from` node, through its `pips` in
order, to the position of its `to` node. The `pips` array holds the interior
points of the line only: it does not repeat either node's position. An arc MAY
have no pips, and is then the straight line between its two nodes. The
elevation at each end of the line is its node's.

An arc pip carries longitude, latitude and optionally elevation. An arc pip
carries no time. A reader that meets a time on an arc pip drops the time
channel, keeps the pip and records one diagnostic for the arc (Section 6.2).

An arc's `from` and `to` MUST name two different nodes. An arc whose `from` and
`to` name the same node is a self-loop, and this format cannot carry one: a pass
over it would have no other end to leave by (Section 3.3.3). A reader drops a
self-loop arc and records a diagnostic.

A reader drops an arc whose `from` or `to` names a node that is not in the
network, and records a diagnostic.

A highlight whose walk names a dropped arc is itself dropped, under the rule for a
pass naming an absent arc (Section 6.2).

#### 3.3.3 Highlights

A highlight is a lightweight container for data objects along a walk of its
network's arcs. It carries a walk over arcs that it does not own, and its `data`
records apply along that walk. A highlight can carry a `name`. It has no notes
or style of its own: what it is, beyond its name, is what its data records say.
A `notes` or `style` on a highlight is an unrecognised key (Sections 3.10 and
6.2).

```json
{
  "name": "Chalamain Gap",
  "walk": [
    { "arc": "arc-7", "from": [-3.6521, 57.1000], "to": "node-5" },
    { "arc": "arc-1" },
    { "arc": "arc-3", "pin": [-3.6495, 57.0984] },
    { "arc": "arc-4", "turn": [-3.6488, 57.0977] },
    { "arc": "arc-3" },
    { "arc": "arc-8", "to": [-3.6402, 57.0931] }
  ],
  "data": [ ]
}
```

`walk` is an ordered array of passes, and the order is the direction of travel.
Each pass covers one arc of the same network, which it names in `arc`. A pass
enters its arc, optionally goes out to a turn and back, and leaves the arc.

A pass carries these keys besides `arc`:

| Key | Value | Meaning |
|---|---|---|
| `from` | a node key, or a position | where the pass enters its arc |
| `to` | a node key, or a position | where the pass leaves its arc |
| `turn` | a position | where the walk turns round on this arc |
| `pin` | a position | the pass is fixed by the user who drew the walk, with a handle at this position |

A position in `from`, `to`, `turn` or `pin` carries longitude and latitude
only. Its elevation is the elevation of the arc's line at the point the position
identifies. A reader that meets an elevation or a time on such a position drops
that channel, keeps the pass and records a diagnostic (Section 6.2).

A pass is pinned when it carries `pin`, and the pin leaves what the pass covers
unchanged. A pass with a `turn` MUST NOT carry a `pin`: the turn already fixes
it.

**The rule for `from` and `to`.** Every walk has one spelling, so that a walk
read and written back is the same walk:

1. The first pass has `from`.
2. The last pass has `to`.
3. A pass whose `from` is a position has `to`.
4. No pass has a `from` or a `to` that rules 1 to 3 do not require.

**What a pass covers.** A pass's entry is its `from` where it has one, and
otherwise the node at which the previous pass left its arc. Its exit is its
`to` where it has one. A pass without `to` leaves by the node it entered by
when it has a `turn`, and by the other end of its arc when it has none. Because
no arc is a self-loop (Section 3.3.2), the other end is always one node.

A pass covers its arc's line from its entry to its turn, where it has one, and
from there to its exit, in that order, whatever order those points lie in along
the line. A pass whose entry and exit are the same point, with no turn, covers
that point alone. Such a pass is legal.

**Positions on an arc.** A position in `from`, `to`, `turn` or `pin` identifies
the point of the arc's line nearest to it. There is no limit on the distance
between the position and the line: every position identifies a point.

Two readers must identify the same point, so the distance is defined exactly.
Every coordinate, the position and every vertex of the arc's line, is projected
to metres by a local equirectangular map about the latitude of the position,
using an Earth radius of 6371008.8 m:

```
x = radians(lon) * 6371008.8 * cos(radians(lat_of_the_position))
y = radians(lat) * 6371008.8
```

The cosine term uses that one latitude for every coordinate, which keeps the map
affine, so a straight segment of the line stays straight and the plane geometry
is exact. The distance to a segment is the distance to the nearest point on that
segment. The point identified is the nearest point on the nearest segment; where
two segments are equally near, it is the one on the segment earlier along the
line from the arc's `from` node.

**A valid walk.** A walk is valid when all of the following hold:

1. Each pass names an arc of the network.
2. The rule for `from` and `to` above holds.
3. A `from` or `to` that is a node key names an end of its pass's arc.
4. Each pass after the first enters its arc at an end of that arc: the node at
   which the previous pass left is one of the arc's two nodes.
5. Only the last pass leaves at a position.
6. No pass carries both `turn` and `pin`.

An empty walk is valid and covers nothing. A reader drops a highlight whose walk is
not valid and records a diagnostic that names the pass and the rule
(Section 6.2).

### 3.4 Plans

A top-level plan owns its graph. It carries `nodes` and `arcs` of the same
shape as a network's (Sections 3.3.1 and 3.3.2), and one `walk` of the same
shape as a highlight's (Section 3.3.3). Nothing outside the plan can reference
its nodes or arcs.

```json
{
  "folder": "cairngorms",
  "name": "Braeriach direct",
  "style": { },
  "nodes": { "node-1": [-3.728, 57.0782, 1296], "node-2": [-3.7101, 57.0699, 1150] },
  "arcs":  { "arc-1": { "from": "node-1", "to": "node-2", "pips": [ ], "data": [ ] } },
  "walk":  [ { "arc": "arc-1", "from": "node-2", "to": "node-1" } ],
  "data":  [ ]
}
```

A plan's walk runs from node to node. Every `from` and `to` in it is a node
key, and no pass in it carries `turn` or `pin`.

Every rule Section 3.3 states for a network's nodes and arcs and for a highlight's
walk applies to a plan's. Where that section has a reader drop a highlight, a
reader drops the plan.

Arc boundaries are preserved by the arcs, so a plan that is partly hand-drawn
and partly engine-routed retains that structure. A walk may cover an arc in
either direction.

### 3.5 Tracks

```json
{
  "folder": "cairngorms",
  "name": "Lairig Ghru, 12 May",
  "started": "2026-05-12T07:03:11Z",
  "ended":   "2026-05-12T16:41:02Z",
  "style":   { "color": "#6a4c93", "width": 3 },
  "segments": [ { "pips": [ [-3.6521, 57.1, 940, 1778569391000] ] } ],
  "data": [ ]
}
```

`segments` is ordered, and each segment carries an ordered `pips` array.

The interval between the last pip of segment *k* and the first pip of segment
*k+1* is a pause. It is not geometry and MUST NOT be drawn.

Moving time is not carried. A reader that reports it derives it as the sum over
segments of the difference between the last and first pip times in that
segment. It is not the wall-clock span between `started` and `ended`, which
pauses lengthen.

Moving time is derivable only when every segment carries a time on its first and
its last pip. Otherwise a reader MUST report it as unknown, MUST NOT report it
as zero, and MUST NOT report a sum over the segments that do carry times. A
track with no segments at all is unknown for the same reason.

An empty segment does not make a track unknown. It has no span to measure, so it
contributes zero.

### 3.6 Points

A point carries one position. Top-level `points` and a network's `points`
share one shape.

```json
{
  "name": "Spring",
  "at": [-3.6521, 57.1, 940],
  "notes": "...",
  "style": {
    "color": "#1e6091", "icon": "water", "icon-size": 21,
    "pin-shape": "teardrop", "label-position": "right",
    "label-visible": true, "importance": 3
  },
  "data": [ ]
}
```

`at` is a position (Section 4.1).

`icon-size` is an integer in points. No range is stated for it, so a reader
does not check it and `"icon-size": "large"` passes unremarked. Section 6.2
checks a value only where this document states a range or an enumeration.

`importance` is an integer from 1 to 5, and that is normative: a value outside
the range is handled under Section 6.2. Neither is spelled as a word.

`label-visible` is a tristate, and its absence is meaningful:

| Value | Meaning |
|---|---|
| absent | Auto: the renderer decides from importance and zoom |
| `true` | forced visible |
| `false` | forced hidden |

A reader MUST NOT treat an absent `label-visible` as `false`. Doing so flattens
Auto to hidden with nothing to report.

### 3.7 Areas

```json
{
  "name": "Boggy ground",
  "polygon": [ [-3.65, 57.1], [-3.64, 57.1], [-3.64, 57.11] ],
  "style": { "fill-color": "#1e6091", "fill-opacity": 0.25, "stroke-color": "#1e6091", "stroke-width": 2 },
  "data": [ ]
}
```

`polygon` is one ring: an array of positions tracing the area's boundary. The
ring is open. Its last position is not a repeat of its first, and the boundary
runs from the last position back to the first. A ring holds at least three
distinct positions. Two positions are distinct when they differ in longitude or
in latitude; no other channel takes part. Holes and multiple rings are not
expressible in this format.

A writer MUST emit a ring open. A reader that meets a ring whose last position
equals its first, in longitude and latitude, drops that last position, keeps
the area and records a diagnostic. It drops that one position only, and then
applies the next paragraph to what remains.

A reader drops an area whose ring holds fewer than three distinct positions,
and records a diagnostic.

A reader that encounters a nested or additional ring MUST report a diagnostic
and MUST drop the area. It MUST NOT silently keep the outer ring, and MUST NOT
merge the rings.

### 3.8 Hidden and pinned state

`hidden` is a boolean carried on folders, networks, highlights, plans, points,
areas and tracks. It is user-visible state rather than a rendering cache.

Absence means visible. A writer MUST emit the key only when its value is `true`,
and a document SHOULD NOT contain `"hidden": false`.

`pinned` is a record-level timestamp (Section 4.3) carried on networks,
highlights, plans, tracks, points and areas: every item except a
folder. Its value is the time the user pinned the item. Like `hidden`, it is
user-visible state.

Absence means not pinned. A writer MUST emit the key only when the item is
pinned.

An item's `pinned` is unrelated to a pass's `pin` (Section 3.3.3). A pass
carries no `pinned`: a `pinned` on a pass is an unrecognised key (Section 6.2).

### 3.9 Application keys

A top-level key of a document whose name contains a full stop is an application
key. It adds a kind of content that this format does not define. The test is
exact: after JSON string unescaping, the name contains the character U+002E
FULL STOP. It is case-sensitive, and nothing else about the name is examined.
No key this format defines contains a full stop, so an application key can
never collide with one.

A name SHOULD be in reverse-DNS form, chosen by the owner of the domain:
`com.example.routes` belongs to whoever owns `example.com`. That is
guidance for whoever names a key. A reader MUST NOT validate it, so `.` and
`points.` are application keys.

The value of an application key is any JSON value, and it is opaque. A reader:

- MUST preserve the key and its value unchanged through a read-modify-write. A
  `null` value stays `null`, and an empty array or object stays empty: the rule
  of Sections 3.1 and 8.1 that makes an absent container equal to an empty one
  does not apply to an application key or to anything inside its value.
- MUST NOT record a diagnostic for an application key, whatever its value.
- MUST NOT apply any rule of this format inside the value. A value under
  `points.` that would be unusable as a point is carried as written, and
  so is an `importance` outside 1 to 5.

The rule applies only at the top level of a document. On any other object this
format defines, except the opaque objects of Section 3.10, a key that contains
a full stop is an unrecognised key, and Section 6.2 applies to it. Inside an
opaque object a dotted key is carried like any other key. Information about one
object belongs in that object's `data` array (Section 5). A data record
attaches information to the object it sits on; an application key adds a new
kind of thing.

A writer MUST give every application key it writes a name that contains a full
stop, and MUST NOT write one application key twice. A key without a full stop
would be read back as an unrecognised key and dropped. Of a key written twice,
which member a reader keeps is its JSON parser's choice (Section 6.4).

### 3.10 Recognised keys

This section is the set of keys this format defines. A reader needs it to apply
Section 6.2's unrecognised-key rule, which cannot be implemented without it.

A key marked REQUIRED must be present for the object to exist. Requiring a key
does not require a non-empty value: `"walk": []` satisfies `walk`, and an empty
array is distinct from an absent one.

| Object | Required | Optional |
|---|---|---|
| document | `vnx` | `generator`, `created`, `folders`, `networks`, `plans`, `tracks`, `points`, `areas`, and every application key (Section 3.9) |
| folder | `id` | `parent`, `name`, `notes`, `hidden`, `data` |
| network | — | `name`, `notes`, `folder`, `hidden`, `pinned`, `nodes`, `arcs`, `highlights`, `points`, `areas`, `data` |
| arc, in a network or a plan | `from`, `to`, `pips` | `data` |
| highlight | `walk` | `name`, `hidden`, `pinned`, `data` |
| pass | `arc` | `from`, `to`, `turn`, `pin` |
| plan | `walk` | `name`, `notes`, `folder`, `hidden`, `pinned`, `style`, `nodes`, `arcs`, `data` |
| track | `segments` | `name`, `notes`, `folder`, `hidden`, `pinned`, `style`, `started`, `ended`, `data` |
| segment | `pips` | — |
| point, at the top level | `at` | `name`, `notes`, `folder`, `hidden`, `pinned`, `style`, `data` |
| point, in a network | `at` | `name`, `notes`, `hidden`, `pinned`, `style`, `data` |
| area, at the top level | `polygon` | `name`, `notes`, `folder`, `hidden`, `pinned`, `style`, `data` |
| area, in a network | `polygon` | `name`, `notes`, `hidden`, `pinned`, `style`, `data` |
| data record | `@type` | opaque; see below |

A node is a position (Section 4.1), not an object, and has no keys.

Notes on the table:

- `folder` names the folder an item sits in. It does not appear on a folder,
  which places itself with `parent`, and it does not appear on a network's
  `points` or `areas`, which their network already contains.
- `name`, `hidden` and `data` appear on every item, and `notes` on every item
  but a highlight, which is a lightweight container for data along its walk
  and has no notes or style of its own (Section 3.3.3). Apart from `data` on an
  arc, none of the four appears on any other object in the table. An
  object to which the table gives none of them is not an item. `hidden` therefore appears on a
  top-level point: Section 3.8 names the shape rather than the collection,
  and a top-level point and a network's point are one shape (Section 3.6).
- `pinned` appears on every item except a folder (Section 3.8).
- An arc carries no `name`, `notes`, `hidden` or `style`, and neither a pass nor
  a segment carries `hidden`.

#### Objects this section does not reach

Two kinds of object are opaque, and a reader MUST NOT apply Section 6.2's
unrecognised-key rule inside either:

- A data record, apart from its `@type` (Section 5.1).
- The value of an application key (Section 3.9).

A style object is opaque for the same reason. A reader MUST carry every key in a
style object through a read-modify-write, and MUST NOT report an unrecognised
one.

Opaque means unrecognised keys are not reported. It does not exempt a value from
a range this document states normatively: `importance` is an integer from 1 to 5
(Section 3.6), and a reader MUST apply that range to an `importance` in any
style object, including one this format does not otherwise document. A
non-integer `importance` is out of range. Every other style value is carried
untouched, because no other style key has a stated range.

A field named `importance` inside a data record body is not checked. Section
6.2's prohibition on inspecting a record body is the mechanism by which a record
of an unrecognised type survives a read-modify-write, and it wins. The same
holds inside an application key's value, where no rule of this format applies
(Section 3.9).

## 4. Values

### 4.1 Positions

A position is a JSON array of fixed positional channels:

```
[ lon, lat, ele, time_ms ]
```

- Coordinate order is longitude first, then latitude, as in [RFC 7946].
- `ele` is elevation in metres.
- `time_ms` is a time in Unix milliseconds (Section 4.2).

The following rules apply:

- A position MUST carry at least `lon` and `lat`.
- `lon` is a number from -180 to 180 and `lat` a number from -90 to 90, both
  bounds included. These ranges are normative.
- A position whose `lon` or `lat` is outside its range will not parse, as one
  that carries fewer than two channels, or a `lon` or `lat` that is not a
  number, will not. Section 6.2 states what a reader drops for a position that
  will not parse, for each place a position appears.
- `null` marks an absent value in any channel from index 2 (`ele`) upward.
  Absent elevation and absent time are both first-class states and MUST be
  expressible.
- Trailing channels MAY be omitted. Arity 2 through 4 carry the channels this
  version defines. Arity 2 is used for the positions of a walk (Section 3.3.3)
  and for area rings (Section 3.7). A node and an arc pip carry no time
  (Sections 3.3.1 and 3.3.2), and a walk position carries neither elevation nor
  time (Section 3.3.3).
- A position MAY carry more channels than this version defines. A reader MUST
  ignore them and MUST NOT report a diagnostic for them.
- Positions of differing arity MAY appear within one `pips` array, and a reader
  MUST read each position independently of its neighbours. One pip may carry an
  elevation and the next may not, which is ordinary in imported data.
- A writer MUST emit the shortest spelling of a position; a reader MUST accept
  any of them. `[lon, lat, null]` and `[lon, lat]` state the same thing, and a
  reader takes either.
- An interior absence keeps its `null`. `[lon, lat, null, t]` cannot be
  shortened, because dropping the `null` would move the time into the elevation
  channel.

#### Precision

This format does not constrain how precisely a writer states a coordinate. Any
binary64 value is a conforming one, and two writers handed the same position may
state it to different precision.

A writer should consider the cost of more precision than it needs. Six decimal
places of longitude and latitude is about 10 cm, well inside the error of a
consumer GPS receiver, and a document of long tracks can be halved by rounding
to it. [RFC 7946] Section 11.2 makes the same point about GeoJSON. This
paragraph is advice and carries no requirement.

A reader MUST NOT round. Decoding preserves the binary64 value the document
states.

### 4.2 Numbers

- Non-finite numbers are forbidden throughout a document. `NaN`, `Infinity` and
  `-Infinity` MUST NOT be written, and a document containing one MUST be
  refused. These are not JSON, but some language runtimes emit and accept them
  by default.
- Numbers are IEEE 754 binary64. Round-trip is defined at value level, not at
  byte level: a reader is not required to preserve the text of a number, only
  its value.
- `time_ms` is a count of Unix milliseconds. It is guaranteed to be below 2^53
  and therefore exact in an IEEE 754 binary64 and in JavaScript.
- A writer MUST NOT encode an absent time as a number, such as 0 or -1. Absent
  time is `null` (Section 4.1).

### 4.3 Timestamps

Record-level timestamps, `created`, `started`, `ended` and `pinned`, are ISO
8601 in UTC, with a literal `Z` suffix and OPTIONAL fractional seconds. An
offset other than `Z` is not permitted.

A record-level timestamp is checked for shape only. A reader MUST NOT refuse or
diagnose one on calendar grounds, even though ISO 8601 would:
`2026-13-45T99:99:99Z` matches the grammar this section states and is accepted.
Per-pip time is a count of epoch milliseconds (Section 4.1) and is not an ISO
8601 string.

### 4.4 Identifiers, keys and order

- Identifiers are scoped to the object that contains them, not to the document.
  A plan's `node-1` and a network's `node-1` are different nodes. A reader MUST
  NOT build one document-wide identifier map.
- Identifiers appear only where something references them: on folders, and as
  the keys of a network's `nodes` and `arcs` objects.
- Keys defined by this format are lower kebab-case. `@type` (Section 5) is the
  single exception. No key this format defines contains a full stop; an
  application key always does (Section 3.9).
- A writer SHOULD derive an identifier from the name of the object it
  identifies, where that object has one: lower kebab-case, about ten
  characters, with a numeric suffix for uniqueness. Where there is no name, a
  writer SHOULD use a sequential identifier such as `node-1` or `arc-1`.
  Readability is preferred to file size.
- Key order is not significant anywhere in a document, including inside data
  records. A reader MUST accept any key order.

Arrays MUST NOT be reordered. The order of an array is data,
and it is meaningful wherever an array appears in this format: the positions in
a `pips` array, the passes of a walk, the positions of a ring, and the segments
of a track. The order of positions in a line is the shape of the line.

## 5. Data records

Every item, and every arc, MAY carry a `data` key holding an array of records.
Each record is a JSON object carrying an `@type` key whose value is a
reverse-DNS identifier, plus arbitrary sibling fields. A record holds whatever
an application attaches to an item or arc, such as a tag, a note, a rating or a
routing preference. This format defines no record type. `@type` values are
compared exactly, including case.

```json
"data": [
  { "@type": "com.example.tag", "value": "day-walk" },
  { "@type": "com.example.checked", "value": true }
]
```

`@type` is the only key in this format carrying a sigil. The sigil marks the key
as structural rather than payload, and leaves the plain name `type` available to
any record that needs a field so named.

Note: the spelling is borrowed from JSON-LD, and a VNX document is not JSON-LD.
No `@context` is emitted, so a JSON-LD processor fails visibly rather than
misreading a VNX document.

### 5.1 Record rules

1. `data` is a bag, not a map. Several records of one type are supported, and
   three tags are three records. What a reader does with several records of one
   type is not defined by this format: a writer MAY emit as many records of a
   type as it means, and a reader MAY take all, one or none as its own semantics
   require.
2. A record body is an opaque JSON object apart from `@type`. A record body MAY
   carry a field named `type`.
3. A record of an unrecognised type is inert. It MUST NOT be treated as an
   error on any host, the item or arc whose `data` carries it, including a host
   on which the reader did not expect it.
4. A record of an unrecognised type MUST survive a read-modify-write at value
   level.
5. The meaning of a record is the pair of its type and its host, not the type
   alone. One type can mean one thing on a highlight and another on an area.
6. A type with a single primary payload names it `value`. A type with several
   fields keeps its own names, and MAY carry `value` alongside modifiers.

## 6. Reading

### 6.1 Content not carried

The following are not carried by this format:

- Cached distance, elevation gain, elevation loss, and bounds. All are derivable
  from geometry the reader has just been handed.
- Deleted items. A document carries what a user would see.
- Durable identity, map tiles, and the creation and modification timestamps
  of items.

How a consumer derives elevation gain and loss is its own business.

### 6.2 Error handling

A reader is tolerant of damage and silent about nothing.

- An unrecognised key on an object defined by this format: the reader MUST
  record a diagnostic, MUST skip the key, and MUST keep the object. An
  unrecognised key is not an unusable value. An application key (Section 3.9)
  is recognised, so a reader records no diagnostic for it. On any other object
  this format defines, except the opaque objects of Section 3.10, a key
  containing a full stop is unrecognised.
- An unusable value, such as a position that will not parse, a pass naming an
  arc that is absent from the document, a `polygon` carrying a ring beyond the
  first, or a record-level timestamp carrying an offset other than `Z`: the
  reader MUST drop the offending element, MUST record a diagnostic, and MUST
  continue.

  The unusable value in the polygon case is the whole `polygon`, not the extra
  ring: Section 3.7 forbids silently keeping the outer ring, so the unusable
  extent is the entire ring set.
- A value of the wrong JSON type is an unusable value. Where the key is
  OPTIONAL (Section 3.10), the reader MUST drop the field and MUST keep the
  object. Where the key is REQUIRED, the reader MUST drop the object, because
  it cannot exist without it.

  The two cases take different codes and are not one diagnostic with two
  remedies. "A field is gone" and "the object is gone" are materially different
  outcomes for whoever holds the file, so a caller must be able to tell them
  apart without parsing English.

  Section 5.1's rule for a record with an unusable `@type` is the same rule: the
  record goes, not the key.
- A value outside a range this document states normatively, such as an
  `importance` outside 1 to 5 (Section 3.6), is an unusable value in an optional
  key, so the reader MUST drop the field and MUST keep the item.

  A reader checks a value only where this document states a normative range or
  enumeration for it. A record body is opaque apart from `@type` (Section 5.1),
  and a reader MUST NOT check the type, range or enumeration of a field inside
  one.
- A refusal. A plain document is refused for exactly four causes, and a reader
  MUST NOT refuse one for any other:

  | Cause | Section |
  |---|---|
  | the top level is not a JSON object, or carries no `vnx` key | 2.2 |
  | `vnx` is not three digit-runs separated by full stops | 2.2 |
  | `vnx` names a MAJOR or, through 0.x, a MINOR series the reader does not support | 2.2 |
  | a number in the document is not finite | 4.2 |

  Section 7 states the causes for which a container is refused. They are
  separate, and a fault in an attachment is not among them.

Every problem the table below names is reported as a diagnostic with a code,
and handled by a remedy local to the element it concerns. None of them is a
refusal. Each row's code is the one a reader MUST put on that diagnostic.

A position that will not parse drops the object that owns it rather than the
position, and the table says which object that is. A missing vertex would leave
a shorter, plausible, wrong line with nothing to indicate it. A track's owner
is the segment rather than the whole track, because Section 3.5 already makes
the interval between two segments a pause rather than geometry.

A walk is used whole or not at all: any problem in a walk drops the highlight that
carries it. A walk with one pass removed is a different walk, not a shorter one.
Removing a pass from the middle joins two arcs that do not meet. Removing only a
`turn` reverses the direction of everything after it. Removing only a `from`
position moves the start of the walk to a node, which is a guess. Keeping the
rest of the highlight would therefore report a journey nobody drew. The diagnostic
names the pass and the rule, so the cause is known.

The remedies for the constructs this document constrains:

| Construct | Remedy | Code |
|---|---|---|
| an unrecognised key (3.10) | skip the key | `unknown-key` |
| a value of the wrong JSON type in an OPTIONAL key (3.10) | drop the field | `wrong-type-optional` |
| a value of the wrong JSON type in a REQUIRED key, or a REQUIRED key that is absent (3.10) | drop the object | `wrong-type-required` |
| a data record with no `@type`, or one that is not a string (5) | drop the record | `wrong-type-required` |
| a record-level timestamp with an offset other than `Z` (4.3) | drop the field | `timestamp-not-utc` |
| a position that will not parse, as a node (3.3.1) | drop the node | `position-unusable` |
| a position that will not parse, as a point's `at` (3.6) | drop the point | `position-unusable` |
| a position that will not parse, inside an arc's `pips` (3.3.2) | drop the arc | `position-unusable` |
| a position that will not parse, inside a track segment's `pips` (3.5) | drop the segment | `position-unusable` |
| a position that will not parse, inside a `polygon` ring (3.7) | drop the area | `position-unusable` |
| a `from`, `to`, `turn` or `pin` that is neither a node key where one is allowed nor a position that parses (3.3.3) | drop the highlight | `position-unusable` |
| a time on a node (3.3.1), or an elevation or a time on a position in a walk (3.3.3) | drop the channel | `extra-channel` |
| a time on an arc pip (3.3.2) | drop the time channel, from every pip of the arc that carries one | `arc-pip-time` |
| an arc whose `from` and `to` name the same node (3.3.2) | drop the arc | `self-loop-arc` |
| an arc whose `from` or `to` names a node that is not in its network or plan (3.3.2) | drop the arc | `arc-node-absent` |
| a pass that is not an object, or that names no arc of its network or plan (3.3.3) | drop the highlight | `pass-arc-absent` |
| a walk that breaks the rule for `from` and `to` (3.3.3) | drop the highlight | `walk-spelling` |
| a walk that is not connected: valid-walk rules 3, 4 and 5 (3.3.3) | drop the highlight | `walk-not-connected` |
| a pass carrying both `turn` and `pin` (3.3.3) | drop the highlight | `pinned-turn` |
| a position, a `turn` or a `pin` in a plan's walk (3.4) | drop the plan | `plan-pin-or-position` |
| a `polygon` carrying a ring beyond the first (3.7) | drop the area | `polygon-nested-rings` |
| a ring whose last position repeats its first (3.7) | drop the last position | `ring-closed` |
| a ring with fewer than three distinct positions (3.7) | drop the area | `ring-too-few-points` |
| an attachment that cannot be recovered (7) | drop the attachment | `attachment-unreadable` |

In a plan, "the highlight" in this table is the plan itself (Section 3.4).

A node dropped by a row above leaves every arc that names it naming an absent
node, and that arc is then dropped under `arc-node-absent`. An arc dropped by
any row above leaves every highlight whose walk names it with a
pass that names no arc of its network, and that highlight is then dropped under
`pass-arc-absent`. Each is its own diagnostic: the first says the arc is gone,
the second that the highlight is.

A ring that repeats its first position and holds too few distinct positions
takes both `ring-closed` and `ring-too-few-points`, at their two paths.

**The order in which a walk is checked.** A reader checks the passes of a walk
in order, and each pass in the order of the list below. At the first check that
drops the highlight, it records that diagnostic and examines nothing further in the
walk. Diagnostics that do not drop the highlight are recorded as they are met.

1. The pass is an object and its `arc` names an arc of its network or plan.
   Otherwise `pass-arc-absent`, at the pass.
2. Each unrecognised key on the pass: `unknown-key`, at the key.
3. `from`, `to`, `turn` and `pin`, in that order, where present: each parses.
   Otherwise `position-unusable`, at that key. A position carrying an elevation
   or a time takes `extra-channel`, at that key.
4. In a plan: `from`, `to`, `turn` and `pin`, in that order, where present,
   carry no position, turn or pin. Otherwise `plan-pin-or-position`, at that
   key.
5. The pass does not carry both `turn` and `pin`. Otherwise `pinned-turn`, at
   the pass.
6. The rule for `from` and `to`: first `from`, then `to`. A key the rule
   requires and the pass lacks takes `walk-spelling` at the pass; a key the pass
   carries and the rule forbids takes it at that key.
7. The walk is connected. In this order: a `from` node that is not an end of
   the pass's arc takes `walk-not-connected` at `from`; a pass after the first
   whose arc does not touch the node at which the previous pass left takes it at
   the pass; a `to` node that is not an end of the arc, or a `to` position on a
   pass other than the last, takes it at `to`.

A `folder` naming a folder that is not present in the document is not an
unusable value. The reader MUST treat the key as absent, so the item sits at the
top level, and MUST record a diagnostic. Dropping the item would lose content
over a lost grouping.

A folder's `parent` naming a folder that is not present takes the same remedy:
the reader MUST treat the key as absent, so the folder sits at the root, and
MUST record a diagnostic.

A folder with no `id` is dropped under the rule above. Items that named it are
then covered by the previous paragraph and sit at the top level.

Neither tolerance nor refusal may be silent.

### 6.3 Diagnostics

A reader MUST return a list of diagnostics alongside the document it read. A
reader that returns a document alone has discarded the only evidence that
anything was wrong.

Each diagnostic carries:

| Field | Value |
|---|---|
| `path` | a JSON Pointer ([RFC 6901]) locating the element the diagnostic is about |
| | for a fault in an archive entry rather than in the document: `#/entries/<name>`, with the member name escaped per [RFC 6901] |
| `code` | a short stable token naming which diagnostic this is, or absent |
| `message` | a short description in English |

A diagnostic that a conformance corpus case pins MUST carry a `code`, and the
code is what the case compares. The English MUST NOT be compared: exact wording
is a reader's own business.

A reader MAY leave `code` absent on any diagnostic no corpus case pins.

Three rules place a diagnostic, and they cover three different kinds of
diagnostic rather than one rule with two exceptions.

1. Where something is unusable, `path` names the smallest element no part of
   which could be used. That is not always the element the remedy drops: a
   `turn` that will not parse names the `turn` while the highlight is dropped around
   it, and a polygon with nested rings names the polygon while the area is
   dropped around it. A ring's repeated closing position is named by its index
   in the ring as written.

   A pass naming an absent arc and a `turn` that will not parse land at
   different depths: when the arc is absent nothing about the pass resolves, so
   the pass is named; when the `turn` will not parse, the arc and the rest of
   the pass still resolve, so only `turn` is.
2. Where the diagnostic is about a relationship between parts that are each
   usable, `path` names the part that breaks it. A pass that does not continue
   from the node where the previous pass left names the later pass, not the one
   it failed to continue from. A pass carrying both `turn` and `pin` names
   the pass. A key the rule for `from` and `to` forbids names that key, and a
   key it requires names the pass that lacks it.
3. Where the diagnostic reports channels dropped across one element, it names
   that element. `arc-pip-time` names the arc's `pips` array, once for the arc,
   however many pips carried a time. `extra-channel` names the node, or the
   `from`, `to`, `turn` or `pin` key, that carried the channel.

One invariant holds across all three. No two diagnostics about one document may
share both a `path` and a `code`. Those are the only two fields a conforming
runner may compare, so a reader that emits two reports differing in neither has
emitted two a runner cannot tell apart.

An archive entry is not part of the document, so a diagnostic about one cannot
be located by a pointer into the document. It uses the separate space
`#/entries/<name>`, not a path under any document key.

A diagnostic carries no severity. Every diagnostic this format defines reports
the same kind of fact, that something in the document could not be used as
written, so there is nothing for a severity to rank.

An empty list means the document was read cleanly. Each diagnostic in a
non-empty list reports one thing the reader could not use as written: a key it
skipped, an element it dropped, or a relationship it could not resolve.

The order in which a reader emits diagnostics is not significant, and a
conformance runner MUST compare a case's reported diagnostics against its
expected diagnostics as a set. A reader that resolves cross-references in a
second pass emits them in a different order from a reader that does not, and
both are correct.

A refusal (Section 6.2) is not a diagnostic, and a reader MUST report it through
a separate channel, so that a caller can distinguish a document that was refused
from one that was read with elements dropped.

### 6.4 Duplicate object keys

A reader is not required to detect two entries with the same key in one JSON
object, for example two `node-1` entries in one `nodes` object, and MUST NOT
report a diagnostic for one. The rules of this format apply to the object as
the reader's JSON parser delivered it. The corpus carries a case for this
(Section 8) which expects no diagnostic.

Note: a JSON parser resolves a repeated key before any rule of this format is
applied, and in every mainstream parser the last entry wins, so the earlier node
disappears and a reader built on such a parser cannot observe it.

## 7. The VNZ container

A VNZ file is a ZIP archive carrying a VNX document and its attachments. This
section states what a reader accepts. Section 7.1 states what a writer emits,
which is narrower in every case: an export stays readable by the widest range of
tools, while an archive from anywhere else is still readable by every conforming
implementation.

- Root document: a reader MUST prefer an entry named `doc.vnx` at the archive
  root. Where there is none, the reader takes the first entry with a `.vnx`
  extension in archive order. Names are compared case-insensitively.
- The root document is selected by the two rules above, and every other entry is
  an attachment whatever its name ends in. A reader MUST NOT decide what is
  payload by looking at a file extension: a reader that sniffs extensions
  diverges silently from one that does not.
- Attachment paths MUST be relative and forward-slash separated. An absolute
  path, a path carrying a drive letter, or a path with a `..` segment MUST be
  refused.
- An entry name containing a backslash anywhere MUST be refused. That check
  stands alone and applies before the `..` rule, so `..\x` is refused for the
  simple reason and the question of whether a backslash separates path segments
  never arises. This is deliberately stricter than [APPNOTE], which permits the
  character. `..` is a path segment, not a substring: `a..b/photo.jpg` is a
  legal name and escapes nothing.
- Duplicate entry names MUST be refused, compared case-insensitively. Language
  runtimes resolve a repeated name differently, so one archive would yield
  different documents in different languages, and that applies to `doc.vnx`
  beside `DOC.VNX` exactly as it applies to an exact repeat, which is why the
  comparison matches the root-document rule above.
- A reader MUST accept stored and deflate. Other methods are not available in
  every target language. An entry compressed by another method is unreadable,
  and the remedy depends on which entry it is: if it is the root document, the
  reader MUST refuse the archive, because there is no document to return; if it
  is an attachment, the reader MUST drop that attachment, MUST record a
  diagnostic, and MUST return the document. Refusing a whole archive because one
  attachment used an unusual method would be disproportionate.
- An attachment whose compression method is one of the two above but whose bytes
  cannot be recovered — a stream that will not decompress, or one whose CRC
  disagrees with the bytes present — takes the same remedy: the reader MUST drop
  that attachment, MUST record a diagnostic, and MUST return the document. The
  same proportion applies, and the document is unaffected by the damage. A reader
  MUST NOT refuse the archive on this ground, and MUST NOT return the attachment
  with empty or partial content, which would be indistinguishable from an
  attachment the archive never carried. Where the ROOT DOCUMENT cannot be
  recovered this way, the reader MUST refuse the archive, as above.

  Note: a reader is not required to detect the damage. Section 7's rule on an
  over-declared size stands: this rule governs what a reader does with damage it
  has observed, not how hard it looks.
- A reader MUST read an archive that uses the ZIP64 extensions of [APPNOTE].
- A reader MUST accept an archive whose entries defer their sizes to a data
  descriptor, and MUST take the sizes from the central directory rather than
  from a local header.
- A reader MUST NOT refuse an entry on the ground that a declared size exceeds
  the bytes present, where the CRC over those bytes agrees. This covers either
  size field: the compressed size, the uncompressed size, or both. A reader is
  not required to detect such a discrepancy. The rule constrains what a reader
  does with one it has observed, so a reader that trusts a declared size and
  never notices a shortfall has nothing to do here.

- A reader MUST NOT refuse an entry on the ground that its two declared sizes
  disagree with each other, where the CRC over the bytes present agrees. For a
  stored entry the two are equal in a well-formed archive, so a disagreement is
  a defect in the archive — but it is a defect in a size field, and the CRC
  settles what the entry contains without reference to either. A reader MAY
  record a diagnostic; it MUST NOT refuse, and MUST NOT drop the entry.

Those last three rules are the same point three times: a size field is not the
authority on what an entry contains, and the checksum is. They are stated
because silence is what produces the question, and an implementer resolving it
alone hardens until they refuse, believing they have improved robustness while
breaking conformance.

Note: an implementation matching the root document on the base name alone
matches at any depth and lets the last match win rather than the first. The
match is on the full entry name.

### 7.1 What a writer emits

A writer emits an archive every conforming reader can read. Five rules do that,
and nothing else about the archive is constrained.

- The root document MUST be an entry named `doc.vnx` at the archive root, and
  MUST be the first entry in the archive. Section 7's fallback takes the first
  `.vnx` entry in archive order, so writing the document first makes the
  fallback and the preferred-name rule agree.
- The UTF-8 filename flag MUST be set on every entry.
- Entry names MUST be unique, compared case-insensitively. Section 7 refuses an
  archive that repeats one.
- An attachment path MUST be relative and forward-slash separated, MUST NOT
  contain a backslash, and MUST NOT contain a `..` segment. Section 7 refuses an
  archive that carries one.
- Every entry MUST be stored or deflated. A writer MAY choose either, per entry.

Every other field of the archive is the writer's own business. Two conforming
writers are not expected to produce the same bytes for the same document.

### 7.2 Where an application puts its files

A container MAY carry any number of attachments, of any kind, beside the root
document: photographs, sound, documents, or anything else an application
attaches. This format does not define what an attachment means. An application
that needs one names it from its own content, usually from a field of a data
record (Section 5) or from inside the value of an application key
(Section 3.9), by its full entry name.

An application that writes the files of a data type SHOULD put them in a folder
at the archive root whose name is that type's `@type` identifier, exactly as
written. The files of an application key SHOULD go in a folder named after the
key in the same way. Each identifier is chosen by the owner of a domain, so two
applications that write into one archive cannot write the same entry name.

```
doc.vnx
com.example.photo/summit.jpg
com.example.photo/descent.jpg
org.example.audio/ridge-notes.m4a
```

That is a recommendation about names, not a rule a reader applies. A reader
MUST treat every attachment alike whatever folder it is in: a folder named after
a type the reader does not know, a folder no record names, and an attachment at
the archive root are all returned as attachments, with no diagnostic. A full
stop in a folder name is an ordinary character in a path.

## 8. Conformance corpus

The corpus distributed with this specification is what decides whether an
implementation conforms. Every implementation runs the same files.

The `write/` tier is the only one that exercises the emit-boundary rule of
Section 4.1, the shortest spelling of a position. Every other tier re-encodes
what it decoded, which applies the rule to values that already satisfy it, so a
defect in it is unreachable without this tier. A runner MUST assert that a
case's input is not already the written form, and that the written form is a
fixed point of the writer. Without those two guards a writer that echoed its
input would pass the tier.

The tiers, and what each requires:

| Directory | Requirement |
|---|---|
| `accept/` | MUST be read, with no diagnostics |
| `reject/` | MUST be refused, and the refusal MUST name its cause |
| `diagnose/` | MUST be read, reporting exactly the listed diagnostics |
| `write/` | a document, and the document a writer MUST produce from it |

### 8.1 Sidecars and comparison

Each case carries a `foo.expect.json` sidecar holding its expected result, as
plain JSON beside the file rather than inside a test in any one language. A file
with no sidecar is a failure, not a skip.

A diagnostic is matched on its `path` and its `code` (Section 6.3). Its English
is advisory and MUST NOT be compared. A refusal is matched as a substring of its
message, which is the one place this corpus compares English.

For every file in `accept/`, a runner MUST decode it, encode the decoded
document, and compare the tree parsed from the original file against the tree
parsed from the encoded output. It MUST NOT be reduced to a comparison of one
decoded model against another: a reader that discarded every data record, every
application key, every style and every `notes` field produces a model equal to
itself. Two normalisations are permitted and no others — trailing `null` channels
dropped from a position, and an absent container equal to an empty one. The
second does not apply to an application key or inside its value, which the
comparison matches exactly (Section 3.9). Files in
`reject/` and `diagnose/` are not subject to this check.

The comparison is over trees because the bytes of a document are not
constrained: key order, number spelling and whitespace are the writer's own
business.

`corpus/README.md` is the runner contract: which cases exist, what each sidecar
field means, and how to add one. It is normative for a runner and is kept in the
corpus so that it cannot drift from the files it describes.

## 9. Security considerations

A VNZ container is a ZIP archive, and extracting one carries the usual hazards.
The path rules in Section 7 are a security requirement, not a tidiness
requirement: an attachment path containing `..` or an absolute path can direct
an extractor to write outside the destination directory. An implementation that
reads a container without extracting it is not exposed to this, and an
implementation that extracts attachments is.

A reader that accepts documents from an untrusted source SHOULD bound the
resources one document may consume. The format places no limit on the number of
items, the number of pips in an arc or track, the depth of the folder tree, or
the size of an opaque data record.

## 10. References

- [APPNOTE] PKWARE, Inc., ".ZIP File Format Specification", APPNOTE.TXT,
  version 6.3.10, November 2022.
- [RFC 2119] Bradner, S., "Key words for use in RFCs to Indicate Requirement
  Levels", BCP 14, RFC 2119, March 1997.
- [RFC 6839] Hansen, T. and A. Melnikov, "Additional Media Type Structured
  Syntax Suffixes", RFC 6839, January 2013.
- [RFC 6901] Bryan, P., Zyp, K. and M. Nottingham, "JavaScript Object Notation
  (JSON) Pointer", RFC 6901, April 2013.
- [RFC 7946] Butler, H., Daly, M., Doyle, A., Gillies, S., Hagen, S. and
  T. Schaub, "The GeoJSON Format", RFC 7946, August 2016.
- [RFC 8174] Leiba, B., "Ambiguity of Uppercase vs Lowercase in RFC 2119 Key
  Words", BCP 14, RFC 8174, May 2017.
- [RFC 8259] Bray, T., "The JavaScript Object Notation (JSON) Data Interchange
  Format", STD 90, RFC 8259, December 2017.

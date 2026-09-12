# VNX Wire Format Specification

Version 0.1.0

## Abstract

VNX (Viatrail Network eXchange) is a JSON interchange format for trail data. It
carries folders, networks, routes, tracks, waypoints and areas. A network is a
graph of nodes and arcs together with chains, which are ordered traversals of
those arcs; two chains that follow the same trail reference the same arc.

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

- A **document** is a JSON object carrying a `vnx` version key and up to seven
  collections (Section 3.1).
- An **item** is an object that can carry `name`, `notes`, `hidden` and `data`
  (Section 3.10). An item is top-level when it is a member of one of the
  document's collections; every other item sits inside a top-level one.
- A **position** is a JSON array of channels: longitude, latitude, and
  optionally elevation and time (Section 4.1).
- A **pip** is one position in the `pips` array of an arc or of a track segment
  (Sections 3.3.2 and 3.5).
- A **folder** is a named grouping with an `id`. A top-level item other than a
  folder places itself in a folder by naming that `id` in its `folder` key; a
  folder places itself in another folder with `parent` (Section 3.2).
- A **network** is a graph of nodes and arcs, together with the chains that
  traverse those arcs and the points and areas that belong to the network
  (Section 3.3).
- A **node** is a position in a network, keyed by an identifier, at which arcs
  start and end (Section 3.3.1).
- An **arc** is a polyline carried as a `pips` array. In a network an arc runs
  between the two nodes its `from` and `to` name, and its pips include both node
  positions (Section 3.3.2); in a route an arc names no nodes and has no
  identifier (Section 3.4).
- A **link** is one step of a chain: the arc it follows and the two endpoints
  between which it follows that arc, each either a node key or an inline
  position (Section 3.3.3).
- A **chain** is an ordered array of links forming one connected walk along a
  network's arcs; the order is the direction of travel, and a chain does not own
  the arcs it follows (Section 3.3.3).
- A **route** is a top-level item that owns an ordered array of arcs, where
  consecutive arcs share an endpoint position (Section 3.4).
- A **segment** is an ordered `pips` array within a track; the interval between
  two segments is a pause (Section 3.5).
- A **track** is a top-level item carrying an ordered array of segments
  (Section 3.5).
- A **waypoint** is a top-level item carrying one position, `at`; a network's
  points have the same shape (Section 3.6).
- An **area** is an item carrying one closed ring of positions, `polygon`,
  either at the top level or within a network (Section 3.7).
- A **ruleset** is one entry of the top-level `rulesets` array, opaque in this
  version (Section 3.9).
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
  "vnx": "0.1.0",
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
            [-3.6712, 57.118, 340],
            [-3.669, 57.1131, 388.2],
            [-3.6605, 57.1042, 512.5]
          ]
        }
      },
      "chains": [
        {
          "name": "Chalamain Gap",
          "style": { "color": "#c1440e", "width": 4 },
          "links": [
            { "arc": "arc-1", "from": "node-1", "to": "node-2" }
          ]
        }
      ]
    }
  ],
  "waypoints": [
    {
      "folder": "cairngorms",
      "name": "Car park",
      "at": [-3.675, 57.1201, 320],
      "data": [
        { "@type": "app.viatrail.tag", "value": "parking" }
      ]
    }
  ]
}
```

`vnx` is the format version (Section 2.2); `generator` and `created` are
informational (Section 3.1). The one folder, `cairngorms`, is named by the
`folder` key of the network and of the waypoint (Section 3.2). The network
holds two nodes, one arc whose `pips` begin at the position of `node-1` and end
at the position of `node-2`, and one chain whose single link follows that arc
from `node-1` to `node-2` (Section 3.3). The waypoint carries its position in
`at` and one data record of type `app.viatrail.tag` (Sections 3.6 and 5).

## 2. Document identification

### 2.1 Names and media types

| Property | Value |
|---|---|
| Format name | Viatrail Network eXchange |
| Plain document | `.vnx`, `application/vnd.viatrail.vnx+json` |
| Container | `.vnz`, `application/vnd.viatrail.vnz+zip` |
| Version key | `vnx` |
| Current version | `0.1.0` |

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
rule against a leading zero: `00.01.0000` is a legal spelling of 0.1.0, and a
reader compares the numeric values rather than the text.

A version string that is not three digit-runs separated by full stops MUST be
refused, and MUST NOT be coerced. `"0.1"`, `"0.1.0.0"`, `"0.1.x"` and `"0.1.+1"`
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
  component. A 0.1.0 reader accepts 0.1.3. It refuses 0.2.0, and it refuses
  0.0.9 unless it declares support for the 0.0 series. A lower minor is refused
  for the same reason as a higher one.

From version 1.0 onward, minor versions are additive: a reader MUST accept any
minor version at or above its own and MUST ignore constructs it does not
recognise, and only a MAJOR increment is a refusal. The rule for PATCH does not
change.

A reader that re-encodes a document MUST write the version the document
declared, not the reader's own. A 0.1.0 reader that reads a 0.1.3 document and
writes it back writes `0.1.3`.

## 3. Document structure

Section 3.10 is the normative key set. The JSON below is illustrative, as
Section 1.3 says of every example.

### 3.1 Envelope

A VNX document is a JSON object.

```json
{
  "vnx": "0.1.0",
  "profile": "full",
  "generator": "Viatrail 0.1.93",
  "created": "2026-08-31T06:45:12Z",

  "folders":   [ ],
  "networks":  [ ],
  "routes":    [ ],
  "tracks":    [ ],
  "waypoints": [ ],
  "areas":     [ ],
  "rulesets":  [ ]
}
```

Every key is OPTIONAL except the ones its object requires, which Section 3.10
lists. A reader MUST accept the absence of every other key.

Absence means "not known". It MUST NOT be read as a default value, with two
exceptions stated where they arise: `label-visible`, where absence means Auto
(Section 3.6), and `hidden`, where absence means visible (Section 3.8).

All seven collections are OPTIONAL. An absent collection and an empty one carry
the same content, and a reader MUST treat them alike when reading. That holds
wherever a container appears in this format: an empty `data` bag, an empty
`links` array and an empty `nodes` object each carry the same content as the
key being absent.

`created` and `generator` are informational. `created` is a timestamp
(Section 4.3).

`generator` names the software that wrote the document.

- An application writes its own name and version, for example
  `"Viatrail 0.1.1498"`. The version is supplied by the caller requesting the
  export. An export request that carries no version is refused rather than
  written with a placeholder version. That is a requirement on an application's
  export path, not on a library: a library has no export request.
- A development or command-line tool names itself, and MUST NOT name itself as
  the application.

`generator` carries the application version. The format version is the value of
`vnx` (Section 2.2). The two are unrelated.

`profile` selects which data records are present; see Section 5.3.

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
because chains reference their entries by key.

```json
{
  "folder": "approaches",
  "name": "Lairig Ghru approaches",
  "notes": "...",
  "nodes": { "node-1": [-3.6521, 57.1000, 940] },
  "arcs":  { "arc-1": { "from": "node-1", "to": "node-2", "pips": [ ], "data": [ ] } },
  "chains": [ ],
  "points": [ ],
  "areas":  [ ],
  "data":   [ ]
}
```

#### 3.3.1 Nodes

A node is a position (Section 4.1) keyed by an identifier. Node identifiers are
scoped to the containing network (Section 4.4).

#### 3.3.2 Arcs

An arc carries `from` and `to`, each naming a node in the same network, and
`pips`, an array of positions.

An arc's `pips` array is its complete point list, from its first point to its
last, and includes the positions of both endpoint nodes. A reader MUST NOT
prepend or append node positions to it.

The first pip states the position of the node `from` names, and the last states
the position of the node `to` names. Where a terminal pip and its node
disagree, the pips are authoritative: the two are one fact written twice, and
the geometry lives in the pips.

A reader MUST compare each terminal pip against its node's position and MUST
record a diagnostic where they are further apart than the projection tolerance
of Section 3.3.3, keeping both.

Note: an implementation whose internal storage holds only the interior points
of an arc adds the endpoint positions when writing and removes them when
reading.

#### 3.3.3 Chains

A chain traverses arcs that it does not own.

```json
{
  "name": "Chalamain Gap",
  "style": { "color": "#c1440e", "width": 4, "opacity": 0.9, "line-style": "solid" },
  "links": [
    { "arc": "arc-1", "from": "node-1", "to": "node-2" },
    { "arc": "arc-2", "from": "node-2", "to": [-3.6521, 57.1000] }
  ],
  "data": [ ]
}
```

`links` is ordered, and the order is the direction of travel. Each link names an
arc in the same network and gives two endpoints.

A link endpoint is either a node key, given as a string, or an inline position,
given as an array. An inline position identifies a point on the named arc at
which the chain starts, ends or reverses.

An inline position MUST lie on the named arc. A reader resolves it by projecting
it onto the arc's polyline; elevation is taken from the polyline and is never
stored on the endpoint. The projection tolerance is 50 metres, measured as
follows.

Every coordinate, the inline position and every vertex of the arc's `pips`, is
projected to metres by a local equirectangular map about the latitude of the
inline position, using an Earth radius of 6371008.8 m:

```
x = radians(lon) * 6371008.8 * cos(radians(lat_of_the_inline_position))
y = radians(lat) * 6371008.8
```

The cosine term uses that one latitude for every coordinate, which keeps the map
affine, so a straight polyline segment stays straight and the plane geometry is
exact. The distance is then the least distance from the projected position to
any projected segment of the polyline, each segment measured as the distance to
the nearest point on that segment.

A polyline of one vertex is that point. A polyline of no vertices has no
distance: the reader MUST record a diagnostic and drop the link rather than
invent a number.

A position at or within 50 metres of its arc is accepted: a distance equal to
the tolerance is within it. No conformance case pins that boundary, because a
distance at exactly the threshold is not reachable by a fixture. This sentence
is the whole statement of the rule.

An inline position that does not project onto its arc within that tolerance is
an unusable value and is handled under Section 6.2.

A chain's links MUST form one connected walk: the point at which link *k* ends
MUST be the point at which link *k+1* begins. Where they do not, a reader keeps
both links and reports the break (Section 6.2).

Two endpoints meet when their longitude and latitude are equal, and no other
channel takes part in the comparison. An inline endpoint carries no elevation
because elevation is resolved from the polyline, while a node may carry one, so
a comparison over all channels would report every junction between a node and
an inline endpoint as broken.

A node key is resolved to its position before comparing. Two distinct node keys
at the same position therefore meet. Key equality is a valid fast path.

A link that was dropped under Section 6.2 takes no part in the comparison on
either side. The links either side of it are not thereby adjacent, and a reader
MUST NOT report a break across the gap.

Two links in one chain MAY share an endpoint position, which is how a reversal
is expressed. Inline positions carry no identity, so:

- A writer MUST emit value-equal coordinates for a shared endpoint.
- A reader MUST treat value-equal inline positions on the same arc as the same
  endpoint.

Without both rules, a continuity check that compares endpoints refuses walks
that are in fact continuous.

### 3.4 Routes

A top-level route owns its arcs. Nothing else can reference them. A route
therefore carries no `nodes` object, no `links`, and its arcs carry no
identifiers.

```json
{
  "folder": "cairngorms",
  "name": "Braeriach direct",
  "style": { },
  "arcs": [
    { "pips": [ ], "data": [ ] },
    { "pips": [ ], "data": [ ] }
  ],
  "data": [ ]
}
```

`arcs` is ordered. Node identity is the shared endpoint between arc *k* and arc
*k+1*, which is present in both pip lists. Direction is pip order. Arc
boundaries are preserved by the array, so a route that is partly hand-drawn and
partly engine-routed retains that structure.

An arc whose two endpoints are the same node is legal and needs no special
handling.

Top-level areas are defined in Section 3.7 and appear in the top-level `areas`
collection, not inside a route.

### 3.5 Tracks

```json
{
  "folder": "cairngorms",
  "name": "Lairig Ghru, 12 May",
  "started": "2026-05-12T07:03:11Z",
  "ended":   "2026-05-12T16:41:02Z",
  "style":   { "color": "#6a4c93", "width": 3 },
  "segments": [ { "pips": [ [-3.6521, 57.1, 940, 1778914991000] ] } ],
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

### 3.6 Waypoints and points

Top-level `waypoints` and a network's `points` share one shape.

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

Note: the values 16, 21, 27, 35 and 46 are the ones an application emits in
practice; that does not constrain a document.

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
  "polygon": [ [-3.65, 57.1], [-3.64, 57.1], [-3.64, 57.11], [-3.65, 57.1] ],
  "style": { "fill-color": "#1e6091", "fill-opacity": 0.25, "stroke-color": "#1e6091", "stroke-width": 2 },
  "data": [ ]
}
```

`polygon` is one closed ring: an array of at least four positions whose first
and last positions are equal. Equal means equal in longitude and latitude; no
other channel takes part, for the same reason as Section 3.3.3's endpoints.
Holes and multiple rings are not expressible in this format.

A reader that encounters a nested or additional ring MUST report a diagnostic
and MUST drop the area. It MUST NOT silently keep the outer ring, and MUST NOT
merge the rings.

### 3.8 Hidden state

`hidden` is a boolean carried on folders, networks, chains, routes, points,
areas and tracks. It is user-visible state rather than a rendering cache.

Absence means visible. A writer MUST emit the key only when its value is `true`,
and a document SHOULD NOT contain `"hidden": false`.

### 3.9 Rulesets

Rulesets are not defined in version 0.1.

A document MAY carry a `rulesets` key, whose value is an array. Its contents are
opaque, but the value itself has a shape, and a value of the wrong JSON type is
handled under Section 6.2 like any other. A 0.1.x reader MUST treat its contents
as opaque and MUST preserve them through a read-modify-write, under the same
rules as an unknown data record (Section 5.1).

### 3.10 Recognised keys

This section is the set of keys this format defines. A reader needs it to apply
Section 6.2's unrecognised-key rule, which cannot be implemented without it.

A key marked REQUIRED must be present for the object to exist. Requiring a key
does not require a non-empty value: `"links": []` satisfies `links`, and an
empty array is distinct from an absent one.

| Object | Required | Optional |
|---|---|---|
| document | `vnx` | `profile`, `generator`, `created`, `folders`, `networks`, `routes`, `tracks`, `waypoints`, `areas`, `rulesets` |
| folder | `id` | `parent`, `name`, `notes`, `hidden`, `data` |
| network | — | `name`, `notes`, `folder`, `hidden`, `nodes`, `arcs`, `chains`, `points`, `areas`, `data` |
| arc, in a network | `from`, `to`, `pips` | `data` |
| chain | `links` | `name`, `notes`, `hidden`, `style`, `data` |
| link | `arc`, `from`, `to` | — |
| route | `arcs` | `name`, `notes`, `folder`, `hidden`, `style`, `data` |
| arc, in a route | `pips` | `data` |
| track | `segments` | `name`, `notes`, `folder`, `hidden`, `style`, `started`, `ended`, `data` |
| segment | `pips` | — |
| waypoint | `at` | `name`, `notes`, `folder`, `hidden`, `style`, `data` |
| point, in a network | `at` | `name`, `notes`, `hidden`, `style`, `data` |
| area, at the top level | `polygon` | `name`, `notes`, `folder`, `hidden`, `style`, `data` |
| area, in a network | `polygon` | `name`, `notes`, `hidden`, `style`, `data` |
| data record | `@type` | opaque; see below |

A node is a position (Section 4.1), not an object, and has no keys.

Three notes on the table:

- `folder` names the folder an item sits in. It does not appear on a folder,
  which places itself with `parent`, and it does not appear on a network's
  `points` or `areas`, which their network already contains.
- `name`, `notes`, `hidden` and `data` appear together on every item and, apart
  from `data` on an arc, on no other object in the table. An object to which
  the table gives none of them is not an item. `hidden` therefore appears on a
  top-level waypoint: Section 3.8 names the shape rather than the collection,
  and a waypoint and a network's point are one shape (Section 3.6).
- An arc carries no `name`, `notes`, `hidden` or `style`, and neither a link nor
  a segment carries `hidden`. A per-arc style is a plausible future addition and
  is not part of version 0.1.

#### Objects this section does not reach

Two kinds of object are opaque, and a reader MUST NOT apply Section 6.2's
unrecognised-key rule inside either:

- A data record, apart from its `@type` (Section 5.1).
- Everything under `rulesets` (Section 3.9).

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
of an unrecognised type survives a read-modify-write, and it wins.

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
- `null` marks an absent value in any channel from index 2 (`ele`) upward.
  Absent elevation and absent time are both first-class states and MUST be
  expressible.
- Trailing channels MAY be omitted. Arity 2 through 4 carry the channels this
  version defines. Arity 2 is used for inline chain endpoints (Section 3.3.3)
  and for area rings (Section 3.7). Arity 4 is legal on an arc, and a reader
  does not depend on an arc pip carrying a time.
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

A writer MUST round longitude and latitude to at most 6 decimal places, and
elevation to at most 1. `time_ms` is an integer count of milliseconds and MUST
NOT be rounded.

The rounded value is the value with at most that many decimal places nearest to
the binary64 value the writer was handed, with a tie resolved to even. An
elevation of `340.25` becomes `340.2`, not `340.3`.

The rounding is by formatting. Format the value to that many decimal places and
parse the text back:

```
round(v, n) = parse(format(v, n decimal places))
```

Scaling by a power of ten, rounding, and dividing back is a different operation
and MUST NOT be used. The scaled product is itself a binary64 and is not always
the exact scaled value, so the multiplication can manufacture a tie the true
decimal never had, and can destroy one it did. `0.15` rounds to `0.1` at one
place where the scaled route gives `0.2`; `0.05` rounds to `0.1` where the
scaled route gives `0`; `0.45` rounds to `0.5` where the scaled route gives
`0.4`.

Rounding a value that underflows to zero yields positive zero, and a writer MUST
NOT emit a negative zero.

`-0` and `0` are the same value, so the tree comparison of Section 8.1 cannot see
this rule and is not meant to. `corpus/numbers/` pins it.

Rounding MUST happen in the emit path and MUST NOT happen in the model. Arc
boundary de-duplication, ring winding and endpoint snapping compare positions by
full value, and rounding ahead of them changes which positions they consider
equal, which is a change to the geometry rather than to its spelling.

A reader MUST NOT round. Decoding preserves full binary64 precision.


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
- An internal sentinel standing for "no time" MUST NOT reach the wire. Absent
  time is `null` (Section 4.1).

### 4.3 Timestamps

Record-level timestamps, `created`, `started` and `ended`, are ISO 8601 in UTC,
with a literal `Z` suffix and OPTIONAL fractional seconds. An offset other than
`Z` is not permitted.

A record-level timestamp is checked for shape only. A reader MUST NOT refuse or
diagnose one on calendar grounds, even though ISO 8601 would:
`2026-13-45T99:99:99Z` matches the grammar this section states and is accepted.
That is a rule rather than a consequence, and it is stated because naming ISO
8601 arguably imports a calendar.

Per-pip time is a count of epoch milliseconds (Section 4.1) and is not an ISO
8601 string.

### 4.4 Identifiers, keys and order

- Identifiers are scoped to the object that contains them, not to the document.
  A route's `node-1` and a network's `node-1` are different nodes. A reader MUST
  NOT build one document-wide identifier map.
- Identifiers appear only where something references them: on folders, and as
  the keys of a network's `nodes` and `arcs` objects.
- Keys defined by this format are lower kebab-case. `@type` (Section 5) is the
  single exception and is the only key in the format carrying a
  non-alphanumeric character.
- A writer SHOULD derive an identifier from the name of the object it
  identifies, where that object has one: lower kebab-case, about ten
  characters, with a numeric suffix for uniqueness. Where there is no name, a
  writer SHOULD use a sequential identifier such as `node-1` or `arc-1`.
  Readability is preferred to file size.
- Key order is not significant anywhere in a document, including inside data
  records. A reader MUST accept any key order.

Array order is the opposite, and the two rules sit together so that neither is
read as the other. Arrays MUST NOT be reordered. The order of an array is data,
and it is meaningful wherever an array appears in this format: the positions in
a `pips` array, the links of a chain, the arcs of a route, and the segments of a
track. The order of positions in a line is the shape of the line.

## 5. Data records

Every item, and every arc, MAY carry a `data` key holding an array of records.
Each record is a JSON object carrying an `@type` key whose value is a
reverse-DNS identifier, plus arbitrary sibling fields.

```json
"data": [
  { "@type": "app.viatrail.tag", "value": "day-walk" },
  { "@type": "com.example.checked", "value": true }
]
```

`@type` is the only key in this format carrying a sigil. The sigil marks the key
as structural rather than payload, and leaves the plain name `type` available to
any vocabulary entry that needs a field so named.

Note: the spelling is borrowed from JSON-LD, and a VNX document is not JSON-LD.
No `@context` is emitted, so a JSON-LD processor fails visibly rather than
misreading a VNX document.

### 5.1 Record rules

1. `data` is a bag, not a map. Several records of one type are supported, and
   three tags are three records. What a reader does with several records of one
   type is not defined by this format: a writer MAY emit as many records of a
   type as it means, and a reader MAY take all, one or none as its own semantics
   require.
2. A record body is an opaque JSON object apart from `@type`. A vocabulary entry
   MAY carry a field named `type`.
3. A record of an unrecognised type is inert. It MUST NOT be treated as an
   error on any host, the item or arc whose `data` carries it, including a host
   on which the reader did not expect it.
4. A record of an unrecognised type MUST survive a read-modify-write at value
   level.
5. The meaning of a record is the pair of its type and its host, not the type
   alone. `app.viatrail.navigation` with `value: "avoid"` on a chain means
   avoid this stretch of path; on an area it means avoid this region. This
   specification defines a type's meaning per host.
6. A type with a single primary payload names it `value`. A type with several
   fields keeps its own names, and MAY carry `value` alongside modifiers.

### 5.2 The `app.viatrail` vocabulary

| Type | Hosts | Payload | Profile |
|---|---|---|---|
| `app.viatrail.camp` | point | `flat` (boolean), `sheltered` (boolean), `water` (boolean), `capacity` (number) | core |
| `app.viatrail.compute-failed` | arc | none | vendor |
| `app.viatrail.hotel` | point | `phone` (string), `url` (string), `booking-required` (boolean) | core |
| `app.viatrail.navigation` | chain, area | `value`, `strength` | vendor |
| `app.viatrail.routing` | arc | `source`, `config` | vendor |
| `app.viatrail.settlement` | point, area | `size` (`hamlet`, `village` or `town`), `shop` (boolean), `resupply` (boolean) | core |
| `app.viatrail.shelter` | point | `kind` (`bothy`, `hut`, `refuge` or `lean-to`), `capacity` (number), `locked` (boolean) | core |
| `app.viatrail.tag` | any item | `value` (string) | core |
| `app.viatrail.trail-note` | any item | `value` (string, may contain line breaks) | core |
| `app.viatrail.water` | point | `seasonal` (boolean), `flow` (`trickle`, `steady` or `strong`), `verified` (timestamp) | core |

This table is normative and defines the core and vendor boundary. It is
maintained as the vocabulary grows.

`app.viatrail.routing` carries `source`, and carries `config` only when the
routing engine has parameters to record. The record carries `source` and nothing
derivable from it.

The following is a requirement on Viatrail's export path rather than on a
library: an exporter verifies that an arc's stored kind follows from its source,
and reports a diagnostic for an arc where it does not.

`app.viatrail.navigation` records an intent. Nothing in this format states
that a routing engine acted on it, so a reader does not expect the record to
have influenced any geometry in the document.

`app.viatrail.compute-failed` marks an arc whose route computation failed. Such
an arc still carries `pips`, holding its two endpoint positions, because a
straight line between them is what is drawn. A reader that recognises the type
SHOULD disclose the failure rather than present the geometry as computed.

### 5.3 Profiles

`profile` takes one of two values:

| Value | Effect |
|---|---|
| `full` | every record is present |
| `portable` | records marked vendor in Section 5.2 are omitted |

`profile` records the choice its writer made. A reader MUST preserve the value
and MUST NOT act on it: the records a profile would have removed are already
absent. A reader MUST accept an unrecognised value and an absent one, and MUST
NOT refuse either.

A record whose type is outside the `app.viatrail.` namespace is kept in both
profiles. A portable export strips Viatrail-private records; it does not strip
another program's extensions.

The `app.viatrail.` namespace is reserved without regard to case. A type that
differs from the prefix only by case, such as `App.viatrail.x`, is inside the
namespace and is not another program's. A writer MUST NOT author one, and a
reader classifying a record for the purposes of this section MUST match the
prefix case-insensitively.

The case-insensitive rule is about the namespace, and it does not reach
identity. A `@type` value is compared exactly everywhere else in this format:
`App.viatrail.tag` is a different type from `app.viatrail.tag` and has no row in
the table in Section 5.2. A reader MUST NOT fold case to resolve one onto the
other. Rules 3 and 4 of Section 5.1 govern it as they govern any unrecognised
type.

## 6. Reading

### 6.1 Content not carried

The following are not carried by this format, by ruling rather than by
omission:

- Cached distance, elevation gain, elevation loss, and bounds. All are derivable
  from geometry the reader has just been handed.
- Items the source library holds as soft-deleted, or trashed. An export carries
  what a user would see in the application.
- Durable identity, plans, offline map tiles and download state, and creation
  and modification timestamps of items.
- A network's or chain's default routing configuration.
- A network's default style for unstyled chains. This is deferred rather than
  refused, and is expected in a later version.

How a consumer derives elevation gain and loss is its own business.

### 6.2 Error handling

A reader is tolerant of damage and silent about nothing.

- An unrecognised key on an object defined by this format: the reader MUST
  record a diagnostic, MUST skip the key, and MUST keep the object. An
  unrecognised key is not an unusable value.
- An unusable value, such as a position that will not parse, a link naming an
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
  enumeration for it. The field lists in Section 5.2 are a vocabulary, not a
  validation rule: a record body is opaque apart from `@type` (Section 5.1), and
  a reader MUST NOT check the type, range or enumeration of a field inside one.
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

A link naming a node that is not in its network keeps the link and excludes that
end from the continuity comparison of Section 3.3.3. The link then runs from its
resolved endpoint along the named arc to that arc's terminus in the direction of
travel: an absent `to` means from the resolved `from` to the far end, and an
absent `from` means from the near terminus to the resolved `to`.

A position that will not parse drops the object that owns it rather than the
position, and the table below says which object that is. A link that cannot be
used drops the link and keeps the chain. Geometry is continuous and structure is
not: a missing vertex leaves a shorter, plausible, wrong line with nothing to
indicate it, while a missing link leaves a gap the continuity rule of
Section 3.3.3 can see and report.

A track's owner is the segment rather than the whole track, for the same reason.
Section 3.5 already makes the interval between two segments a pause rather than
geometry.

The three unparsable-position rows below take one code. The code plus the table
already tells a consumer what was lost, so the path is free to name the cause.

The remedies for the constructs this document constrains:

| Construct | Remedy |
|---|---|
| an inline link endpoint that does not project onto its arc within tolerance (3.3.3) | drop the link |
| a `polygon` that does not close, or holds fewer than four positions (3.7) | drop the area |
| an arc naming a node that is not in its network (3.3.2) | keep the arc; its geometry is real |
| a link naming a node that is not in its network (3.3.3) | keep the link; see above |
| a position that will not parse, inside an arc's `pips` (4.1) | drop the arc |
| a position that will not parse, inside a track segment's `pips` (4.1) | drop the segment |
| a position that will not parse, inside a `polygon` ring (4.1) | drop the area |
| a data record with no `@type`, or one that is not a string (5) | drop the record |
| consecutive links in a chain that do not meet (3.3.3) | keep both links; report the break |

The diagnostic for a break addresses the second link of the pair.

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
   which could be used. That is not always the element the remedy drops: an
   inline endpoint that does not project names the endpoint while the link is
   dropped around it, and a polygon with nested rings names the polygon while
   the area is dropped around it.

   "No part of which" is load-bearing. A link naming an absent arc and an inline
   endpoint that misses its arc are near-twins, and they land at different
   depths for a reason. When the arc is absent nothing about the link resolves,
   so the link is named; when the endpoint misses the arc, the arc and the other
   endpoint are still fine, so only `to` is.
2. Where the diagnostic is about a relationship, nothing is unusable at all: a
   chain whose links do not meet keeps both links. Such a diagnostic names the
   second of the pair, the link that failed to continue, not the one it failed
   to continue from.
3. Where the diagnostic reports a disagreement between two values that both
   survive, it names the value being reported rather than the one it is measured
   against. An arc's terminal pip that has drifted from its node names the pip's
   end, because Section 3.3.2 makes the pips authoritative and the node is the
   reference.

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
  diagnostic, and MUST return the document. Refusing a whole library because one
  photograph used an unusual method would be disproportionate.
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

  It is a prohibition rather than an obligation so that no reader has to be
  structured any particular way.
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

## 8. Conformance corpus

The corpus distributed with this specification is what decides whether an
implementation conforms. Every implementation runs the same files.

The corpus has four kinds of case, and one reference table:

| Directory | Requirement |
|---|---|
| `accept/` | MUST be read, with no diagnostics |
| `reject/` | MUST be refused, and the refusal MUST name its cause |
| `diagnose/` | MUST be read, reporting exactly the listed diagnostics |
| `write/` | a document, and the document a writer MUST produce from it |
| `numbers/` | reference roundings to 6 and to 1 decimal places |

The `write/` tier is the only one that exercises the emit-boundary rules of
Section 4.1: rounding, and the shortest spelling of a position. Every other tier
re-encodes what it decoded, which applies rounding to values that already
satisfy it, so a defect in either rule is unreachable without the `write/` tier
and `corpus/numbers/`. A runner MUST assert that a case's input is not already
the written form, and that the written form is a fixed point of the writer.
Without those two guards a writer that echoed its input would pass the tier.

Accept cases cover at least: a worked example exercising every construct; absent
elevation and absent time spelled with `null`; a position that renders visibly
wrong if longitude and latitude are transposed; a coordinate that survives only
at full binary64 precision; unrecognised data types on unexpected hosts,
preserved through a read-modify-write; a chain with a shared reversal endpoint;
a route with mixed hand-drawn and engine-routed arcs; an arc marked
compute-failed; a track with a pause; a track with no pip times; a document
whose patch version is above the reader's; and a container under each of the two
root-document rules of Section 7.

Reject cases cover at least: a `NaN` literal; an `Infinity` literal; a version
above the reader's; a two-component version; well-formed JSON with no `vnx` key;
a container with a `..` attachment path; and a container with duplicate entry
names.

Diagnose cases cover at least: an unrecognised key on an object defined by this
format; a link naming an absent arc; a chain whose consecutive links do not
meet; a polygon with a second ring; a record-level timestamp with an offset
other than `Z`; and a duplicate object key, which expects no diagnostic and
records the limitation of Section 6.4.

### 8.1 Storage and sidecars

The corpus lives at `corpus/`, at the root of this repository, in directories
named for the kinds above, and carries its own README.

Each `foo.vnx` or `foo.vnz` has a `foo.expect.json` sidecar holding its expected
result. The sidecar is plain JSON beside the file rather than inside a test in
any one language, so that an implementation in any language can run the corpus.
A file with no sidecar is a failure, not a skip.

Three sidecar fields do work that is not evident from their names:

- `counts` gives the exact number of entries in each collection, and prevents an
  accept case passing vacuously. A decoder returning an empty document for every
  input would otherwise satisfy "no error and no diagnostics" on all of them.
- `positions` pins which number reached which channel, and is the only available
  defence against a longitude and latitude transposition: both orderings parse,
  both are in range, and the point lands 8,000 km away with every structural
  check still passing. The expected values are JSON numbers, so the comparison
  is between doubles rather than between formatted text.
- `moving_time_ms` set to `null` means unknown, which is not zero
  (Section 3.5).

A diagnostic is matched on its `path` and its `code` (Section 6.3). Its English
is advisory and MUST NOT be compared.

A refusal is matched as a substring of its message, which is the one place this
corpus compares English across implementations. The exact wording of a refusal
is an implementation's own business and naming its cause is not, so the
substring is a weak proxy for the second.

For every file in `accept/`, a runner MUST decode it, encode the decoded
document, and compare the tree parsed from the original file against the tree
parsed from the encoded output.

Two normalisations are permitted, and no others: on a position, trailing `null`
channels are dropped and the precision rules of Section 4.1 are applied to both
sides; and an absent container and an empty one normalise to the same value,
which Section 3.1 requires a reader to do. Nothing else may be normalised away.
Files in `reject/` and `diagnose/` are not subject to this check.

The comparison is over trees because the bytes of a document are not
constrained: key order, number spelling and whitespace are all the writer's own
business.

It MUST NOT be reduced to a comparison of one decoded model against another. A
reader that discarded every data record, every ruleset, every style and every
`notes` field produces a model that compares equal to itself, and that is the
whole extensibility mechanism, all styling and all user prose. Comparing against
the tree parsed from the original file is what proves a reader kept what it
read.


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

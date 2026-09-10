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

Two documents ship beside this one and NEITHER IS NORMATIVE.

`corpus/README.md` explains how to run the corpus and why particular cases are
shaped as they are. Some of what it explains is load-bearing and appears nowhere
else — why two fixtures must NOT be made consistent with each other, what a
runner that pairs one report with two expectations fails to catch — and an
implementation may reasonably rely on it. But where it and this document differ,
THIS ONE DECIDES, and anything in it that a conforming implementation must obey
belongs in Section 8 instead. A rule discovered only by reading a README is a
rule the next implementer will not find.

`IMPLEMENTING.md` is a guide to the traps in this specification. It is
informative throughout and normative nowhere, and it quotes this document rather
than restating it.

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
digits, separated by U+002E FULL STOP. All three components are REQUIRED.

Which regime applies is selected by the DOCUMENT's MAJOR component: 0.x documents
are read under the rules for 0.x below, and 1.0-and-later documents under the
rules from 1.0. A regime describes the compatibility guarantees a document's
author was working under, and those belong to the document rather than to
whoever opens it.

Within the selected regime, every comparison is against the READER's own version.

A reader MAY support more than one MAJOR series, and one that supports several
applies each document's own regime. A reader that does not support a document's
MAJOR series MUST refuse it, naming the version, rather than read it under the
rules of a series it does support. Reading a 0.x document under the 1.0 rule
that a reader ignores what it does not recognise is a guess about guarantees the
document never carried.

Support for an earlier series is a MAY rather than a MUST because carrying it is
a real burden on an implementation that never met one, and because refusing is
visible to whoever holds the file where a wrong guess is silent. Note that every
VNX document written so far is 0.1.x, so a 1.0 reader that drops 0.x support
refuses every file in existence.

A reader MUST compare the MAJOR and MINOR components of the document against
its own supported version. The PATCH component MUST NOT be used to refuse a
document at any version, because a patch increment is additive by definition;
constructs introduced by a patch reach a reader through the unknown-key
diagnostics of Section 6.2.

Through 0.x:

- Each MINOR is its own series. The minor number is a breaking axis through
  0.x, which is to say a 0.2 document and a 0.1 document give a reader different
  guarantees, exactly as a 2.x and a 1.x document would after 1.0. So the rule
  above applies unchanged with MINOR in the place of MAJOR: a reader MAY support
  several 0.x minor series, and MUST refuse a document in a series it does not
  support, naming the version.
- A reader MUST accept a document in a series it supports, whatever the PATCH
  component. A 0.1.0 reader accepts 0.1.3. It refuses 0.2.0, and it refuses
  0.0.9 unless it declares support for the 0.0 series.

  A lower minor is refused for the same reason as a higher one, and the symmetry
  is the point. A breaking axis means a construct may have CHANGED MEANING, not
  merely been added, so a 0.1 reader can no more assume it understands 0.0 than
  0.2. There is no argument that makes a downgrade safe while an upgrade is not.

From version 1.0 onward, minor versions are additive: a reader MUST accept any
minor version at or above its own and MUST ignore constructs it does not
recognise, and only a MAJOR increment is a refusal. The rule for PATCH does not
change.

Each component is one or more decimal digits, with no bound on length and no
rule against a leading zero: `00.01.0000` is a legal spelling of 0.1.0, and a
reader compares the numeric values rather than the text.

A version string that is not three digit-runs separated by full stops MUST be
refused, and MUST NOT be coerced. `"0.1"`, `"0.1.0.0"`, `"0.1.x"` and `"0.1.+1"`
are all refused. A two-component version is not a spelling of a three-component
one: reading `"0.1"` as `0.1.0` would be a guess about which patch the document
was written against. The refusal MUST name the expected shape, because that is
what tells the holder of the document what to produce instead.

A reader that re-encodes a document MUST write the version the document
declared, not the reader's own. A 0.1.0 reader that reads a 0.1.3 document and
writes it back writes `0.1.3`.

The JSON Schema (Section 8.2) is named for the MINOR series and covers every
patch of it. A new minor version gets a new schema file.

## 3. Document structure

THE NORMATIVE KEY SET IS SECTION 3.10. The JSON below is illustrative, as
Section 1.3 says of every example: a reader building a key list from these blocks
will get one that is close and wrong, and Section 6.2 makes an unrecognised key a
diagnostic, so the mistake is one this format punishes.

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
the same content, and a reader MUST treat them alike when reading.

This rule is not about the envelope. It holds WHEREVER A CONTAINER APPEARS in
this format: an empty `data` bag, an empty `links` array, an empty `nodes`
object are each distinct from the key being absent, and Section 3.10 states the
same thing from the other side. They are
different DOCUMENTS, though, so a reader MUST record which it was given and a
formatter MUST reproduce it (Section 8.1). Whether a collection was present is
model state, in the same way a position's arity is (Section 4.1).

`created` and `generator` are informational. `created` is a timestamp
(Section 4.3).

`generator` names the software that wrote the document.

- An application writes its own name and version, for example
  `"Viatrail 0.1.1498"`. The version is supplied by the caller requesting the
  export. An export request that carries no version is refused rather than
  written with a placeholder version, because a header that reads as a fact and
  is not one is worse than no export. That is a requirement on an application's
  export path, not on a library: a library has no export request.
- A development or command-line tool names itself, and MUST NOT name itself as
  the application. A document written by such a tool did not come from an
  application export.

`generator` carries the application version. The format version is the value of
`vnx` (Section 2.2). The two are unrelated.

`profile` selects which data records are present; see Section 5.3.

### 3.2 Folders

```json
{ "id": "cairngorms", "name": "Cairngorms", "parent": "approaches" }
```

`parent` is absent at the root. Folders are the only top-level items that carry
an `id`, because they are the only top-level items referenced from elsewhere.

Any item may carry a `folder` key naming the folder that contains it.

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
the position of the node `to` names. Where a terminal pip and its node disagree,
THE PIPS ARE AUTHORITATIVE: the two are one fact written twice, and the geometry
lives in the pips. That is why Section 6.2 keeps an arc naming an absent node —
its geometry is real without the node.

A reader MUST compare each terminal pip against its node's position and MUST
record a diagnostic where they are further apart than the projection tolerance
of Section 3.3.3, keeping both. The same 50 metres, deliberately: this is the
same question about a different pair of objects, and two thresholds for one
question is worse than one slightly loose one. Exact equality would be defensible
for a document written by one program from one source and wrong for a
hand-authored one, which is the population that tolerance exists for.

Note: an implementation whose internal storage holds only the interior points of
an arc must add the endpoint positions when writing and remove them when
reading. Treating one representation as the other loses or invents two
positions without producing an error.

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

Every coordinate — the inline position and every vertex of the arc's `pips` — is
projected to metres by a local equirectangular map about the LATITUDE OF THE
INLINE POSITION, using an Earth radius of 6371008.8 m:

```
x = radians(lon) * 6371008.8 * cos(radians(lat_of_the_inline_position))
y = radians(lat) * 6371008.8
```

The cosine term uses that one latitude for every coordinate, which keeps the map
affine, so a straight polyline segment stays straight and the plane geometry is
exact. The distance is then the least distance from the projected position to
any projected segment of the polyline, each segment measured as the distance to
the nearest point on that segment.

The model is stated because the choice decides which documents are legal, not
because the arithmetic is delicate. Measured against one endpoint of the worked
example, four plausible models give 19.81 m, 20.34 m, 25.52 m and 36.47 m — a
factor of 1.8 — and Web Mercator, the largest of the four, is the one an
implementation carrying a tile pipeline reaches for first.

A polyline of one vertex is that point. A polyline of no vertices has no
distance: the reader MUST record a diagnostic and drop the link rather than
invent a number. A position at or within 50 metres of its arc is accepted: a distance equal to the
tolerance is within it.
No conformance case can pin that boundary and none should be attempted: a
distance at exactly the threshold is not reachable by a fixture, because the
computed value depends on the model and no two models land on 50.000. This
sentence is the permanent statement of the rule, as Section 4.5.5's escaping
rules are for escaping.

An inline position that does not project onto its arc within that tolerance is an
unusable value and is handled under Section 6.2.

Note: an export computed from an existing polyline projects to well under a
metre. The tolerance exists for hand-authored and third-party documents, whose
coordinates are typed rather than computed.

A chain's links MUST form one connected walk: the point at which link *k* ends
MUST be the point at which link *k+1* begins. Where they do not, a reader keeps
both links and reports the break (Section 6.2).

Two endpoints meet when their LONGITUDE AND LATITUDE ARE EQUAL, and no other
channel takes part in the comparison. That follows from the paragraph above: an
inline endpoint carries no elevation because elevation is resolved from the
polyline, while a node may carry one, so a comparison over all channels reports
EVERY junction between a node and an inline endpoint as broken.

A node key is resolved to its position before comparing. Two distinct node keys
at the same position therefore meet: comparing the keys alone would report a
continuous walk as broken, which is the failure this rule exists to prevent. Key
equality is a valid fast path.

A link that was dropped under Section 6.2 takes no part in the comparison on
either side. The links either side of it are not thereby adjacent, and a reader
MUST NOT report a break across the gap: one defect must not become two.

Two links in one chain MAY share an endpoint position, which is how a
reversal is expressed. Inline positions carry no identity, so:

- A writer MUST emit byte-identical coordinate arrays for a shared endpoint.
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

Because each arc carries its own geometry, parallel arcs between the same pair
of implied nodes need no disambiguation.

An arc whose two endpoints are the same node is legal and needs no special
handling. A loop trail leaving and returning to one junction is an ordinary
thing on the ground, and its geometry is as real as any other arc's.

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
segments of the difference between the last and first pip times in that segment.
It is deliberately not the wall-clock span between `started` and `ended`, which
pauses lengthen.

Moving time is derivable only when EVERY segment carries a time on its first and
its last pip. Otherwise a reader MUST report it as unknown, MUST NOT report it as
zero, and MUST NOT report a sum over the segments that do carry times.

A partial sum is worse than zero rather than better: it is indistinguishable from
a complete one, so no consumer can tell which it was given, where zero is at
least recognisable as suspicious. A track with no segments at all is unknown for
the same reason.

An EMPTY segment does not make a track unknown. It has no span to measure, so it
is not an unmeasured span: it contributes zero. The rule above reads either way
without this sentence, which is enough for two implementations to differ on a
track containing an empty segment with nothing to catch it.

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

`icon-size` is an integer in points. The values 16, 21, 27, 35 and 46 are the
ones Viatrail emits; that sentence is informative and does not constrain a
document. NO RANGE IS STATED FOR IT, so a reader does not check it and
`"icon-size": "large"` passes unremarked — Section 6.2 checks a value only where
this document states a range or an enumeration. `importance` is an integer from 1 to 5, and that is normative: a value
outside the range is handled under Section 6.2. Neither is spelled as a word.

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
and last positions are equal. Equal means equal in LONGITUDE AND LATITUDE; no
other channel takes part, for the same reason as Section 3.3.3's endpoints. Holes and multiple rings are not expressible in
this format.

A reader that encounters a nested or additional ring MUST report a diagnostic
and MUST drop the item. It MUST NOT silently keep the outer ring, and MUST NOT
merge the rings.

### 3.8 Hidden state

`hidden` is a boolean carried on folders, networks, chains, routes, points,
areas and tracks. It is user-visible state rather than a rendering cache.

Absence means visible. A writer MUST emit the key only when its value is `true`,
and a document SHOULD NOT contain `"hidden": false`.

### 3.9 Rulesets

Rulesets are not defined in version 0.1.

A document MAY carry a `rulesets` key, whose value is an array. Its CONTENTS are
opaque, but the value itself has a shape, and a value of the wrong JSON type is
handled under Section 6.2 like any other. A 0.1.x reader MUST treat its contents
as opaque and MUST preserve them through a read-modify-write, under the same rules
as an unknown data record (Section 5.1).

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
- `hidden` appears on every item that can be hidden, including a top-level
  waypoint. Section 3.8 names the shape rather than the collection, and a
  waypoint and a network's point are one shape (Section 3.6).
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
STYLE object, including one this format does not otherwise document. A
non-integer `importance` is out of range. Every other style value is carried
untouched, because no other style key has a stated range.

A field named `importance` inside a DATA RECORD body is not checked. Section
6.2's prohibition on inspecting a record body is the mechanism by which a record
of an unrecognised type survives a read-modify-write, which is load-bearing, and
it wins. The range above is a rule about styles. Style vocabulary grows faster
than structural vocabulary, so a fixed style key set would make every
implementation report a diagnostic for a valid document each time one
implementation added a style key, until all of them caught up. That cost is
larger than the mistyped-key detection it would buy.

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
- Trailing channels MAY be omitted. A reader MUST ignore channels beyond those
  it knows, so that a future channel is additive.
- Arity 2 through 4 carry the channels this version defines. That is not an
  upper bound; see the bullet below on further channels. Arity 2 is used
  for inline chain endpoints (Section 3.3.3) and for area rings (Section 3.7).
  Arity 4 is legal on an arc, but Viatrail writes no arc pip carrying a time,
  and a reader should not expect one.
- A position MAY carry more channels than this version defines. A reader MUST
  ignore their meaning and MUST preserve their values through a
  read-modify-write, and MUST NOT report a diagnostic for them. Ignoring a
  channel is not discarding it: a channel a reader dropped could not survive a
  re-render (Section 8.1), which is what would make a future channel a flag day
  rather than an addition.
- Positions of differing arity MAY appear within one `pips` array, and a reader
  MUST read each position independently of its neighbours. This follows from
  trailing omission: one pip may carry an elevation and the next may not, which
  is ordinary in imported data. Forcing a whole array to one arity would mean
  either inventing elevations or discarding them.

A writer MUST emit the shortest spelling of a position; a reader MUST accept any
of them. `[lon, lat, null]` and `[lon, lat]` state the same thing, and a reader
takes either.

Shortening happens where rounding happens, at the serialisation boundary
(Section 4.5.4), and it is NOT part of canonical form. A formatter re-renders
the spelling the document carries, so a position's arity survives a
read-modify-write.

Note: a model that normalises `[lon, lat, null]` to `[lon, lat]` cannot
re-render the conformance corpus, because `accept/absent-elevation-and-time.vnx`
is stored canonical and carries the longer spelling. An elevation field that is
merely optional is that model, and it is the first one most implementations
reach for. Position arity is state a reader has to keep.

An interior absence keeps its `null`. `[lon, lat, null, t]` cannot be shortened,
because dropping the `null` would move the time into the elevation channel.

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

Record-level timestamps — `created`, `started` and `ended` — are ISO 8601 in
UTC, with a literal `Z` suffix and OPTIONAL fractional seconds. An offset other
than `Z` is not permitted.

A record-level timestamp is checked for SHAPE only. A reader MUST NOT refuse or
diagnose one on calendar grounds, EVEN THOUGH ISO 8601 WOULD:
`2026-13-45T99:99:99Z` matches the grammar this section states and is accepted.

That is a rule rather than a consequence, and it is stated because the
consequence points the other way. Naming ISO 8601 arguably imports a calendar,
so an implementation that validates months against 01 to 12 can cite this
section and believe it is reading the document correctly. Silence diverges here
rather than converging: a reader built on a date library gets the calendar check
for free and one parsing the grammar directly does not.

Per-pip time is a count of epoch milliseconds (Section 4.1) and is not an ISO
8601 string. A track holds tens of thousands of pips and they are not read by
people.

### 4.4 Identifiers and keys

- Identifiers are scoped to the object that contains them, not to the document.
  A route's `node-1` and a network's `node-1` are different nodes. A reader MUST
  NOT build one document-wide identifier map.
- Identifiers appear only where something references them: on folders, and as
  the keys of a network's `nodes` and `arcs` objects.
- Keys defined by this format are lower kebab-case. `@type` (Section 5) is the
  single exception and is the only key in the format carrying a
  non-alphanumeric character.
- A writer SHOULD derive an identifier from the item's name where it has one:
  lower kebab-case, about ten characters, with a numeric suffix for uniqueness.
  Where there is no name, a writer SHOULD use a sequential identifier such as
  `node-1` or `arc-1`. Readability is preferred to file size.
- Key order is not significant anywhere in a document, including inside data
  records. A reader MUST accept any key order.

### 4.5 Canonical form

`IMPLEMENTING.md`, distributed beside this document, is an informative guide to
the rules in this section and the traps in them. It is not normative.

Canonical form is the byte layout a conforming formatter emits. Every
conforming writer, in every language, MUST emit these bytes for a given
document, so that exports of one library from different implementations are
byte-identical.

Canonical form is not required of a document. A reader MUST accept any
well-formed VNX document whatever its layout.

Note: the corpus, not this prose, is the enforcement. Every file in
`corpus/accept/` is stored in canonical form and the runner asserts that
canonicalising it reproduces it byte for byte (Section 8.1).

#### 4.5.1 Key order

Object keys sort in two groups:

1. Scalars — strings, numbers, booleans and nulls — in lexicographic order.
2. Containers — objects and arrays — in lexicographic order.

Lexicographic means CODE-POINT ORDER, which this document defines here and uses
by that name wherever strings are ordered:

> CODE-POINT ORDER compares two strings by the Unicode code points they contain,
> in order. An implementation whose default string comparison is by UTF-16 code
> unit MUST supply its own comparator, because the two disagree above U+FFFF:
> JavaScript's `Array.prototype.sort()` is such a default, while Python's
> `sorted()` and Go's `sort.Strings` already order by code point.

Every key this format defines is ASCII, where code point, UTF-8 byte and UTF-16
code unit order all agree — but a data record body may carry any key
(Section 5.1), and an archive entry name may be anything at all (Section 7.1).

The term is defined rather than restated wherever it is used because a section
CROSS-REFERENCE invites the assumption that a reader already knows what is there,
and this rule's whole content is a thing a reader does not know. An implementer
who sees "code point" and believes their language does that is not being
careless; they are being told the rule is defined elsewhere and that nothing here
needs their attention.

Classification is by the value present in this object, not by the key name. No
key is named by the rule.

For example:

```
top level:    created, generator, profile, vnx | folders, networks, routes, ...
network:      folder, name, notes              | arcs, areas, chains, data, nodes, points
arc:          from, to                         | data, pips
route:        folder, name, notes              | arcs, data, style
track:        ended, folder, name, started     | data, segments, style
data record:  @type, kind, source              | config
```

Note: `@type` sorts first within a record because U+0040 COMMERCIAL AT is below
every letter. This is a consequence of the rule, not a guarantee; a digit sorts
below it.

Entries of an object whose keys are identifiers, namely `nodes` and `arcs`
within a network, are sorted by key under the same rule.

#### 4.5.2 Array layout

Arrays MUST NOT be reordered. Order is meaningful wherever an array appears in
this format: pips, links, a route's arcs, and a track's segments.

An array holding only numbers and `null` is written on one line, with elements
separated by a comma and a single space. `null` counts as a number for this
rule. This covers every position in the format, and nothing else the format
defines, and it applies equally to a number array inside an opaque data record.

Note: `null` must count, or a position that lost its elevation would also lose
its inline rendering, making the shape of a document depend on its data. The
saving is substantial: on the seeded reference library, measured 2026-09-01,
1,277 lines and 29,073 bytes become 641 lines and 20,909 bytes, and a day's
recorded track holds tens of thousands of pips.

#### 4.5.3 Number rendering

Numbers are rendered with ECMAScript `Number::toString` semantics. `340.0`
renders as `340`, `0.000001` renders as `0.000001`, and `1e-7` renders as
`1e-7`.

THE ANCHOR IS THE ECMASCRIPT SPECIFICATION, not any implementation of it.
`JSON.stringify` is where those semantics are most easily observed and Go's
`encoding/json` reproduces them deliberately, but neither is normative here: an
implementation that agrees with one of them and not with this section is wrong,
and Go is the worked example, since it renders negative zero as `-0` where this
section requires `0`.

The one place an implementation is the anchor is a tie, and only because
`Number::toString` declines to fix the last digit of one. That case is settled
below rather than left to observation, so nothing in this section requires
consulting a runtime.

A tie is resolved by rounding to even: `1497898211722073.25` renders
`1497898211722073.2`, not `…073.3`. That is the rule, and it is stated here
because `Number::toString` does not state it — where the ECMAScript
specification declines to decide, this one does, and V8 and Go happen to agree
with it.

Note: a rendering tie is rare and does not arise in a coordinate. It needs 16 or
17 significant digits, so it lives between roughly 1e10 and 1e16. Measured over
a million values drawn from each of longitude, latitude, elevation and epoch
milliseconds, none produced one: coordinates and elevations are too small, and
`time_ms` is an integer count. Ties do occur over uniformly drawn binary64 bit
patterns, at about one in 4,200.

Where this rule bites is an arbitrary number inside a data record body, which
this format permits anywhere and does not constrain. That is a different hazard
from the rounding tie of Section 4.5.4, which is ordinary rather than rare and
reaches every coordinate and every elevation in a document. The two are not the
same size, and the smaller one is not the one an implementer would guess.

Negative zero MUST be rendered as `0`. `-0` and `0` are the same value, and two
writers disagreeing about the sign is a byte incompatibility carrying no
information. Go's `encoding/json` renders it as `-0` and must be corrected.

Language defaults do not agree, measured 2026-08-31 and 2026-09-01:

| Value | Go | JavaScript | Python | Swift `JSONEncoder` |
|---|---|---|---|---|
| `340.0` | `340` | `340` | `340.0` | `340` |
| `0.000001` | `0.000001` | `0.000001` | `1e-06` | `1e-06` |
| `0.0000001` | `1e-7` | `1e-7` | `1e-07` | `1e-07` |
| `1778914991000.0` | `1778914991000` | `1778914991000` | `1778914991000.0` | `1778914991000` |

Go and JavaScript agree with the required rendering apart from negative zero.
Python and Swift agree with each other and disagree on both exponent cases. An
implementation in any language other than Go or JavaScript renders numbers
itself.

#### 4.5.4 Rounding

A writer rounds at the serialisation boundary: longitude and latitude to 6
decimal places, elevation to 1. Time is an integer count of milliseconds and is
not rounded.

The rounded value is defined as a RESULT, not as a procedure. It is the value
with at most that many decimal places nearest to the exact binary64 value the
writer was handed, with a tie resolved to even. Equivalently: take the exact
decimal expansion of the double, and round it at the target place.

Multiplying by a power of ten, rounding, and dividing back does not always
produce that value, and is not a conforming route unless an implementation has
checked it against the number table in `corpus/numbers/`. The scaled product is
itself a binary64 and is not always the exact scaled value, so the
multiplication can manufacture a tie the true decimal never had, and can destroy
one it did. Measured over ten ordinary elevation-shaped values the two routes
disagree on six, in both directions: `0.15` rounds to `0.1` where the scaled
route gives `0.2`; `0.05` rounds to `0.1` where the scaled route gives `0`;
`0.45` rounds to `0.5` where the scaled route gives `0.4`.

A tie rounds to even. An elevation of `340.25` becomes `340.2`, not `340.3`. Ties
are structured rather than scattered: at one decimal place they are the odd
quarters, so every quarter-metre elevation is one, and at six places they are the
odd multiples of 1/128.

No tier that RE-RENDERS can detect a disagreement about any of this, because
rounding happens only when a writer emits a model. The `write/` tier and
`corpus/numbers/` are what reach it; Section 8 says which tiers do which.

Note: 6 decimal places of latitude is about 11 cm, and a consumer GPS receiver
is accurate to 3 to 5 m, so digits below the sixth place are the arithmetic's
noise rather than the instrument's reading. A fully populated pip is 57 bytes at
full precision and 27 bytes at 6 and 1 places.

Rounding MUST happen in the emit path and MUST NOT happen in the model. Arc
boundary de-duplication, ring winding and endpoint snapping compare positions by
full value, and rounding ahead of them changes which positions they consider
equal, which is a change to the geometry rather than to its spelling.

The one exception is the winding of a ring written to GeoJSON, which [RFC 7946]
constrains in the file: a sub-metre sliver that is counterclockwise at full
precision can be clockwise once its vertices are snapped to the grid, so its
winding is computed from the rounded ring.

A channel beyond the four this version names is written back unchanged. Rounding
is definable only where the units are known: six places is a statement about
degrees and one place a statement about metres, so a channel whose units this
version does not define cannot be rounded without guessing. That is the reason
time is not rounded, applied to a channel this version does not name.

A reader MUST NOT round, and neither MUST a formatter. Decoding preserves full
binary64 precision, and canonicalising a document re-renders the bytes another
program wrote. Canonical form constrains key order and number spelling, not
precision.

A conforming library therefore offers TWO emit paths, named distinctly:

| Path | Applies the emit-boundary rules? |
|---|---|
| WRITE — turn a model into a document | yes: round to 6 and 1 places, emit the shortest spelling of a position |
| RE-RENDER — turn a decoded document back into that document | no: reproduce the arity and the precision it carried |

A library with only one of them cannot satisfy this specification. With only a
writer it cannot pass the accept tier (Section 8.1), because writing what it
read is not what it read. With only a formatter the rounding rules above are
unreachable by construction, so nothing in the library can ever apply them.

A writer applies the negative-zero rule of Section 4.5.3 AFTER rounding, not
before. Rounding can CREATE a negative zero that was never in the model:
rounding −0.04 to one place, or −0.00000004 to six, gives a zero with its sign
bit set in most languages. A writer that collapsed negative zero only across the
values it was handed would emit `-0`. Round first, then render, and let the
renderer decide the final bytes.

#### 4.5.5 Encoding

UTF-8, no byte order mark, two-space indent, and a newline at end of file.

A string escapes only what [RFC 8259] requires: `"`, `\` and the control
characters below U+0020. Every other character is written raw, including
non-ASCII characters, `<`, `>`, `&`, U+2028 and U+2029.

A control character with a short escape is written with it: `\b`, `\f`, `\n`,
`\r`, `\t`. Every other control character is written `\u00xx`. The hexadecimal
digits are lowercase.

A control character at or above U+0020 is not escaped. U+007F DELETE is written
raw.

Note: measured against Go 1.25.6's `encoding/json`, which agrees with these rules
on the short escapes and on lowercase hexadecimal, and disagrees on two counts.
It escapes `<`, `>` and `&`, which `SetEscapeHTML(false)` corrects. It also
escapes U+2028 and U+2029, and no exported option reaches those: they are escaped
at every setting, because they are legal in JSON and illegal inside a JavaScript
string literal, so the encoder protects output that might be pasted into a
script. Sound for a general encoder, and wrong for a format that pins its bytes.
The consequence is that Go's standard library cannot emit canonical form at any
configuration, so "configure the encoder" is not an instruction that can be
followed and a conforming implementation writes its own string renderer.

Note: Go's `encoding/json` escapes those last five by default and must be
configured not to. No character above U+007F may be written as a `\u` escape.

An empty array is written `[]` and an empty object `{}`, each on one line.

#### 4.5.6 Note on stock encoders

No stock JSON encoder in the target languages emits canonical form. Every
implementation writes its own serialiser. Measured 2026-09-01:

| Encoder | Key order | Inline positions | Numbers | Separator |
|---|---|---|---|---|
| Go `json.MarshalIndent` | lexicographic | no | yes, apart from `-0` | yes |
| JavaScript `JSON.stringify(v, null, 2)` | insertion order | no | yes | yes |
| Python `json.dumps(indent=2, sort_keys=True)` | lexicographic | no | `340.0`, `1e-06` | yes |
| Swift `JSONSerialization` (`.prettyPrinted`, `.sortedKeys`) | lexicographic | no | `-3.6699999999999999` | `" : "` |
| Swift `JSONEncoder` (`.prettyPrinted`, `.sortedKeys`) | lexicographic | no | `1e-06`, `1e-07` | `" : "` |

Three points follow from the table:

- The key ordering alone rules out every stock encoder, independently of the
  inline-position rule. `sort_keys` and `.sortedKeys` produce plain
  lexicographic order, which places `networks` before `vnx` and `nodes` before
  `notes`. The inline-position rule therefore costs an implementer only a few
  lines in a serialiser that had to exist regardless.
- `JSONSerialization` on Apple platforms must not be used for VNX. It renders
  `-3.67` as `-3.6699999999999999`: the same double, 17 significant digits, and
  roughly twice the size for a file that is mostly coordinates.
- The reference serialiser is about 170 lines of code, most of which is the
  ECMAScript number renderer that every non-Go implementation needs in any case.

## 5. Data records

Every item MAY carry a `data` key holding an array of records. Each record is a
JSON object carrying an `@type` key whose value is a reverse-DNS identifier,
plus arbitrary sibling fields.

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
No `@context` is emitted, and `@type` without `@context` is not valid JSON-LD, so
a JSON-LD processor run against a VNX document fails visibly rather than
misreading it.

### 5.1 Record rules

1. `data` is a bag, not a map. Several records of one type are supported, and
   three tags are three records. What a reader does with several records of one
   type is not defined by this format: a writer MAY emit as many records of a
   type as it means, and a reader MAY take all, one or none as its own semantics
   require.
2. A record body is an opaque JSON object apart from `@type`. A vocabulary entry
   MAY carry a field named `type`.
3. A record of an unrecognised type is inert. It MUST NOT be treated as an
   error, on any host item, including a host on which the reader did not expect
   it.
4. A record of an unrecognised type MUST survive a read-modify-write at value
   level.
5. The meaning of a record is the pair of its type and its host item, not the
   type alone. `app.viatrail.navigation` with `value: "avoid"` on a chain means
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

`app.viatrail.camp`'s `flat`, `sheltered` and `water` are booleans. `capacity`
is an ordinary number.

`app.viatrail.routing` carries `source`, and carries `config` only when the
routing engine has parameters to record. A configuration that holds nothing but
a restatement of its own kind is omitted, which is the case for every
straight-line and every frozen arc.

The record carries `source` and nothing derivable from it. In Viatrail's model,
the arc kind is a total function of the source and the configuration kind is a
total function of the arc kind, so writing either alongside `source` would state
one fact three times.

The following is a requirement on Viatrail's export path rather than on a
library, because the relationship it names holds between values that exist only
inside Viatrail's model: an exporter verifies that relationship for the arc it
is writing rather than assuming it, and reports a diagnostic for an arc whose
stored kind and source fall outside it, because writing `source` alone for such
an arc would lose the kind. A library reading this format sees only `source`.

`app.viatrail.navigation` is aspirational. Viatrail's routing engine has no
avoid-this-area or avoid-this-span input, so a reader must not expect the record
to have influenced any geometry in the document.

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
absent, and a reader that filtered again would remove records from a document
that had kept them deliberately. A reader MUST accept an unrecognised value and
an absent one, and MUST NOT refuse either.

A record whose type is outside the `app.viatrail.` namespace is kept in both
profiles. A portable export strips Viatrail-private records; it does not strip
another program's extensions.

The `app.viatrail.` namespace is reserved without regard to case. A type that
differs from the prefix only by case, such as `App.viatrail.x` or
`APP.VIATRAIL.X`, is inside the namespace and is not another program's. A writer
MUST NOT author one, and a reader classifying a record for the purposes of this
section MUST match the prefix case-insensitively. Otherwise a near-miss produced
by a capitalising keyboard is classified as third-party extensibility and kept
in a portable export that was meant to strip it.

The two rules that follow look contradictory and are not: THE CASE-INSENSITIVE
RULE IS ABOUT THE NAMESPACE, AND THE EXACT RULE IS ABOUT IDENTITY. A `@type`
value is compared exactly everywhere else in this format: `App.viatrail.tag` is a
different type from `app.viatrail.tag` and has no row in the table in
Section 5.2. A reader MUST NOT fold case to resolve one onto the other, which
would change what the record means. Rules 3 and 4 of Section 5.1 govern it as
they govern any unrecognised type: it is inert, and it survives a
read-modify-write untouched.

## 6. Reading

### 6.1 Content not carried

The following are not carried by this format, by ruling rather than by
omission:

- Cached distance, elevation gain, elevation loss, and bounds. All are derivable
  from geometry the reader has just been handed, and a reader that trusts a
  written figure trusts the writer's arithmetic over its own.
- Items the source library holds as soft-deleted, or trashed. An export carries
  what a user would see in the application. This follows from VNX being a
  snapshot rather than a backup (Section 1.1), and it keeps a document from
  carrying items its author had deleted.
- Durable identity, plans, offline map tiles and download state, and creation
  and modification timestamps of items.
- A network's or chain's default routing configuration.
- A network's default style for unstyled chains. This is deferred rather than
  refused, and is expected in a later version.

How a consumer derives elevation gain and loss is its own business. Gain and
loss depend on a noise threshold in a way distance does not, so two consumers
may reasonably differ. That is an application question, and this specification
says nothing further about it.

### 6.2 Error handling

A reader is tolerant of damage and silent about nothing.

- An unrecognised key on an object defined by this format: the reader MUST
  record a diagnostic, MUST skip the key, and MUST keep the item. An
  unrecognised key is not an unusable value.
- An unusable value — a position that will not parse, a link naming an arc that
  is absent from the document, a POLYGON carrying a ring beyond the first, a
  record-level timestamp carrying an offset other than `Z` — the reader MUST drop
  the offending element, MUST record a diagnostic, and MUST continue.

  The unusable value in the polygon case is the whole `polygon`, not the extra
  ring: Section 3.7 forbids silently keeping the outer ring, so the outer ring is
  unusable too and the unusable extent is the entire ring set.
- A value of the wrong JSON type is an unusable value. Where the key is
  OPTIONAL (Section 3.10), the reader MUST drop the field and MUST keep the
  item. Where the key is REQUIRED, the reader MUST drop the item, because the
  object cannot exist without it.

  The two cases are DIFFERENT DIAGNOSTICS and not one diagnostic with two
  remedies. "a field is gone" and "the item is gone" are materially different
  outcomes for whoever holds the file — the first is recoverable from the rest of
  the item and the second is not — so a caller must be able to tell them apart
  without parsing English, which means a distinct code for each.

  The exception is not a special case: dropping a required field would leave a
  waypoint that is nowhere, a link that traverses nothing, or an arc with no
  geometry. Section 5.1's rule for a record with an unusable `@type` is the same
  rule — the record goes, not the key.
- A value outside a range this document states normatively — an `importance`
  outside 1 to 5 (Section 3.6) — is an unusable value in an optional key, so the
  reader MUST drop the field and MUST keep the item.

  A reader checks a value only where this document states a normative range or
  enumeration for it. The field lists in Section 5.2 are a vocabulary, not a
  validation rule: a record body is opaque apart from `@type` (Section 5.1), and
  a reader MUST NOT check the type, range or enumeration of a field inside one.
  Checking them would break the mechanism by which a record of an unrecognised
  type survives a read-modify-write, and would make every implementation
  diagnose a valid document each time the vocabulary grew.
- A refusal. A plain document is refused for exactly four causes, and a reader
  MUST NOT refuse one for any other:

  | Cause | Section |
  |---|---|
  | the top level is not a JSON object, or carries no `vnx` key | 2.2 |
  | `vnx` is not three digit-runs separated by full stops | 2.2 |
  | `vnx` names a MAJOR or MINOR above the reader's | 2.2 |
  | a number in the document is not finite | 4.2 |

  Section 7 states the causes for which a container is refused. They are
  separate, and a fault in an attachment is not among them.

A link naming a node that is not in its network keeps the link and excludes that
end from the continuity comparison of Section 3.3.3. The link then runs from its
RESOLVED endpoint along the named arc to that arc's terminus in the direction of
travel: an absent `to` means from the resolved `from` to the far end, and an
absent `from` means from the near terminus to the resolved `to`.

Saying only that the link is kept is not enough to render it. A link is not its
arc — it is a portion of an arc between two endpoints — so "render it from the
arc" permits the whole arc or any stretch of it, and a chain drawing a whole arc
where it meant a third of one still draws a plausible line. The rule above is
deterministic and invents no position.

A position that will not parse drops the object that owns it rather than the
position, and the table above says which object that is. A link that cannot be
used drops the link and keeps the chain. Those look opposite and are not, and
the difference is worth stating because the two rules sit next to each other:

GEOMETRY IS CONTINUOUS AND STRUCTURE IS NOT. A missing vertex changes the shape
of a line and leaves nothing to indicate it — a shorter, plausible, wrong line.
A missing link leaves a gap between two endpoints that the continuity rule of
Section 3.3.3 can see and report, so nothing is lost silently.

A track's owner is the SEGMENT rather than the whole track, for the same reason.
Section 3.5 already makes the interval between two segments a pause rather than
geometry, so segments are discontinuous from each other by construction and a
missing one leaves a gap a consumer can see. Dropping the whole track would lose
geometry the document is not wrong about.

All three take ONE code. The code plus this table already tells a consumer what
was lost, so the path is free to name the cause — which is why the wrong-typed
value needs two codes and this needs one: required-ness cannot be derived from a
path, and the owning container can.

This is not the required-key rule of Section 6.2. That rule is about a key whose
VALUE is the wrong type: `"pips": "hello"`. Here `pips` is an array and is
perfectly usable; one element inside it is not.

The remedies for the remaining constructs this document constrains:

| Construct | Remedy |
|---|---|
| an inline link endpoint that does not project onto its arc within tolerance (3.3.3) | drop the link |
| a `polygon` that does not close, or holds fewer than four positions (3.7) | drop the item |
| an arc naming a node that is not in its network (3.3.2) | keep the arc; its geometry is real |
| a link naming a node that is not in its network (3.3.3) | keep the link; see below |
| a position that will not parse, inside an arc's `pips` (4.1) | drop the ARC |
| a position that will not parse, inside a track segment's `pips` (4.1) | drop the SEGMENT |
| a position that will not parse, inside a `polygon` ring (4.1) | drop the ITEM |
| a data record with no `@type`, or one that is not a string (5) | drop the record |
| consecutive links in a chain that do not meet (3.3.3) | keep both links; report the break |

The diagnostic for a break addresses the second link of the pair.

A `folder` naming a folder that is not present in the document is not an
unusable value. The reader MUST treat the key as absent, so the item sits at the
top level, and MUST record a diagnostic. Dropping the item would lose content
over a lost grouping.

A folder's `parent` naming a folder that is not present takes the same remedy: the
reader MUST treat the key as absent, so the folder sits at the root, and MUST
record a diagnostic.

A folder with no `id` is dropped under the rule above. Items that named it are
then covered by the previous paragraph and sit at the top level, so a folder's
contents survive the loss of the folder.

Neither tolerance nor refusal may be silent.

### 6.3 Diagnostics

A reader MUST return a list of diagnostics alongside the document it read. A
reader that returns a document alone has discarded the only evidence that
anything was wrong.

Each diagnostic carries:

| Field | Value |
|---|---|
| `path` | a JSON Pointer ([RFC 6901]) locating the element the diagnostic is about |
| | for a fault in an ARCHIVE ENTRY rather than in the document: `#/entries/<name>`, with the member name escaped per [RFC 6901] |
| `code` | a short stable token naming which diagnostic this is, or absent |
| `message` | a short description in English |

A diagnostic that a conformance corpus case pins MUST carry a `code`, and the
code is what the case compares. The English MUST NOT be compared: exact wording
is a reader's own business, and a corpus that matched substrings of prose across
implementations would be pinning translations of a rule rather than the rule.

A reader MAY leave `code` absent on any diagnostic no corpus case pins. Codes
are wire commitments, and a format does not need one for every way a document
can be malformed.

`path` names the SMALLEST ELEMENT NO PART OF WHICH COULD BE USED. That is not
always the element the remedy drops: an inline endpoint that does not project
names the endpoint while the link is dropped around it, and a polygon with nested
rings names the polygon while the area is dropped around it. Those two agree with
each other, and naming what was dropped instead would point a reader at an item
that is often perfectly good apart from one value inside it.

"No part of which" is load-bearing. A link naming an absent arc and an inline
endpoint that misses its arc are near-twins — both well-formed values that fail
to resolve against another object — and they land at different depths for a
reason. When the arc is absent nothing about the link resolves, so the link is
named; when the endpoint misses the arc, the arc and the other endpoint are
still fine, so only `to` is. Without that phrase an implementer pins the first at
`/links/1/arc` to match the second, and is wrong.

There is a THIRD RULE, for a diagnostic that reports a DISAGREEMENT between two
values that both survive. Neither rule above reaches one: nothing is unusable,
because both values are kept, and the pair is not ordered, so there is no second
of it. Such a diagnostic names the value BEING REPORTED rather than the one it is
measured against — an arc's terminal pip that has drifted from its node names the
pip's end, because Section 3.3.2 makes the pips authoritative and the node is the
reference.

AND ONE INVARIANT ACROSS ALL THREE. No two diagnostics about one document may
share BOTH a `path` and a `code`. A `path` and a `code` are the
only two fields a conforming runner may compare, so a reader that emits two
reports differing in neither has emitted two a runner cannot tell apart — and a
runner pairing each expectation with one distinct report would then pass a reader
that found one of the two faults twice.

Unlike a judgement about where each new diagnostic points, that is checkable
mechanically, over any document, by any runner.

There is a SECOND RULE, for a different kind of diagnostic rather than an
exception to the one above. Where a diagnostic is about a relationship, nothing
is unusable at all — a chain whose links do not meet keeps both links — so the
first rule does not reach the case. Such a diagnostic names the SECOND of the
pair: the link that failed to continue, not the one it failed to continue
from.

An archive entry is not part of the document, so a diagnostic about one cannot
be located by a pointer into the document. It uses the separate space
`#/entries/<name>` — not a path under any document key, because every such
spelling collides with something: `/attachments/…` is already the name of a
sidecar expectation field, and an absent `path` would make the field optional
and grow a null case in every runner comparison, which is the defect the
`message` field had one column over.

A diagnostic carries no severity. Every diagnostic this format defines reports
the same kind of fact — something in the document could not be used as written —
so there is nothing for a severity to rank.

An empty list means the document was read cleanly. Each diagnostic in a
non-empty list reports one thing the reader could not use as written: a key it
skipped, an element it dropped, or a relationship it could not resolve.

The order in which a reader emits diagnostics is not significant, and a
conformance runner MUST compare a case's reported diagnostics against its
expected diagnostics as a set. A reader that resolves cross-references in a
second pass emits them in a different order from a reader that does not, and
both are correct. Nothing in this format consumes the order.

A refusal (Section 6.2) is not a diagnostic, and a reader MUST report it through
a separate channel, so that a caller can distinguish a document that was refused
from one that was read with elements dropped.

### 6.4 Duplicate object keys

Two entries with the same key in one JSON object — for example two `node-1`
entries in one `nodes` object — resolve last-wins in every mainstream JSON
parser, before any VNX rule is applied. A node therefore disappears and a reader
built on a standard parser has no way to observe it.

This is a documented limitation of this format rather than a rule. The corpus
carries a case for it (Section 8.1) which expects no diagnostic.

## 7. The VNZ container

A VNZ file is a ZIP archive carrying a VNX document and its attachments.

- Root document: a reader MUST prefer an entry named `doc.vnx` at the archive
  root. Where there is none, the reader takes the first entry with a `.vnx`
  extension in archive order. Names are compared case-insensitively.
- The root document is selected by the two rules above, and every other entry is
  an attachment whatever its name ends in. A reader MUST NOT decide what is
  payload by looking at a file extension: a reader that sniffs extensions
  diverges silently from one that does not.
- Attachment paths MUST be relative and forward-slash separated. An absolute
  path, a path carrying a drive letter, or a path with a `..` SEGMENT MUST be
  refused.
- An entry name containing a backslash anywhere MUST be refused. That check
  stands alone and applies before the `..` rule, so `..\x` is refused for the
  simple reason and the question of whether a backslash separates path segments
  never arises. This is deliberately stricter than [APPNOTE], which permits the
  character. `..` is a path segment, not a substring: `a..b/photo.jpg` is a
  legal name and escapes nothing.
- Duplicate entry names MUST be refused, compared case-insensitively. Language
  runtimes resolve a repeated name differently, so one archive would yield
  different documents in different languages — and that applies to `doc.vnx`
  beside `DOC.VNX` exactly as it applies to an exact repeat, which is why the
  comparison matches the root-document rule above.
- A reader MUST accept stored and deflate. Other methods are not available in
  every target language. An entry compressed by another method is unreadable,
  and the remedy depends on which entry it is: if it is the root document, the
  reader MUST refuse the archive, because there is no document to return; if it
  is an attachment, the reader MUST drop that attachment, MUST record a
  diagnostic, and MUST return the document. Refusing a whole library because one
  photograph used an unusual method would be disproportionate.
- The UTF-8 filename flag is set.
- A reader MUST read an archive that uses the ZIP64 extensions of [APPNOTE].
- A reader MUST accept an archive whose entries defer their sizes to a data
  descriptor, and MUST take the sizes from the CENTRAL DIRECTORY rather than
  from a local header.

This section states what a READER accepts. Section 7.1 states what a writer
emits, which is narrower in every case: an export stays readable by the widest
range of tools, while an archive from anywhere else is still readable by every
conforming implementation. That asymmetry is the shape this format uses
throughout — canonical form constrains what a library writes and never what it
will read.

Note: an implementation matching the root document on the base name alone
matches at any depth and lets the last match win rather than the first. The
match is on the full entry name.

### 7.1 What a writer emits

A container written by a conforming writer is byte-identical to one written by
any other conforming writer for the same document and attachments. Every field
below is fixed, in both the local and the central header where a field appears
in both.

| Field | Value |
|---|---|
| compression method | 0, stored |
| general purpose flags | `0x0800`, the UTF-8 name flag, and nothing else |
| version made by, version needed | 10, that is 1.0 |
| modification time, date | `0x0000`, `0x0021`: 1980-01-01 00:00:00 |
| create system | 0 |
| extra field, comment | length 0 |
| disk numbers | 0 |
| internal attributes, external attributes | 0 |
| CRC and both sizes | written in the local header |
| general purpose bit 3, the data descriptor flag | not set |
| entry order | the root document first, then attachments sorted by entry name in CODE-POINT ORDER, as defined in Section 4.5.1 |
| ZIP64 records | not written |
| archive comment | none |

A writer MUST emit stored for every entry, and MUST NOT deflate. It MUST write
the CRC and both sizes in the local header and MUST NOT set general purpose bit
3. Every one of those is narrower than what Section 7 has a reader accept.

Bit 3 defers the CRC and the sizes to a descriptor after the payload, which
forces a forward-only reader to scan for the next entry's signature — and for a
STORED entry the payload can itself contain that signature. The always-stored
rule above therefore turns a theoretical ambiguity into a reachable one, which
is what decides this. Bit 3 exists for a writer that cannot seek, and a VNX
writer holds the whole document before it writes a byte.

Note: a conforming reader takes the sizes from the CENTRAL DIRECTORY and never
from the local header, which is why an archive written with bit 3 still reads.
A reader that trusts the local header believes a deferred size of zero. That is
the same shape as an archive with ZIP64 records reading correctly against a
parser that ignores extra fields: in both cases the fixture passes because the
reader happened not to consult the field that was wrong.

Version 10 rather than 20 because a stored-only archive requires 1.0, and that
field states a requirement rather than a habit.

The root document first because the fallback rule above takes the first `.vnx`
entry in archive order. Putting the document first makes the fallback and the
preferred-name rule agree, so a reader that implements the fallback subtly wrong
still lands on the right entry.

Attachments SORTED rather than in the order the caller supplied them, because
caller order is not a property of the content. Two conforming exporters using one
library — one iterating a hash map, one a sorted list — would otherwise emit
different archives for the same document while satisfying every other field in
this table, which defeats what the table is for.

The ordering is CODE-POINT ORDER, the term Section 4.5.1 defines, and an entry
name is exactly where its warning bites: a document's keys are ASCII, where every
comparator agrees, and an entry name may hold anything. Two archives can both
look sorted and differ, and nothing in the bytes says which comparator produced
them unless a name reaches above U+FFFF.

Three of those fields are worth their own sentence, because each one alone looks
like tidiness and none of them is.

Stored, because DEFLATE HAS NO CANONICAL OUTPUT. For the document inside one
corpus archive, Go's `compress/flate` emits 116 bytes and zlib emits 108, and no
zlib level from 0 to 9 reproduces the other. Java and Python agree with each
other because both are zlib; Go is a clean-room implementation and agrees with
neither. A deflated archive is therefore byte-reproducible only by the language
that wrote it, whatever a writer pins in its headers.

A fixed timestamp, because a real modification time records when a writer ran.

Create system and external attributes pinned to zero, because THEY ENCODE THE
WRITING MACHINE RATHER THAN THE CONTENT. Create system is 3 on macOS and Linux
and 0 on Windows, so one library on two platforms emitted two different archives
for one document; external attributes carried a file mode the writing library
stamped in.

That last pair is the mechanism to take away: the stored rule and the fixed
timestamp were each necessary and neither was sufficient, and the remaining
difference was invisible from inside one machine, because writing the same
archive twice in a row was perfectly reproducible. Only a cross-language
comparison exposed it. A writer that is deterministic on one machine can still
be non-deterministic across a fleet.

The rule delivers what it claims: a script written from this table alone,
sharing no code with any existing implementation, produced byte-identical output
for a document plus two attachments including a non-ASCII entry name.

## 8. Conformance corpus

The corpus distributed with this specification is what decides whether an
implementation conforms. Every implementation runs the same files.

The corpus has five kinds of case:

| Directory | Requirement |
|---|---|
| `accept/` | MUST be read, with no diagnostics |
| `reject/` | MUST be refused, and the refusal MUST name its cause |
| `diagnose/` | MUST be read, reporting exactly the listed diagnostics |
| `format/` | a non-canonical input, and exactly the bytes a formatter MUST emit for it |
| `write/` | a document, and exactly the bytes a writer MUST emit for it |

The `write/` tier is the only one that exercises the emit-boundary rules of
Section 4.5.4. Every other tier RE-RENDERS, which by definition applies none of
them, so rounding and position shortening are unreachable without it. A runner
MUST assert that a case's input is not already the written form, and that the
written form is a fixed point of the writer — the same two guards `format/`
carries, for the same reason: without them a writer that echoed its input would
pass the tier.

Formatter cases are separate because the accept files cannot catch a formatter
defect. Accept files are stored in canonical form, so canonicalising one only
asks the renderer about values already spelled correctly; a writer that renders
negative zero as `-0`, or `0.000001` as `1e-06`, is never asked the question. A
formatter case asserts that its input is not already canonical, so it cannot
pass for a formatter that echoes its input.

Accept cases cover at least: a worked example exercising every construct; absent
elevation and absent time spelled with `null`; a position that renders visibly
wrong if longitude and latitude are transposed; a coordinate that survives only
at full binary64 precision; number rendering across the four cases of
Section 4.5.3; unrecognised data types on unexpected hosts, preserved through a
read-modify-write; a chain with a shared reversal endpoint; a route with mixed
hand-drawn and engine-routed arcs; an arc marked compute-failed; a track with a
pause; a track with no pip times; a document whose patch version is above the
reader's; and a container under each of the two root-document rules of
Section 7.

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
named for the four kinds, and carries its own README.

Each `foo.vnx` or `foo.vnz` has a `foo.expect.json` sidecar holding its expected
result. The sidecar is plain JSON beside the file rather than inside a test in
any one language, so that an implementation in any language can run the corpus.

A file with no sidecar is a failure, not a skip. The exception is `format/`,
where a case is a pair — `X.in.vnx` and `X.canonical.vnx` — and the expected
result is the second file rather than a sidecar.

A runner MUST assert that each `format/` input is not already canonical.
Without that assertion a formatter that echoed its input would pass every case
in the tier.

Three sidecar fields do work that is not evident from their names:

- `counts` gives the exact number of items per collection, and prevents an
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

A refusal is still matched as a substring of its message, which is the one place
this corpus compares English across implementations. The exact wording of a
refusal is an implementation's own business and naming its cause is not, so the
substring is a weak proxy for the second: a reader refusing for the right reason
in different words fails. Refusals want codes for the reason diagnostics have
them.

Every file in `accept/` is stored in canonical form (Section 4.5). A runner MUST
decode it, RE-RENDER the decoded document, and require the original bytes back.
Files in `reject/` and `diagnose/` are not required to be canonical, and most
are not.

This document distinguishes two operations that both produce a document, and the
accept tier requires the second:

| Operation | What it does |
|---|---|
| WRITE | turn a model into a document, applying the emit-boundary rules: round coordinates (Section 4.5.4) and emit the shortest spelling of a position (Section 4.1) |
| RE-RENDER | turn a document a reader was handed back into that document, applying neither, so arity and precision survive |

A runner MUST NOT use its writer for this check: writing what was read is
legitimately not what was read, because a writer rounds.

Nor may a runner reformat the file's BYTES without decoding them. Reformatting
bytes into bytes never builds a model, so it cannot observe anything about what
a model kept — a reader that discarded every data record, every ruleset, every
style and every `notes` passes such a check, which is the whole extensibility
mechanism, all styling and all user prose. Measured 2026-09-09.

The check is therefore two guarantees at once: the byte-compatibility guarantee
between implementations, and the only thing that proves a reader KEPT what it
read, including data records of types no implementation recognises, whose bodies
nothing else inspects.

### 8.2 JSON Schema

A JSON Schema for the format, draft 2020-12, is published at
`corpus/schema/vnx-0.1.schema.json` with its own README. A document can be
validated against it with no VNX implementation present.

A document that validates against the schema is not necessarily a valid VNX
document. A schema checks shape, and several of this format's rules are not
about shape.

The schema checks: the envelope and each collection's structure; the fields
required per object; position arity and longitude and latitude ranges, with
`null` permitted from index 2 and trailing channels ignored; a ring holding at
least four positions, which catches a GeoJSON-shaped polygon with nested rings;
`Z`-only record-level timestamps; and the presence of `@type` on every data
record.

The schema cannot check the following:

| Rule | Reason |
|---|---|
| longitude and latitude transposition | both orderings are numbers in range |
| a link naming an absent arc | a cross-reference |
| a ring that does not close | a relationship between two values |
| an inline endpoint not on its arc | arithmetic, not shape |
| container-scoped identifiers | about scope, not shape |
| a shared reversal endpoint | requires comparing positions by value |
| a version above the reader's | reader-relative |
| a duplicate object key | resolved last-wins before the schema runs |
| `NaN` and `Infinity` | not JSON; no validator sees the document |
| canonical form | about bytes |
| anything about a VNZ container | an archive is not a JSON document |

That list is asserted by a test, so that a schema which gains the reach to catch
one of them fails the test rather than leaving the table stale.

The schema permits unrecognised keys deliberately. Section 6.2 makes an
unrecognised key on an object a reader diagnostic that keeps the item, so a
schema refusing them would disagree with every conforming reader. The same
applies to data record bodies, which are opaque by design, and to `rulesets`
(Section 3.9).

Each corpus case records a schema verdict, so that the places where the schema
and a reader disagree are visible rather than assumed.

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

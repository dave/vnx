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

A reader MUST compare the MAJOR and MINOR components of the document against
its own supported version. The PATCH component MUST NOT be used to refuse a
document at any version, because a patch increment is additive by definition;
constructs introduced by a patch reach a reader through the unknown-key
diagnostics of Section 6.2.

Through 0.x:

- A reader MUST refuse a document whose MAJOR or MINOR component is above its
  own, and the refusal MUST state that a newer reader is required. During 0.x
  the minor number is a breaking axis, so a 0.1.x reader cannot determine
  whether 0.2.0 changed a construct it depends on.
- A reader MUST accept a document whose MAJOR and MINOR match its own,
  whatever the PATCH component. A 0.1.0 reader accepts 0.1.3 and 0.0.9; it
  refuses 0.2.0 and 1.0.0.

From version 1.0 onward, minor versions are additive: a reader MUST accept any
minor version at or above its own and MUST ignore constructs it does not
recognise, and only a MAJOR increment is a refusal. The rule for PATCH does not
change.

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

Only `vnx` is REQUIRED. Every other key in every object defined by this format
is OPTIONAL, and a reader MUST accept its absence.

Absence means "not known". It MUST NOT be read as a default value, with two
exceptions stated where they arise: `label-visible`, where absence means Auto
(Section 3.6), and `hidden`, where absence means visible (Section 3.8).

All seven collections are OPTIONAL and default to empty.

`created` and `generator` are informational. `created` is a timestamp
(Section 4.3).

`generator` names the software that wrote the document.

- An application writes its own name and version, for example
  `"Viatrail 0.1.1498"`. The version is supplied by the caller requesting the
  export. An export request that carries no version MUST be refused rather than
  written with a placeholder version, because a header that reads as a fact and
  is not one is worse than no export.
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
stored on the endpoint. The projection tolerance is 50 metres. An inline
position that does not project onto its arc within that tolerance is an
unusable value and is handled under Section 6.2.

Note: an export computed from an existing polyline projects to well under a
metre. The tolerance exists for hand-authored and third-party documents, whose
coordinates are typed rather than computed.

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

Note: an arc's two endpoints are distinct. A self-loop arc, whose endpoints are
the same node, is not produced by Viatrail and is not a case this shape is
designed for.

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

Where the pips of a track carry no times, moving time cannot be derived. A
reader MUST report it as unknown and MUST NOT report it as zero. Zero is
indistinguishable from a track on which nobody moved.

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

`icon-size` is an integer in points; the values Viatrail uses are 16, 21, 27, 35
and 46. `importance` is an integer from 1 to 5. Neither is spelled as a word.

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
and last positions are equal. Holes and multiple rings are not expressible in
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

A document MAY carry a `rulesets` key. A 0.1.x reader MUST treat its contents as
opaque and MUST preserve them through a read-modify-write, under the same rules
as an unknown data record (Section 5.1).

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
- Arity 2 through 4 are legal. Arity 2 is used for inline chain endpoints
  (Section 3.3.3) and for area rings (Section 3.7). Arity 4 is legal on an arc,
  but Viatrail writes no arc pip carrying a time, and a reader should not
  expect one.
- Positions of differing arity MAY appear within one `pips` array, and a reader
  MUST read each position independently of its neighbours. This follows from
  trailing omission: one pip may carry an elevation and the next may not, which
  is ordinary in imported data. Forcing a whole array to one arity would mean
  either inventing elevations or discarding them.

A writer MUST emit the shortest spelling of a position; a reader MUST accept any
of them. `[lon, lat, null]` and `[lon, lat]` state the same thing, and a reader
takes either. Canonical form (Section 4.5) requires one spelling on the way out
so that two exports of one library are byte-identical.

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

Numbers are rendered with ECMAScript `Number::toString` semantics, which is what
`JSON.stringify` emits. `340.0` renders as `340`, `0.000001` renders as
`0.000001`, and `1e-7` renders as `1e-7`.

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

A reader MUST NOT round, and neither MUST a formatter. Decoding preserves full
binary64 precision, and canonicalising a document re-renders the bytes another
program wrote. Canonical form constrains key order and number spelling, not
precision.

#### 4.5.5 Encoding

UTF-8, no byte order mark, two-space indent, and a newline at end of file.

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
one fact three times. A writer MUST verify that relationship for the arc it is
writing rather than assume it, and MUST report a diagnostic for an arc whose
stored kind and source fall outside it, because writing `source` alone for such
an arc would lose the kind.

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

This is a rule about the namespace, not about identity. A `@type` value is
compared exactly everywhere else in this format: `App.viatrail.tag` is a
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
  is absent from the document, a polygon ring beyond the first, a record-level
  timestamp carrying an offset other than `Z` — the reader MUST drop the
  offending element, MUST record a diagnostic, and MUST continue.
- A non-finite number (Section 4.2), or a `vnx` version above the reader's
  (Section 2.2): the reader MUST refuse the document. These are the only two
  refusals defined for a plain document.

Neither tolerance nor refusal may be silent.

### 6.3 Diagnostics

A reader MUST return a list of diagnostics alongside the document it read. A
reader that returns a document alone has discarded the only evidence that
anything was wrong.

Each diagnostic carries two fields:

| Field | Value |
|---|---|
| `path` | a JSON Pointer ([RFC 6901]) locating the element the diagnostic is about |
| `message` | a short description in English |

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
- Attachment paths MUST be relative and forward-slash separated. A path
  containing `..`, an absolute path, or a path carrying a drive letter MUST be
  refused.
- Duplicate entry names MUST be refused. Language runtimes resolve them
  differently, so one archive would yield different documents in different
  languages.
- Compression MUST be stored or deflate. Other methods are not available in
  every target language.
- The UTF-8 filename flag is set.
- A reader MUST read an archive that uses the ZIP64 extensions of [APPNOTE].
  A writer MAY write a plain archive without them. The asymmetry is deliberate:
  a small export stays readable by the widest range of tools, and a large one is
  still readable by every conforming implementation.

Note: an implementation matching the root document on the base name alone
matches at any depth and lets the last match win rather than the first. The
match is on the full entry name.

## 8. Conformance corpus

The corpus distributed with this specification is what decides whether an
implementation conforms. Every implementation runs the same files.

The corpus has four kinds of case:

| Directory | Requirement |
|---|---|
| `accept/` | MUST be read, with no diagnostics |
| `reject/` | MUST be refused, and the refusal MUST name its cause |
| `diagnose/` | MUST be read, reporting exactly the listed diagnostics |
| `format/` | a non-canonical input, and exactly the bytes a formatter MUST emit for it |

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

A file with no sidecar is a failure, not a skip.

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

`message` and `refusal` are matched as substrings. The exact wording of a
diagnostic or a refusal is an implementation's own business; naming the cause is
not.

Every file in `accept/` is stored in canonical form (Section 4.5), and the
runner asserts that canonicalising it reproduces it byte for byte. That check
is the byte-compatibility guarantee between implementations, and it also proves
that the whole document — including data records of types no implementation
recognises, whose bodies nothing else inspects — survives parse and re-render at
value level.

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

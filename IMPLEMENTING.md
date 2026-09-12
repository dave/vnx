# Implementing VNX

You are writing a VNX library in a new language. You are done when `corpus/`
passes. `SPEC.md` is normative and this page is informative; where the two
appear to disagree, `SPEC.md` is right.

---

## 1. Start here

1.  Get the repository and read `SPEC.md` end to end. It is short, and every
    rule this document discusses is stated there.
2.  Read `corpus/README.md`. The sidecar fields listed there are the contract
    your test runner implements.
3.  Watch a green run before writing anything, so you know what one looks like:

    ```sh
    cd js  && node corpus/run.js     # prints "n/m cases passed" and exits 0
    cd go  && go test ./...
    ```

4.  Write the runner before the library. Point it at `corpus/`, walk every
    fixture, compare against the sidecar beside it, and treat a fixture with no
    sidecar as a failure rather than a skip.
5.  Run it. Every case fails. Make them pass one tier at a time, in the order
    of section 2.
6.  Red-proof the runner. Break your library one rule at a time, run the check
    that should notice, and require it to go red (section 10.3).
7.  Ship when `corpus/` is green and every red proof is red.

The rest of this document is where a language's defaults quietly disagree with
the format, and where a test written to catch that disagreement fails to catch
it. Six implementations were written from `SPEC.md` and `corpus/` alone — Go,
Python, Swift, Kotlin, JavaScript and Rust — and everything below is something
at least one of them got wrong first. Each entry gives the requirement, the
languages that fall into it, and the measurement. Measurements are attributed to
the implementation that made them; nothing here is inferred from another
implementation's source.

Read it before writing your encoder, not after your corpus run goes red.

A caution about the numbers in it. Each is a measurement of a particular file at
a particular moment, not a property of the format. Fixtures move. A number
copied out of this document into a test asserts, from then on, that a file says
what it said, so name the artefact beside it or write the claim as a
counterfactual, which has no artefact to go stale against.

---

## 2. Build in this order

Each tier forces a different part of the library into existence. Green on a
tier means you have built the thing in the right-hand column, and nothing else
proves it.

| Tier | What having it green forces you to have built |
|---|---|
| `accept/` | A parser, the whole model of `SPEC §3`, an encoder, and a JSON-tree comparison against the original file. |
| `reject/` | A refusal channel separate from your return value, carrying one of `SPEC §6.2`'s causes. |
| `diagnose/` | One diagnostic per bad path, each with a stable code, with the rest of the document read and returned. |
| `write/` | A real emit boundary: `SPEC §4.1`'s rounding and the shortest spelling of a position. No other document tier reaches it. |
| `numbers/` | Rounding as an operation on a value, correct on ordinary values as well as on ties, compared by bit pattern. |

Two properties of that order are worth knowing in advance.

`accept/` is the hardest tier and the one that repays being built first, because
its comparison is against the **tree parsed from the original file**, never
against a second model. A reader that discarded every data record, every
ruleset, every style and every `notes` produces a model equal to itself and
passes a model-versus-model tier green — that is the whole extensibility
mechanism, all styling and all user prose, dropped by an implementation that
reports itself conforming. *(Frozen as a proof by the Rust, Swift, Python and
JavaScript implementations independently.)*

`write/` and `numbers/` are the only tiers that reach a writer. The other three
start from a document that exists, so every value in them satisfies `SPEC §4.1`
before your writer is asked anything (section 10.1).

---

## 3. Choose your JSON layer

This is the first decision and the most expensive one to revisit, because the
format's reading rules are stated in detail and a stock parser answers on its
own terms instead. Work down this table against the library you were going to
reach for.

| What the format needs | What a stock JSON layer tends to do | Cite |
|---|---|---|
| `NaN`, `Infinity` and `-Infinity` refused | Several accept them by default. It is a flag to turn off rather than a behaviour you inherit. | `SPEC §4.2` |
| A literal whose value is not finite in binary64 refused | `1e400` is well-formed JSON. Parsers differ: one errors, one throws, one returns infinity in silence, one keeps the text and fails only on conversion. | `SPEC §4.2` |
| One diagnostic per bad path, the rest of the document kept | Stops at the first syntax error, and reports one failure for the whole document. | `SPEC §6.2` |
| Every number parsed as binary64, integer literals included | Widens or narrows to the type it infers — an integer type for an integer literal, a decimal type for a number with a point. | `SPEC §4.2` |
| The last bit of a coordinate preserved | Short-parse paths trade the last bit for speed, and a full-precision coordinate is exactly where that shows. | `SPEC §4.1` |
| Duplicate keys seen, or the limitation accepted knowingly | Resolves last-wins before any format rule runs, so an entry vanishes unobservably. | `SPEC §6.4` |
| Strict reading | Offers permissive modes — trailing commas, comments, unquoted keys — that accept exactly what `SPEC §4.2` forbids. | `SPEC §4.2` |
| Number rendering | Renderers disagree about the shortest round-tripping spelling. A renderer that spells `-3.67` as `-3.6699999999999999` round-trips correctly and is legal. | `SPEC §8.1` |

Read the table in two halves, because reading and writing land differently.

**The reader is the half you probably write yourself.** Six of the eight rows
above are reader rules, and the format spells out what a reader does with a
document that is wrong in a particular way. A parser that stops at the first
error cannot produce the per-path diagnostics `SPEC §6.2` asks for, whatever
else it does well. Writing a JSON parser that produces a value tree is a day's
work, and it buys strictness, the position of each value for a JSON Pointer, and
the option of seeing a duplicate key.

**The writer is the half you should not write.** Nothing about a document's
bytes is a rule — `SPEC §4.2` defines round-trip at value level and `SPEC §8.1`
compares parsed trees — so key order, number spelling and whitespace are your
own business, and a hand-written renderer buys nothing. Take the stock one.
Check that it renders a double as a shortest round-tripping decimal before you
commit to it.

**Do not decode through a derive macro, a `Codable` conformance, or any other
schema-driven binding.** It is the shape every one of these languages pushes you
towards, and it is wrong twice over. It stops at the first field it cannot read,
where `SPEC §6.2` wants the document read and the bad path reported. And it
silently drops every key your type does not name, which is the extensibility
mechanism of `SPEC §3.10`, `SPEC §5.1` and `SPEC §3.9`. Parse to a generic value
tree and walk it by hand.

Two settings are worth naming because they are easy to miss:

- A parser may offer an exact-float mode as a non-default feature flag. Without
  it, `corpus/accept/full-precision-coordinate.vnx` decodes one bit low, and
  the last bit of a coordinate is what that case exists to protect.
- A parser may hand you a decimal or big-integer type rather than a double.
  `SPEC §4.2` defines numbers as IEEE 754 binary64, so the conversion has to
  happen, and doing it late means two values that are one document compare as
  two.

---

## 4. The model

The section that saves the most time, because these are decisions made in the
first hour and discovered in the last.

### 4.1 Absence, and the two places it means something different

`SPEC §3.1`: absence means "not known" and is not a default value, with two
stated exceptions.

Two rules pull in opposite directions here, and each holds in its own place.

For an OPTIONAL collection, absent and empty carry the same content and
`SPEC §3.1` requires a reader to treat them alike. `SPEC §8.1` normalises the
two to one value, so the accept tier cannot see the difference and is not meant
to. A model that holds both shapes is free to; a model that folds them together
is equally conforming.

For a REQUIRED key, they are distinct. `SPEC §3.10`: `"links": []` satisfies
`links` and an absent `links` does not, because a required key must be present
for the object to exist. That is the difference worth building for.

A `hidden` written as `false` is a third case again: `SPEC §3.8` gives absence
the meaning visible, and a writer emits the key only when it is true.

`label-visible` is where getting this wrong is invisible. Absence means Auto, and
a reader that treats it as `false` flattens Auto to hidden with nothing to report
(`SPEC §3.6`).

The language pulls here. A plain list for every collection, where absent and
empty are one value, is more idiomatic in Rust than an optional list, and every
reviewer would suggest the change. For the seven document collections that
change is safe, because `SPEC §3.1` puts the two shapes on the same footing.
Where it is not safe is a required key, and a model that cannot tell an absent
`links` from an empty one cannot report the object as malformed.

The two absences that do carry a meaning are named in `SPEC §3.1`, and they are
the only ones: an absent `label-visible` is Auto, and an absent `hidden` is
visible. Every other absence is "not known" and stays that way through a
read-modify-write.

### 4.2 Opaque bodies survive at value level

Data records (`SPEC §5.1`), `rulesets` (`SPEC §3.9`) and style objects
(`SPEC §3.10`) are opaque: no key inside them is ever unrecognised, and each has
to survive a read-modify-write untouched.

That is a modelling decision rather than a serialisation one. Decode a record
body into a typed structure and you lose the fields your type does not name. The
accept tier's tree comparison is what catches it, and it is the only thing that
does: every other assertion in a sidecar is about a construct the format names.

Keep such a body as whatever your JSON layer produced. It is the one place where
holding an untyped value is the correct model.

An opaque body can hold a normative *value* without ceasing to be opaque. A
style body is opaque by ruling, and `SPEC §3.6` puts a normative range on its
`importance`. That is not a contradiction: a normative range is a rule about a
value, not about a key set, exactly as `SPEC §5.2` gives typed payloads to
records whose bodies stay opaque.

### 4.3 A position's arity is a spelling, not a fact

`[lon, lat]` and `[lon, lat, null]` state the same thing, and a reader must take
either. Implementations split on whether the model holds the arity the file
spelled or a fixed set of channels, and **both pass**, because a writer emits the
shortest spelling of a position (`SPEC §4.1`) and the accept tier's comparison
normalises trailing `null` channels on both sides (`SPEC §8.1`).

The distinction that is not free: an *interior* `null` is meaning, not spelling.
`[lon, lat, null, time]` says the elevation is unknown and the time is known, and
a model that cannot express it has lost a channel. Whichever shape you pick,
check it against `corpus/accept/absent-elevation-and-time.vnx` before you build
on it.

---

## 5. Parsing

### 5.1 Non-finite literals

`NaN`, `Infinity` and `-Infinity` are not JSON, and a document containing one is
refused (`SPEC §4.2`). Several standard libraries accept them by default,
Python's `json` module among them, so this is a flag to turn off rather than a
behaviour you inherit.

A literal like `1e999` or `1e400` is well-formed JSON whose value is not finite
in binary64: it parses to infinity rather than failing. Check after parsing as
well as during; a `JSON.parse` reviver or equivalent hook is enough.

The three literals fail differently in different runtimes, and the differences do
not line up:

| literal in `{"a": …}` | Go `encoding/json` | Node `JSON.parse` | Python `json.loads` |
|---|---|---|---|
| `NaN` | error | throws | accepts, giving `nan` |
| `Infinity` | error | throws | accepts, giving `inf` |
| `1e400` | error into `float64` | returns infinity silently | accepts, giving `inf` | Go accepts `1e400` into
`json.Number`, keeping the text, and refuses it only on unmarshal into `float64`.

Two consequences. The `reject/` sidecars expect the substring `malformed JSON`,
which describes what a *strict* parser says; both Go and Node are strict here, so
it is a strict-parser artefact rather than one language's. The implementation it
bites is Python, whose `json.loads` accepts all three by default: a Python
implementation either makes its parser strict or refuses under `SPEC §4.2`, and a
`SPEC §4.2` refusal legitimately does not contain the words "malformed JSON".

And `1e400` is the case every library catches for itself, with the runtimes
failing differently rather than uniformly. Node hands back infinity with no
complaint, so a reviver or an equivalent post-parse check is the only thing
between it and a coordinate of infinity.

### 5.2 Duplicate object keys

Two entries with the same key resolve last-wins in Go's `encoding/json`, Node's
`JSON.parse` and Python's `json`, all measured, before any format rule runs, so
an entry vanishes and a reader built on a stock parser cannot observe it
(`SPEC §6.4`). `corpus/diagnose/duplicate-object-key.vnx` expects no diagnostic.

Before taking that at face value: the format pushes you into writing your own
parser anyway (section 3), and once you have one, detecting a duplicate key is
about three lines. The limitation belongs to stock parsers rather than to the
format. Since the corpus case expects
no diagnostic, a reader that gains the ability turns it red, which is the signal
to revisit the limitation rather than a failure.

### 5.3 Parse every number as binary64

Integer literals included. `SPEC §4.2` defines numbers as IEEE 754 binary64, so a
JSON integer is a binary64 too, and rendering it as one is what makes
`1778914991000.0` and `1778914991000` the same document.

---

## 6. Numbers

Rounding is where the languages differ most and where the corpus helps least.
Each implementation encodes JSON with its own standard library, so a number's
spelling is not a rule of this format; the value that reaches the encoder is.

### 6.1 "Round to N places" names two different operations

Take this one before the tie rule, because it is the larger defect and the
independent one.

Scaling by a power of ten, rounding, and dividing back is not "the nearest value
with n decimal places, ties to even". It is a different rule wearing the right
name, and every language's idiom hands it to you. Multiplying by the scale is
itself a rounding step, and it moves values onto an exact half that were never on
one.

| value | nearest 1 dp of the double | scale, then round ties to even |
|---|---|---|
| `0.15` | `0.1` (it is 0.1499999999999999944…) | `0.2` |
| `0.05` | `0.1` | `0` |
| `1.05` | `1.1` | `1` |
| `0.45` | `0.5` | `0.4` |
| `1.85` | `1.9` | `1.8` |
| `0.95` | `0.9` | `1.0` |
| `340.25` | `340.2` (exact, a genuine tie) | `340.2` | Six of those seven disagree, in both
directions, and they are ordinary elevation-shaped values rather than corner
cases. Scaling has a second failure the table cannot show: a large value
multiplied by 10^6 can overflow to infinity, where rounding against the double
does not.

The number table decides it. Its `kind` column names the rows that separate the
two operations:

```sh
awk -F'\t' '$2=="scaling-route"' corpus/numbers/numbers.tsv
```

Every one of them sides with rounding against the double, at one place and at
six. The table's own header states its row count and its per-kind counts; read
them from the file rather than from here.

The tie direction of section 6.2 disagrees only on exact halves; the operation
disagrees on most values. **A library can have the tie rule right and be wrong on
almost every elevation it writes.** Coordinates carry more of that exposure than
elevations, six places being longitude and latitude.

Two details there matter more than any count. Somebody mined the `scaling-route`
rows specifically to catch a library that rounds by scaling, and **the library
that generated the table did not read it.** The instrument and the defect sat in
one repository and never met.

The guard that existed beside the defect used `340.25` and `340.35` — the last
row of the table above, which agrees under both operations. `0.15` would have
caught it the day that test was written. So the repair was two repairs: the
operation, and running every row of the published table through the encoder,
which is the comparison the other five libraries were making.

A magnitude bound guarding the scaled product against overflow went with it. Such
a bound is not a rule of the format; it is an artefact of the operation, and it
stops existing when the operation changes.

Formatting to a decimal string and parsing it back is the correct
implementation, because the formatter expands the double exactly and rounds ties
to even. The caveat is that the formatted text is an intermediate and never the
output. Round to a *value*, and hand the value to your encoder.

Decimal intuition lies about which way a value rounds, whichever operation you
pick. `3.05` rounds down to `3.0`, correctly, because the literal is really
`3.04999999999999982236431605997495353221893310546875`. A test written from
intuition asserts `3.1` and reports a correct writer as broken. But
`3.05` gives `3.0` under both operations above, so it cannot tell them apart,
which is why the worked example here is `0.15`.

### 6.2 The rounding tie goes to even

`SPEC §4.1` rounds longitude and latitude to 6 decimal places and elevation to 1,
at the writer boundary. Ties there go to even, and every language's idiom breaks
that in a different place.

| value to 1 place | Go | Python | JavaScript `toFixed` |
|---|---|---|---|
| `340.25` | `340.2` | `340.2` | `340.3` |
| `-340.25` | `-340.2` | `-340.2` | `-340.3` |
| `340.75` | `340.8` | `340.8` | `340.8` |

`toFixed` strips the sign and rounds the magnitude, so it is symmetric: away from
zero on both signs, `-340.25` to `-340.3` as well.

The asymmetric rule is the one everybody reaches for first.
`Math.round(x * 10) / 10` is half-up in both JavaScript and Java, because
`Math.round` breaks a tie toward positive infinity, so it is wrong on every
positive tie and right on every negative one by accident. **A guard testing only
negative ties passes it.**

There are two naive routes rather than one, and they differ on sign:
`Math.round(x*10)/10` is half-up, `round(x*10)/10` on a half-away-from-zero
rounder is half-away-from-zero. They agree on every positive tie and disagree on
every negative one, so a suite catching one and not the other has a gap that
reads as closed. Red-proof both.

Five wrong tie rules and the two values that separate them:

| value | rule | half-odd | away from 0 | toward 0 | half-up | half-down |
|---|---|---|---|---|---|---|
| `340.25` | `340.2` | `340.3` | `340.3` | `340.2` | `340.3` | `340.2` |
| `340.75` | `340.8` | `340.7` | `340.8` | `340.7` | `340.8` | `340.7` |

`340.25` catches half-away-from-zero and half-up; `340.75` catches
half-toward-zero and half-down; either catches half-to-odd. **Only the pair
identifies half-to-even**, so a fixture carrying one of them tests less than it
looks.

The values are reachable rather than contrived. A tie at one decimal place needs
a fractional part of exactly `.x5`, and of the ten candidates only `.25` and
`.75` are dyadic, but a quarter-metre elevation grid produces both.

Rounding also creates negative zeros that were never in the model — `-4e-08` to
six places, `-0.04` to one. The table gives `0` on every row that reaches it, so
collapse the sign.

### 6.3 Building a rounding-tie fixture is a trap

`0.0000005` is not a tie and is not a binary64 value at all. Its nearest double
is `4.99999999999999977e-7`, below the midpoint, so it rounds down under every
rule, and a suite pinning it passes whichever way the tie branch is written.

A tie at *n* decimal places needs the midpoint to be exactly representable, which
means dyadic, which means 5^n must divide (2k+1). At six places the smallest real
one is 2^-7, or `0.0078125`, sitting exactly between `0.007812` and `0.007813`;
`0.0234375` is the next. At one place, `340.25` qualifies because its fractional
part is a quarter. *(Reached independently by the Python and Kotlin
implementations, which is why it is worth stating twice: two routes to one value
are stronger evidence than either route.)*

The JavaScript suite keeps `0.0000005` beside the real ties as a named non-tie,
so nobody later mistakes it for the tie case.

A related trap on the mining side: a detector that asks whether a neighbouring
spelling of the same length also round-trips does not identify a tie. It asks
whether the value is the nearest double to a decimal, and many decimals share one
nearest double. A table mined that way decorates rather than discriminates
(section 10.2).

---

## 7. Geometry

### 7.1 The projection model, and why the cosine reference decides

`SPEC §3.3.3` gives a 50 metre tolerance for an inline chain endpoint and names
the distance function: a local equirectangular projection about the query point's
latitude, mean Earth radius 6371008.8 m, longitude scaled by the cosine of that
latitude.

Every part of that sentence is load-bearing. Four plausible models measured
against one corpus endpoint spread from 19.81 m to 36.47 m, a factor of 1.8:

| Model | Distance |
|---|---|
| equirectangular, cosine of the query point's latitude, as specified | 19.81 m |
| equirectangular, cosine of each point's own latitude | 20.34 m |
| degrees scaled by 111 320 on both axes, no cosine | 25.52 m |
| Web Mercator | 36.47 m | An implementation reaching for Web
Mercator, or scaling by degrees with no cosine term, passes every structural
check and disagrees about which documents are legal.

### 7.2 A refusal fixture cannot discriminate, so the arc's direction is the test

This is the least obvious property in the corpus and the first casualty of a
tidy-up.

A fixture that must be refused cannot tell a correct model from one that
over-reads, because a model reading a larger distance also refuses. Every wrong
model refuses a 55 m case.

Discrimination comes from the arc's direction. Run the arc north to south, so its
first vertex sits at high latitude and the query point at low, and a model taking
the cosine of the segment's start latitude under-reads: 36.845 m against a true
55, which accepts a document the format refuses. That is the more dangerous
direction, and only an arc pointing that way exposes it.

The two tolerance cases therefore run in opposite directions on purpose.
`corpus/accept/inline-endpoint-within-tolerance.vnx` catches a model that
over-reads and refuses good geometry;
`corpus/diagnose/inline-endpoint-beyond-tolerance.vnx` catches one that
under-reads and accepts bad geometry.

The separation has to be large enough for the error to cross the threshold rather
than merely move the number, and that is a stronger requirement than it sounds.
On an arc spanning three degrees of latitude, a segment-start-cosine model reads
48.87 m and 59.73 m where the correct model reads 45 and 55: an 8.5 per cent
error, plainly visible in the numbers, and the verdict is unchanged on both
files. Thirteen degrees gives 67.447 m and 36.845 m, which flips both. A fixture
that moves a number without moving a verdict separates nothing.

All four figures assume the mean radius of 6 371 008.8 m that `SPEC §3.3.3`
names, and only that constant reproduces the 45 and 55 the fixtures are named
for: at 111 132 m per degree the same coordinates solve to 44.974 and 54.969.
The difference never moves a verdict, which is exactly why a distance quoted
without its constant outlives both.

---

## 8. Diagnostics and refusal

### 8.1 Two channels, and a caller who can tell them apart

`SPEC §6.2` and `SPEC §6.3` divide every bad document into two outcomes, and they
are separate channels rather than two severities on one list. A document that
cannot be read at all is **refused**: your language's error mechanism, carrying
one of `SPEC §6.2`'s causes. A document that can be read with pieces dropped
comes back as a document **alongside** its diagnostics.

Collapsing them is the common shape and it costs the caller the distinction
between "you have nothing" and "you have most of it". Every implementation here
raises a dedicated refusal type and returns diagnostics as data.

`SPEC §6.3` gives a diagnostic a stable `code`. That is what a conformance case
compares; the English message beside it is advisory and nothing compares it. Keep
both, and keep the code a closed set with no catch-all member — a catch-all is
how an unclassified failure reaches a caller wearing a classification.

Refusals are matched as an English substring by the `reject/` sidecars, which is
the remaining place the corpus compares English across implementations. Structure
your refusal causes as a closed set regardless, so the day refusals gain codes is
a mapping rather than a rewrite.

### 8.2 Nothing malformed becomes a default

There are no fallbacks. A malformed value is dropped and a diagnostic names the
cause and the JSON Pointer. It never becomes a zero, an empty string, or an empty
collection.

The rule is easy to agree with and easy to break in one specific place: a
"sensible" default for a missing or unparseable number reads as robustness and is
indistinguishable, downstream, from a document that said so. `Track#movingTime`
returning zero where the answer is unknown is the worked example — unknown is not
zero, and `corpus/accept/track-without-times.vnx` and
`corpus/accept/track-partly-timed.vnx` are where it shows.

The two absences that legitimately carry a meaning are in section 4.1, and they
are absences in a conforming document rather than malformed values.

### 8.3 Geometry is continuous and structure is not

`SPEC §6.2`'s remedies read as inconsistent and are not. A position inside `pips`
that will not parse drops **the arc or segment that holds it**, not the position.
A link that cannot be used is dropped and its chain survives.

They are the same rule. A missing vertex changes the shape of a line and leaves
nothing to indicate it: a shorter, plausible, wrong line. A missing link leaves a
gap the continuity rule of `SPEC §3.3.3` can see and report, so nothing is lost
silently. A ring vertex that will not parse drops the **area**, for the same
reason plus `SPEC §3.7`: a polygon is one closed ring of at least four positions,
so dropping a vertex both moves the boundary and can turn a four-vertex ring into
a three-vertex non-ring.

A diagnostic should therefore name *what was dropped* rather than only what was
wrong, because the remedy is not the same everywhere and a caller should not have
to derive it from the pointer. `corpus/diagnose/position-unusable.vnx` is the
case.

### 8.4 Resource bounds are the caller's decision, not yours

`SPEC §9` says a reader SHOULD bound what one document may consume, and the
library cannot know the bound while the caller can: an application exporting its
own library legitimately produces a ten-million-waypoint document, and a server
accepting an upload from a stranger does not. Any constant picked inside the
library is wrong for one of them.

So the answer, if you implement it, is **a lever rather than a limit**: an
optional cap, defaulting to none, refusing beyond it with a cause. That is the
no-fallbacks rule applied to a resource policy — do not invent one, expose the
decision.

One bound belongs in the library where the others do not: **nesting depth**.
Recursion depth is the one resource whose failure is a stack overflow rather than
slowness, and a stack overflow is not a refusal a caller can handle. That makes
it a correctness bound rather than a policy one.

---

## 9. The container

`SPEC §7` restricts a `.vnz` to store and deflate and constrains no other field
of the archive, which makes a general-purpose ZIP library look sufficient. It is
not, for three reader rules and one instrument problem.

### 9.1 Three rules a general-purpose ZIP library will not give you

- **An over-declared entry size is accepted where the CRC agrees.** A library
  that treats a size mismatch as corruption refuses a document `SPEC §7` says
  to read. `corpus/accept/container-over-declared-size.vnz` is the case.
- **An unusable compression method takes a different remedy for the root
  document than for an attachment.** A library gives you one behaviour for
  both. `corpus/diagnose/container-unusable-method.vnz` is the case.
- **A duplicate entry name is seen twice rather than resolved.** Every library
  resolves it, because resolving it is what an archive reader is for.
  `corpus/reject/container-duplicate-entries.vnz` is the case.

Add ZIP64: `SPEC §7` requires a reader to accept it. Writing it is optional, and
every implementation here writes plain archives.

Build your own ZIP64 fixture with care, because the obvious route leaves the
parser untested. A reader takes its sizes from the central directory, so an
archive that masks them in the **local header only** — which is what Python's
`force_zip64` produces — can be read correctly with the extra-field parser
deleted entirely. Mask both sizes and the local-header offset in the *central
directory*, and saturate every field the end record can, so the ZIP64 locator
has to be followed.

### 9.2 A reader and a writer by the same hand agree perfectly

Two halves written by one author can be wrong in the same way and never
disagree, so **read every archive you produce back with a library that never saw
this specification**.

Necessary and not sufficient, which is the sharper half. Every ZIP library
resolves an entry from the **central directory**: it is the authoritative index
and the local header a hint. So no library-based check can witness a rule about a
local header, whoever wrote the library. Corrupt a local header's version and CRC
and a library-based reader reports nothing wrong — it never consults that header.

> An independent reader is not automatically an independent instrument. It can
> consult the same field the defect is not in.

`SPEC §7.1` requires the UTF-8 filename flag on **every entry**, and that flag
sits in the local header as well as in the central directory. No reader built on
a ZIP library can witness the local copy, because every such library answers from
the central directory. Walk the archive forward as bytes.

The same blindness makes a fixture look like coverage when it is not. A
committed `.vnz` whose local headers carry zeros where the central directory
carries the truth is read correctly by every implementation, because a conforming
reader takes its sizes from the central directory. A fixture can hold a construct
and test nothing about it.

### 9.3 Three hazards in the reader, each with a mutation behind it

- **Never reserve a declared uncompressed size up front.** Use it as a
  **ceiling**: hand the decompressor one byte more than the declared size and no
  more, so a stream that over-produces is stopped there rather than run to
  completion and measured afterwards. In some languages an allocation failure
  aborts the process rather than returning an error, which no caller can
  intercept.
- **Check the CRC.** A corrupt *attachment* is caught by nothing else: the
  document keeps parsing, so the archive reads clean.
- **A stored entry whose two declared sizes disagree is not a refusal.** The
  temptation is strong, because for a stored entry the two are equal in a
  well-formed archive and a disagreement looks like a truncated entry read as
  whole. `SPEC §7` forbids refusing on it, for the same reason it forbids
  refusing on a declared size that exceeds the bytes present: the CRC settles
  what the entry contains and a size field does not. Record a diagnostic if you
  like; do not refuse, and do not drop the entry.

  Work out what bounds the read once the two sizes need not agree, and write it
  down. The declared *compressed* size, capped by the next local header or the
  central directory, is the one that decides how many bytes a stored entry
  yields; the declared uncompressed size is not consulted on that path, so a
  disagreement between the two cannot change what comes back.

A sweep of every prefix and every single-byte flip of a container reaches none
of the following, and it is worth knowing why. A ZIP64 extended information
field can name a local-header offset anywhere in a 64-bit range, so
`offset + length` can overflow a pointer-sized integer before any bounds check
runs. In a release build it wraps and the archive is refused, so nobody notices;
in a debug build it panics. Reaching it needs an offset near the top of the
64-bit range, which is a hand-built archive rather than a bit-flip of a
committed one.

Every corpus fixture is well-formed, including the ones that must be refused —
they are refused for *format* reasons rather than for being damaged. So no tier
asks what happens to a truncated download or a corrupt archive. Sweep for it
yourself, and require a refusal or a clean read, never a crash.

---

## 10. Testing this format

### 10.1 Know which rules no fixture reaches

The accept, reject and diagnose tiers all start from a document that exists, so
`SPEC §4.1`'s rounding rule is satisfied by every value in them before a writer
is asked anything. That put rounding outside the corpus's reach, and each
implementation carried its own tests for it.

`corpus/write/` decodes an input and calls the write path, so rounding, both tie
directions and the shortest spelling of a position are all reachable. An
implementation exposing no write path fails it by name rather than by a confusing
diff.

The entry is worth keeping for what happened to the claim attached to it. The
writer boundary was described, in this document and elsewhere, as permanently
beyond the corpus's reach. It was beyond the reach of the tiers that existed, and
a new tier reached it.

> An unreachability claim is a claim about the current state of somebody's
> ingenuity, not about the format.

The opposite failure looks like tidying up. An artefact can be emptied by an act
that reads as conformance: sixty decorative tie rows, a sorted attachment pair.
**Before regenerating a fixture, say which rule each existing one is the only
witness for.**

A related trap, and the one condition here that care does not fix: a wrong reason
under a right conclusion has nothing with a motive to check it. A wrong reason
under a wrong conclusion is caught by whoever tests the conclusion; under a right
one it holds everything up while being false. Which gives the rule: **a reason
that has only ever answered one kind of question has never been tested.**

So the question is not which rules are unreachable but which are unreached
today, and what shape a case would take. Where two readings of a rule produce a
different document for one input, the accept tier reaches it. Where a rule takes
effect only when a model becomes a document, a write tier does. Where neither is
true, the prose is the only statement of the rule and your own tests the only
enforcement: the tolerance boundary at exactly 50 metres is one, because no two
distance models land on exactly 50.

### 10.2 A case only separates the readings its own data can separate

A fixture that *contains* a construct is not a test *of* it. What a case can
discriminate is decided by the data in the case, not by the construct it holds or
the note it carries.

The instances that have turned up fall into three mechanisms rather than one. The
symptom is shared and the repairs are not.

#### A case whose data cannot separate the readings

These were never checking the thing they named, because no answer they could
produce would differ between a correct implementation and a wrong one.

**A node-key comparison.** `SPEC §6.2` resolves a node key to its position before
comparing chain continuity, demoting key equality to a fast path. Compare the keys
instead and two distinct keys at one position do not meet, so a continuous walk is
reported as broken. One library shipped that reading and ran the whole corpus
green; its own tests could not help either, having been written to match it, and
the fix was as invisible as the defect.

`corpus/diagnose/chain-does-not-join.vnx` varies the node keys *and* the
positions together, so both comparators return the same verdict on it. It looks
like coverage, being in `diagnose/` and named for continuity, and it cannot
attribute its verdict to either reading. **Any fixture that changes two variables
together tests neither.**

**A ZIP64 fixture** that masked sizes in the local header only, so disabling the
extra-field parser left the test green (section 9.2).

**A tie table.** Sixty rows labelled as ties, mined by a detector that asked
whether a neighbouring spelling of the same length also round-trips. That
question does not identify a tie (section 6.3). Every one of the sixty stayed
green when the tie rule was reverted.

#### A check that was discriminating and quietly stopped

These worked when written. Something moved underneath them.

**A runner that stopped comparing.** A sidecar pins a diagnostic by reason code;
one matcher looked for an English `message` field, found nothing, and fell
through to comparing paths alone, leaving every diagnose case green while
checking half of what it pinned.

**Absence-proofs that go stale.** A proof that no tier catches some defect is
true until a case reaches it. See 10.3.

**A fixture retired by a rule change.** A test used two positions that collide
once rounded, to prove that arc-boundary de-duplication compares full values.
Change the rounding rule so the two stop colliding and the test asserts that a
de-duplication has not merged two points it could not have merged. Nothing about
it looks wrong at any point.

#### A guard that was never connected to what it names

The sharpest of the three, because the other two at least once measured
something.

**A test named for a fixture that never reads the fixture.** One called
`the_corpus_fixtures_land_where_their_names_say` would have passed with both
corpus files deleted, because it restated numbers rather than reading the
artefact. Beside it, a test asserting that the wrong cosine reference changes
neither verdict passed green while asserting something false of the corpus it
named. **A test that restates a number cannot notice when the artefact moves.**

**A diagnose tier that cannot see a code.** Every `diagnose/` fixture carries at
most one diagnostic and its path identifies it, so no case can separate a matcher
that compares the reason code from one that ignores it — measured: weakening the
matcher to compare the path alone leaves the whole tier green. The tier cannot
fix this for you, so the runner has to self-test its own matcher. Right path with
the wrong code must fail, and a sidecar carrying no code must fail rather than be
silently ignored.

The repair for all three is to make the test read the artefact. Mutate the
implementation, run the real corpus, and assert exactly which cases fail. That
turns "the corpus discriminates" from a restated number into a measurement, and
it goes red when a fixture moves.

Five implementations hit this class and each wrote a different guard, which is
the useful part, because the guards compose:

- fail a sidecar carrying a field the runner does not compare, rather than
  ignoring it *(JavaScript, Python)*;
- red-proof right-path, right-English, wrong-cause *(Kotlin)*;
- assert your cause table against a live corpus run in both directions, so a
  table entry the corpus stopped exercising fails as loudly as a corpus
  expectation the table never mentions *(Swift, JavaScript)*;
- sweep the fields *inside* a nested expectation, not only the top level of a
  sidecar *(Python)*;
- read your own output back with a foreign implementation *(Kotlin)*.

The detection recipe is cheap. Write down what the note claims the case tests,
then write down which values the case actually varies. Where it varies more than
one, ask which readings it can separate at all.

That recipe found three real gaps in one afternoon: the continuity fixture above;
`corpus/diagnose/link-names-absent-arc.vnx`, whose two links made "one defect
must not become two" a guarantee of arity rather than of the reader; and a
tolerance pair whose arc spanned too little latitude to separate the distance
models it existed to separate (section 7.2).

### 10.3 Red-proof everything, and check the proof reached its target

A passing corpus is not evidence that the corpus discriminates. Break one thing,
run the check that should notice, watch it go red, restore.

A red proof lies in five ways.

**It can miss its target.** One patched the library's function while the test
module had bound the name at import, so the assertion was calling the real one
and the guard stayed green. "The red proof stayed green" is the evidence someone
uses to throw a good test away.

**Its mutation can match nothing.** Which is why one suite makes each mutation
assert that its own search string was found: a substitution matching nothing
makes the whole file a test of nothing.

**The mutation can be broader than the rule it claims to test.** An
away-from-zero rounder built on a round-trip tie detector moves phantoms as well
as ties, reddening four corpus cases instead of one. Writing the assertion to
match that would attribute three failures to a rule they had nothing to do with.
The repair is a companion test freezing that the extra value is a phantom.

**The mutation may never reach the code it claims to break.** A notes-discarding
mutation came back green because it patched only the first of several call sites,
and no accept fixture had a folder carrying `notes`. A mutation that misses its
target is indistinguishable from a runner finding nothing wrong.

**It may not come back green** — the only one leaving a proof that measures
nothing. One patched two modules and left one active during the restore, so the
check it named was permanently red, which reads as confidence rather than as a
broken instrument. A red-proof author watches for the red, so the revert is the
half nobody tests. Assert both directions.

And a proof whose conclusion is an absence needs re-running whenever the thing it
claims to be absent from grows. A red proof asserting that no tier catches some
defect is a green-asserting proof, and those rot silently as the corpus gains
cases. Rounding is the worked example: the claim that no tier reached the writer
boundary was true when it was written and false once the write tier existed.

Write those deliberately. A mutation carrying an expected verdict of *green*
records a gap in the corpus rather than assuming one, and it goes red the moment
a fixture closes the gap, which is how its author finds out. That mechanism has
paid for itself: one entry reaching inward to a segment's first and last *timed*
pip survived, because `corpus/accept/track-partly-timed.vnx` carried no times at
all in its untimed segment and both readings returned unknown. The fixture gained
a timed middle pip, and the entry went red on the next run — so the promotion was
noticed rather than remembered.

The mutations that come back green are the ones worth the exercise. Of the 34
injected into one implementation, 26 went red as designed and taught their author
nothing not believed beforehand. Four came back green, and three of those paid
for the whole run.

They come back green for three reasons that are indistinguishable from the
console and call for three different actions.

**The mutation was bad.** It assigned a field overwritten three lines later, or
it deleted an assertion rather than injecting a defect, and removing a check
cannot make a suite fail. Fix the mutation.

**The mutation was fine and the guard was not there.** A ZIP64 test stayed green
with the extra-field parser deleted (section 9.2). A rounding oracle reached
neither the 64-shift arm of its 128-bit arithmetic nor the subnormal exponent, so
deleting either left the suite green; regenerating it to reach every arm
immediately found a real defect — rounding was one double wrong above about 9e14,
because an identity branch tested a power of two where the true boundary is a
power of ten, so past it the rounded integer exceeded 2^53. The green proof did
not merely reveal a weak test; it forced a better instrument, which then caught a
bug. The oracle is `swift/Tests/VNXTests/RoundingOracle.swift`, and its own
docstring states its size. Fix the test.

**The branch is unreachable.** A mutation of the version rule for 1.0 and above
is a provable no-op in a 0.1 reader, which can never take that branch
(`SPEC §2.2`), so it comes back green for a reason that is neither a weak guard
nor a bad mutation. Record the proof in the code, so the next person does not run
the same mutation, see green, and conclude the guard is weak.

An unreachable branch and an untested one look identical forever unless somebody
writes down which was proved. So: **when a red proof comes back green, you have
learned nothing until you have said which of the three it is.**

### 10.4 Red-proof the instrument, not only the code

> A comparison that has never been seen to fail is not a comparison. It is a
> green light with no bulb in it.

Build such a check from two halves that share no code: one language writes raw
IEEE 754 bits and its own answers to a table, and the other bit-casts each row
back and compares. Sample the whole binary64 range through a bits-to-double
constructor, add dense small magnitudes, and add every value the specification
and the corpus name.

Zero disagreements is worth nothing on its own. What makes the check worth having
is the count of disagreements that reappear when the tie mode is reverted, and a
check that cannot produce that number is indistinguishable from one that is not
running.

An agreement certifies only what its shared input varies. Four implementations
agreed on a container built from one input and cited it as their strongest
cross-language evidence. That input carried a single attachment, so the one rule
all four were breaking was a rule about several: **obeying it changed nothing
about the artefact they agreed on.** Silent on exactly the field in question, and
a matching hash offers no surface on which that silence shows. A note can be
doubted; a SHA-256 reads as agreement.

Two implementations measuring one fact is overlap only if their inputs differ.
Four writers fed one input is one measurement, four times.

Three traps in building such a table *(Kotlin implementation)*: bits must travel
as bits, since an unsigned parser fails on every negative double and, with the
error ignored, scores every one of them as a disagreement against `0`; a mining
criterion can decorate rather than discriminate (section 6.3); and the generator
must be deterministic, because a table nobody can diff is useless.

`corpus/numbers/numbers.tsv` is the shipped form of that idea: a set of doubles
as IEEE 754 hex,
with two reference answers each, rounded to 6 places and to 1. About ten lines to
read in any language, and it is compared **by bit pattern, never by text** —
comparing a spelling would put a rendering rule this format does not have back
into the measurement through the side door.

---

## 11. Guards

### 11.1 A rule with no guard at all

Section 10 is about a guard that fails. This is the state with no guard in it,
and it is the one made invisible by its own clarity.

A rule that is unambiguous, stated in the normative document, and enforced by
nothing produces total confidence and zero enforcement. An open question makes
people uneasy, so it stays on a list and gets checked. A settled one does not,
and nobody asks whether anything checks it, because the question feels answered.

The evidence is the partly-timed track. Three implementations summed a partial
moving time with every corpus tier green, and that was a rule `SPEC §3.5` stated.
Prose did not change what passed; a fixture did.

> Prose changes what people believe. A fixture changes what passes.

So "this one is ruled, so it must be handled" is backwards. A written rule with
no fixture is the one most likely to be quietly violated, because everyone
reading the specification believes it is enforced. Four rules sat in that state
at once: node-key resolution, the full-entry-name root document match,
`a..b/photo.jpg` being legal, and a backslash in an entry name being refused.

When you find one, the remedy is a fixture, and a local test in your own suite
until one exists. Say which it is in your notes, so the next implementation does
not assume the corpus covers it.

### 11.2 What a guard that works looks like

Three properties, from a test written to keep a corpus runner strict about which
field a sidecar pins.

**Build the input on disk; do not assert about the corpus.** A guard tested only
against inputs that satisfy it has not been tested, and the whole corpus
satisfies this one: every sidecar is well-formed, so no corpus file can exercise
the refusal. The test writes malformed sidecars into a temporary directory and
parses those.

**Carry a well-formed control.** Without one, two refusal tests prove only that
the parser rejects *something*, not that it rejects the thing they name. The
control is a valid sidecar asserted to parse and to yield the expected values,
and it is what makes the refusals discriminate rather than merely fail.

**Pin the state of the world the strictness assumes.** A separate test walks
every tier and asserts that no sidecar carries the field the strict parser
refuses. If one appears, that goes red naming the corpus and the rule, instead of
surfacing as an opaque parse failure inside whichever tier happened to load it
first. A guard should fail informatively about why the world changed, not about
what it hit.

Two caveats came with it. The third property is a source scan, so it sees a
literal spelling and a sidecar spelling it differently escapes: a tripwire for
one regression rather than a validator. And it rests on a measurement of the
corpus as it stands. If the corpus deliberately reintroduces what the guard
refuses, the answer is to change the rule rather than loosen the guard back into
a fallback.

One note on what a guard's comment is for. Where a check's shape looks arbitrary
without the argument that produced it, the comment earns that argument: a reader
who finds a rule and its check agreeing should be able to see why the check is
spelled as it is, and the alternative is somebody simplifying it back. That is
not licence for a published document to narrate its own past, since a reader of
the prose has no stake in which draft came first. The two get confused because
both look like history. **The test is whether a reader needs it to avoid undoing
the check.**

---

## 12. Where each language fell

Where each of the six met the traps above, with the section that covers each in
brackets. The link on each name goes to that library's page, which documents the
library rather than repeating what is here.

**Go** — [`go/README.md`](go/README.md). `math.Round` is half
away from zero (6.2), and the reference implementation rounded by scaling, which
is a different operation (6.1). `encoding/json` accepts `1e400` into
`json.Number` and refuses it only into `float64` (5.1).

**Python** — [`python/README.md`](python/README.md). `json`
accepts `NaN` and `Infinity` by default, so a Python reader either makes its
parser strict or refuses under `SPEC §4.2` with wording the `reject/` sidecars do
not expect (5.1). Decimal intuition about which way a value rounds is wrong in
the same way in every language, and Python is where it was measured (6.1).

**Swift** — [`swift/README.md`](swift/README.md). `.rounded()`
does not round half to even; the emit boundary needs `.toNearestOrEven` (6.2).
`JSONSerialization` spells `-3.67` as `-3.6699999999999999`, so the renderer is
`JSONEncoder` while the parser is hand-written (3).

**Kotlin** — [`kotlin/README.md`](kotlin/README.md).
`BigDecimal.HALF_UP` at the rounding boundary is half away from zero, and
`Math.round(x * 10) / 10` is half-up on the JVM, so a guard testing only negative
ties passes it (6.2). The JVM standard library has no JSON API at all, which
makes the reader-versus-renderer split of section 3 an explicit decision rather
than a default.

**JavaScript** — [`js/README.md`](js/README.md). `toFixed`
rounds ties away from zero on both signs, and `Math.round(x * 10) / 10` is
half-up (6.2). `1e400` parses to infinity rather than failing, so a reviver or an
equivalent post-parse check is the only thing between it and a coordinate of
infinity (5.1).

**Rust** — [`rust/README.md`](rust/README.md). `f64::round` is
half away from zero, and `round_ties_even` is the right rounding mode applied to
the wrong operation (6.1, 6.2). `serde_json` needs an opt-in feature to keep the
last bit of a full-precision coordinate (3). The idiomatic model of a collection
loses a distinction the format carries (4.1). A lone surrogate cannot be
represented in a `String`, so the parser refuses one for free.

---

One last thing, from the implementations that found two invisible defects in a
day, both by comparing against another implementation rather than by reasoning:

> When you have a choice between arguing about a specification clause and
> measuring two implementations, measure.

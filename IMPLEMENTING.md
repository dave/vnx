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
3.  Watch a green run before writing anything:

    ```sh
    (cd js && node corpus/run.js)    # prints "n/m cases passed" and exits 0
    (cd go && go test -count=1 ./...)
    ```

4.  Write the runner before the library. Point it at `corpus/`, walk every
    fixture, compare against the sidecar beside it, and treat a fixture with no
    sidecar as a failure, not a skip.
5.  Run it. Every case fails. Make them pass one tier at a time, in the order
    of section 2.
6.  Red-proof the runner. Break your library one rule at a time, run the check
    that should notice, and require it to go red (section 10.3).
7.  Ship when `corpus/` is green and every red proof is red.

The rest of this document is where a language's defaults quietly disagree with
the format, and where a test written to catch that disagreement fails to catch
it. Six implementations were written from `SPEC.md` and `corpus/` alone: Go,
Python, Swift, Kotlin, JavaScript and Rust. Everything below is something at
least one of them got wrong first. Each entry gives the requirement, the
languages that fall into it, and the measurement. Measurements are attributed to
the implementation that made them; nothing here is inferred from another
implementation's source.

Read it before writing your encoder, not after your corpus run goes red.

A caution about the numbers in it. Each is a measurement of a particular file at
a particular moment, not a property of the format. Fixtures move. A number
copied out of this document into a test asserts, from then on, that a file says
what it said. Name the artefact beside it, or write the claim as a
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
| `write/` | The one emit-boundary rule: `SPEC §4.1`'s shortest spelling of a position. No other document tier reaches it. |

Two properties of that order:

`accept/` is the hardest tier and the one to build first. Its comparison is
against the tree parsed from the original file, never against a second model. A
reader that discarded every data record, every application key and
every `notes` produces a model equal to itself and passes a model-versus-model tier
green: the whole extensibility mechanism, all styling and all user prose,
dropped by an implementation that reports itself conforming. *(Frozen as a
proof by the Rust, Swift, Python and JavaScript implementations independently.)*

`write/` is the only tier that reaches a writer. The other three
start from a document that exists, so every value in them satisfies `SPEC §4.1`
before your writer is asked anything (section 10.1).

---

## 3. Choose your JSON layer

This is the first decision and the most expensive one to revisit. The format's
reading rules are stated in detail, and a stock parser answers on its own terms.
Work down this table against the library you were going to reach for.

| What the format needs | What a stock JSON layer tends to do | Cite |
|---|---|---|
| `NaN`, `Infinity` and `-Infinity` refused | Several accept them by default. It is a flag to turn off, not a behaviour you inherit. | `SPEC §4.2` |
| A literal whose value is not finite in binary64 refused | `1e400` is well-formed JSON. Parsers differ: one errors, one throws, one returns infinity in silence, one keeps the text and fails only on conversion. | `SPEC §4.2` |
| One diagnostic per bad path, the rest of the document kept | Stops at the first syntax error, and reports one failure for the whole document. | `SPEC §6.2` |
| Every number parsed as binary64, integer literals included | Widens or narrows to the type it infers: an integer type for an integer literal, a decimal type for a number with a point. | `SPEC §4.2` |
| The last bit of a coordinate preserved | Short-parse paths trade the last bit for speed, and a full-precision coordinate is exactly where that shows. | `SPEC §4.1` |
| Duplicate keys seen, or the limitation accepted knowingly | Resolves last-wins before any format rule runs, so an entry vanishes unobservably. | `SPEC §6.4` |
| Strict reading | Offers permissive modes (trailing commas, comments, unquoted keys) that accept exactly what `SPEC §4.2` forbids. | `SPEC §4.2` |
| Number rendering | Renderers disagree about the shortest round-tripping spelling. A renderer that spells `-3.67` as `-3.6699999999999999` round-trips correctly and is legal. | `SPEC §8.1` |

Read the table in two halves; reading and writing land differently.

The reader is the half you probably write yourself. Six of the eight rows above
are reader rules, and the format spells out what a reader does with a document
that is wrong in a particular way. A parser that stops at the first error cannot
produce the per-path diagnostics `SPEC §6.2` asks for. Writing a JSON parser
that produces a value tree is a day's work, and it buys strictness, the position
of each value for a JSON Pointer, and the option of seeing a duplicate key.

The writer is the half you should not write. Nothing about a document's bytes
is a rule: `SPEC §4.2` defines round-trip at value level and `SPEC §8.1`
compares parsed trees. Key order, number spelling and whitespace are your own
business, and a hand-written renderer buys nothing. Take the stock one. Check
that it renders a double as a shortest round-tripping decimal before you commit
to it.

**Do not decode through a derive macro, a `Codable` conformance, or any other
schema-driven binding.** It is the shape every one of these languages pushes you
towards, and it is wrong twice over. It stops at the first field it cannot read,
where `SPEC §6.2` wants the document read and the bad path reported. And it
silently drops every key your type does not name, which is the extensibility
mechanism of `SPEC §3.10`, `SPEC §5.1` and `SPEC §3.9`. Parse to a generic value
tree and walk it by hand.

Two settings are easy to miss:

- A parser may offer an exact-float mode as a non-default feature flag. Without
  it, `corpus/accept/full-precision-coordinate.vnx` decodes one bit low, and
  the last bit of a coordinate is what that case exists to protect.
- A parser may hand you a decimal or big-integer type instead of a double.
  `SPEC §4.2` defines numbers as IEEE 754 binary64, so the conversion has to
  happen, and doing it late means two values that are one document compare as
  two.

---

## 4. The model

These are decisions made in the first hour and discovered in the last.

### 4.1 Absence, and the places it means something different

`SPEC §3.1`: absence means "not known" and is not a default value, with two
stated exceptions: `hidden` and `pinned`.

For an OPTIONAL collection, absent and empty carry the same content and
`SPEC §3.1` requires a reader to treat them alike. `SPEC §8.1` normalises the
two to one value, so the accept tier cannot see the difference and is not meant
to. A model that holds both shapes is free to; a model that folds them together
is equally conforming.

For a REQUIRED key, they are distinct. `SPEC §3.10`: `"walk": []` satisfies
`walk` and an absent `walk` does not. A required key must be present for the
object to exist.

A `hidden` written as `false` is a third case: `SPEC §3.8` gives absence the
meaning visible, and a writer emits the key only when it is true.

The language pulls here. A plain list for every collection, where absent and
empty are one value, is more idiomatic in Rust than an optional list, and every
reviewer would suggest the change. For the six document collections that
change is safe: `SPEC §3.1` puts the two shapes on the same footing. It is not
safe for a required key. A model that cannot tell an absent `walk` from an
empty one cannot report the object as malformed.

Those two absences, visible and not pinned, are the only ones `SPEC §3.1`
gives a meaning. Every other absence is "not known" and stays that way through a
read-modify-write.

### 4.2 Opaque bodies survive at value level

Data records (`SPEC §5.1`) and application keys (`SPEC §3.9`) are opaque: no key inside them is ever unrecognised, and each has
to survive a read-modify-write untouched.

An application key differs from a data record in three ways, and each is a
place to go wrong:

- The rule is about position, not about the key. A key containing a full stop
  is an application key only at the top level of a document. On any other
  object the format defines it is an unrecognised key and is reported, except
  inside the opaque objects of `SPEC §3.10`, where it is carried. A decoder
  that tests every object's keys for a full stop skips diagnostics the corpus
  expects, and `corpus/diagnose/dotted-key-on-item.vnx` is what catches it.
  One that reports a dotted key inside a record body or an
  application key's value fails `corpus/accept/application-keys.vnx`.
- The value is any JSON value, not only an array or an object. A `null` has to
  come back as `null`, and an empty array as an empty array. The absent-equals-
  empty normalisation of `SPEC §8.1` does not apply, so a model that drops a
  key whose value is empty or `null` fails `corpus/accept/application-keys.vnx`.
- The key set is open. Hold application keys as a list of key-value pairs in the
  document model, not as one field per known key. A field per key is the
  derive-macro failure of section 3 again, one level up. A list can hold one
  key twice, so refusing a repeated key is the writer's job (`SPEC §3.9`).

That is a modelling decision, not a serialisation one. Decode a record body into
a typed structure and you lose the fields your type does not name. The accept
tier's tree comparison is the only thing that catches it: every other assertion
in a sidecar is about a construct the format names.

Keep such a body as whatever your JSON layer produced. It is the one place where
holding an untyped value is the correct model.

A highlight is a lightweight container for data objects along a walk of its
network's arcs (`SPEC §3.3.3`). It carries `walk`, an optional `name`,
`hidden`, `pinned` and `data`, and no notes of its own. A decoder that
shares one key set across every item, or a model that gives a highlight a
`notes` field because every other item has one, accepts a `notes` on a
highlight without a word. `corpus/diagnose/highlight-notes-unknown.vnx` expects
the unrecognised-key diagnostic, and `corpus/accept/highlight-name.vnx` reads a
named and an unnamed highlight with none.

### 4.3 A position's arity is a spelling, not a fact

`[lon, lat]` and `[lon, lat, null]` state the same thing, and a reader must take
either. Implementations split on whether the model holds the arity the file
spelled or a fixed set of channels, and both pass: a writer emits the shortest
spelling of a position (`SPEC §4.1`) and the accept tier's comparison normalises
trailing `null` channels on both sides (`SPEC §8.1`).

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
Python's `json` module among them. This is a flag to turn off, not a behaviour
you inherit.

A literal like `1e999` or `1e400` is well-formed JSON whose value is not finite
in binary64: it parses to infinity instead of failing. Check after parsing as
well as during; a `JSON.parse` reviver or equivalent hook is enough.

The three literals fail differently in different runtimes, and the differences do
not line up:

| literal in `{"a": …}` | Go `encoding/json` | Node `JSON.parse` | Python `json.loads` |
|---|---|---|---|
| `NaN` | error | throws | accepts, giving `nan` |
| `Infinity` | error | throws | accepts, giving `inf` |
| `1e400` | error into `float64` | returns infinity silently | accepts, giving `inf` |

Go accepts `1e400` into `json.Number`, keeping the text, and refuses it only on
unmarshal into `float64`.

Two consequences. The `reject/` sidecars expect the substring `malformed JSON`,
which describes what a *strict* parser says; both Go and Node are strict here, so
it is a strict-parser artefact, not one language's. The implementation it bites
is Python, whose `json.loads` accepts all three by default: a Python
implementation either makes its parser strict or refuses under `SPEC §4.2`, and a
`SPEC §4.2` refusal legitimately does not contain the words "malformed JSON".

And `1e400` is the case every library catches for itself. Node hands back
infinity with no complaint, so a reviver or an equivalent post-parse check is
the only thing between it and a coordinate of infinity.

### 5.2 Duplicate object keys

Two entries with the same key resolve last-wins in Go's `encoding/json`, Node's
`JSON.parse` and Python's `json`, all measured, before any format rule runs. The
entry vanishes and a reader built on a stock parser cannot observe it
(`SPEC §6.4`). `corpus/diagnose/duplicate-object-key.vnx` expects no diagnostic.

The format pushes you into writing your own parser anyway (section 3), and once
you have one, detecting a duplicate key is about three lines. The limitation
belongs to stock parsers, not to the format. Since the corpus case expects no
diagnostic, a reader that gains the ability turns it red. That is the signal to
revisit the limitation, not a failure.

### 5.3 Parse every number as binary64

Integer literals included. `SPEC §4.2` defines numbers as IEEE 754 binary64, so a
JSON integer is a binary64 too, and rendering it as one is what makes
`1778914991000.0` and `1778914991000` the same document.

---

## 6. Numbers

Parse every number as binary64 and write back the value, not the text
(`SPEC §4.2`). That is the whole of it.

Nothing here requires you to round. `SPEC §4.1` does not constrain a writer's
precision, so an implementation that writes what it was handed is conforming and
has no rounding rule to get right.

If your application chooses to round, to make its files smaller, which is the
only thing rounding buys, two traps are hard to see. `Math.round(x * 10) / 10`
is half-up on the JVM and in JavaScript, not half-to-even. And scaling by a
power of ten before rounding is a different operation from formatting to N
places: the scaled product is itself a binary64, so it manufactures ties the
true decimal never had and destroys ones it did. Both are your implementation's
business, not the format's.

The reader's half is a rule. A reader MUST NOT round. Whatever precision a
document states is what the reader keeps, and
`corpus/accept/full-precision-coordinate.vnx` is the case that proves it. In
Rust this needs `serde_json`'s `float_roundtrip` feature, which is why
`Cargo.toml` pins it with a comment: without it `-3.6712345678901235` parses one
bit away from what `str::parse` gives.

---

## 7. Geometry

### 7.1 The projection model, and why the cosine reference decides

`SPEC §3.3.3` has a walk position identify the nearest point of its arc's line,
at any distance, and names the distance function that decides "nearest": a
local equirectangular projection about the position's latitude, mean Earth
radius 6371008.8 m, longitude scaled by the cosine of that latitude.

Every part of that sentence is load-bearing. Four plausible models measured
against one position 20 m from its arc spread from 19.81 m to 36.47 m, a
factor of 1.8:

| Model | Distance |
|---|---|
| equirectangular, cosine of the query point's latitude, as specified | 19.81 m |
| equirectangular, cosine of each point's own latitude | 20.34 m |
| degrees scaled by 111 320 on both axes, no cosine | 25.52 m |
| Web Mercator | 36.47 m |

Distances that disagree by that much disagree about which segment is nearest
wherever a line bends back past a position, and so about the point a walk
starts, ends or turns at. A reader stores a walk position as written, so the
disagreement reaches no reader: it reaches every writer that draws a walk.

### 7.2 A walk is read whole, and checked in one order

A pass does not stand alone. Its entry is the node where the previous pass
left, and where it leaves depends on whether it has a `to` and whether it has a
`turn` (`SPEC §3.3.3`). Carry the current node through the walk, pass by pass;
do not try to validate a pass from its own keys.

`SPEC §6.2` drops the whole highlight at the first problem in its walk and lists the
order in which to look. The order is what makes two readers name the same pass
and the same code for a walk with two problems in it. Checking the rule for
`from` and `to` across the whole walk before looking at connection is a
plausible structure and reports a different pass.

The two node-level problems, a self-loop and an arc naming an absent node, are
reported on the arc. The highlights that walk it are then dropped for naming an
absent arc, each with its own diagnostic, so a reader resolves the arcs before
it reads any walk.

---

## 8. Diagnostics and refusal

### 8.1 Two channels, and a caller who can tell them apart

`SPEC §6.2` and `SPEC §6.3` divide every bad document into two outcomes, and they
are separate channels, not two severities on one list. A document that cannot be
read at all is refused: your language's error mechanism, carrying one of
`SPEC §6.2`'s causes. A document that can be read with pieces dropped comes back
as a document alongside its diagnostics.

Collapsing them is the common shape, and it costs the caller the distinction
between "you have nothing" and "you have most of it". Every implementation here
raises a dedicated refusal type and returns diagnostics as data.

`SPEC §6.3` gives a diagnostic a stable `code`. That is what a conformance case
compares; the English message beside it is advisory and nothing compares it. Keep
both, and keep the code a closed set with no catch-all member. A catch-all is how
an unclassified failure reaches a caller wearing a classification.

Refusals are matched as an English substring by the `reject/` sidecars, which is
the remaining place the corpus compares English across implementations. Structure
your refusal causes as a closed set regardless, so the day refusals gain codes is
a mapping, not a rewrite.

### 8.2 Nothing malformed becomes a default

There are no fallbacks. A malformed value is dropped and a diagnostic names the
cause and the JSON Pointer. It never becomes a zero, an empty string, or an empty
collection.

The rule is easy to agree with and easy to break in one specific place: a
"sensible" default for a missing or unparseable number reads as robustness and is
indistinguishable, downstream, from a document that said so. `Track#movingTime`
returning zero where the answer is unknown is the worked example. Unknown is not
zero, and `corpus/accept/track-without-times.vnx` and
`corpus/accept/track-partly-timed.vnx` are where it shows.

The two absences that legitimately carry a meaning are in section 4.1, and they
are absences in a conforming document, not malformed values.

### 8.3 A position drops its owner, and a walk drops its highlight

`SPEC §6.2`'s remedies read as severe and are not. A position inside `pips`
that will not parse drops the arc or segment that holds it, not the position. A
missing vertex changes the shape of a line and leaves nothing to indicate it: a
shorter, plausible, wrong line. A ring vertex that will not parse drops the
area for the same reason.

A walk goes further: any problem in it drops the highlight. A pass removed from the
middle joins two arcs that do not meet, a `turn` removed reverses everything
after it, and a `from` removed moves the start to a node. Each of those is a
different walk that reads as a valid one, and a caller holding it has no way to
know.

A diagnostic should therefore name *what was dropped*, not only what was wrong.
The remedy is not the same everywhere, and a caller should not have to derive it
from the pointer. `corpus/diagnose/position-unusable.vnx` is the case.

### 8.4 Resource bounds are the caller's decision, not yours

`SPEC §9` says a reader SHOULD bound what one document may consume. The library
cannot know the bound while the caller can: an application exporting its own
library legitimately produces a ten-million-point document, and a server
accepting an upload from a stranger does not. Any constant picked inside the
library is wrong for one of them.

So the answer, if you implement it, is a lever, not a limit: an optional cap,
defaulting to none, refusing beyond it with a cause. That is the no-fallbacks
rule applied to a resource policy. Do not invent one; expose the decision.

One bound belongs in the library where the others do not: nesting depth.
Recursion depth is the one resource whose failure is a stack overflow, not
slowness, and a stack overflow is not a refusal a caller can handle. That makes
it a correctness bound, not a policy one.

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
- **A duplicate entry name is seen twice, not resolved.** Every library
  resolves it; resolving it is what an archive reader is for.
  `corpus/reject/container-duplicate-entries.vnz` is the case, and
  `corpus/reject/container-duplicate-entries-case.vnz` is the one whose names
  differ only in case, which a comparison on the exact name accepts.

Add ZIP64: `SPEC §7` requires a reader to accept it. Writing it is optional, and
every implementation here writes plain archives.

Build your own ZIP64 fixture with care; the obvious route leaves the parser
untested. A reader takes its sizes from the central directory, so an archive
that masks them in the local header only, which is what Python's `force_zip64`
produces, can be read correctly with the extra-field parser deleted entirely.
Mask both sizes and the local-header offset in the *central directory*, and
saturate every field the end record can, so the ZIP64 locator has to be
followed.

### 9.2 A reader and a writer by the same hand agree perfectly

Two halves written by one author can be wrong in the same way and never
disagree. Read every archive you produce back with a library that never saw
this specification.

Necessary and not sufficient. Every ZIP library resolves an entry from the
central directory: it is the authoritative index and the local header a hint. So
no library-based check can witness a rule about a local header, whoever wrote
the library. Corrupt a local header's version and CRC and a library-based reader
reports nothing wrong; it never consults that header. An independent reader is
not automatically an independent instrument. It can consult the same field the
defect is not in.

`SPEC §7.1` requires the UTF-8 filename flag on every entry, and that flag sits
in the local header as well as in the central directory. No reader built on a
ZIP library can witness the local copy. Walk the archive forward as bytes.

The same blindness makes a fixture look like coverage when it is not. A
committed `.vnz` whose local headers carry zeros where the central directory
carries the truth is read correctly by every implementation, because a conforming
reader takes its sizes from the central directory. A fixture can hold a construct
and test nothing about it.

### 9.3 Three hazards in the reader, each with a mutation behind it

- **Never reserve a declared uncompressed size up front.** Use it as a ceiling:
  hand the decompressor one byte more than the declared size and no more, so a
  stream that over-produces is stopped there instead of run to completion and
  measured afterwards. In some languages an allocation failure aborts the
  process instead of returning an error, which no caller can intercept.
- **Check the CRC.** A corrupt *attachment* is caught by nothing else: the
  document keeps parsing, so the archive reads clean.
- **A stored entry whose two declared sizes disagree is not a refusal.** The
  temptation is strong: for a stored entry the two are equal in a well-formed
  archive, and a disagreement looks like a truncated entry read as whole.
  `SPEC §7` forbids refusing on it, for the same reason it forbids refusing on
  a declared size that exceeds the bytes present: the CRC settles what the
  entry contains and a size field does not. Record a diagnostic if you like; do
  not refuse, and do not drop the entry.

  Work out what bounds the read once the two sizes need not agree, and write it
  down. The declared *compressed* size, capped by the next local header or the
  central directory, is the one that decides how many bytes a stored entry
  yields; the declared uncompressed size is not consulted on that path, so a
  disagreement between the two cannot change what comes back.

A sweep of every prefix and every single-byte flip of a container reaches none
of the following. A ZIP64 extended information field can name a local-header
offset anywhere in a 64-bit range, so `offset + length` can overflow a
pointer-sized integer before any bounds check runs. In a release build it wraps
and the archive is refused, so nobody notices; in a debug build it panics.
Reaching it needs an offset near the top of the 64-bit range, which is a
hand-built archive, not a bit-flip of a committed one.

Every corpus fixture is well-formed, including the ones that must be refused;
they are refused for *format* reasons, not for being damaged. So no tier asks
what happens to a truncated download or a corrupt archive. Sweep for it
yourself, and require a refusal or a clean read, never a crash.

---

## 10. Testing this format

### 10.1 Know which rules no fixture reaches

The accept, reject and diagnose tiers all start from a document that exists, so
every emit-boundary rule is already satisfied by the values in them before a
writer is asked anything.

`corpus/write/` decodes an input and calls the write path, so the shortest
spelling of a position is reachable. An implementation exposing no write path
fails it by name, not by a confusing diff.

The writer boundary was described, in this document and elsewhere, as permanently beyond
the corpus's reach. It was beyond the reach of the tiers that existed, and a new
tier reached it. An unreachability claim is a claim about the current state of
somebody's ingenuity, not about the format.

The opposite failure looks like tidying up. An artefact can be emptied by an act
that reads as conformance: sixty decorative tie rows, a sorted attachment pair.
Before regenerating a fixture, say which rule each existing one is the only
witness for.

A related trap, and the one condition here that care does not fix: a wrong reason
under a right conclusion has nothing with a motive to check it. A wrong reason
under a wrong conclusion is caught by whoever tests the conclusion; under a right
one it holds everything up while being false. A reason that has only ever
answered one kind of question has never been tested.

So the question is not which rules are unreachable but which are unreached
today, and what shape a case would take. Where two readings of a rule produce a
different document for one input, the accept tier reaches it. Where a rule takes
effect only when a model becomes a document, a write tier does. Where neither is
true, the prose is the only statement of the rule and your own tests the only
enforcement. The nearest point of an arc is one: a reader stores a walk position
as written, so no tier observes the point it identifies (section 7.1). A
writer's refusals are another. Every tier starts from a document, and the
reader drops any walk, arc or ring the writer would refuse, so only a model
built in code reaches those refusals. Test them in your own suite, once per
refusal, and red-proof each.

### 10.2 A case only separates the readings its own data can separate

A fixture that *contains* a construct is not a test *of* it. What a case can
discriminate is decided by the data in the case, not by the construct it holds or
the note it carries.

The instances that have turned up fall into three mechanisms. The symptom is
shared and the repairs are not.

#### A case whose data cannot separate the readings

These were never checking the thing they named. No answer they could produce
would differ between a correct implementation and a wrong one.

**A continuity comparison.** A continuity rule that compares two endpoints can
be read as comparing their keys or their positions. A fixture that varies the
keys *and* the positions together gets the same verdict from both comparators.
It looks like coverage, being in `diagnose/` and named for continuity, and it
cannot attribute its verdict to either reading. Any fixture that changes two
variables together tests neither.

**A ZIP64 fixture** that masked sizes in the local header only, so disabling the
extra-field parser left the test green (section 9.2).

**A tie table.** Sixty rows labelled as ties, mined by a detector that asked
whether a neighbouring spelling of the same length also round-trips. That
question does not identify a tie; it asks whether the value is the nearest
double to a decimal, and many decimals share one nearest double. Every one of
the sixty stayed green when the tie rule was reverted.

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
`SPEC §4.1` then stopped requiring a writer to round at all, so the two never
collide and the test asserts that a de-duplication has not merged two points it
could not have merged. Nothing about it looks wrong at any point. When a rule
leaves a format, search for the fixtures that were built on it; do not wait for
one to fail.

#### A guard that was never connected to what it names

The sharpest of the three; the other two at least once measured something.

**A test named for a fixture that never reads the fixture.** One called
`the_corpus_fixtures_land_where_their_names_say` would have passed with both
corpus files deleted, because it restated numbers instead of reading the
artefact. Beside it, a test asserting that the wrong cosine reference changes
neither verdict passed green while asserting something false of the corpus it
named. A test that restates a number cannot notice when the artefact moves.

**A diagnose tier that cannot see a code.** In every `diagnose/` fixture each
expected diagnostic has a path of its own, so no case can separate a matcher
that compares the reason code from one that ignores it. Measured: weakening the
matcher to compare the path alone leaves the whole tier green. The tier cannot
fix this for you, so the runner has to self-test its own matcher. Right path with
the wrong code must fail, and a sidecar carrying no code must fail, not be
silently ignored.

The repair for all three is to make the test read the artefact. Mutate the
implementation, run the real corpus, and assert exactly which cases fail. That
turns "the corpus discriminates" from a restated number into a measurement, and
it goes red when a fixture moves.

Five implementations hit this class and each wrote a different guard. The guards
compose:

- fail a sidecar carrying a field the runner does not compare; do not ignore it
  *(JavaScript, Python)*;
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
a dangling-reference fixture whose two elements made "one defect must not
become two" a guarantee of arity, not of the reader; and a distance fixture
whose arc spanned too little latitude to separate the distance models it
existed to separate (section 7.1).

### 10.3 Red-proof everything, and check the proof reached its target

A passing corpus is not evidence that the corpus discriminates. Break one thing,
run the check that should notice, watch it go red, restore.

A red proof lies in five ways.

**It can miss its target.** One patched the library's function while the test
module had bound the name at import, so the assertion was calling the real one
and the guard stayed green. "The red proof stayed green" is the evidence someone
uses to throw a good test away.

**Its mutation can match nothing.** One suite makes each mutation assert that
its own search string was found: a substitution matching nothing makes the whole
file a test of nothing.

**The mutation can be broader than the rule it claims to test.** An
away-from-zero rounder built on a round-trip tie detector moves phantoms as well
as ties, reddening four corpus cases instead of one. Writing the assertion to
match that would attribute three failures to a rule they had nothing to do with.
The repair is a companion test freezing that the extra value is a phantom.

**The mutation may never reach the code it claims to break.** A notes-discarding
mutation came back green because it patched only the first of several call sites,
and no accept fixture had a folder carrying `notes`. A mutation that misses its
target is indistinguishable from a runner finding nothing wrong.

**It may not come back green.** This is the only one leaving a proof that
measures nothing. One patched two modules and left one active during the
restore, so the check it named was permanently red, which reads as confidence,
not as a broken instrument. A red-proof author watches for the red, so the revert
is the half nobody tests. Assert both directions.

A proof whose conclusion is an absence needs re-running whenever the thing it
claims to be absent from grows. A red proof asserting that no tier catches some
defect is a green-asserting proof, and those rot silently as the corpus gains
cases. Rounding is the worked example: the claim that no tier reached the writer
boundary was true when it was written and false once the write tier existed.

Write those deliberately. A mutation carrying an expected verdict of *green*
records a gap in the corpus instead of assuming one, and it goes red the moment
a fixture closes the gap, which is how its author finds out. That mechanism has
paid for itself: one entry reaching inward to a segment's first and last *timed*
pip survived, because `corpus/accept/track-partly-timed.vnx` carried no times at
all in its untimed segment and both readings returned unknown. The fixture gained
a timed middle pip, and the entry went red on the next run. The promotion was
noticed, not remembered.

The mutations that come back green are the ones worth the exercise. Of the 34
injected into one implementation, 26 went red as designed and taught their author
nothing not believed beforehand. Four came back green, and three of those paid
for the whole run.

They come back green for three reasons that are indistinguishable from the
console and call for three different actions.

**The mutation was bad.** It assigned a field overwritten three lines later, or
it deleted an assertion instead of injecting a defect, and removing a check
cannot make a suite fail. Fix the mutation.

**The mutation was fine and the guard was not there.** A ZIP64 test stayed green
with the extra-field parser deleted (section 9.2). A rounding oracle reached
neither the 64-shift arm of its 128-bit arithmetic nor the subnormal exponent, so
deleting either left the suite green; regenerating it to reach every arm
immediately found a real defect. Rounding was one double wrong above about 9e14,
because an identity branch tested a power of two where the true boundary is a
power of ten, so past it the rounded integer exceeded 2^53. The green proof did
not merely reveal a weak test; it forced a better instrument, which then caught a
bug. Fix the test.

The oracle was 4,110 lines of 128-bit arithmetic and it is gone, deleted with
the rounding rule it certified. Both the defect it found and the defect it had
existed only because the rule did. That is the arithmetic of a rule: it does not
only cost the words that state it.

**The branch is unreachable.** A mutation of the version rule for 1.0 and above
is a provable no-op in a 0.11 reader, which can never take that branch
(`SPEC §2.2`), so it comes back green for a reason that is neither a weak guard
nor a bad mutation. Record the proof in the code, so the next person does not run
the same mutation, see green, and conclude the guard is weak.

An unreachable branch and an untested one look identical forever unless somebody
writes down which was proved. When a red proof comes back green, you have
learned nothing until you have said which of the three it is.

### 10.4 Red-proof the instrument, not only the code

A comparison that has never been seen to fail is not a comparison. It is a
green light with no bulb in it.

Build such a check from two halves that share no code: one language writes raw
IEEE 754 bits and its own answers to a table, and the other bit-casts each row
back and compares. Sample the whole binary64 range through a bits-to-double
constructor, add dense small magnitudes, and add every value the specification
and the corpus name.

Zero disagreements is worth nothing on its own. What makes the check worth having
is the count of disagreements that reappear when the tie mode is reverted. A
check that cannot produce that number is indistinguishable from one that is not
running.

An agreement certifies only what its shared input varies. Four implementations
agreed on a container built from one input and cited it as their strongest
cross-language evidence. That input carried a single attachment, so the one rule
all four were breaking was a rule about several: obeying it changed nothing
about the artefact they agreed on. Silent on exactly the field in question, and
a matching hash offers no surface on which that silence shows. A note can be
doubted; a SHA-256 reads as agreement.

Two implementations measuring one fact is overlap only if their inputs differ.
Four writers fed one input is one measurement, four times.

Three traps in building such a table *(Kotlin implementation)*: bits must travel
as bits, since an unsigned parser fails on every negative double and, with the
error ignored, scores every one of them as a disagreement against `0`; a mining
criterion can decorate instead of discriminate (section 10.2); and the generator
must be deterministic, because a table nobody can diff is useless.

The shipped form of that idea is a table: a set of doubles as IEEE 754 hex with
a reference answer for each, about ten lines to read in any language. Compare it
by bit pattern and never by text. Comparing a spelling puts a rendering rule this
format does not have back into the measurement through the side door.

---

## 11. Guards

### 11.1 A rule with no guard at all

Section 10 is about a guard that fails. This is the state with no guard in it,
and it is the one made invisible by its own clarity.

A rule that is unambiguous, stated in the normative document, and enforced by
nothing produces total confidence and zero enforcement. An open question makes
people uneasy, so it stays on a list and gets checked. A settled one does not,
and nobody asks whether anything checks it. The question feels answered.

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
control is a valid sidecar asserted to parse and to yield the expected values.
It is what makes the refusals discriminate instead of merely fail.

**Pin the state of the world the strictness assumes.** A separate test walks
every tier and asserts that no sidecar carries the field the strict parser
refuses. If one appears, that goes red naming the corpus and the rule, instead of
surfacing as an opaque parse failure inside whichever tier happened to load it
first. A guard should fail informatively about why the world changed, not about
what it hit.

Two caveats came with it. The third property is a source scan, so it sees a
literal spelling and a sidecar spelling it differently escapes: a tripwire for
one regression, not a validator. And it rests on a measurement of the corpus as
it stands. If the corpus deliberately reintroduces what the guard refuses, change
the rule; do not loosen the guard back into a fallback.

One note on what a guard's comment is for. Where a check's shape looks arbitrary
without the argument that produced it, the comment earns that argument: a reader
who finds a rule and its check agreeing should be able to see why the check is
spelled as it is, or somebody will simplify it back. That is not licence for a
published document to narrate its own past; a reader of the prose has no stake
in which draft came first. The two get confused because both look like history.
The test is whether a reader needs it to avoid undoing the check.

---

## 12. Where each language fell

Where each of the six met the traps above, with the section that covers each in
brackets. The link on each name goes to that library's page, which documents the
library and does not repeat what is here.

**Go**: [`go/README.md`](go/README.md). `math.Round` is half away from zero,
and the reference implementation rounded by scaling, which is a different
operation (6). `encoding/json` accepts `1e400` into `json.Number` and refuses it
only into `float64` (5.1).

**Python**: [`python/README.md`](python/README.md). `json` accepts `NaN` and
`Infinity` by default, so a Python reader either makes its parser strict or
refuses under `SPEC §4.2` with wording the `reject/` sidecars do not expect
(5.1). Decimal intuition about which way a value rounds is wrong in the same way
in every language, and Python is where it was measured (6).

**Swift**: [`swift/README.md`](swift/README.md). `.rounded()` does not round
half to even; half to even is `.toNearestOrEven` (6). `JSONSerialization` spells
`-3.67` as `-3.6699999999999999`, so the renderer is `JSONEncoder` while the
parser is hand-written (3).

**Kotlin**: [`kotlin/README.md`](kotlin/README.md). `BigDecimal.HALF_UP` is
half away from zero and `Math.round(x * 10) / 10` is half-up on the JVM, so a
guard testing only negative ties passes either. That is a trap for an
implementation that rounds by choice; the format asks for no rounding (6). The
JVM standard library has no JSON API at all, which makes the reader-versus-
renderer split of section 3 an explicit decision, not a default.

**JavaScript**: [`js/README.md`](js/README.md). `toFixed` rounds ties away from
zero on both signs, and `Math.round(x * 10) / 10` is half-up (6). `1e400` parses
to infinity instead of failing, so a reviver or an equivalent post-parse check is
the only thing between it and a coordinate of infinity (5.1).

**Rust**: [`rust/README.md`](rust/README.md). `f64::round` is half away from
zero, and `round_ties_even` is the right rounding mode applied to the wrong
operation (6). `serde_json` needs an opt-in feature to keep the last bit of a
full-precision coordinate (3). The idiomatic model of a collection loses a
distinction the format carries (4.1). A lone surrogate cannot be represented in
a `String`, so the parser refuses one for free.

---

One last thing, from the implementations that found two invisible defects in a
day, both by comparing against another implementation and not by reasoning:

> When you have a choice between arguing about a specification clause and
> measuring two implementations, measure.

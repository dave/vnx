# Implementing VNX

Informative. Nothing here defines the format. `SPEC.md` does, and where the two
appear to disagree, `SPEC.md` is right.

What follows is where a language's defaults quietly disagree with the format, and
where a test written to catch that disagreement fails to catch it. Each entry
gives the requirement, the languages that fall into it, and the measurement.
Measurements are attributed to the implementation that made them; nothing here is
inferred from another implementation's source.

Six implementations were written from `SPEC.md` and `corpus/` alone: Go, Python,
Swift, Kotlin, JavaScript and Rust. Everything below is something at least one of
them got wrong first.

Every number here is a measurement of a particular file at a particular moment,
not a property of the format. Fixtures move. A number copied out of this document
into a test asserts, from then on, that a file still says what it said, so name
the artefact beside it or write it as a counterfactual, which has no artefact to
go stale against.

Read it before writing your encoder, not after your corpus run goes red.

---

## 1. Numbers

Rounding is where the languages differ most and where the corpus can help least.
Each implementation encodes JSON with its own standard library, so a number's
spelling is not a rule of this format; the value that reaches the encoder is.

### 1.1 The rounding tie goes to even

Section 4.1 of the specification rounds longitude and latitude to 6 decimal
places and elevation to 1, at the writer boundary. Ties there go to even, and
every language's idiom breaks that in a different place.

| value to 1 place | Go | Python | JavaScript `toFixed` |
|---|---|---|---|
| `340.25` | `340.2` | `340.2` | `340.3` |
| `-340.25` | `-340.2` | `-340.2` | `-340.3` |
| `340.75` | `340.8` | `340.8` | `340.8` |

*(Measured by the JavaScript implementation on Go 1.25.6, CPython 3.14 and
Node 22.19.)*

`toFixed` strips the sign and rounds the magnitude, so it is symmetric: away from
zero on both signs, `-340.25` to `-340.3` as well.

The asymmetric rule is the one everybody reaches for first. `Math.round(x * 10) / 10`
is half-up in both JavaScript and Java, because `Math.round` breaks a tie toward
positive infinity, so it is wrong on every positive tie and right on every
negative one by accident. **A guard testing only negative ties passes it.**
*(Measured by the Kotlin implementation.)*

Five wrong tie rules and the two values that separate them:

| value | rule | half-odd | away from 0 | toward 0 | half-up | half-down |
|---|---|---|---|---|---|---|
| `340.25` | `340.2` | `340.3` | `340.3` | `340.2` | `340.3` | `340.2` |
| `340.75` | `340.8` | `340.7` | `340.8` | `340.7` | `340.8` | `340.7` |

`340.25` catches half-away-from-zero and half-up; `340.75` catches
half-toward-zero and half-down; either catches half-to-odd. **Only the pair
identifies half-to-even**, so a fixture carrying one of them tests less than it
looks. *(Measured by the JavaScript implementation.)*

The values are reachable rather than contrived. A tie at one decimal place needs
a fractional part of exactly `.x5`, and of the ten candidates only `.25` and
`.75` are dyadic, but a quarter-metre elevation grid produces both. *(Measured by the JavaScript implementation.)*

Kotlin fixed its renderer and shipped this anyway: `BigDecimal.HALF_UP` at the
emit boundary agrees with `toFixed` and disagrees with Go and Python. Its own
summary is the warning worth carrying, that finding the first one did not find
it. *(Measured by the Kotlin implementation.)*

Swift's `.rounded()` does not round half to even; its emit boundary specifies
`.toNearestOrEven`. *(Measured by the Swift implementation.)*

Go's `math.Round` is half away from zero, and the reference implementation shipped
this defect too, fixing it to `math.RoundToEven`. Two implementations got it
wrong, which is what turns "an implementation should check this" into
"implementations do get this wrong". *(Reported by the Go implementation, which
found it in itself.)*

### 1.2 Building a rounding-tie fixture is a trap

`0.0000005` is not a tie and is not a binary64 value at all. Its nearest double
is `4.99999999999999977e-7`, below the midpoint, so it rounds down under every
rule, and a suite pinning it passes whichever way the tie branch is written.
*(Measured by the JavaScript implementation, who measured the nearest double.)*

A tie at *n* decimal places needs the midpoint to be exactly representable, which
means dyadic, which means 5^n must divide (2k+1). At six places the smallest real
one is 2^-7, or `0.0078125`, sitting exactly between `0.007812` and `0.007813`;
`0.0234375` is the next. At one place, `340.25` qualifies because its fractional
part is a quarter. *(Reached independently by the Python and Kotlin implementations, which is why it is worth stating twice: two routes to one value are stronger evidence than either route.)*

The JavaScript suite keeps `0.0000005` beside the real ties as a named non-tie,
so nobody later mistakes it for the tie case.

### 1.3 "Round to N places" names two different operations

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
| `340.25` | `340.2` (exact, a genuine tie) | `340.2` |

*(Measured by the Rust implementation.)* Six of those seven disagree, in both directions, and they are
ordinary elevation-shaped values rather than corner cases. Run against the
5,315-row number table, **160 rows separate the two readings and all 160 side with
rounding against the double**; three more overflow to infinity when scaled, where
rounding against the double does not. *(Measured by the Rust implementation.)*

This is a larger defect than the tie direction of 1.1, and a separate one. The tie
direction disagrees only on exact halves; the operation disagrees on most values.
A library can have the tie rule right and still be wrong on almost every elevation
it writes.

The reference implementation shipped the scaling route, and the number table
disagreed with it on ordinary values at both places. Coordinates carry far more
of that exposure than elevations, six places being longitude and latitude.

Two details there matter more than the count. The table's `kind` column names those
rows `scaling-route`. Somebody mined rows specifically to catch a library that
rounds by scaling, and **the library that generated the table never read it.** The
instrument and the defect sat in one repository and never met. And the guard that
did exist used `340.25` and `340.35`: the last row of the table above, which agrees
under both operations. `0.15` would have caught it the day that test was written.
So the repair was two repairs: the operation, and running every row of the
published table through the encoder, which is the comparison the other five
libraries were already making. *(Reported by the Go implementation, which wrote
both the defect and the test.)*

A magnitude bound guarding the scaled product against overflow went with it. Such
a bound is not a rule of the format; it is an artefact of the operation, and it
stops existing when the operation changes.

Formatting to a decimal string and parsing it back is the correct implementation,
because the formatter expands the double exactly and rounds ties to even. The
caveat is that the formatted text is an intermediate and never the output. Round
to a *value*, and hand the value to your encoder.

Decimal intuition lies about which way a value rounds, whichever operation you
pick. `3.05` rounds down to `3.0`, correctly, because the literal is really
`3.04999999999999982236431605997495353221893310546875`. A test written from
intuition asserts `3.1` and reports a correct writer as broken. *(Python implementation, which wrote that expectation before measuring it.)* But `3.05` gives `3.0` under both
operations above, so it cannot tell them apart, which is why the worked example
here is `0.15`.



---

## 2. The model

The section that saves the most time, because these are decisions made in the
first hour and discovered in the last.

### 2.1 Absent, empty and false are three different documents

Section 3.1 of the specification: absence means "not known" and is not a default
value, with two stated exceptions. A key that is absent, a key present with an
empty array, and a `hidden` written as `false` are three different documents and
have to round-trip as three.

`label-visible` is where getting this wrong is invisible. Absence means Auto, and
a reader that treats it as `false` flattens Auto to hidden with nothing to report
(Section 3.6).

The language pulls the same way here as it does over a position's channels. A plain list for every collection, where absent and empty are one
value, is more idiomatic in Rust than an optional list, and every reviewer would
suggest the change. Until a fixture carrying an empty collection landed, no corpus
case had one, so an implementation that conflated them passed every tier. The Rust
implementations reports getting this right by accident and for the wrong reason.
*(Measured by the Rust implementation.)*

### 2.2 Opaque bodies survive at value level

Data records (Section 5.1), `rulesets` (Section 3.9) and style objects (Section
3.10) are opaque: no key inside them is ever unrecognised, and each has to
survive a read-modify-write untouched.

That is a modelling decision rather than a serialisation one. Decode a record
body into a typed structure and you lose the fields your type does not name. The
accept tier's tree comparison is what catches it, and it is the only thing that
does: every other assertion in a sidecar is about a construct the format names.



---

## 3. Parsing

### 3.1 Non-finite literals

`NaN`, `Infinity` and `-Infinity` are not JSON, and a document containing one is
refused (Section 4.2). Several standard libraries accept them by default, Python's
`json` module among them, so this is a flag to turn off rather than a behaviour
you inherit.

A literal like `1e999` or `1e400` is well-formed JSON whose value is not finite in
binary64: it parses to infinity rather than failing. Check after parsing as well
as during; a `JSON.parse` reviver or equivalent hook is enough. *(Measured by the JavaScript implementation.)*

The three literals fail differently in different runtimes, and the differences do
not line up:

| literal in `{"a": …}` | Go `encoding/json` | Node `JSON.parse` | Python `json.loads` |
|---|---|---|---|
| `NaN` | error | throws | accepts, giving `nan` |
| `Infinity` | error | throws | accepts, giving `inf` |
| `1e400` | error into `float64` | returns infinity silently | accepts, giving `inf` |

*(Measured by the JavaScript implementation, measured.)* Go accepts `1e400` into `json.Number`, keeping the
text, and refuses it only on unmarshal into `float64`.

Two consequences. The corpus's expected substring `malformed JSON` describes what
a *strict* parser says, and both Go and Node are strict here, so it is a
strict-parser artefact rather than one language's. The implementations it bites is Python,
whose `json.loads` accepts all three by default: a Python implementation either
makes its parser strict or refuses under Section 4.2, and a Section 4.2 refusal
legitimately does not contain the words "malformed JSON". *(Measured by the Python implementation, which is that counterexample.)*

And `1e400` is the case every library catches for itself, with the runtimes
failing differently rather than uniformly. Node hands back infinity with no
complaint, so a reviver or an equivalent post-parse check is the only thing
between it and a coordinate of infinity.

### 3.2 Duplicate object keys

Two entries with the same key resolve last-wins in Go's `encoding/json`, Node's
`JSON.parse` and Python's `json`, all measured,
before any format rule runs, so an item vanishes and a reader built on a stock
parser cannot observe it (Section 6.4). The corpus carries a case that expects no
diagnostic.

Before taking that at face value: the format already pushes you into writing your
own parser, and once you have one, detecting a duplicate key is about three lines.
The limitation belongs to stock parsers rather than to the format. *(Measured by the Kotlin implementation.)* Since the corpus case expects no diagnostic, a reader that gains the
ability turns it red, which is the signal to revisit the limitation rather than a
failure.

### 3.3 Parse every number as binary64

Integer literals included. Section 4.2 defines numbers as IEEE 754 binary64, so a
JSON integer is a binary64 too, and rendering it as one is what makes
`1778914991000.0` and `1778914991000` the same document.



---

## 4. Geometry

### 4.1 The projection model, and why the cosine reference decides

Section 3.3.3 gives a 50 metre tolerance for an inline chain endpoint and names
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
| Web Mercator | 36.47 m |

*(Measured by the Python implementation.)* An implementation reaching for Web Mercator, or scaling by
degrees with no cosine term, passes every structural check and disagrees about
which documents are legal.

### 4.2 A refusal fixture cannot discriminate, so the arc's direction is the test

This is the least obvious property in the corpus and the first casualty of a
tidy-up.

A fixture that must be refused cannot tell a correct model from one that
over-reads, because a model reading a larger distance also refuses. Every wrong
model refuses a 55 m case.

Discrimination comes from the arc's direction. Run the arc north to south, so its
first vertex sits at high latitude and the query point at low, and a model taking
the cosine of the segment's start latitude under-reads: 36.845 m against a true 55,
which accepts a document the format refuses. That is the more dangerous
direction, and only an arc pointing that way exposes it.

The two tolerance cases therefore run in opposite directions on purpose. The
accept case catches a model that over-reads and refuses good geometry; the
refusal case catches one that under-reads and accepts bad geometry. *(Measured by the Python implementation.)*

The separation has to be large enough for the error to cross the threshold rather
than merely move the number, and that is a stronger requirement than it sounds. On
an arc spanning three degrees of latitude, a segment-start-cosine model reads
48.87 m and 59.73 m where the correct model reads 45 and 55: an 8.5 per cent
error, plainly visible in the numbers, and the verdict is unchanged on both
files. Thirteen degrees gives 67.447 m and 36.845 m, which flips both. A fixture that
moves a number without moving a verdict separates nothing.

All four figures assume the mean radius of 6 371 008.8 m that Section 3.3.3
names, and only that constant reproduces the 45 and 55 the fixtures are named
for: at 111 132 m per degree the same coordinates solve to 44.974 and 54.969.
The difference never moves a verdict, which is exactly why a distance quoted
without its constant outlives both. *(Measured by the Rust implementation.)*



---

## 5. Testing this format

### 5.1 Know which rules no fixture reaches

The accept, reject and diagnose tiers all start from a document that already
exists, so the rounding rule of Section 4.1 of the specification is satisfied by
every value in them before a writer is asked anything. For a long time that put
rounding outside the corpus, and each implementation carried its own tests for
it.

`corpus/write/` decodes an input and calls the write path, so rounding, both tie
directions and the shortest spelling of a position are all reachable. An
implementation exposing no write path fails it by name rather than by a confusing
diff.

The reason to keep this entry is what happened to the claim it used to make. The
writer boundary was described, in this document and elsewhere, as permanently
beyond the corpus's reach. It was not: it was beyond the reach of the tiers that
existed.

> An unreachability claim is a claim about the current state of somebody's
> ingenuity, not about the format.

The opposite failure looks like tidying up. An artefact can be emptied by an act
that reads as conformance: sixty decorative tie rows, an already-sorted
attachment pair. **Before
regenerating a fixture, say which rule each existing one is the only witness
for.** *(Kotlin implementation, which declined the regeneration.)*

A related trap, and the one condition here that care does not fix: a wrong reason
under a right conclusion has nothing with a motive to check it. A wrong reason
under a wrong conclusion is caught by whoever tests the conclusion; under a right
one it holds everything up while being false. Which gives the rule: **a reason
that has only ever answered one kind of question has never been tested.**
*(JavaScript implementation, against its own earlier claim.)*

So the question is not which rules are unreachable but which are unreached
*today*, and what shape a case would take. Where two readings of a rule produce
a different document for one input, the accept tier reaches it. Where a rule
takes effect only when a model becomes a document, a write tier does. Where
neither is
true, the prose is the only statement of the rule and your own tests the only
enforcement: the tolerance boundary at exactly 50 metres is one, because no two
distance models land on exactly 50.

### 5.2 A case only separates the readings its own data can separate

A fixture that *contains* a construct is not a test *of* it. What a case can
discriminate is decided by the data in the case, not by the construct it holds or
the note it carries.

Eight instances have turned up, and they fall into three mechanisms rather than
one. The symptom is shared and the repairs are not.

#### A case whose data cannot separate the readings

These were never checking the thing they named, because no answer they could
produce would differ between a correct implementation and a wrong one.

**A node-key comparison.** Section 6.2 resolves a node key to its position before
comparing chain continuity, demoting key equality to a fast path. Compare the keys
instead and two distinct keys at one position do not meet, so a continuous walk is
reported as broken. One library shipped that reading and ran 31 of 31, green; its
own tests could not help either, having been written to match it, and the fix was
as invisible as the defect.

`diagnose/chain-does-not-join.vnx` varies the node keys *and* the positions
together, so both comparators return the same verdict on it. It looks like
coverage, being in `diagnose/` and named for continuity, and it cannot attribute
its verdict to either reading. Any fixture that changes two variables together
tests neither. *(Reported by the JavaScript implementation.)*

**A ZIP64 fixture** that masked sizes in the local header only, so disabling the
extra-field parser left the test green. *(Reported by the Swift implementation.)*

**A tie table.** Sixty rows labelled as ties, mined by a detector that asked
whether a neighbouring spelling of the same length also round-trips. That
question does not identify a tie: it asks whether the value is the nearest double
to a decimal, and many decimals share one nearest double. Every one of the sixty
stayed green when the tie rule was reverted. *(Measured by the Kotlin
implementation.)*

#### A check that was discriminating and quietly stopped

These worked when written. Something moved underneath them.

**A runner that stopped comparing.** A sidecar pins a diagnostic by reason code;
one matcher looked for an English `message` field, found nothing, and fell through
to comparing paths alone, leaving every diagnose case green while checking half of
what it pinned. *(Measured by the Python implementation.)*

**Absence-proofs that went stale.** A proof that no tier catches some defect is
true until a case reaches it. See 6.3.

**A fixture retired by a rule change.** A test used two positions that collide
once rounded, to prove that arc-boundary de-duplication compares full values.
Change the rounding rule so they no longer collide and the test asserts that a
de-duplication has not merged two points it could not have merged. Nothing about
it looks wrong at any point.

#### A guard that was never connected to what it names

The sharpest of the three, because the other two at least once measured something.

**A test named for a fixture that never reads the fixture.** One called
`the_corpus_fixtures_land_where_their_names_say` would still have passed if both
corpus files were deleted, because it restated numbers rather than reading the
artefact. Beside it, a test asserting that the wrong cosine reference changes
neither verdict passed green while asserting something false of the corpus it
named. A test that restates a number cannot notice when the artefact moves.
*(Reported by the Rust implementation.)*

**A ZIP reader and writer by the same hand.** The container section of `SPEC.md`
needs a reader a stock library may not give you: per-entry compression method
inspection, and seeing *both* halves of a duplicated entry name. A reader and a
writer by the same hand can be wrong in the same way and agree perfectly, so read
every archive you produce back with a library that never saw this
specification. *(Reported by the
Kotlin implementation, which reads its own archives with `java.util.zip`.)*

Necessary and not sufficient, which is the sharper half. Every ZIP
library resolves an entry from the **central directory**: it is the
authoritative index and the local header a hint. So no library-based check can
witness a rule about a local header, whoever wrote the library. Corrupt a local header's version and CRC and a
library-based reader reports nothing wrong: it never consults that header.

> An independent reader is not automatically an independent instrument. It can
> still consult the same field the defect is not in.

The property is that the two headers **agree**, and no reader of either can check
it. Walk the archive forward as bytes. *(Reported by the Kotlin and JavaScript
implementations; the corruption measured by the Python one, whose own assertions
were blind to it.)*

The repair for all three is to make the test read the artefact. Mutate
the implementation, run the real corpus, and assert exactly which cases fail. That
turns "the corpus discriminates" from a restated number into a measurement, and it
goes red when a fixture moves.


Five implementations hit this class and each wrote a different guard, which
is the useful part, because the guards compose:

- fail a sidecar carrying a field the runner does not compare, rather than
  ignoring it *(JavaScript, Python)*;
- red-proof right-path, right-English, wrong-cause *(Kotlin)*;
- assert your cause table against a live corpus run in both directions, so a
  table entry the corpus stopped exercising fails as loudly as a corpus
  expectation the table never mentions *(Swift, JavaScript)*;
- sweep the fields *inside* a nested expectation, not only the top level of a
  sidecar *(Python)*;
- read your own output back with a foreign implementation *(Kotlin)*.

The detection recipe is cheap. Write down what the note claims the case tests, then write down which values the
case actually varies. Where it varies more than one, ask which readings it can
still separate.

That recipe found three real gaps in one afternoon: the continuity fixture above;
`link-names-absent-arc.vnx`, whose two links made "one defect must not become
two" a guarantee of arity rather than of the reader; and a tolerance pair whose
arc spanned too little latitude to separate the distance models it existed to
separate.

### 5.3 Red-proof everything, and check the proof reached its target

A passing corpus is not evidence that the corpus discriminates. Break one thing,
run the check that should notice, watch it go red, restore.

A red proof lies in five ways. It can miss its target: one patched the library's
function while the test module had bound the name at import, so the assertion was
still calling the real one and the guard stayed green. "The red proof stayed green" is
the evidence someone uses to throw a good test away. *(Measured by the
Python implementation.)* Its mutation can match nothing, which is why one suite
makes each mutation assert that its own search string was found: a substitution
matching nothing makes the whole file a test of nothing. *(Measured by the
JavaScript implementation.)*

Third, and least obvious: **the mutation can be broader than the rule it claims
to test.** An away-from-zero rounder built on a round-trip tie detector moves
phantoms as well as ties, reddening four corpus cases instead of one. Writing the
assertion to match that would attribute three failures to a rule they had nothing
to do with. The repair is a companion test freezing that the extra value is a
phantom. *(Measured by the JavaScript implementation.)*

Fourth: **the mutation may never reach the code it claims to break.** A
notes-discarding mutation came back green because it patched only the first of
several call sites, and no accept fixture had a folder carrying `notes`. A
mutation that misses its target is indistinguishable from a runner finding nothing
wrong.

Fifth, and the only one leaving a proof that measures nothing: **it may not come
back green.** One patched two modules and left one active during the
restore, so the check it named was permanently red, which reads as confidence
rather than as a broken instrument. A red-proof author watches for the red, so the
revert is the half nobody tests. Assert both directions. *(Measured by the Python
implementation.)*

And a proof whose conclusion is an absence needs re-running whenever the thing
it claims to be absent from grows. A red-proof asserting that no tier catches
some defect is a green-asserting proof, and those rot silently as the corpus gains
cases. Rounding is the worked example: the claim that no tier reached the writer
boundary was true when it was written and false once the write tier existed.

Three of one suite's 25 mutations assert that a defect is *not* caught, recording
a gap rather than assuming one; another injected 34. Such an assertion goes red
the moment a fixture closes the gap, which is how its author finds out.

The mutations that come back green are the ones worth the exercise. Of 34
injected into one implementation, 26 went red as designed and taught their author
nothing not already believed. Four came back green, and three of those paid for
the whole run. *(Reported by the Swift implementation.)*

They come back green for three reasons that are indistinguishable from the
console and call for three different actions.

**The mutation was bad.** It assigned a field overwritten three lines later, or it
deleted an assertion rather than injecting a defect, and removing a check cannot
make a suite fail. Fix the mutation.

**The mutation was fine and the guard was not there.** A ZIP64 test stayed green
with the extra-field parser deleted, because the fixture generator masked sizes in
the *local* header while the reader takes them from the central directory: the
fixture contained the construct and did not test it. A rounding oracle of 3,308
rows reached neither the 64-shift arm of its 128-bit arithmetic nor the subnormal
exponent, so deleting either left the suite green. Regenerating it to reach every
arm immediately found a real defect: rounding was one double wrong above about
9e14, because an identity branch tested a power of two where the true boundary is
a power of ten, so past it the rounded integer exceeded 2^53. The green proof did
not merely reveal a weak test; it forced a better instrument, which then caught a
bug. Fix the test.

**The branch is unreachable.** Two mutations of the version rule for 1.0 and later
came back green and were *provably* no-ops, because a 0.1 reader can never take
that branch. That is neither a weak guard nor a bad mutation. Record it, in the
code, so the next person does not run the same mutation, see green, and conclude
the guard is weak.

An unreachable branch and an untested one look identical forever unless somebody
writes down which was proved. So: when a red proof comes back green, you have
learned nothing until you have said which of the three it is.

### 5.4 Red-proof the instrument, not only the code

> A comparison that has never been seen to fail is not a comparison. It is a
> green light with no bulb in it. *(Measured by the Kotlin implementation.)*

One cross-language check covers 460,007 doubles with zero disagreements: 300,000
uniform over the binary64 range via a bits-to-double constructor, 40,000 dense
small magnitudes, and every value the specification and corpus name. The two
halves share no code: one language writes raw IEEE 754 bits and its own answers
to a table, and thirty lines in the other bit-cast each row back and compare.

The number that makes it worth having is not 460,007 but 78, the disagreements
that reappear when the tie mode is reverted. Without those, zero disagreements is
indistinguishable from a comparison that is not running.

An agreement certifies only what its shared input varies. Four implementations
agreed on a container built from one input and cited it as their strongest
cross-language evidence. That input carried a single attachment, so the one rule
all four were breaking was a rule about several: **obeying it changed nothing
about the artefact they agreed on.** Silent on exactly the field in question, and
a matching hash offers no surface on which that silence shows. A note can be
doubted; a SHA-256 reads as agreement. *(Reported by the JavaScript
implementation, against its own result.)*

Two implementations measuring one fact is overlap only if their inputs differ.
Four writers fed one input is one measurement, four times.

Three traps in building one *(Kotlin implementation)*: bits must travel as bits,
since an unsigned parser fails on every negative double and, with the error
ignored, reported 149,667 disagreements against `0`; a mining criterion can
decorate rather than discriminate; and the generator must be deterministic,
because a table nobody can diff is useless.

The result, `corpus/numbers/numbers.tsv`, carries a set of doubles with two
reference answers each: rounded to 6 places, and rounded to 1. About ten lines to
read in any language.



---

## 6. Guards

### 6.1 A rule with no guard at all

Section 5 is about a guard that fails. This is the state with no guard in it, and
it is the one made invisible by its own clarity.

A rule that is unambiguous, stated in the normative document, and enforced by
nothing produces total confidence and zero enforcement. An open question makes
people uneasy, so it stays on a list and gets checked. A settled one does not,
and nobody asks whether anything checks it, because the question feels answered.

The evidence is the partly-timed track. Three implementations summed a partial
moving time with every corpus tier green, and that was already a rule the
specification stated. Prose did not change what passed; a fixture did.

> Prose changes what people believe. A fixture changes what passes.

So "this one is ruled, so it must be handled" is backwards. A written rule with no
fixture is the one most likely to be quietly violated, because everyone reading
the specification believes it is enforced. Four rules sat in that state at once:
node-key resolution, the full-entry-name root document match, `a..b/photo.jpg`
being legal, and a backslash in an entry name being refused. *(Reported by the
JavaScript implementation, which holds a proof for each.)*

The corpus can do worse than fail to enforce a rule. A committed `.vnz` fixture
whose local headers carry zeros where the central directory carries the truth is
read correctly by every implementation, because a conforming reader takes its
sizes from the central directory and never consults the field that is wrong. A
fixture can hold a construct and test nothing about it. *(Reported by the Kotlin
implementation.)*

### 6.2 What a guard that works looks like

Three properties, from a test written to keep a corpus runner strict about which
field a sidecar pins. *(Reported by the Swift implementation.)*

*Build the input on disk; do not assert about the corpus.* A guard tested only
against inputs that satisfy it has not been tested, and the whole corpus satisfies
this one: every sidecar is well-formed, so no corpus file can ever exercise the
refusal. The test writes malformed sidecars into a temporary directory and parses
those.

*Carry a well-formed control.* Without one, two refusal tests prove only that
the parser rejects *something*, not that it rejects the thing they name. The
control is a valid sidecar asserted to parse and to yield the expected values, and
it is what makes the refusals discriminate rather than merely fail.

*Pin the state of the world the strictness assumes.* A separate test walks every
tier and asserts that no sidecar carries the field the strict parser refuses. If
one reappears, that goes red naming the corpus and the rule, instead of surfacing
as an opaque parse failure inside whichever tier happened to load it first. A
guard should fail informatively about why the world changed, not about what it
hit.

Two caveats came with it. The third property is a source scan, so it sees a
literal spelling and a sidecar spelling it differently escapes: a tripwire for one
regression rather than a validator. And it rests on a measurement with a date on
it. If the corpus deliberately reintroduces what the guard refuses, the answer is
to change the rule rather than loosen the guard back into a fallback.

One note on what a guard's comment is for. Where a check's shape looks arbitrary
without the argument that produced it, the comment earns that argument: a reader
who finds a rule and its check agreeing should still be able to see why the check
is spelled as it is, and the alternative is somebody simplifying it back. That is
not licence for a published document to narrate its own past, since a reader of
the prose has no stake in which draft came first. The two get confused because
both look like history. **The test is whether a reader needs it to avoid
undoing the check.**

---

## 7. Where each language falls in

Navigation only. Every fact here is stated above.

**Go.** `math.Round` is half away from zero (1.1). It accepts `1e400` into
`json.Number` and refuses it only into `float64` (3.1).

**Python.** `json` accepts `NaN` and `Infinity` by default (3.1). Decimal
intuition about which way a value rounds is wrong in the same way in every
language, and Python is where it was measured (1.3).

**Swift.** `.rounded()` does not round half to even; the emit boundary needs
`.toNearestOrEven` (1.1).

**Kotlin.** `BigDecimal.HALF_UP` at the rounding boundary is half away from zero
(1.1). `Math.round(x * 10) / 10` is half-up on the JVM, so a guard testing only
negative ties passes it (1.1).

**JavaScript.** `toFixed` rounds ties away from zero on both signs, and
`Math.round(x * 10) / 10` is half-up (1.1). `1e400` parses to infinity rather
than failing (3.1).

**Rust.** `f64::round` is half away from zero, and `round_ties_even` is the right
rounding mode applied to the wrong operation (1.1, 1.3). A lone surrogate cannot
be represented in a `String`, so the parser refuses one for free. The idiomatic
model of a collection loses a distinction the format carries (2.1).

---

One last thing, from the implementations that found two invisible defects in a
day, both by comparing against another implementation rather than by reasoning:

> When you have a choice between arguing about a specification clause and
> measuring two implementations, measure. *(Measured by the Kotlin
> implementation.)*

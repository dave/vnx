# Implementing VNX

This document is informative. It defines nothing.

`SPEC.md` says what the format requires. This says **where your language will
get it wrong** — the places where a standard library's defaults quietly
disagree with the specification, and the places where a test written to catch
that disagreement fails to catch it.

Every entry has the same three parts: what the format requires, which languages
fall into it and how, and **the measurement**. The measurements are what make
this a document rather than folklore, and each is attributed to the
implementation that made it. Nothing here is inferred from another
implementation's source.

Five implementations were written from `SPEC.md` and `corpus/` — Go, Python,
Swift, Kotlin, JavaScript, with Rust in progress — and everything below is
something at least one of them got wrong first.

Read it before you write your serialiser, not after your corpus run goes red.

---

## 1. Numbers

The largest section, because numbers are where every language differs and where
the corpus can help you least.

### 1.1 No stock encoder emits canonical form

`SPEC.md` §4.5.6 has the table. The short version: key ordering alone rules out
every stock encoder in every target language, because canonical form sorts
scalars before containers and every `sort_keys` / `.sortedKeys` option sorts
plain lexicographically. That puts `networks` before `vnx` and `nodes` before
`notes`.

**So every implementation writes its own serialiser.** Once you have accepted
that, the inline-position rule and the number rules cost a few lines each. The
reference serialiser is about 170 lines, most of which is the number renderer.

### 1.2 Shortest round-trip digits, and where the decimal point goes

§4.5.3 defines rendering by reference to ECMAScript `Number::toString`. That is
two rules, and languages tend to get the first free and the second wrong.

The **digits** are the shortest decimal string that round-trips to the same
binary64. Most languages give you this: Python's `repr`, Swift's
`Double.description`, Go's `strconv.FormatFloat(…, 'g', -1, 64)`.

The **placement** is the part you implement. ECMAScript picks between plain and
exponential notation on the decimal exponent, not on the digit count, and the
thresholds are unintuitive:

| Value | Renders as | Why |
|---|---|---|
| `340.0` | `340` | integral, under 21 digits |
| `0.000001` | `0.000001` | exponent −5, still plain |
| `0.0000001` | `1e-7` | exponent −6, goes exponential |
| `9.9e-7` | `9.9e-7` | same exponent as above, so also exponential — the digit count is irrelevant |
| `1e20` | `100000000000000000000` | 21 digits, still plain |
| `1e21` | `1e+21` | 22 digits, goes exponential — note the `+` |

Python and Swift emit `1e-06` and `1e-07` where the format needs `0.000001` and
`1e-7`, so both implement the placement rule by hand. *(Measured: Python seat,
6,009 doubles cross-checked against V8; Swift seat, 220,011 doubles.)*

### 1.3 A rendering tie rounds to even

When two equally short decimal spellings both round-trip to the same double and
sit **exactly** equidistant from its true value, the format takes the one whose
last digit is even (§4.5.3).

Kotlin shipped away-from-zero and emitted `1497898211722073.3` where V8, Go,
Python and Swift emit `…073.2`. The exact value is `…073.25`. Same double,
different bytes, and nothing anywhere went red. *(Kotlin seat.)*

Swift's `Double.description` and Python's `repr` already do this. Verify rather
than assume it: it costs one test, and it is not documented behaviour in either
language.

### 1.4 Detecting a rendering tie is itself a trap

**The obvious detector is wrong, and it is wrong in the dangerous direction.**

Asking *"does a neighbouring spelling of the same length also round-trip?"*
seems to identify a tie. It does not. Above 2^49 the gap between adjacent
doubles exceeds 0.1, so several one-decimal spellings map to one double and only
one of them is nearest. That is an ordinary shortest-representation choice, not
a tie.

Two implementations built that detector independently and both were caught by
red-proofing, not by review:

- The Kotlin generator mined **60 rows labelled as ties**, and every one stayed
  green when the tie rule was reverted. *(Kotlin seat.)*
- The Python search reported **400 ties of which 91 looked like failures**, and
  every one of the 91 was an artefact of the detector. *(Python seat.)*

**A true tie needs exact equidistance, which needs exact arithmetic** — rational
or big-decimal, not floating point. Kotlin's generator now defines the detector
*as the thing under test* — shortest-to-even against shortest-away-from-zero in
exact rational arithmetic — and refuses to emit a table in which any tie
category fails to discriminate.

### 1.5 A rounding tie also rounds to even, and it is a different code path

§4.5.4 rounds longitude and latitude to 6 decimal places and elevation to 1, at
the writer boundary. Ties there go to even as well — and **this is a separate
defect from §1.3 in a separate part of your code.**

Kotlin fixed its renderer and shipped the rounding bug anyway: `BigDecimal.HALF_UP`
at the emit boundary agrees with JavaScript's `toFixed` and disagrees with Go
and Python, which write `340.25` to one place as `340.2`. *(Kotlin seat: "finding
the first one did not find it.")*

`340.25` is an exact double, and **any quarter-metre elevation is such a value**,
so this is reachable rather than theoretical. JavaScript measured the same
divergence from the other side: `340.25.toFixed(1)` is `"340.3"`. *(JavaScript
seat.)*

Swift's `.rounded()` does not round half to even; its emit boundary uses
`.toNearestOrEven` explicitly. *(Swift seat.)*

### 1.6 Building a rounding-tie fixture is a trap

**`0.0000005` is not a tie and is not even a binary64 value.** Its nearest double
is `4.99999999999999977e-7`, which is *below* the midpoint, so it rounds down
under every rule — and a test suite built on it passes whichever way the branch
reads. *(JavaScript seat, who measured the nearest double.)*

A tie at *n* decimal places needs the midpoint to be exactly representable, which
means dyadic, which means 5^n has to divide (2k+1). At six places the smallest
real one is **2^-7 = 0.0078125**, sitting exactly between `0.007812` and
`0.007813`. At one place, `340.25` works because its fractional part is a
quarter. *(Python seat.)*

JavaScript found the same class from another angle: looking for a trailing `5`
in `toFixed(places + 1)` calls `0.15` a tie, and it is not one. **Decide the tie
on the double's exact value**, in `BigInt`, `BigDecimal`, `Fraction` or whatever
your language calls exact arithmetic. *(JavaScript seat.)*

### 1.7 Decimal intuition lies about which way a value rounds

`3.05` rounds **down** to `3.0`, and that is correct: the literal is really
`3.04999999999999982236431605997495353221893310546875`, so it is below the
midpoint and not a tie at all.

A rounding test written from decimal intuition asserts `3.1` here and reports a
correct writer as broken. *(Python seat, who wrote exactly that expectation
before measuring it.)*

Write the expected value by computing it, or pick values whose exactness you
have checked.

### 1.8 Negative zero, twice over

§4.5.3 renders negative zero as `0`. There are two ways to meet that rule and
most implementations only find the first.

**Collapse it on render.** Go's `encoding/json` emits `-0` and has to be
corrected.

**And know that rounding CREATES one.** `round(-0.04, 1)` is `-0.0`, with the
sign bit set. So the negative-zero rule applies to what rounding produced, not
only to what the document carried — and a writer that collapses on input and not
on output emits a byte sequence no document ever contained. *(Python seat;
JavaScript and Swift both added the same guard.)*

### 1.9 Never let a formatting primitive produce the final bytes

`format(-0.04, '.1f')` is `-0.0`. `format(340.0, '.1f')` is `340.0`. Both are
wrong bytes, and both come from using a formatting function to do a serialiser's
job.

**Round, then hand the result to your renderer.** The renderer is the only thing
that decides what a number looks like: it collapses negative zero, strips the
trailing zero, and applies the placement rule of §1.2. A formatting primitive
does none of that and cannot be told to.

---

## 2. Key order and strings

### 2.1 Scalars sort before containers

§4.5.1. Classification is by the value present in *this* object, not by the key
name, so the same key can sort into either group in different objects. No stock
encoder does this — see §1.1.

### 2.2 "Lexicographic" over what

§4.5.1 says lexicographic and the format's own keys are all ASCII, so code
points, UTF-8 bytes and UTF-16 code units agree for everything the format
defines.

**They do not agree above U+FFFF**, and a data record body may carry any key at
all (§5.1.2). JavaScript's `Array.prototype.sort()` with no comparator orders by
UTF-16 code unit; Python's `sorted()` orders by code point. If your language
sorts strings by UTF-16 code unit, a record key in an astral plane will sort
differently from everyone else's. *(Python seat, from the specification; no
implementation has yet reported hitting it.)*

### 2.3 String escaping

§4.5.5 is explicit and worth reading rather than guessing, because two languages'
defaults disagree with it in opposite directions.

The rule: escape only `"`, `\` and the control characters below U+0020. Five
control characters take their short escape — `\b`, `\f`, `\n`, `\r`, `\t` — and
every other one is `\u00xx` with **lowercase** hex. Nothing else is escaped,
including non-ASCII, `<`, `>`, `&`, U+2028 and U+2029, and nothing above U+007F
is ever written as a `\u` escape.

Go's `encoding/json` escapes `<`, `>`, `&`, U+2028 and U+2029 by default and has
to be configured not to. It writes `\b` and `\f` in their short forms, which
agrees with the format. *(Kotlin seat measured Go's behaviour; relayed to this
document by the lead rather than read here.)*

No corpus file exercised any of this until an escaping tier was added, and it is
still the rule most likely to differ silently between two implementations that
both pass everything else.

---

## 3. The model

The section that saves the most time, because these are decisions you make in
the first hour and discover in the last.

### 3.1 A position's arity is model state

`[lon, lat]` and `[lon, lat, null]` state the same thing (§4.1) and are different
bytes. `corpus/accept/absent-elevation-and-time.vnx` is stored canonical carrying
trailing nulls, and the accept tier re-renders a decoded document and compares
bytes (§8.1).

**So a model with an `ele: Double?` field cannot reproduce the accept corpus.**
Store the channels as written. Both the Python and Swift implementations record
this as the first thing they got right only because the specification warned
them. *(Swift seat: "an `ele: Double?` model cannot re-render
absent-elevation-and-time.vnx.")*

Shortening a position is a *writer* rule, not a formatter rule — see §3.3.

### 3.2 Absent, empty and false are three different documents

§3.1: absence means "not known" and is not a default value, with two stated
exceptions. A key that is absent, a key present with an empty array, and a
`hidden` written as `false` are three different documents and have to round-trip
as three different documents.

`label-visible` is the one where getting this wrong is invisible: absent means
Auto, and a reader that treats it as `false` flattens Auto to hidden with nothing
to report (§3.6).

### 3.3 There are two emit paths, and a codec will reason itself into having one

This is the subtlest entry in the document.

- A **formatter** reproduces what a document carried. It does not round and does
  not re-spell a position. §4.5.4: "A reader MUST NOT round, and neither MUST a
  formatter."
- A **writer** emits a model as a document, applying the boundary rules: round to
  6 and 1 places, shortest spelling of every position.

`corpus/accept/full-precision-coordinate.vnx` is the file that separates them. It
reformats to itself byte for byte, and it encodes to `-3.671235`. Its own sidecar
says both.

**The trap:** a library that only ever round-trips documents will conclude, from
§4.5.4's own words, that it must not round at all — and be consistent, and pass
the whole corpus, and be missing half its API. The Python implementation reached
exactly that conclusion and argued it from the specification; the fixture that
contradicted it was one the same seat had already quoted for another purpose.
**Reading the format through what your library happens to be is the failure this
whole exercise exists to avoid.**

Go names them `Encode` and `Reformat` *(relayed, not read here)*; Swift
`VNX.write` and `VNX.render`;
JavaScript `write` and `rerender`; Python `encode_bytes` and `reformat_bytes`.
Swift's account puts the reason plainly: "separate entry points rather than one
with a flag, because a writer rounds."

The same split reaches the container. Repackaging a `.vnz` unchanged must not
silently round its coordinates, so the container gets two entry points too.

### 3.4 Opaque bodies survive at value level

Data records (§5.1), `rulesets` (§3.9) and style objects (§3.10) are opaque: no
key inside them is ever "unrecognised", and every one has to survive a
read-modify-write untouched.

That is a modelling decision, not a serialisation one. If you decode a record
body into a typed structure, you lose the fields your type does not name, and
nothing in the corpus will tell you — the canonical check compares bytes to bytes
and agrees with whatever the fixture already says.

---

## 4. Parsing

### 4.1 Non-finite literals

`NaN`, `Infinity` and `-Infinity` are not JSON, and a document containing one is
refused (§4.2). **Several standard libraries accept them by default** — Python's
`json` module does, silently — so this is a flag you have to turn off rather than
a behaviour you inherit.

A separate case: a literal like `1e999` *is* well-formed JSON whose value is not
finite in binary64. Check after parsing, not only during.

### 4.2 Duplicate object keys

Two entries with the same key resolve last-wins in every mainstream parser,
before any format rule runs, so an item vanishes and a reader built on a standard
parser has no way to observe it (§6.4). This is a documented limitation of the
format rather than a rule, and the corpus carries a case that expects no
diagnostic.

### 4.3 Parse every number as binary64

Including integer literals. §4.2 defines numbers as IEEE 754 binary64, so a JSON
integer is a binary64 too, and rendering it as one is what makes
`1778914991000.0` and `1778914991000` the same document.

---

## 5. Geometry

### 5.1 The projection model, and why the cosine reference decides

§3.3.3 gives a 50 metre tolerance for an inline chain endpoint and names the
distance function: a local equirectangular projection about the query point's
latitude, mean Earth radius 6371008.8 m, longitude scaled by the cosine of that
latitude.

**Every part of that sentence is load-bearing.** Four plausible models measured
against one corpus endpoint spread from 19.81 m to 36.47 m — a factor of 1.8:

| Model | Distance |
|---|---|
| equirectangular, cosine of the query point's latitude — as specified | 19.81 m |
| equirectangular, cosine of each point's own latitude | 20.34 m |
| degrees scaled by 111 320 on both axes, no cosine | 25.52 m |
| Web Mercator | 36.47 m |

*(Python seat.)* An implementation that reached for Web Mercator, or that scaled
by degrees without a cosine term, would pass every structural check and disagree
about which documents are legal.

### 5.2 A refusal fixture cannot discriminate, so the arc's direction is the test

This is the least obvious thing in the corpus and the first casualty of a later
tidy-up.

A fixture that must be **refused** cannot tell a correct model from one that
over-reads, because a model reading a larger distance also refuses. Every wrong
model refuses a 55 m case.

**Discrimination comes from the arc's direction.** Run the arc north-to-south so
its first vertex sits at high latitude and the query point at low, and a model
taking the cosine of the segment's start latitude *under*-reads — 36.8 m against
a true 55 — and **accepts a document the format refuses.** That is the more
dangerous direction, and only an arc pointing the other way exposes it.

So the two tolerance cases run in opposite directions on purpose: the accept case
catches a model that over-reads and refuses good geometry, the refusal case
catches one that under-reads and accepts bad geometry. *(Python seat; the pair is
in `corpus/accept/` and `corpus/diagnose/`.)*

The separation has to be large enough for the error to cross the threshold rather
than merely move the number. A three-degree arc makes an 8.5% model error visible
and does not change any verdict; thirteen degrees changes both.

---

## 6. Testing this format

### 6.1 The corpus's reach ends at the writer boundary

All four tiers **re-render an existing document**, which §8.1 requires precisely
because a writer rounds. So no tier can reach anything that only happens when a
model is emitted from scratch: the rounding of §4.5.4, the shortest-spelling rule
of §4.1, a negative zero that rounding created, and the tie direction of §1.5.

Kotlin measured that boundary rather than asserting it: **reverting the rounding
tie rule reddens two columns of its number table and nothing else in a 91-test
suite.**

A rendering tie is unreachable for a second, stronger reason: it is a *value*,
not a shape. Nobody hand-writes a double whose shortest decimal sits exactly
halfway between two candidates, and both spellings parse to the same double
anyway. *(Kotlin seat.)*

Kotlin's answer is `number-conformance/`, a table of 5,315 doubles as raw
IEEE 754 bits, each with its canonical spelling, its spelling rounded to 6 places
and its spelling rounded to 1. Reading it is about ten lines in any language.

### 6.2 A case only separates the readings its own data can separate

**The most valuable entry here, and there are four instances of it in one day.**

A fixture that *contains* a construct is not a test *of* it. What a case can
discriminate is decided by the data in the case, not by the construct it
contains or the note it carries.

- **JavaScript's node-key comparison.** §6.2 resolves a node key to its position
  before comparing chain continuity; the library compared the keys, which reports
  a continuous walk as broken. The corpus could not see the difference:
  `diagnose/chain-does-not-join.vnx` has keys **and** positions that differ, so a
  key-only comparator returns the same verdict. When the rule changed, nothing
  went red. *(JavaScript seat.)*
- **Kotlin's tie table.** Sixty rows labelled as rendering ties, mined by an
  undiscriminating detector; every one stayed green when the tie rule was
  reverted. The decoration was measurable: 60 rows, zero discrimination.
  *(Kotlin seat.)*
- **Swift's ZIP64 fixture.** Its first one masked sizes in the local header only,
  so disabling the extra-field parser left the test green. *(Swift seat; relayed
  by the lead rather than read here.)*
- **A corpus runner that stopped comparing.** When the sidecars moved from
  pinning English to pinning a reason code, one runner's matcher looked for a
  `message` field that no longer existed, found nothing, and fell through to
  comparing paths alone. Every diagnose case stayed green while checking half of
  what it pinned. *(Python seat.)*

**The common shape is that a check which quietly stops discriminating always
passes.** Nothing goes red, because red requires a comparison.

Four independent seats hit this class within an hour, and each wrote a different
guard for it — which is the useful part, because the guards compose:

- fail a sidecar carrying a field the runner does not compare, rather than
  ignoring it *(JavaScript, Python)*;
- red-proof right-path, right-English, **wrong-cause** *(Kotlin)*;
- assert your cause table against a live corpus run **in both directions**, so a
  table entry the corpus stopped exercising fails as loudly as a corpus
  expectation the table never mentions *(Swift, JavaScript)*;
- sweep the fields *inside* a nested expectation, not only the top level of a
  sidecar *(Python)*.

### 6.3 Red-proof everything, and check the proof reached its target

"The corpus passes" is not evidence that the corpus discriminates. Break one
thing, run the check that should notice, watch it go red, restore.

Two ways a red proof lies:

**It can miss its target.** One red proof patched the library's function while
the test module had bound the name at import, so the assertion was still calling
the real one and the guard stayed green. "The red proof stayed green" is exactly
the evidence someone uses to throw a good test away. *(Python seat.)*

**Its mutation can match nothing.** JavaScript's red-proof suite makes each
mutation assert that its own search string was found, because a substitution that
matched nothing would make the whole file a test of nothing. *(JavaScript seat.)*

Three of JavaScript's 25 mutations assert a defect is **not** caught, which
records a gap in the corpus rather than assuming one. Swift injected 34. Freezing
a gap this way pays off directly: one such frozen assertion went red the day the
`link-names-absent-arc` fixture gained a third link, which is how its author
learned the gap had closed.

### 6.4 A relayed fact is not a measurement

Five implementations coordinating through one person means most facts reach most
seats second-hand, and **a confident relay is indistinguishable from a measured
one by the time it arrives.** Two instances in a single day, both from the seat
best placed to be believed:

- A claim about which control characters Go escapes was passed on as measured and
  was not. It was later measured properly, and the answer differed. Had it landed
  in a document like this one first, it would have calcified.
- A ruling was relayed to a seat by another seat rather than issued directly —
  and the relaying seat was the author of the thing being adopted, which is the
  one case where its interest and the message coincide.

**Mark provenance on every fact you did not measure yourself**, and name the seat
that did. This document does that inline, and the two entries above are the
reason. Where you have only a relay, say so; a reader can then go and measure it,
which is the whole difference between a document and a rumour.

### 6.5 Two runs against a shared tree, and they answer different questions

If more than one implementation is being written in one repository, your suite
can fail for reasons that are not yours.

- **Against a clean extract of a commit** — `git archive HEAD corpus | tar -x -C
  <scratch>` — answers *is my change correct?* It is the only run whose failures
  are about you.
- **Against the working tree** answers *what have my peers just added that I will
  have to handle?* It is the only run that can see a fixture that does not exist
  at HEAD.

Run both, label which produced any number you report, and establish which one you
were in before reading a failure. JavaScript's runner takes the corpus path from
an environment variable so the second measurement is one command rather than an
edit.

---

## 7. Where each language falls in

Navigation only. Every entry is above; nothing here is stated only here.

**Go** — negative zero renders as `-0` (§1.8). `encoding/json` escapes `<`, `>`,
`&`, U+2028, U+2029 by default (§2.3). Key order (§1.1).

**Python** — `1e-06` and `1e-07` at the exponent boundaries (§1.2). `json`
accepts `NaN` and `Infinity` by default (§4.1). `sort_keys` is plain
lexicographic (§1.1, §2.1).

**Swift** — `JSONSerialization` renders `-3.67` as `-3.6699999999999999` and must
not be used. `JSONEncoder` gives `1e-06` and `1e-07`, and both stock encoders
separate a key from its value with `" : "` (§1.1, §1.2). `.rounded()` is not
half-to-even (§1.5). `Double.description` is (§1.3).

**Kotlin** — shortest-repr ties away from zero (§1.3). `BigDecimal.HALF_UP` at
the rounding boundary (§1.5).

**JavaScript** — `toFixed` rounds ties away from zero (§1.5). `JSON.stringify`
uses insertion order for keys (§1.1). `Array.prototype.sort` with no comparator
orders by UTF-16 code unit (§2.2). Number rendering is otherwise correct, apart
from having no negative-zero problem to fix.

**Rust** — not yet reported. This section will be filled in from that
implementation's own account.

---

## What this document does not cover

The rules themselves. Where this and `SPEC.md` appear to disagree, `SPEC.md` is
right and this is a defect worth reporting.

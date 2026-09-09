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

Read it before writing your serialiser, not after your corpus run goes red.

---

## 1. Numbers

The longest section, because numbers are where the languages differ most and
where the corpus can help least.

### 1.1 No stock encoder emits canonical form

Section 4.5.6 carries the table. The short version is that key ordering alone
rules out every stock encoder in every target language: canonical form sorts
scalars before containers, and every `sort_keys` or `.sortedKeys` option sorts
plain lexicographically, which puts `networks` before `vnx` and `nodes` before
`notes`.

Once you accept that you are writing your own serialiser, the inline-position
rule and the number rules cost a few lines each. The reference serialiser is
about 170 lines, most of which is the number renderer.

### 1.2 Shortest round-trip digits, and where the decimal point goes

Section 4.5.3 defines rendering by reference to ECMAScript `Number::toString`.
That is two rules, and languages tend to give you the first and get the second
wrong.

The digits are the shortest decimal string that round-trips to the same binary64.
Most languages provide this: Python's `repr`, Swift's `Double.description`, Go's
`strconv.FormatFloat(…, 'g', -1, 64)`.

The placement is the part you implement. ECMAScript chooses between plain and
exponential notation on the decimal exponent rather than on the digit count, and
the thresholds are unintuitive:

| Value | Renders as | Why |
|---|---|---|
| `340.0` | `340` | integral, under 21 digits |
| `0.000001` | `0.000001` | exponent −5, still plain |
| `0.0000001` | `1e-7` | exponent −6, exponential |
| `9.9e-7` | `9.9e-7` | same exponent, so also exponential; the digit count is irrelevant |
| `1e20` | `100000000000000000000` | 21 digits, still plain |
| `1e21` | `1e+21` | 22 digits, exponential, and note the `+` |

Rust is the clearest case of the split, because its standard library gives you
the hard half and none of the easy one. `{}` on an `f64` produces shortest
round-trip digits and never uses exponent notation at all, so `1e21` comes out as
twenty-two digits and `1e-7` as `0.0000001`; `{:e}` always uses it and spells the
exponent `e21` where the format needs `e+21`. *(Measured by the Rust implementation.)*

Python and Swift emit `1e-06` and `1e-07` where the format needs `0.000001` and
`1e-7`, so both implement the placement rule by hand. *(Measured by the Python implementation, 6,009 doubles cross-checked against V8; Swift implementation, 220,011 doubles.)*

### 1.3 A rendering tie rounds to even, and here is the route to getting it wrong

When two equally short decimal spellings both round-trip to the same double and
sit exactly equidistant from its true value, the format takes the one whose last
digit is even (Section 4.5.3).

The rule is one line. The route is the entry, because the route is what catches
the next person, and it looks like diligence at every step. *(Measured by the Kotlin implementation, in its own account.)*

Section 4.5.3 says numbers render with ECMAScript `Number::toString`. So go and
read ECMAScript. Find the clause about choosing between candidate digit strings.
In the edition you are likely to land on, it says: *"if there are two such
possible values of s, choose the one that is larger."* That is unambiguous, it is
a normative standard, and the format pointed you at it. Implement half away from
zero, write a test asserting it, ship.

Two things are wrong with that, and neither is carelessness.

The standard does not decide it. Its current text says the opposite of deciding:
*"the least significant digit of s is not necessarily uniquely determined by
these criteria."* The "choose the larger" line is superseded text. Following a
normative-sounding sentence produced a more confident wrong answer than guessing
would have.

What the anchor cannot settle, the format settles itself, and Section 4.5.3 now
does: a tie is resolved by rounding to even, with `1497898211722073.25` rendering
`1497898211722073.2`. The implementations agree on that:

```
1.4978982117220732e15
  V8                     1497898211722073.2
  Go encoding/json       1497898211722073.2
  Kotlin, before its fix 1497898211722073.3
```

The transferable rule is about anchors rather than about ties. An anchor to an
external standard settles only what that standard settles. Before adopting one,
read it for what it declines to decide, because those are the places your format
has to answer for itself and the places a careful implementer will otherwise
answer differently from you.

Both spellings parse to the identical double, so every round-trip test passes,
every value-level assertion passes: two conforming libraries emit different files
and no test comparing values can see it.

A rendering tie needs 16 or 17 significant digits, which places it roughly between
1e10 and 1e16. Measured over a million values drawn from each of longitude,
latitude, elevation and epoch milliseconds, none produced one: coordinates and
elevations are too small, and a millisecond count is an integer. Over uniformly
drawn binary64 bit patterns they occur at about one in 4,200. So the hazard is an
arbitrary number inside a data record body, which the format permits anywhere and
does not constrain — and it is a smaller hazard than the rounding tie of 1.5,
which reaches every coordinate and every elevation. *(Measured by the Go implementation; the population matters more than the rate, and the rate alone points at the wrong risk.)*

Rust is the third implementation to ship the wrong direction:
`format!("{}", 1497898211722073.25)` gives `…073.3`. *(Measured by the Rust implementation.)* Its route is
worth naming because it is the opposite of carelessness: a shortest-round-trip
formatter looks safe precisely because it gets the difficult part right. The
language hands you the expensive half and quietly loses the cheap one.

Swift's `Double.description` and Python's `repr` already round to even. Verify
rather than assume; it costs one test, and neither language guarantees it.

Then check your two number paths separately. Fixing the renderer does not fix the
writer, which is Section 1.5 below.

### 1.4 The obvious tie detector finds phantoms

Asking whether a neighbouring spelling of the same length also round-trips seems
to identify a tie. It does not, and it errs in the direction that hurts: it says
yes to values that are not ties.

The reason is one sentence, and it explains every phantom in this section. A
round-trip check does not ask whether a value *is* a given decimal. It asks
whether the value is the *nearest double* to that decimal, and many decimals
share one nearest double. *(Measured by the JavaScript implementation.)*

Two shapes of the same error:

`(0.15).toFixed(2)` is `"0.15"`, and that string parses back to `0.15`, so the
check fires. But `0.15` as a double is `0.1499999999999999944…`, strictly below
the midpoint. It is an ordinary round-down that every language takes to `0.1`.

Above 2^49 the gap between adjacent doubles exceeds 0.1, so several one-decimal
spellings map to one double and only one of them is nearest. That is an ordinary
shortest-representation choice, not a tie.

`-3.6712345678901235`, the coordinate in two accept files, is the same trap at
full precision. Its exact value is `-3.67123456789012347912…`; near 3.67 the
spacing between adjacent doubles is about 4.4e-16 while the seventeenth
significant digit steps by 1e-16, so three spellings map to that one double and
only the middle one is nearest. The shortcut calls it a tie and exact arithmetic
does not. *(Measured by the JavaScript implementation, measured.)*

Those two instances are the same rule at magnitudes eleven orders of magnitude
apart. Each was found while the other was unknown to its finder, one searching for
candidate ties and one writing a mutation, so the general form:

> Wherever the decimal step at the shortest-round-trip length is finer than the
> spacing between adjacent doubles, several spellings share one double and the
> round-trip shortcut finds phantoms.

Two implementations built this detector, and red-proofing caught both. The Kotlin generator mined 60 rows labelled as ties, every one of which
stayed green when the tie rule was reverted; the category was decoration. *(Measured by the Kotlin implementation.)* A Python search reported 400 candidates of which 91 looked like failures,
and all 91 were artefacts of the detector. *(Measured by the Python implementation.)*

A true tie needs exact equidistance, which needs exact arithmetic: rational or
big-decimal, never floating point. `fractions.Fraction(v)` and `decimal.Decimal(v)`
are both exact for a Python float; BigInt over the decomposed double works in
JavaScript. Integer arithmetic on the mantissa needs no big-number type at all:
with the value as `m · 2^e` and the two candidate spellings summing to an odd
`half`, a tie is `m · 2^(e+1−scale) == half · 5^scale`, and both sides fit in a
128-bit integer for every case that can be a tie, because the odd parts must be
equal and one is bounded by 2^53. Bailing out on overflow is sound there rather
than a fallback. *(Measured by the Rust implementation.)* Kotlin's generator goes further and defines the detector *as* the
thing under test, shortest-to-even against shortest-away-from-zero in exact
rational arithmetic, then refuses to emit a table in which any tie category fails
to discriminate.

### 1.5 The rounding tie is a second defect in a second place

Section 4.5.4 rounds longitude and latitude to 6 decimal places and elevation to
1, at the writer boundary. Ties there go to even as well, and this is separate
code from Section 1.3 with a separate bug in it.

| value to 1 place | Go | Python | JavaScript `toFixed` |
|---|---|---|---|
| `340.25` | `340.2` | `340.2` | `340.3` |
| `-340.25` | `-340.2` | `-340.2` | `-340.3` |
| `340.75` | `340.8` | `340.8` | `340.8` |

*(Measured by the JavaScript implementation on Go 1.25.6, CPython 3.14 and
Node 22.19.)*

ECMAScript's `toFixed` is specified to pick "the larger n" on a tie, which is
half away from zero for a positive value and toward zero for a negative one. It
therefore agrees with half-to-even in one direction, which is worse than
disagreeing consistently: a guard that tests only positive values passes by
accident.

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

### 1.6 Building a rounding-tie fixture is a trap

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

### 1.7 "Round to N places" names two different operations

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

This is a larger defect than the tie direction of 1.5, and a separate one. The tie
direction disagrees only on exact halves; the operation disagrees on most values.
A library can have the tie rule right and still be wrong on almost every elevation
it writes.

Formatting to a decimal string and parsing it back is one correct implementation,
because the formatter expands the double exactly and rounds ties to even. The
caveat is 1.9: the formatted text must never become the emitted bytes, since a
one-place format of `340.0` is `340.0`. Round to a *value*, then let the renderer
decide the bytes.

Decimal intuition lies about which way a value rounds, whichever operation you
pick. `3.05` rounds down to `3.0`, correctly, because the literal is really
`3.04999999999999982236431605997495353221893310546875`. A test written from
intuition asserts `3.1` and reports a correct writer as broken. *(Python implementation, which wrote that expectation before measuring it.)* But `3.05` gives `3.0` under both
operations above, so it cannot tell them apart, which is why the worked example
here is `0.15`.

### 1.8 Negative zero, twice over

Section 4.5.3 renders negative zero as `0`. There are two ways to meet that and
most implementations find only the first.

Collapse it on render: Go's `encoding/json` emits `-0` and has to be corrected.

Then note that rounding creates one. `round(-0.04, 1)` is `-0.0`, sign bit set.
The rule therefore applies to what rounding produced, not only to what the
document carried, and a writer that collapses on input but not on output emits a
byte sequence no document ever contained. *(Measured by the Python implementation; the JavaScript and Swift implementations added the same guard.)*

### 1.9 Never let a formatting primitive produce the final bytes

`format(-0.04, '.1f')` is `-0.0`. `format(340.0, '.1f')` is `340.0`. Both are
wrong bytes, and both come from using a formatting function to do a serialiser's
job.

Round, then hand the result to your renderer. The renderer is the only thing that
decides what a number looks like: it collapses negative zero, strips the trailing
zero, and applies the placement rule of Section 1.2. A formatting primitive does
none of that and cannot be told to.

### 1.10 Being the language a rule is defined *by* settles less than it looks

Section 4.5.3 anchors number text to ECMAScript `Number::toString`, which makes
`String(n)` in a JavaScript runtime the reference implementation of that rule.
That looks like the one implementation that cannot get numbers wrong. The reasons it is
not generalise to any rule defined by reference to another system.

It does not settle a rendering tie: the ECMAScript prose declines to fix the last
digit when two shortest spellings are equally close, which is why the tie
direction had to be stated separately. It does not settle the rounding tie, which
is a different operation at a different boundary. And being anchored to ECMAScript
for rendering is exactly what makes numbers feel like the settled part.

What it does settle, it settles invisibly. `String(-0)` is already `"0"`,
`String(1e21)` is `1e+21` with the plus sign, `String(1e-7)` is `1e-7`, and
`String(n)` equals `JSON.stringify(n)` for every finite double. Negative zero
costs a JavaScript implementation nothing, and the `1e+21` spelling appears in
the corpus while being stated nowhere in Section 4.5.3's prose or table, so it
arrives correct without anyone deciding it.

The sharpest instance is on the other side of the codec. Section 4.5.3 anchors
number *text* to `Number::toString`, which is the emit side. Nothing anchors the
*parse* side to ECMAScript, and the parse side is where that runtime's one silent
non-finite lives: `1e400` comes back as infinity rather than throwing (Section
4.1). Being the reference for how numbers come out bought nothing about how they
go in.

That is the real hazard of being the reference language: the things that are right
by accident are the things you are least likely to notice the specification never
said. *(Measured by the JavaScript implementation.)*

The pattern is not confined to it. A Rust `String` is UTF-8 by
construction, so a lone surrogate cannot be represented and its parser refuses
`"\ud83d"` rather than substituting U+FFFD. Section 4.5.5 says nothing about a
lone surrogate on the *parse* side, and a language with UTF-16 strings accepts one
and carries it. Correct behaviour, arriving free, on a question the format has not
answered. *(Measured by the Rust implementation.)*

---

## 2. Key order and strings

### 2.1 Scalars sort before containers

Section 4.5.1. Classification is by the value present in *this* object rather
than by the key name, so one key can sort into either group in different objects.
No stock encoder does this; see Section 1.1.

### 2.2 Lexicographic means by code point, and three defaults disagree

Every key the format defines is ASCII, where code point, UTF-8 byte and UTF-16
code unit order all agree. A serialiser that only ever sees format keys is
therefore correct forever and gives you no signal. A data record body may carry
any key (Section 5.1), and that is the reachable path.

Above U+FFFF the orders disagree, because an astral character is stored as a
surrogate pair beginning in U+D800..U+DBFF. By code unit an astral character
sorts below everything in U+E000..U+FFFF; by code point it sorts above all of it.
*(Measured by the JavaScript implementation.)*

| Language | Default string comparison | Astral pair | Comparator needed |
|---|---|---|---|
| Go `sort.Strings` | code point | correct | no |
| Python `sorted()` | code point | correct | no |
| Swift `<` on `String` | Unicode canonical equivalence | correct | yes, for a different input |
| JavaScript `Array.prototype.sort()` | UTF-16 code unit | wrong | yes |
| Java `String.compareTo` | UTF-16 `char` | wrong | yes |
| Kotlin `String.compareTo` | UTF-16 `char` on the JVM | expected wrong | yes |
| Rust `str` ordering | UTF-8 byte, so code point | correct | no |

*(Measured by the JavaScript implementation: measured JavaScript on Node 22.19, Java through `jshell`, Python on CPython 3, Go, and Swift. Kotlin is reasoned from Kotlin/JVM delegating to `java.lang.String.compareTo`, which was measured; Kotlin/Native does not go through `java.lang.String` and may differ.)*

`String.compareTo` returns -10176 on that pair, which is 0xD83D minus 0xFFFD, so
the mechanism is visible in the return value. Rust orders by UTF-8 byte, which for
well-formed UTF-8 is code-point order, and gets both of the pairs the others
had to fix. *(Measured by the Rust implementation.)*

Swift is the row that rewards care. Its default sorts the astral pair correctly,
so a Swift test written against U+1F600 passes while the library is still wrong.
The divergence is a different input with a different trigger:

```swift
"\u{212B}" < "\u{00C6}"     // true in Swift
0x212B < 0x00C6             // false by code point
```

U+212B ANGSTROM SIGN canonically decomposes to U+00C5, which is below U+00C6, so
Swift orders by canonical equivalence rather than by scalar value. That is a real
divergence from Section 4.5.1, reachable by the same route as the surrogate case,
and it is the pair a Swift implementation should test. *(JavaScript implementation, measured.)*

The comparator is about nine lines: iterate both strings with a code-point
iterator rather than by index, and compare code points.

Test it in both directions on the same pair, asserting that the default gets it
wrong and that the comparator gets it right. A test asserting only the
comparator's answer keeps passing if someone deletes the comparator and the
default happens to agree, which on ASCII it always does. *(Measured by the JavaScript implementation.)*

### 2.3 String escaping

A rule that says *which* characters are escaped has not said *how*. RFC 8259
offers two conforming spellings for the same character, the two-character escape
and the six-character `\uXXXX`, so a `notes` field carrying a form feed can be
`\f` from one implementation and `\u000c` from another with both satisfying such
a rule exactly. Section 4.5.5 states both halves.

Section 4.5.3 had already reached outside the format for one byte-level rule, to
ECMAScript. Once a format has chosen an anchor,
reaching to the same place for the next byte-level question is consistency, and
reaching anywhere else is a second arbitrary choice the reader has no way to
predict. So the answer is `JSON.stringify`'s, not because ECMAScript is
authoritative about JSON strings, but because it was already the format's anchor.
*(Measured by the Rust implementation.)*

Two languages' defaults disagree with it in opposite directions.

Escape only `"`, `\` and the control characters below U+0020. Five control
characters take their short escape, `\b`, `\f`, `\n`, `\r` and `\t`, and every
other one is `\u00xx` with lowercase hex. Nothing else is escaped, including
non-ASCII, `<`, `>`, `&`, U+2028 and U+2029, and nothing above U+007F is ever
written as a `\u` escape.

Go's `encoding/json` escapes `<`, `>`, `&`, U+2028 and U+2029 by default, and
`SetEscapeHTML(false)` turns off only the first three. It still escapes U+2028
and U+2029, so the Go standard library cannot emit canonical form at any setting,
which makes writing strings yourself forced rather than preferred. It writes `\b`
and `\f` in their short forms, agreeing with the format. *(Kotlin implementation, measured.)*

Go's escaping of U+2028 and U+2029 is unconditional because its encoder is
defending output that might be pasted into a `<script>` tag: a sound default for a
general-purpose encoder and wrong for a format that pins its bytes. So strings
are a second independent reason every implementation writes its own serialiser,
with the reference implementation's own standard library as the instance.
*(Measured by the Rust implementation.)*

Nothing constrains the case of the hex digits except the rule itself, and Go, V8
and Kotlin all emit lowercase, so no implementation disagrees and no fixture
could expose one that did. That is the same shape as the two ties: a degree of
freedom that costs nothing until someone exercises it. *(Measured by the Kotlin implementation.)*

The escaping tier is the only place the corpus reaches any of this, and it remains
the rule most likely to differ silently between two implementations that pass
everything else.

---

## 3. The model

The section that saves the most time, because these are decisions made in the
first hour and discovered in the last.

### 3.1 A position's arity is model state

`[lon, lat]` and `[lon, lat, null]` state the same thing (Section 4.1) and are
different bytes. `corpus/accept/absent-elevation-and-time.vnx` is stored
canonical carrying trailing nulls, and the accept tier re-renders a decoded
document and compares bytes (Section 8.1).

A model with an `ele: Double?` field therefore cannot reproduce the accept corpus.
Store the channels as written. *(Measured by the Swift implementation: "an `ele: Double?` model cannot re-render absent-elevation-and-time.vnx.")*

The pull is strongest in the languages best at modelling absence. `Option<f64>` in Rust is zero-cost,
idiomatic, obviously correct, and what every tutorial would produce. A type
system can be very good at "this value may be missing" and have no idiom at all
for "this value may be missing, and how its absence was spelled is data." The
correct model reads like an under-designed one: the position carries its channels
and its arity is a method. *(Measured by the Rust implementation.)*

Shortening a position is a writer rule, not a formatter rule; see Section 3.3.

### 3.2 Absent, empty and false are three different documents

Section 3.1 of the specification: absence means "not known" and is not a default
value, with two stated exceptions. A key that is absent, a key present with an
empty array, and a `hidden` written as `false` are three different documents and
have to round-trip as three.

`label-visible` is where getting this wrong is invisible. Absence means Auto, and
a reader that treats it as `false` flattens Auto to hidden with nothing to report
(Section 3.6).

This is the arity trap of 3.1 one level of nesting out, and the language pulls
the same way. A plain list for every collection, where absent and empty are one
value, is more idiomatic in Rust than an optional list, and every reviewer would
suggest the change. Until a fixture carrying an empty collection landed, no corpus
case had one, so an implementation that conflated them passed every tier. The Rust
implementations reports getting this right by accident and for the wrong reason, having been
warned about arity and not about collections. *(Measured by the Rust implementation.)*

### 3.3 There are two emit paths, and a codec will reason itself into having one

A formatter reproduces what a document carried. It does not round and does not
re-spell a position. Section 4.5.4: "A reader MUST NOT round, and neither MUST a
formatter." A writer emits a model as a document, applying the boundary rules,
rounding to 6 and 1 places and emitting the shortest spelling of every position.

`corpus/accept/full-precision-coordinate.vnx` separates them. It reformats to
itself byte for byte, and it encodes to `-3.671235`. Its sidecar says both.

The trap is that a library which only ever round-trips documents will conclude,
from Section 4.5.4's own words, that it must not round at all, and be consistent,
and pass the whole corpus, and be missing half its API. The Python implementation
reached exactly that conclusion and argued it from the specification; the fixture
that contradicted it was one that implementation had already quoted for another purpose.
Reading the format through what your library happens to be is the failure this
exercise exists to avoid.

Go names them `Encode` and `Reformat` *(relayed, not read here)*; Swift
`VNX.write` and `VNX.render`; JavaScript `write` and `rerender`; Python
`encode_bytes` and `reformat_bytes`. Swift's account puts the reason plainly:
separate entry points rather than one with a flag, because a writer rounds.

The split reaches the container. Repackaging a `.vnz` unchanged must not silently
round its coordinates, so the container gets two entry points too.

### 3.4 Opaque bodies survive at value level

Data records (Section 5.1), `rulesets` (Section 3.9) and style objects (Section
3.10) are opaque: no key inside them is ever unrecognised, and each has to
survive a read-modify-write untouched.

That is a modelling decision rather than a serialisation one. Decode a record
body into a typed structure and you lose the fields your type does not name, and
nothing in the corpus will tell you, because the canonical check compares bytes
to bytes and agrees with whatever the fixture already says.

---

## 4. Parsing

### 4.1 Non-finite literals

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

### 4.2 Duplicate object keys

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

### 4.3 Parse every number as binary64

Integer literals included. Section 4.2 defines numbers as IEEE 754 binary64, so a
JSON integer is a binary64 too, and rendering it as one is what makes
`1778914991000.0` and `1778914991000` the same document.

---

## 5. Geometry

### 5.1 The projection model, and why the cosine reference decides

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

### 5.2 A refusal fixture cannot discriminate, so the arc's direction is the test

This is the least obvious property in the corpus and the first casualty of a
tidy-up.

A fixture that must be refused cannot tell a correct model from one that
over-reads, because a model reading a larger distance also refuses. Every wrong
model refuses a 55 m case.

Discrimination comes from the arc's direction. Run the arc north to south, so its
first vertex sits at high latitude and the query point at low, and a model taking
the cosine of the segment's start latitude under-reads: 36.8 m against a true 55,
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
files. Thirteen degrees gives 67.4 m and 36.8 m, which flips both. A fixture
that moves a number without moving a verdict separates nothing.

---

## 6. Testing this format

### 6.1 Know which rules no fixture reaches

Four of the corpus tiers re-render an existing document, and a re-render neither
rounds nor re-spells a position. For a long time that put the whole of Section
4.5.4 outside the corpus, and each implementation carried its own tests for it.

A fifth tier, `corpus/write/`, decodes an input and calls the write path, so the
rounding of Section 4.5.4, the shortest-spelling rule of Section 4.1, both tie
directions and a negative zero the writer created are all now reachable. An
implementation exposing no write path fails it by name rather than by a
confusing byte diff.

The reason to keep this entry is what happened to the claim it used to make.
The writer boundary was described, in this document and elsewhere, as permanently
beyond the corpus's reach. It was not: it was beyond the reach of the tiers that
existed. The same correction had already been made once, to a claim that a
rendering tie could not be reached because a tie is a value rather than a shape —
until a formatter case carried one spelling of such a double as its input and the
other as its expected output.

> An unreachability claim is a claim about the current state of somebody's
> ingenuity, not about the format.

A related trap, and the one condition here that care does not fix: a wrong fact
travels unchallenged when the conclusion it supports is right anyway. One
implementation reasoned about a standard library's escaping rather than running
it, reached the correct answer by a wrong route, and nobody had reason to check,
because the answer was the same either way. Evidence that is never load-bearing
is never tested. When a claim you are relying on would not change your conclusion
if it were false, that is exactly when to measure it.

So the useful question is not which rules are unreachable, but which are
unreached *today*, and what shape a case would have to take. Where the two
readings of a rule produce different text for the same value, a re-render tier
can reach it. Where a rule only takes effect when a model becomes a document, a
write tier can. Where neither is true, the prose is the only statement of the
rule and your own tests are the only enforcement: the tolerance boundary at
exactly 50 metres is one, because no two distance models land on exactly 50.

### 6.2 A case only separates the readings its own data can separate

The most valuable entry here, with five instances found in one day.

A fixture that *contains* a construct is not a test *of* it. What a case can
discriminate is decided by the data in the case, not by the construct it holds or
the note it carries.

Eight instances have turned up, and they fall into three mechanisms rather than
one. The symptom is shared and the repairs are not.

#### A case whose data cannot separate the readings

These were never checking the thing they named, because no answer they could
produce would differ between a correct implementation and a wrong one.

**JavaScript's node-key comparison, where the order of events is the lesson.** It
implemented an earlier reading of Section 6.2 literally, that two node keys meet
when the keys are equal, so two distinct keys at one position did not meet and a
continuous walk was reported as broken. The rule was then settled the other way,
resolving a key to its position first and demoting key equality to a fast path.
The library had shipped the opposite reading, and on re-running the corpus: 31 of
31, green. Nothing went red, and its own tests could not help either, having been
written to match its own reading.

The fixture that exists for continuity, `diagnose/chain-does-not-join.vnx`,
varies the node keys *and* the positions together, so a key comparator and a
position comparator return the same verdict on it. It looks like coverage. It is
in `diagnose/`, it is named for continuity, its note explains it, and it cannot
attribute its verdict to either reading. Any fixture that changes two variables
together tests neither. *(Measured by the JavaScript implementation.)* The fix was as invisible as the
defect, still 31 of 31 afterwards.

**Kotlin's tie table.** Sixty rows labelled as rendering ties, mined by an
undiscriminating detector, every one green when the tie rule was reverted. The
decoration was measurable: 60 rows, zero discrimination. *(Measured by the Kotlin implementation.)*

**Swift's ZIP64 fixture.** Its first one masked sizes in the local header only,
so disabling the extra-field parser left the test green. *(Reported by the Swift
implementation.)*

#### A check that was discriminating and quietly stopped

These worked when written. Something moved underneath them.

**A corpus runner that stopped comparing.** A sidecar pins a diagnostic by reason
code; one runner's matcher looked for an English `message` field, found nothing,
and fell through to comparing paths alone. Every diagnose case stayed green while checking half of what it
pinned. *(Measured by the Python implementation.)*

**Absence-proofs that went stale.** A red-proof asserting that no corpus tier
catches some defect was true when written and false once a formatter case reached
it. See 6.3.

**A fixture retired by a rule change.** A test used two positions that collide
once rounded, to prove that arc-boundary de-duplication compares full values.
Change the rounding rule so they no longer collide and the test asserts that a
de-duplication has not merged two points it could not have merged. Nothing about
it looks wrong at any point.

#### A guard that was never connected to what it names

The sharpest of the three, because the other two at least once measured something.

**A test named for a fixture that never reads the fixture.** A Rust test called
`the_corpus_fixtures_land_where_their_names_say` would still have passed if both
corpus files were deleted: it restated numbers rather than reading the artefact.
Beside it, a test asserting that the wrong cosine reference changes neither corpus
verdict passed green while asserting something false of the corpus it named.
Restating a number is not reading an artefact, and a test that restates one cannot
notice when the artefact moves. *(Reported by the Rust implementation.)*

**A ZIP reader and writer by the same hand**, which is the container-boundary
instance and the only one whose fix is to check against an implementation not
written from this specification. See 6.5 below.

The repair for this third mechanism is to make the test read the artefact. Mutate
the implementation, run the real corpus, and assert exactly which cases fail. That
turns "the corpus discriminates" from a restated number into a measurement, and it
goes red when a fixture moves.

The common shape across all three is that a check which does not discriminate
always passes. Nothing goes red, because red requires a comparison.

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

The detection recipe is cheap and worth running over every fixture you rely on.
Write down what the note claims the case tests, then write down which values the
case actually varies. Where it varies more than one, ask which readings it can
still separate.

That recipe found three real gaps in one afternoon: the continuity fixture above;
`link-names-absent-arc.vnx`, whose two links made "one defect must not become
two" a guarantee of arity rather than of the reader; and a tolerance pair whose
arc spanned too little latitude to separate the distance models it existed to
separate.

**### 6.3 Red-proof everything, and check the proof reached its target

A passing corpus is not evidence that the corpus discriminates. Break one thing,
run the check that should notice, watch it go red, restore.

A red proof lies in two ways. It can miss its target: one patched the library's
function while the test module had bound the name at import, so the assertion was
still calling the real one and the guard stayed green. "The red proof stayed
green" is exactly the evidence someone uses to throw a good test away. *(Measured by the Python implementation.)* And its mutation can match nothing, which is why the JavaScript suite
makes each mutation assert that its own search string was found: a substitution
matching nothing would make the whole file a test of nothing. *(Measured by the JavaScript implementation.)*

A third way, and the least obvious: **the mutation can be broader than the rule
it claims to test.** A first attempt at an away-from-zero renderer used the
round-trip shortcut of Section 1.4, so it changed the spelling of phantoms as well
as of ties and reddened four corpus cases instead of one. Writing the assertion to
match what was observed would have recorded a broader claim than the truth and
attributed three failures to a rule they had nothing to do with. The repair is a
companion test freezing that the extra value is a phantom, so the single
remaining failure can be attributed to the tie rule. *(Measured by the JavaScript implementation.)*

A fourth way, and the simplest: **the mutation can never reach the code it claims
to break.** A notes-discarding mutation came back green because it patched only
the first of several call sites, and no accept fixture had a folder carrying
`notes`. A mutation that misses its target is indistinguishable from a runner that
found nothing wrong.

And a proof whose conclusion is an absence needs re-running whenever the thing
it claims to be absent from grows. A red-proof asserting that no tier catches
some defect is a green-asserting proof, and those rot silently as the corpus gains
cases. The rendering tie is the worked example: the assertion was true when
written and false once a formatter case reached it.

Three of JavaScript's 25 mutations assert that a defect is *not* caught, which
records a gap in the corpus rather than assuming one. Swift injected 34. Freezing a
gap pays off directly: such an assertion goes red the moment a fixture closes the
gap, which is how its author finds out.

### 6.4 Red-proof the instrument, not only the code

> A comparison that has never been seen to fail is not a comparison. It is a
> green light with no bulb in it. *(Measured by the Kotlin implementation.)*

The Kotlin cross-check against Go covers 460,007 doubles with zero
disagreements: 300,000 uniform over the whole binary64 range via
`Double.fromBits`, every value the specification and corpus name, and 40,000
dense small magnitudes. The two halves share no code. One language writes raw
IEEE 754 bits plus its own rendering to a table, and about thirty lines in the
other bit-casts each row back and compares.

The number that makes it worth having is not 460,007 but 78 — the disagreements
that reappear when the tie mode is reverted. Without those, a zero-disagreement
result is indistinguishable from a comparison that is not running.

Three traps in building such an instrument, all first-hand *(Kotlin implementation)*:

- Bits must travel as bits. Parsing the bit patterns with an unsigned parser
  fails on every negative double, because the sign bit makes the integer
  negative. With the error ignored, that reported 149,667 disagreements against a
  value of `0`. Parse signed and convert, or write the bits as hex.
- A mining criterion can decorate rather than discriminate; Section 1.4.
- Determinism. Go randomises map iteration, so a generated table's header changed
  on every run, and a table nobody can diff is most of the way to useless.

The result is a language-neutral table of 5,315 doubles with three reference
answers each, rendered, rounded to 6 places and rounded to 1, which is about ten
lines to read in any language.

### 6.5 A reader and a writer by the same hand agree with each other

The format pushes you into writing your own serialiser (Section 1.1), your own
JSON parser (Section 4.1), and for `.vnz` your own ZIP writer, because Section 7
needs control a stock library may not expose: the UTF-8 flag set explicitly,
per-entry compression method inspection, and seeing *both* halves of a duplicated
entry name rather than whichever one the library resolves to.

A reader and a writer written by the same person can be wrong in the same way and
agree perfectly. So read every archive you produce back with a library that has
never seen this specification. The Kotlin implementation reads its own archives with
`java.util.zip` for that reason: it is cheap, and it is the only thing standing
between an implementation and a self-consistent private dialect of ZIP. *(Measured by the Kotlin implementation.)*

The corpus already does that job for the JSON layer, which is a good reason to
notice that no tier does it for archive structure.

---

## 7. Where each language falls in

Navigation only. Every fact here is stated above.

**Go.** Negative zero renders as `-0` (1.8). `math.Round` is half away from zero
(1.5). `encoding/json` escapes `<`, `>`, `&`, U+2028 and U+2029, and cannot be
configured to stop escaping the last two (2.3). It accepts `1e400` into
`json.Number` and refuses it only into `float64` (4.1). Key order (1.1).

**Python.** `1e-06` and `1e-07` at the exponent boundaries (1.2). `json` accepts
`NaN` and `Infinity` by default (4.1). `sort_keys` is plain lexicographic (1.1,
2.1).

**Swift.** `JSONSerialization` renders `-3.67` as `-3.6699999999999999` and must
not be used; `JSONEncoder` gives `1e-06` and `1e-07`, and both stock encoders
separate a key from its value with `" : "` (1.1, 1.2). `.rounded()` is not
half-to-even (1.5); `Double.description` is (1.3). `<` on `String` orders by
Unicode canonical equivalence, which sorts the astral pair correctly and U+212B
wrongly (2.2).

**Kotlin.** Shortest-repr ties away from zero (1.3). `BigDecimal.HALF_UP` at the
rounding boundary (1.5). `String.compareTo` orders by UTF-16 `char` on the JVM,
and Kotlin/Native is unmeasured (2.2). `java.util.zip` cannot write what Section 7
needs, not exposing the UTF-8 flag or the version fields, while remaining a sound
independent reader (6.5).

**JavaScript.** `toFixed` rounds ties away from zero for a positive value and
toward zero for a negative one (1.5). `JSON.stringify` uses insertion order for
keys (1.1). `Array.prototype.sort` with no comparator orders by UTF-16 code unit
(2.2). `1e400` parses to infinity rather than failing (4.1). Number rendering is
otherwise free, which is its own hazard (1.10).

**Rust.** `{}` on an `f64` never uses exponent notation and `{:e}` always does,
spelling it `e21` rather than `e+21` (1.2). It resolves a rendering tie away from
zero (1.3). `f64::round` is half away from zero, and `round_ties_even` is the
right rounding mode applied to the wrong operation (1.5, 1.7). `str` ordering is
by UTF-8 byte and needs no comparator (2.2). A lone surrogate cannot be
represented, so the parser refuses one for free (1.10). The idiomatic model of an
optional field, and of a collection, both lose distinctions the format carries
(3.1, 3.2).

---

One last thing, from the implementations that found two invisible defects in a day, both by
comparing against another implementation rather than by reasoning:

> When you have a choice between arguing about a specification clause and
> measuring two implementations, measure. *(Measured by the Kotlin implementation.)*

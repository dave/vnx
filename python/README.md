# vnx — a Python implementation of the VNX format

A codec: it reads and writes `.vnx` and `.vnz` and reports diagnostics (§6.3).
That is what `SPEC.md` calls a VNX library, and it passes the whole conformance
corpus.

Python 3.11+. Standard library only — no dependencies, at all, by design.

```python
import vnx

result = vnx.read_file("trip.vnx")
for diagnostic in result.diagnostics:
    print(diagnostic.path, diagnostic.message)

vnx.encode_file("trip.vnx", result.document)

container = vnx.read_container_file("trip.vnz")
print(container.document_name, container.attachments)
```

**Writing applies the emit-boundary rules of §4.1** — longitude and latitude to
6 decimal places, elevation to 1, and the shortest spelling of every position.
Reading applies none of them: `accept/full-precision-coordinate.vnx` decodes to
`-3.6712345678901235` and encodes to `-3.671235`, which its own sidecar says is
correct.

| Call | What it does |
|---|---|
| `encode_bytes` / `encode_file` | a document, rounded and shortest-spelled |
| `encode_container_bytes` | the same, inside a deflated `.vnz` |

The bytes of a document are not a rule. Key order, number spelling and
whitespace are this library's own business, and two conforming writers are not
expected to agree byte for byte. It writes compact JSON with no whitespace.

A document that cannot be read at all raises `vnx.VNXRefusal`. A document that
could be read with pieces dropped comes back with diagnostics. They are separate
channels on purpose (§6.3), and nothing malformed ever becomes a default.

## Running the corpus

```sh
cd python && python3 -m unittest discover -s tests -t .
```

216 tests, about a second, no toolchain beyond Python. The corpus runner is
`tests/test_corpus.py`; it walks `../corpus/` and runs every case against its
sidecar, and a case file without a sidecar is a failure rather than a skip.

`tests/test_red_proof.py` is the reason to believe the runner. Each test there
breaks one thing — transposes longitude and latitude, rounds by scaling instead
of by formatting, sums a track's moving time across its pause, drops a ruleset,
stops recording diagnostics — runs the corpus check that is supposed to notice,
and asserts it goes red.

Two of them carry the finding the accept tier rests on. A reader that discarded
every data record, every ruleset, every style or every `notes` produces a model
that compares equal to itself, so a tier comparing one model against another
passes all four green. Comparing against the tree parsed from the original file
catches all four.

## What it does not do

- **No GPX, KML or GeoJSON.** Those are conversion tools rather than part of the
  format, Go-only by ruling.
- **No writer-side vocabulary rules.** §5.2's rule for `app.viatrail.routing` —
  that a writer verifies the arc kind against the source before writing — is
  stated in terms of Viatrail's internal model, which this library does not
  have. §5.3's `portable` profile filtering is likewise a writer's decision
  about its own records, not a codec operation: `profile` is read, preserved and
  not acted on.
- **No resource bounds.** §9 recommends bounding what one document may consume.
  This library places no limit on item counts, pip counts or folder depth.
- **No duplicate-key detection.** §6.4: `json` resolves duplicates last-wins
  before any VNX rule runs, like every mainstream parser.
- **Almost nothing inside a style.** A style body is opaque by ruling, so no
  key inside one is ever unknown and every key survives a read-modify-write.
  One *value* is inspected — `importance`, ruled normative at 1 to 5. That is
  not a contradiction: a normative range is a rule about a value, not about a
  key set, exactly as §5.2 gives typed payloads to records whose bodies stay
  opaque. Everything else passes uninspected, including `line-style`,
  `pin-shape`, `label-position`, the enumerations in §5.2, and `label-visible`,
  whose tristate §3.6 makes load-bearing. Those ranges are stated in the
  specification with no rule saying what a reader does with a value outside
  them.

## Decisions this implementation had to make

Each is a place the specification is silent or the corpus answered instead. They
are listed here so that a disagreement with another implementation has somewhere
to start.

Dave ruled on all of these on 2026-09-09; `SPEC.md` is being updated separately
and has not caught up yet. Rows marked **ruled** are the ruling, not a choice.

| Question | What this library does |
|---|---|
| Does a position keep the arity the document spelled? | No. `[lon, lat]` and `[lon, lat, null]` are one model, and the writer emits the shortest spelling of it (§4.1). A comparison of two documents normalises the same way (§8.1). |
| Which keys are "defined by this format"? | `vnx/keys.py`, read off the §3 examples and constrained from below by the accept corpus. §1.3 says an example defines no rule, so no key set in this format is normative today. |
| Are style objects key-checked? | **Ruled: no, a style body is opaque**, like a data record body. This library checked style key names at first, from three invented key sets; the ruling reversed that, because a style vocabulary grows faster than a structural one and key sets would diagnose a valid document every time Viatrail adds a style key. |
| Does a network's own point or area carry `folder`? | **Ruled: no.** §3.2 gives `folder` to "any item"; an item a network already contains does not also sit in a folder. Conditional on Viatrail's exporter not writing one — `vnx/keys.py` says how to flip it. |
| Is `importance` a constraint or a description? | **Ruled: normative**, 1 to 5. `icon-size`'s list of sizes is informative and unchecked. An out-of-range `importance` is diagnosed and the key dropped; the remedy is this library's reading, since §6.2 gives none for a value outside a stated range. |
| Is `hidden` legal on a top-level waypoint? | **Ruled: yes** — §3.6's "share one shape" governs §3.8's host list. |
| Is `notes` legal on a track? | **Ruled: yes.** This library had assumed it on no evidence at all before it was ruled. |
| How are strings escaped? | Whatever `json.dumps` does, with `ensure_ascii` off so non-ASCII stays UTF-8. The bytes of a document are not a rule. |
| Which distance function measures the 50 m tolerance? | A local equirectangular projection, `vnx/geo.py`. Unspecified. |
| What does a ring that does not close do? | Diagnostic, and the area is dropped — the same remedy §3.7 gives a nested ring. |
| What does a link endpoint naming an absent node do? | Diagnostic, link kept. Only an absent *arc* is named by §6.2, and only an arc is a link's whole meaning. |
| Do two links either side of a dropped one have to meet? | No. Continuity is checked between links adjacent in the document, so one defect stays one diagnostic. |
| What is a track's moving time when only some pips carry times? | The first and last timed pip of each segment. §3.5 addresses all-timed and never-timed only. |
| Does an omitted `counts` key mean zero? | No, unchecked: `accept/example-network.expect.json` omits `rulesets` for a document that has one. |

## Layout

```
vnx/jsonio.py      strict parsing: no NaN, no Infinity (SPEC 4.2)
vnx/keys.py        the key vocabulary, and where each set came from
vnx/model.py       the document model (SPEC 3)
vnx/decode.py      reading, with diagnostics and refusals (SPEC 6)
vnx/encode.py      model to JSON tree
vnx/container.py   the VNZ container (SPEC 7, 9)
vnx/writer.py      the writer emit path: rounding, then `json.dumps`
vnx/geo.py         point-to-polyline distance, for SPEC 3.3.3
vnx/pointer.py     RFC 6901
vnx/errors.py      VNXRefusal and Diagnostic
```

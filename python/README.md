# vnx — the VNX format in Python

`vnx` reads and writes VNX documents and VNZ containers in Python, and reports
diagnostics.

Python 3.11 or later, and no dependencies. Implements VNX 0.1.0 —
[`SPEC.md`](../SPEC.md) is the format definition.

## Install

Clone the repository and install the package from its path. The editable
install needs pip 21.3 or later, so a fresh virtual environment is the shortest
route.

```sh
python3 -m venv .venv
.venv/bin/pip install -e ../vnx/python
```

`import vnx` then resolves from any working directory.

## Usage

Read a file, list what is in it, report every fault, and write it back.

```python
import vnx

reading = vnx.read_file("corpus/accept/example-network.vnx")
doc = reading.document

for route in doc.routes or []:
    print("route", route.name, len(route.arcs or []), "arcs")
for network in doc.networks or []:
    for chain in network.chains or []:
        print("chain", chain.name, len(chain.links or []), "links")
for track in doc.tracks or []:
    print("track", track.name, "moving", track.moving_time_ms())
for d in reading.diagnostics:
    print(d.code, d.path, d.message)

vnx.encode_file("out.vnx", doc)
```

```
route Braeriach direct 2 arcs
chain Chalamain Gap 2 links
chain Rock fall 1 links
track Lairig Ghru, 12 May moving 167000.0
```

A `.vnz` opens with `vnx.read_container_file`. It carries `document` and
`diagnostics` beside the container's own fields, and each attachment arrives as
a path and its bytes.

```python
import vnx

container = vnx.read_container_file("corpus/accept/container-root-doc.vnz")
print("document entry", container.document_name)
for attachment in container.attachments:
    print("attachment", attachment.path, len(attachment.bytes), "bytes")

try:
    vnx.read_bytes(b'{"vnx": "0.0.9"}')
except vnx.VNXRefusal as refusal:
    print("refused:", refusal)
```

```
document entry doc.vnx
attachment attachments/photo.txt 19 bytes
refused: this document is version 0.0.9 and this reader does not support the 0.0 series: it reads 0.1.x
```

## API

| Call | Takes | Returns | Raises |
|---|---|---|---|
| `vnx.read_file` | a path | `ReadResult` | `VNXRefusal` |
| `vnx.read_bytes` | `bytes` | `ReadResult` | `VNXRefusal` |
| `vnx.decode_document` | a parsed JSON value | `ReadResult` | `VNXRefusal` |
| `vnx.read_container_file` | a path | `ContainerReadResult` | `VNXRefusal` |
| `vnx.read_container_bytes` | `bytes` | `ContainerReadResult` | `VNXRefusal` |
| `vnx.encode_file` | a path, a `Document` | `None` | — |
| `vnx.encode_bytes` | a `Document` | `bytes` | — |
| `vnx.encode_document` | a `Document` | a JSON value | — |
| `vnx.encode_container_bytes` | a `Document`, `dict[str, bytes]` of attachments | `bytes` | `VNXRefusal` |
| `vnx.parse_json` | `bytes` | a JSON value | `VNXRefusal` |

| Type | Fields |
|---|---|
| `ReadResult` | `document: Document`, `diagnostics: list[Diagnostic]` |
| `ContainerReadResult` | the same two, plus `document_name: str` and `attachments: list[Attachment]` |
| `Attachment` | `path: str`, `bytes: bytes` — frozen |
| `Diagnostic` | `path: str`, `message: str`, `code: str | None` |
| `Document` | `networks`, `routes`, `tracks`, `waypoints`, `areas`, `folders`, `rulesets`, `vnx`, `profile`, `generator`, `created`, and `counts()` |
| `Network` | `nodes: dict[str, Position]`, `arcs: dict[str, Arc]`, `chains`, `points`, `areas` |
| `Arc`, `RouteArc` | `pips: list[Position]`; an `Arc` also has `from_` and `to` |
| `Chain` | `links: list[Link]`, each `Link` carrying `arc`, `from_`, `to` |
| `Route` | `arcs: list[RouteArc]` |
| `Track` | `segments: list[Segment]`, each carrying `pips`, and `moving_time_ms()` |
| `Position` | `lon`, `lat`, `ele`, `time_ms` |

Items also carry `name`, `notes`, `folder`, `hidden`, `style` and `data` where
SPEC §3 gives them. `from_` takes the underscore because `from` is a keyword.

Encoding what you decoded is legitimately not the bytes you read, because a
writer applies the emit-boundary rules of SPEC §4.1.

## Errors and diagnostics

A refusal and a diagnostic are separate channels (SPEC §6.3), and in Python that
is a raised exception against the `diagnostics` list returned beside a document.

- **A refusal is a `vnx.VNXRefusal`.** Every cause of SPEC §6.2 and SPEC §7
  arrives as that exception, a damaged archive included, so a reading call has
  one type to catch.
- **One damaged attachment does not cost you the document.** An attachment the
  reader cannot decompress, or whose method is neither stored nor deflate, is
  dropped with an `attachment-unreadable` diagnostic and the rest of the
  container comes back (SPEC §7). It is never returned with empty bytes. Only a
  damaged root document refuses, because that leaves nothing to return.
- **A diagnostic comes back with the document.** `Diagnostic.code` is the stable
  token to switch on; `message` is English prose, advisory, and never compared
  (SPEC §6.3). `vnx/decode.py` and `vnx/container.py` raise every code.
- **An absent collection is `None`, not `[]`.** `None` means the key was not
  present. It carries the same content as an empty one (SPEC §3.1), so the
  distinction is representation rather than meaning — but write `or []` around a
  collection you iterate, or `None` will raise.
- **`Track.moving_time_ms()` is a call and returns `None` for unknown.** Unknown
  is not zero. `Position.ele` and `time_ms` are `None` for a channel that was
  `null` or left off.
- **A version outside this reader's series is refused, in either direction.**
  Through 0.x each MINOR is its own series (SPEC §2.2), so `0.0.9` is refused
  exactly as `0.2.0` is, and the message names the series. Any PATCH within
  `0.1` reads.
- **Damaged geometry costs its container.** A position inside `pips` that will
  not parse drops the arc or segment holding it, while a link that cannot be
  used is dropped and its chain survives (SPEC §6.2).

## Run the tests

```sh
cd python && python3 -m unittest discover -s tests -t .
```

Unit tests and the corpus runner over all four tiers. `tests/test_red_proof.py`
breaks the reader in turn and requires the corpus check to notice each break.

## Layout

| Module | What it is |
|---|---|
| `jsonio` | strict parsing: no `NaN`, no `Infinity` (SPEC §4.2) |
| `keys` | the key vocabulary |
| `model` | the document model (SPEC §3) |
| `decode` | reading, with diagnostics and refusals (SPEC §6) |
| `encode` | model to JSON tree |
| `writer` | the emit boundary: shortest spelling, `hidden` (SPEC §4.1, §3.8) |
| `container` | the VNZ container (SPEC §7, §9) |
| `geo` | the projection tolerance of SPEC §3.3.3 |
| `pointer` | RFC 6901 |
| `errors` | `VNXRefusal` and `Diagnostic` |

## Notes for porters

General form in [`IMPLEMENTING.md`](../IMPLEMENTING.md); these are the parts
that are shaped by Python.

- `jsonio` wraps `json` rather than calling it, because `json` accepts the
  `NaN` and `Infinity` literals SPEC §4.2 excludes. `json` also resolves a
  repeated key last-wins before any VNX rule runs, so duplicate keys are
  undetectable here (SPEC §6.4).
- Decoding is a hand-written walk over the parsed tree rather than a mapper into
  typed structures, because SPEC §6.2 wants one diagnostic per bad path with the
  rest of the document kept.
- `Position` holds typed fields rather than the channel array, so the arity a
  document spelled is not model state; the writer emits the shortest spelling
  (SPEC §4.1).

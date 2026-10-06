# vnx for Rust

`vnx` reads and writes VNX documents and VNZ containers in Rust, and reports
diagnostics.

Rust 1.77 or later. Two dependencies, resolved by cargo: `serde_json` and
`flate2`. Implements VNX 0.9.0, and reads the 0.9 series only —
[`SPEC.md`](../SPEC.md) is the format definition.

## Install

Clone the repository and depend on the crate by path. In the consuming
`Cargo.toml`:

```toml
[dependencies]
vnx = { path = "../vnx/rust" }
```

## Usage

Read a file, list what is in it, report every fault, and write it back.

```rust
use std::fs;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let bytes = fs::read("corpus/accept/example-network.vnx")?;
    let reading = vnx::decode(&bytes)?;
    let doc = &reading.document;

    for route in doc.routes.as_deref().unwrap_or(&[]) {
        println!("route {:?}: {} passes", route.name, route.walk.len());
    }
    for network in doc.networks.as_deref().unwrap_or(&[]) {
        for highlight in network.highlights.as_deref().unwrap_or(&[]) {
            println!("highlight {:?}: {} passes", highlight.name, highlight.walk.len());
        }
    }
    for d in &reading.diagnostics {
        println!("{} {}: {}", d.code(), d.path, d.message());
    }

    fs::write("out.vnx", vnx::encode(doc)?)?;
    Ok(())
}
```

## Command-line tool

This library has none. The reference command-line tool is Go's; see
[`go/README.md`](../go/README.md#command-line-tool).

## API

| Call | Takes | Returns | Refuses with |
|---|---|---|---|
| `vnx::decode` | `&[u8]` | `Reading` | `Refusal`, one of the four document causes |
| `vnx::decode_json` | `&Json` | `Reading` | `Refusal`, for an already-parsed value |
| `vnx::encode` | `&Document` | `String` | `EncodeError`, for an application key it cannot write (SPEC §3.9), a number that is not finite (SPEC §4.2), or an arc, a walk or a ring its own reader would drop or repair (SPEC §3.3, §3.7) |
| `vnx::encode::to_json` | `&Document` | `Json` | `EncodeError`, as `encode` |
| `vnx::vnz::read` | `&[u8]` | `Container` | `Refusal`, a document or container cause |
| `vnx::vnz::write` | `&[u8]`, `&[Attachment]` | `Vec<u8>` | `Refusal`, for an attachment path a reader would reject |

Container reading lives in the `vnz` module and is not re-exported at the crate
root.

| Type | Fields |
|---|---|
| `Reading` | `document: Document`, `diagnostics: Vec<Diagnostic>` |
| `Container` | `document_entry: String`, `reading: Reading`, `attachments: Vec<Attachment>` |
| `Attachment` | `path: String`, `bytes: Vec<u8>` |
| `Diagnostic` | `path: String`, `cause: Cause`, and `code()` / `message()` |
| `AttachmentFault` | `UnusableMethod(u16)` or `CorruptStream(String)`, carried by `Cause::AttachmentUnreadable` |
| `Document` | `networks`, `routes`, `tracks`, `waypoints`, `areas`, `folders`, `app_keys`, `vnx`, `generator`, `created` |
| `AppKey` | `key: String`, `value: Json`: one application key (SPEC §3.9), `Json::Null` for a `null`; `Document::app_keys` is sorted by key on decode |
| `Network`, `Route` | `nodes: Option<Vec<(String, Vertex)>>`, `arcs: Option<Vec<(String, Arc)>>`, and `node(key)` / `arc(key)`; a network holds `highlights`, a route its own `walk` |
| `Arc` | `from`, `to`, `pips: Vec<Vertex>` (the interior points only), `data` |
| `Highlight` | `walk: Vec<Pass>`, and `name`, `notes`, `hidden`, `style`, `data` |
| `Pass` | `arc`, `from` and `to` (`Option<Endpoint>`), `turn: Option<LonLat>`, `pinned: bool` |
| `Endpoint` | `Node(String)` or `At(LonLat)` |
| `Vertex` | `lon`, `lat`, `ele: Option<f64>`: a node or an arc pip, which carries no time |
| `LonLat` | `lon`, `lat`: a position in a walk |
| `Position` | `channels()`, `lon()`, `lat()`, `ele()`, `time_ms()`, `arity()`: a waypoint's `at`, a track pip, a ring position |
| `Track` | `segments: Vec<Segment>`, and `moving_time_ms() -> Option<f64>` |

Encoding what you decoded is legitimately not the bytes you read, because a
writer applies the emit-boundary rules of SPEC §4.1.

## Errors and diagnostics

A refusal and a diagnostic are separate channels (SPEC §6.3), and in Rust that
is `Err` against the `diagnostics` list returned beside a document.

- **A refusal is an `Err(Refusal)`.** `Refusal` implements
  `std::error::Error`. Each variant is one of SPEC §6.2's document causes or
  SPEC §7's container causes; there is no catch-all variant to match.
- **A diagnostic comes back with the document.** `Diagnostic::code()` is the
  stable token to switch on. `message()` is English prose, advisory, and never
  compared (SPEC §6.3). `src/diag.rs` holds every code.
- **An absent collection is `None`, not an empty `Vec`.** On `Document` each
  collection is `Option<Vec<_>>`, so `None` means the key was absent. Inside an
  item — `Highlight::walk`, `Route::walk`, `Track::segments`, an arc's `pips` — the
  collection is a plain `Vec`.
- **`Track::moving_time_ms()` returns `None` for unknown.** Unknown is not zero.
- **A position keeps the arity the document spelled**, and `Position::ele()` and
  `time_ms()` return `None` for a channel that was absent or `null`. A node and
  an arc pip are a `Vertex` and a walk position is a `LonLat`, so a channel the
  format does not give them cannot be built.
- **A damaged attachment costs only itself.** An entry compressed by a method
  other than stored or deflate, and an entry whose bytes will not decompress,
  are both dropped with an `attachment-unreadable` diagnostic while the
  document is returned. `AttachmentFault` says which. Only a fault in the root
  document refuses the archive, because then there is nothing to return.
  SPEC §7 rules on the method. It does not rule on a corrupt stream, which
  takes the same remedy by the reasoning SPEC §7 gives for the method.
- **Damaged geometry costs its container.** A position inside `pips` that will
  not parse drops the arc or segment holding it, and one in a ring drops the
  area (SPEC §6.2). `position-unusable` names what was dropped, so a caller need
  not derive it from the pointer.
- **A walk is used whole or not at all.** The first pass that breaks a rule
  drops the highlight, or the route, and is the only fault reported for that walk.
  A dropped arc takes every highlight that walks it, each under its own
  `pass-arc-absent`.

## Run the tests

```sh
cd rust && cargo test
```

Unit tests, the corpus runner over all four tiers, and the crate's doc example.
`cargo clippy --all-targets` lints, and `python3 tools/redproof.py` breaks the
reader in turn and requires the corpus runner to notice each break.

## Layout

| Module | What it is |
|---|---|
| `json` | a JSON value, parsed and serialised by `serde_json` |
| `model` | the decoded document (SPEC §3) |
| `decode` | reading, tolerant of damage and silent about nothing (SPEC §6) |
| `encode` | the emit boundary: the shortest spelling of a position (SPEC §4.1) |
| `diag` | diagnostic causes and their codes (SPEC §6.3) |
| `error` | the refusal causes (SPEC §6.2, §7) |
| `walk` | the rules of a walk (SPEC §3.3.3, §3.4), one copy for the reader and the writer |
| `zip` | a ZIP reader and writer, with `flate2` under both |
| `vnz` | the container (SPEC §7) |

## Notes for porters

General form in [`IMPLEMENTING.md`](../IMPLEMENTING.md); these are the parts
that are shaped by Rust.

- `serde_json` is taken with `features = ["float_roundtrip"]`, which is
  load-bearing rather than a tuning knob: without it a full-precision
  coordinate parses one bit low and the corpus fails.
- Decoding is a hand-written walk over `serde_json::Value`, not a derive,
  because SPEC §6.2 wants one diagnostic per bad path with the rest of the
  document kept, and a derive stops at the first field it cannot read.
- `Position` holds `Vec<Option<f64>>` rather than a struct of `Option<f64>`
  fields, so the arity a document spelled survives into the model. Typed fields
  also conform, because SPEC §4.1 asks a writer for the shortest spelling rather
  than for the arity it read; `python/` takes that route.
- The ZIP reader is written out rather than taken from a crate, because three of
  SPEC §7's reader rules are ones a general-purpose ZIP library does not offer.
- `#![forbid(unsafe_code)]`, so the worst outcome of a hostile archive is a
  panic rather than memory corruption.

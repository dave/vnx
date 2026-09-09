//! VNX — the Viatrail Network eXchange interchange format, in Rust.
//!
//! A codec: it reads and writes `.vnx` and `.vnz`, produces canonical form
//! (SPEC 4.5) and reports diagnostics (SPEC 6.3). It does not convert to or
//! from GPX, KML or GeoJSON — those are lossy exports, not part of the format.
//!
//! Written from `SPEC.md` alone and validated against `corpus/`, with no
//! reference to any other implementation.
//!
//! # Zero dependencies
//!
//! The JSON parser, the JSON renderer, the ECMAScript number formatter, the
//! ZIP reader and the DEFLATE decompressor are all in this crate. No stock
//! JSON encoder in any target language can emit VNX canonical form
//! (SPEC 4.5.6), so a serialiser had to be written in any case.
//!
//! # The three operations that are not one operation
//!
//! ```text
//! decode    bytes  -> a document, plus every diagnostic  (SPEC 6)
//! re_render model  -> the bytes the document carried     (SPEC 8.1)
//! write     model  -> bytes, rounded and shortened       (SPEC 4.5.4, 4.1)
//! ```
//!
//! `re_render` and `write` are different operations and the difference is not
//! cosmetic: a writer rounds coordinates and emits the shortest spelling of a
//! position, so writing what you read is legitimately not what you read. The
//! conformance corpus's accept tier requires `re_render`.
//!
//! # No fallbacks
//!
//! A malformed value never becomes a default. It is dropped, and a diagnostic
//! names what it was and where it was. The only exception is where the format
//! itself defines a meaning for an absence: an absent `label-visible` is Auto
//! (SPEC 3.6) and an absent `hidden` is visible (SPEC 3.8).
//!
//! # Example
//!
//! ```
//! let bytes = br#"{"waypoints":[{"name":"Spring","at":[-3.6521,57.1,940]}],"vnx":"0.1.0"}"#;
//! let reading = vnx::decode(bytes).expect("a VNX document");
//! assert!(reading.diagnostics.is_empty());
//! assert_eq!(reading.document.waypoints.as_ref().unwrap().len(), 1);
//!
//! // Canonical form: scalars before containers, positions on one line.
//! assert_eq!(vnx::re_render(&reading.document), concat!(
//!     "{\n",
//!     "  \"vnx\": \"0.1.0\",\n",
//!     "  \"waypoints\": [\n",
//!     "    {\n",
//!     "      \"name\": \"Spring\",\n",
//!     "      \"at\": [-3.6521, 57.1, 940]\n",
//!     "    }\n",
//!     "  ]\n",
//!     "}\n",
//! ));
//! ```

#![forbid(unsafe_code)]
#![warn(missing_debug_implementations)]

pub mod canonical;
pub mod decode;
pub mod diag;
pub mod error;
pub mod geo;
pub mod json;
pub mod model;
pub mod render;
pub mod vnz;
pub mod zip;

pub use decode::{decode, decode_json, Reading};
pub use diag::{Cause, Diagnostic};
pub use error::Refusal;
pub use json::Json;
pub use model::Document;
pub use render::{re_render, write, Mode};

/// Canonicalise a document's BYTES: decode, then re-render.
///
/// This is the accept tier's check (SPEC 8.1) and it is deliberately not a
/// byte-to-byte reformat. Reformatting bytes into bytes never builds a model,
/// so it cannot observe anything about what a model kept — a reader that threw
/// away every data record, every ruleset, every style and every `notes` passes
/// such a check.
pub fn canonicalise(bytes: &[u8]) -> Result<(String, Vec<Diagnostic>), Refusal> {
    let reading = decode(bytes)?;
    Ok((re_render(&reading.document), reading.diagnostics))
}

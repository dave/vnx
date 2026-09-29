//! VNX — the Viatrail Network eXchange interchange format, in Rust.
//!
//! A codec: it reads and writes `.vnx` and `.vnz`, and reports diagnostics
//! (SPEC 6.3). It does not convert to or from GPX, KML or GeoJSON — those are
//! lossy exports, not part of the format.
//!
//! Written from `SPEC.md` alone and validated against `corpus/`.
//!
//! # Two operations
//!
//! ```text
//! decode  bytes -> a document, plus every diagnostic  (SPEC 6)
//! encode  model -> bytes, shortest spelling            (SPEC 4.1)
//! ```
//!
//! ⚠ ENCODING WHAT YOU DECODED IS LEGITIMATELY NOT THE SAME BYTES, because a
//! writer emits the shortest spelling of a position — `[lon, lat, null]` goes
//! out as `[lon, lat]`. No VALUE changes: this crate does not round, on either
//! path. The bytes of a document are not constrained (SPEC 8.1), so the
//! conformance corpus compares JSON TREES, never bytes and never one model
//! against another.
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
//! let bytes = br#"{"waypoints":[{"name":"Spring","at":[-3.6521,57.1,940]}],"vnx":"0.2.0"}"#;
//! let reading = vnx::decode(bytes).expect("a VNX document");
//! assert!(reading.diagnostics.is_empty());
//! assert_eq!(reading.document.waypoints.as_ref().unwrap().len(), 1);
//!
//! let out = vnx::encode(&reading.document).expect("encodable");
//! assert!(out.contains(r#""at":[-3.6521,57.1,940]"#));
//! ```

#![forbid(unsafe_code)]
#![warn(missing_debug_implementations)]

pub mod decode;
pub mod diag;
pub mod encode;
pub mod error;
pub mod json;
pub mod model;
pub mod vnz;
pub mod walk;
pub mod zip;

pub use decode::{decode, decode_json, Reading};
pub use diag::{AttachmentFault, Cause, Diagnostic};
pub use encode::encode;
pub use error::{EncodeError, Refusal};
pub use json::Json;
pub use model::Document;

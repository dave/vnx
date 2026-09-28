//! Read a VNX document with the Rust implementation and write it back out.
//!
//! One of six drivers, one per language; `scripts/interop.py` runs each over
//! each other's output. No fallback: a refusal, a diagnostic, or an unwritable
//! path exits non-zero with the cause on stderr.

use std::process::ExitCode;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 3 {
        eprintln!("usage: interop <in.vnx> <out.vnx>");
        return ExitCode::from(2);
    }
    let bytes = match std::fs::read(&args[1]) {
        Ok(b) => b,
        Err(e) => {
            eprintln!("interop: read: {e}");
            return ExitCode::from(1);
        }
    };
    let reading = match vnx::decode(&bytes) {
        Ok(r) => r,
        Err(e) => {
            eprintln!("interop: refused: {e}");
            return ExitCode::from(1);
        }
    };
    for d in &reading.diagnostics {
        eprintln!(
            "interop: diagnostic {} at {}: {}",
            d.code(),
            d.path,
            d.message()
        );
    }
    if !reading.diagnostics.is_empty() {
        return ExitCode::from(1);
    }
    let text = match vnx::encode(&reading.document) {
        Ok(t) => t,
        Err(e) => {
            eprintln!("interop: encode: {e}");
            return ExitCode::from(1);
        }
    };
    if let Err(e) = std::fs::write(&args[2], text) {
        eprintln!("interop: write: {e}");
        return ExitCode::from(1);
    }
    ExitCode::SUCCESS
}

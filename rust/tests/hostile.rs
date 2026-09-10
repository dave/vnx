//! Hostile input: every path must REFUSE, never panic.
//!
//! `#![forbid(unsafe_code)]` guarantees a panic rather than memory corruption,
//! which is the good version of getting this wrong. It is still not a refusal.
//! A library that panics on a malformed archive cannot be called from a server
//! or a mobile app without a catch around every call, and SPEC 9 asks a reader
//! that accepts documents from an untrusted source to bound what one may
//! consume.
//!
//! No corpus tier reaches any of this: every fixture is well-formed, including
//! the ones that must be refused, which are refused for FORMAT reasons rather
//! than for being damaged. So these are the crate's own, and they are
//! systematic rather than illustrative — a hand-picked corrupt file tests the
//! corruption someone imagined.

use vnx::{decode, vnz, zip};

/// A small, valid container, built by this crate's own writer.
fn container() -> Vec<u8> {
    vnz::write(
        b"{\n  \"vnx\": \"0.1.0\",\n  \"waypoints\": [\n    {\n      \"at\": [-3.675, 57.1201]\n    }\n  ]\n}\n",
        &[vnz::Attachment {
            path: "attachments/photo.txt".into(),
            bytes: b"not really a photo\n".to_vec(),
        }],
    )
    .expect("written")
}

/// A real deflated archive, taken from the corpus, so the inflate path is
/// exercised rather than only the stored path this crate writes.
fn deflated_container() -> Vec<u8> {
    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("the crate sits inside the repository")
        .join("corpus")
        .join("accept")
        .join("container-root-doc.vnz");
    std::fs::read(path).expect("the corpus archive reads")
}

/// Every prefix of a valid archive must be refused, not panic.
///
/// This is the shape a truncated download has, and it walks the reader through
/// every partial header: a central directory that stops mid-entry, an entry
/// name cut in half, a deflate stream that ends mid-symbol.
#[test]
fn every_truncation_of_a_container_is_refused() {
    for source in [container(), deflated_container()] {
        for length in 0..source.len() {
            let prefix = &source[..length];
            let outcome = std::panic::catch_unwind(|| vnz::read(prefix).is_ok());
            match outcome {
                Ok(true) => panic!("a {length}-byte prefix was READ as a whole container"),
                Ok(false) => {}
                Err(_) => panic!("a {length}-byte prefix PANICKED instead of being refused"),
            }
        }
    }
}

/// Every single-byte corruption of a valid archive must be refused or read
/// cleanly — never panic.
///
/// Reading cleanly is a legitimate outcome: a byte inside a comment, a
/// timestamp, or an unused header field changes nothing this reader looks at.
/// What is not legitimate is a crash.
#[test]
fn no_single_byte_corruption_of_a_container_panics() {
    for source in [container(), deflated_container()] {
        for index in 0..source.len() {
            // Two flips per byte: one small, one that lands in the high bits,
            // because a length field's low byte and its high byte fail
            // differently.
            for delta in [0x01u8, 0x80u8] {
                let mut damaged = source.clone();
                damaged[index] ^= delta;
                let outcome = std::panic::catch_unwind(|| {
                    let _ = vnz::read(&damaged);
                });
                assert!(
                    outcome.is_ok(),
                    "flipping bit {delta:#04x} of byte {index} PANICKED instead of \
                     being refused"
                );
            }
        }
    }
}

/// The deflate decompressor on its own, against streams that are not deflate
/// at all and against every truncation of one that is.
#[test]
fn inflate_refuses_rubbish_and_truncation_without_panicking() {
    // A stored block carrying "hello", hand-assembled, then every prefix.
    let mut stream = vec![0x01, 0x05, 0x00, 0xfa, 0xff];
    stream.extend_from_slice(b"hello");
    for length in 0..stream.len() {
        let outcome =
            std::panic::catch_unwind(|| zip::inflate::inflate(&stream[..length], 5).is_ok());
        assert_eq!(
            outcome.map_err(|_| "panicked"),
            Ok(false),
            "a {length}-byte prefix of a stored block"
        );
    }

    // Pseudo-random bytes, at several declared sizes, including sizes far
    // larger than the input so the allocation hint is exercised.
    let mut state = 0x9e37_79b9_7f4a_7c15_u64;
    for _ in 0..4_000 {
        state ^= state >> 12;
        state ^= state << 25;
        state ^= state >> 27;
        let len = (state % 48) as usize + 1;
        let noise: Vec<u8> = (0..len)
            .map(|i| (state.wrapping_mul(i as u64 + 1) >> 24) as u8)
            .collect();
        for declared in [0usize, 5, 1024, usize::MAX / 2] {
            let outcome = std::panic::catch_unwind(|| {
                let _ = zip::inflate::inflate(&noise, declared);
            });
            assert!(
                outcome.is_ok(),
                "inflate PANICKED on {len} bytes of noise declared as {declared}"
            );
        }
    }
}

/// A declared uncompressed size must not be trusted as an allocation.
///
/// SPEC 9: "A reader that accepts documents from an untrusted source SHOULD
/// bound the resources one document may consume." A four-byte archive claiming
/// four gigabytes must fail rather than try.
#[test]
fn a_declared_size_is_not_an_allocation() {
    let outcome = std::panic::catch_unwind(|| zip::inflate::inflate(&[0x01, 0, 0], usize::MAX));
    assert!(outcome.is_ok(), "a huge declared size PANICKED");
    assert!(
        zip::inflate::inflate(&[0x01, 0x05, 0x00, 0xfa, 0xff, b'h'], 4 << 30).is_err(),
        "a six-byte stream declaring four gigabytes was accepted"
    );
}

/// The declared size must bound the output WHILE decompressing, not only be
/// compared with it afterwards.
///
/// Both orders return an error, so the result cannot tell them apart and no
/// assertion about the returned document ever will. What separates them is
/// how much a hostile stream is allowed to produce first, which is SPEC 9's
/// concern rather than a correctness one — so this pins WHICH error comes back.
/// A stream that over-produces must be stopped by the running bound and say so,
/// not run to completion and be measured at the end.
///
/// Pinning a message is normally a bad guard. Here it is the only observable
/// difference between a bounded and an unbounded decompressor, and the
/// alternative is measuring memory, which is fragile in a test suite.
#[test]
fn the_declared_size_bounds_the_output_while_it_is_produced() {
    // A stored block carrying five bytes, declared as three.
    let mut stream = vec![0x01, 0x05, 0x00, 0xfa, 0xff];
    stream.extend_from_slice(b"hello");
    let error = zip::inflate::inflate(&stream, 3).expect_err("must be refused");
    let text = error.to_string();
    assert!(
        text.contains("more bytes than the archive declared"),
        "the bound has moved to the end of the stream, so a hostile stream is \
         decompressed in full before being rejected: {text}"
    );
}

/// The JSON side of the same question. A document is bytes from somewhere, and
/// every truncation and corruption of a valid one must be refused or read —
/// never panic.
#[test]
fn no_truncation_or_corruption_of_a_document_panics() {
    let source: &[u8] = br#"{"vnx":"0.1.0","profile":"full","networks":[{"nodes":{"n1":[-3.68,57.12]},
        "arcs":{"a":{"from":"n1","to":"n1","pips":[[-3.68,57.12],[-3.66,57.1,340,1778914991000]]}},
        "chains":[{"links":[{"arc":"a","from":"n1","to":[-3.67,57.11]}]}]}],
        "tracks":[{"segments":[{"pips":[[-3.6,57.1,1,2]]}]}],
        "areas":[{"polygon":[[-3.7,57.09],[-3.68,57.09],[-3.68,57.0],[-3.7,57.09]]}],
        "waypoints":[{"at":[-3.6,57.1],"style":{"importance":3},"data":[{"@type":"x.y","v":[1,2,3]}]}]}"#;
    assert!(decode(source).is_ok(), "the sample must itself be valid");

    for length in 0..source.len() {
        let outcome = std::panic::catch_unwind(|| {
            let _ = decode(&source[..length]);
        });
        assert!(outcome.is_ok(), "a {length}-byte prefix PANICKED");
    }
    for index in 0..source.len() {
        for delta in [0x01u8, 0x20u8, 0x80u8] {
            let mut damaged = source.to_vec();
            damaged[index] ^= delta;
            let outcome = std::panic::catch_unwind(|| {
                let _ = decode(&damaged);
            });
            assert!(
                outcome.is_ok(),
                "flipping bit {delta:#04x} of byte {index} PANICKED"
            );
        }
    }
}

/// Deep nesting must be refused rather than overflow the stack. The parser is
/// recursive, so this is the one hostile input that a bounds check cannot save
/// — only the depth limit can.
#[test]
fn deep_nesting_is_refused_rather_than_overflowing_the_stack() {
    for depth in [300usize, 10_000, 1_000_000] {
        let document = format!(
            "{{\"vnx\":\"0.1.0\",\"rulesets\":[{}{}]}}",
            "[".repeat(depth),
            "]".repeat(depth)
        );
        let outcome = std::panic::catch_unwind(|| decode(document.as_bytes()).is_ok());
        assert_eq!(
            outcome.map_err(|_| "panicked or overflowed the stack"),
            Ok(false),
            "nesting {depth} deep"
        );
    }
}

/// A ZIP64 field can name an offset near `u64::MAX`, and reaching for four
/// bytes there overflows a `usize` before the bounds check ever runs.
///
/// This is the one hostile input the truncation and bit-flip sweeps above
/// cannot reach: no committed archive carries a ZIP64 extra field, so no
/// single-byte flip of one can produce a 64-bit offset. It has to be built.
///
/// In a release build the addition wraps and the slice comes back `None`, so
/// the reader refuses and nobody notices. In a debug build it panics. "Correct
/// in release" is not an answer for a library.
#[test]
fn a_zip64_offset_near_the_top_of_the_range_is_refused() {
    let mut out: Vec<u8> = Vec::new();
    let name = b"doc.vnx";

    // A local header, so the archive is not obviously empty.
    out.extend_from_slice(&0x0403_4b50u32.to_le_bytes());
    out.extend_from_slice(&[0u8; 22]);
    out.extend_from_slice(&(name.len() as u16).to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes());
    out.extend_from_slice(name);

    let cd_offset = out.len() as u32;
    let mut cd: Vec<u8> = Vec::new();
    // The ZIP64 extended information field, carrying an offset two below the
    // top of the range.
    let mut extra: Vec<u8> = Vec::new();
    extra.extend_from_slice(&0x0001u16.to_le_bytes());
    extra.extend_from_slice(&8u16.to_le_bytes());
    extra.extend_from_slice(&(u64::MAX - 2).to_le_bytes());

    // [APPNOTE] 4.3.12, offsets spelled out because getting one wrong makes
    // this case fail EARLY and pass for the wrong reason — which it did on the
    // first attempt, with the CRC field omitted.
    cd.extend_from_slice(&0x0201_4b50u32.to_le_bytes()); //  0: signature
    cd.extend_from_slice(&[0u8; 16]); //  4: version, flags, method, time, date, crc
    cd.extend_from_slice(&4u32.to_le_bytes()); // 20: compressed size
    cd.extend_from_slice(&4u32.to_le_bytes()); // 24: uncompressed size
    cd.extend_from_slice(&(name.len() as u16).to_le_bytes()); // 28
    cd.extend_from_slice(&(extra.len() as u16).to_le_bytes()); // 30
    cd.extend_from_slice(&[0u8; 10]); // 32: comment, disk, attributes
    cd.extend_from_slice(&0xffff_ffffu32.to_le_bytes()); // 42: offset -> ZIP64
    cd.extend_from_slice(name);
    cd.extend_from_slice(&extra);

    let cd_size = cd.len() as u32;
    out.extend_from_slice(&cd);
    out.extend_from_slice(&0x0605_4b50u32.to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes());
    out.extend_from_slice(&1u16.to_le_bytes());
    out.extend_from_slice(&1u16.to_le_bytes());
    out.extend_from_slice(&cd_size.to_le_bytes());
    out.extend_from_slice(&cd_offset.to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes());

    // The case must REACH the offset before it can test it. Without this the
    // test passes for the wrong reason: a misbuilt directory is refused during
    // the walk and the offset is never used.
    let archive = zip::Archive::open(&out).expect("the crafted directory parses");
    assert_eq!(archive.entries().len(), 1);
    assert_eq!(archive.entries()[0].name, "doc.vnx");

    let outcome = std::panic::catch_unwind(|| vnz::read(&out).is_ok());
    assert_eq!(
        outcome.map_err(|_| "panicked on an offset near usize::MAX"),
        Ok(false)
    );
}

//! A hand-built ZIP64 container, because the corpus has none.
//!
//! SPEC 7 says a reader MUST read an archive that uses the ZIP64 extensions,
//! and all four `.vnz` fixtures are plain archives. So the whole ZIP64 path is
//! a MUST with no case behind it: a reader can omit the extra-field parser
//! entirely and pass the corpus.
//!
//! The archive here is assembled byte by byte rather than by a library, and it
//! masks the two sizes AND the local-header offset in the CENTRAL DIRECTORY,
//! with a saturated entry count and central-directory offset in the end record
//! so the ZIP64 locator has to be followed. Python's `zipfile` with
//! `force_zip64` masks the LOCAL header only, and a fixture built that way
//! leaves a reader green with its central-directory extra-field parser
//! disabled — which is the mistake this file exists not to make.
//!
//! `tools/redproof.py` confirms the test goes red with that parser disabled.

use vnx::vnz;

const DOCUMENT: &[u8] = b"{\n  \"vnx\": \"0.10.0\",\n  \"points\": [\n    {\n      \"name\": \"In a ZIP64 container\",\n      \"at\": [-3.675, 57.1201]\n    }\n  ]\n}\n";

/// Bit 11: the entry name is UTF-8 (SPEC 7).
const UTF8_FLAG: u16 = 1 << 11;
/// The value a 32-bit field takes when its real value lives in the ZIP64 extra
/// field ([APPNOTE] 4.5.3).
const MASK32: u32 = 0xffff_ffff;
const MASK16: u16 = 0xffff;

/// CRC-32, recomputed here rather than reached for inside the crate, so this
/// fixture does not depend on the code it is testing to be correct.
fn crc32(bytes: &[u8]) -> u32 {
    let mut crc = 0xffff_ffffu32;
    for &byte in bytes {
        crc ^= byte as u32;
        for _ in 0..8 {
            let mask = (crc & 1).wrapping_neg();
            crc = (crc >> 1) ^ (0xedb8_8320 & mask);
        }
    }
    !crc
}

/// Build a one-entry ZIP64 archive whose real sizes and local-header offset
/// are reachable only through the central directory's ZIP64 extra field.
fn zip64_archive(name: &str, content: &[u8]) -> Vec<u8> {
    let name = name.as_bytes();
    let crc = crc32(content);
    let size = content.len() as u64;

    let mut out: Vec<u8> = Vec::new();

    // Padding, so the local header does not sit at offset 0. A masked offset
    // that happens to be right is not a test of reading the real one.
    out.extend_from_slice(b"VNX ZIP64 fixture padding\n");
    let local_offset = out.len() as u64;

    // ---- local file header ----
    // Its own size fields are honest: this reader takes sizes from the central
    // directory and reads only the name and extra lengths here.
    out.extend_from_slice(&0x0403_4b50u32.to_le_bytes());
    out.extend_from_slice(&45u16.to_le_bytes()); // version needed: 4.5, ZIP64
    out.extend_from_slice(&UTF8_FLAG.to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes()); // method: stored
    out.extend_from_slice(&0u16.to_le_bytes()); // time
    out.extend_from_slice(&0u16.to_le_bytes()); // date
    out.extend_from_slice(&crc.to_le_bytes());
    out.extend_from_slice(&(size as u32).to_le_bytes());
    out.extend_from_slice(&(size as u32).to_le_bytes());
    out.extend_from_slice(&(name.len() as u16).to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes()); // extra length
    out.extend_from_slice(name);
    out.extend_from_slice(content);

    // ---- central directory ----
    let cd_offset = out.len() as u64;
    let mut cd: Vec<u8> = Vec::new();

    // [APPNOTE] 4.5.3: Original Size, Compressed Size, Relative Header Offset,
    // in that order, each present only because its 32-bit field is saturated.
    let mut extra: Vec<u8> = Vec::new();
    extra.extend_from_slice(&0x0001u16.to_le_bytes());
    extra.extend_from_slice(&24u16.to_le_bytes());
    extra.extend_from_slice(&size.to_le_bytes());
    extra.extend_from_slice(&size.to_le_bytes());
    extra.extend_from_slice(&local_offset.to_le_bytes());

    cd.extend_from_slice(&0x0201_4b50u32.to_le_bytes());
    cd.extend_from_slice(&45u16.to_le_bytes()); // version made by
    cd.extend_from_slice(&45u16.to_le_bytes()); // version needed
    cd.extend_from_slice(&UTF8_FLAG.to_le_bytes());
    cd.extend_from_slice(&0u16.to_le_bytes()); // method: stored
    cd.extend_from_slice(&0u16.to_le_bytes()); // time
    cd.extend_from_slice(&0u16.to_le_bytes()); // date
    cd.extend_from_slice(&crc.to_le_bytes());
    cd.extend_from_slice(&MASK32.to_le_bytes()); // compressed size -> ZIP64
    cd.extend_from_slice(&MASK32.to_le_bytes()); // uncompressed size -> ZIP64
    cd.extend_from_slice(&(name.len() as u16).to_le_bytes());
    cd.extend_from_slice(&(extra.len() as u16).to_le_bytes());
    cd.extend_from_slice(&0u16.to_le_bytes()); // comment length
    cd.extend_from_slice(&0u16.to_le_bytes()); // disk number
    cd.extend_from_slice(&0u16.to_le_bytes()); // internal attributes
    cd.extend_from_slice(&0u32.to_le_bytes()); // external attributes
    cd.extend_from_slice(&MASK32.to_le_bytes()); // local offset -> ZIP64
    cd.extend_from_slice(name);
    cd.extend_from_slice(&extra);

    let cd_size = cd.len() as u64;
    out.extend_from_slice(&cd);

    // ---- ZIP64 end of central directory record ([APPNOTE] 4.3.14) ----
    let zip64_eocd_offset = out.len() as u64;
    out.extend_from_slice(&0x0606_4b50u32.to_le_bytes());
    out.extend_from_slice(&44u64.to_le_bytes()); // size of the rest of this record
    out.extend_from_slice(&45u16.to_le_bytes()); // version made by
    out.extend_from_slice(&45u16.to_le_bytes()); // version needed
    out.extend_from_slice(&0u32.to_le_bytes()); // this disk
    out.extend_from_slice(&0u32.to_le_bytes()); // disk with the directory
    out.extend_from_slice(&1u64.to_le_bytes()); // entries on this disk
    out.extend_from_slice(&1u64.to_le_bytes()); // total entries
    out.extend_from_slice(&cd_size.to_le_bytes());
    out.extend_from_slice(&cd_offset.to_le_bytes());

    // ---- ZIP64 end of central directory locator ([APPNOTE] 4.3.15) ----
    out.extend_from_slice(&0x0706_4b50u32.to_le_bytes());
    out.extend_from_slice(&0u32.to_le_bytes()); // disk with the ZIP64 end record
    out.extend_from_slice(&zip64_eocd_offset.to_le_bytes());
    out.extend_from_slice(&1u32.to_le_bytes()); // total number of disks

    // ---- end of central directory ----
    // Every field a ZIP64 archive can saturate IS saturated, so a reader that
    // does not follow the locator sees one entry count of 65535 and a
    // directory offset past the end of the file.
    out.extend_from_slice(&0x0605_4b50u32.to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes()); // this disk
    out.extend_from_slice(&0u16.to_le_bytes()); // disk with the directory
    out.extend_from_slice(&MASK16.to_le_bytes()); // entries on this disk
    out.extend_from_slice(&MASK16.to_le_bytes()); // total entries
    out.extend_from_slice(&MASK32.to_le_bytes()); // size of the directory
    out.extend_from_slice(&MASK32.to_le_bytes()); // offset of the directory
    out.extend_from_slice(&0u16.to_le_bytes()); // comment length
    out
}

#[test]
fn a_zip64_container_reads() {
    let bytes = zip64_archive("doc.vnx", DOCUMENT);
    let container = vnz::read(&bytes).unwrap_or_else(|e| panic!("refused: {e}"));
    assert_eq!(container.document_entry, "doc.vnx");
    assert_eq!(container.reading.diagnostics, vec![]);
    assert_eq!(
        container
            .reading
            .document
            .points
            .as_ref()
            .expect("a point")
            .len(),
        1
    );
    // Encode and compare TREES, so this proves the document survived rather
    // than merely that the archive opened. Trees, because the bytes of a
    // document are not constrained (SPEC 8.1).
    assert_eq!(
        vnx::json::parse(
            vnx::encode(&container.reading.document)
                .expect("encodable")
                .as_bytes()
        )
        .expect("the encoded document parses"),
        vnx::json::parse(DOCUMENT).expect("the fixture parses")
    );
}

/// The fixture has to be a genuine ZIP64 archive, or reading it proves nothing.
/// These assertions are about the BYTES, so they hold even if the reader is
/// wrong — which is what stops this file drifting into a fixture that only
/// looks like the thing it is named for.
#[test]
fn the_fixture_really_does_need_the_zip64_records() {
    let bytes = zip64_archive("doc.vnx", DOCUMENT);

    // The end record saturates every field it can, so nothing in it is usable
    // without following the locator.
    let eocd = bytes.len() - 22;
    assert_eq!(&bytes[eocd..eocd + 4], &0x0605_4b50u32.to_le_bytes());
    assert_eq!(&bytes[eocd + 8..eocd + 10], &MASK16.to_le_bytes());
    assert_eq!(&bytes[eocd + 10..eocd + 12], &MASK16.to_le_bytes());
    assert_eq!(&bytes[eocd + 12..eocd + 16], &MASK32.to_le_bytes());
    assert_eq!(&bytes[eocd + 16..eocd + 20], &MASK32.to_le_bytes());

    // The locator sits immediately before it.
    let locator = eocd - 20;
    assert_eq!(&bytes[locator..locator + 4], &0x0706_4b50u32.to_le_bytes());

    // And the central directory entry masks all three of its own fields, so a
    // reader that skips the extra field has no sizes and no local offset. This
    // is the half a `force_zip64` fixture does not have.
    let entry = bytes
        .windows(4)
        .position(|w| w == 0x0201_4b50u32.to_le_bytes())
        .expect("a central directory entry");
    assert_eq!(
        &bytes[entry + 20..entry + 24],
        &MASK32.to_le_bytes(),
        "compressed size is not masked"
    );
    assert_eq!(
        &bytes[entry + 24..entry + 28],
        &MASK32.to_le_bytes(),
        "uncompressed size is not masked"
    );
    assert_eq!(
        &bytes[entry + 42..entry + 46],
        &MASK32.to_le_bytes(),
        "local header offset is not masked"
    );
    // The extra BLOCK is 28 bytes: a four-byte header plus a 24-byte body
    // carrying the three u64 values. Checking the block length alone would not
    // say that, so the inner header is checked too.
    let name_len = u16::from_le_bytes([bytes[entry + 28], bytes[entry + 29]]) as usize;
    assert_eq!(
        &bytes[entry + 30..entry + 32],
        &28u16.to_le_bytes(),
        "the extra block is not 28 bytes"
    );
    let extra = entry + 46 + name_len;
    assert_eq!(
        &bytes[extra..extra + 2],
        &0x0001u16.to_le_bytes(),
        "the extra block is not the ZIP64 extended information field"
    );
    assert_eq!(
        &bytes[extra + 2..extra + 4],
        &24u16.to_le_bytes(),
        "the ZIP64 field body is not 24 bytes, so it does not carry all three values"
    );

    // The local header is not at offset 0, so a reader that lost the masked
    // offset cannot accidentally be right.
    let local = bytes
        .windows(4)
        .position(|w| w == 0x0403_4b50u32.to_le_bytes())
        .expect("a local file header");
    assert!(local > 0, "the local header sits at offset 0");
}

/// A saturated field with no locator behind it is a corrupt archive, not an
/// invitation to guess. Red-proof for the branch that follows the locator.
#[test]
fn a_saturated_field_with_no_locator_is_refused() {
    let mut bytes = zip64_archive("doc.vnx", DOCUMENT);
    let locator = bytes.len() - 42;
    assert_eq!(&bytes[locator..locator + 4], &0x0706_4b50u32.to_le_bytes());
    // Break the locator's signature and nothing else.
    bytes[locator + 3] = 0x00;
    assert!(
        vnz::read(&bytes).is_err(),
        "read an archive whose ZIP64 locator is gone"
    );
}

//! A ZIP reader and writer, enough for SPEC 7.
//!
//! ⚠ THE READER IS WRITTEN OUT RATHER THAN TAKEN FROM A CRATE, and SPEC 7 is
//! why. Three of its reader rules are ones a general-purpose ZIP library does
//! not offer: an entry whose declared size exceeds the bytes present MUST be
//! accepted where the CRC agrees, a per-entry unusable-method remedy differs
//! for the root document and for an attachment, and a duplicate name must be
//! SEEN twice rather than resolved. It reads the central directory rather than
//! scanning local headers, so it sees the archive the way the format defines
//! it, and it understands the ZIP64 extensions SPEC 7 requires a reader to
//! accept.
//!
//! DEFLATE itself is `flate2`, both ways.

use flate2::write::DeflateEncoder;
use flate2::Compression;
use std::io::Write as _;

/// One entry, in central-directory order.
#[derive(Debug, Clone)]
pub struct Entry {
    /// The full entry name, as recorded. Not a base name: SPEC 7 notes that
    /// matching the root document on the base name alone matches at any depth.
    pub name: String,
    /// The compression method. 0 is stored and 8 is deflate; SPEC 7 allows no
    /// others, and the remedy for another depends on which entry it is.
    pub method: u16,
    pub(crate) crc32: u32,
    pub(crate) compressed_size: u64,
    pub(crate) uncompressed_size: u64,
    pub(crate) local_header_offset: u64,
    /// Where this entry's payload cannot extend past: the nearest following
    /// local header, or the central directory.
    ///
    /// ⚠ IT IS THE BOUND, AND THE DECLARED SIZE IS A CEILING. SPEC 7 forbids
    /// refusing an entry that declares more bytes than it holds where the CRC
    /// over the bytes present agrees, so the payload is read as the bytes that
    /// are there and the checksum decides whether that was all of it. Reading
    /// the declared count instead runs into the next local header and produces
    /// a CRC failure on a conforming archive.
    pub(crate) payload_bound: u64,
}

impl Entry {
    /// True for a directory entry, which carries no content.
    pub fn is_directory(&self) -> bool {
        self.name.ends_with('/')
    }

    pub fn method_is_readable(&self) -> bool {
        self.method == 0 || self.method == 8
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ZipError(pub String);

impl std::fmt::Display for ZipError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

fn bad<T>(what: impl Into<String>) -> Result<T, ZipError> {
    Err(ZipError(what.into()))
}

#[derive(Debug)]
pub struct Archive<'a> {
    bytes: &'a [u8],
    entries: Vec<Entry>,
}

impl<'a> Archive<'a> {
    pub fn entries(&self) -> &[Entry] {
        &self.entries
    }

    /// Walk the central directory.
    pub fn open(bytes: &'a [u8]) -> Result<Archive<'a>, ZipError> {
        let eocd = find_eocd(bytes)?;
        let mut count = u16(bytes, eocd + 10)? as u64;
        let mut cd_offset = u32(bytes, eocd + 16)? as u64;
        let mut cd_size = u32(bytes, eocd + 12)? as u64;

        // [APPNOTE] 4.3.15/4.3.14: when a field is saturated, the real value
        // lives in the ZIP64 end of central directory record, found through
        // the locator that sits immediately before the EOCD.
        if count == 0xffff || cd_offset == 0xffff_ffff || cd_size == 0xffff_ffff {
            let (c, o, s) = read_zip64_eocd(bytes, eocd)?;
            count = c;
            cd_offset = o;
            cd_size = s;
        }
        if cd_offset.saturating_add(cd_size) > bytes.len() as u64 {
            return bad("the central directory runs past the end of the archive");
        }

        let mut entries = Vec::new();
        let mut at = cd_offset as usize;
        for _ in 0..count {
            if u32(bytes, at)? != 0x0201_4b50 {
                return bad("a central directory entry has no signature");
            }
            let method = u16(bytes, at + 10)?;
            let crc32 = u32(bytes, at + 16)?;
            let mut compressed_size = u32(bytes, at + 20)? as u64;
            let mut uncompressed_size = u32(bytes, at + 24)? as u64;
            let name_len = u16(bytes, at + 28)? as usize;
            let extra_len = u16(bytes, at + 30)? as usize;
            let comment_len = u16(bytes, at + 32)? as usize;
            let mut local_header_offset = u32(bytes, at + 42)? as u64;

            let name_at = at + 46;
            let name_bytes = slice(bytes, name_at, name_len)?;
            // SPEC 7: the UTF-8 filename flag is set. A name that is not UTF-8
            // is an error rather than a guess at a legacy code page — guessing
            // would make one archive yield two different names in two
            // languages, which is the fault SPEC 7's duplicate rule exists to
            // avoid.
            let name = std::str::from_utf8(name_bytes)
                .map_err(|_| ZipError("an entry name is not UTF-8".into()))?
                .to_string();

            let extra = slice(bytes, name_at + name_len, extra_len)?;
            read_zip64_extra(
                extra,
                &mut uncompressed_size,
                &mut compressed_size,
                &mut local_header_offset,
            )?;

            entries.push(Entry {
                name,
                method,
                crc32,
                compressed_size,
                uncompressed_size,
                local_header_offset,
                payload_bound: 0,
            });
            at = name_at + name_len + extra_len + comment_len;
        }

        // The bound on each payload takes every entry to know, and every entry
        // needs it, so it is computed here once the directory is complete.
        for i in 0..entries.len() {
            let mine = entries[i].local_header_offset;
            let mut bound = cd_offset;
            for other in &entries {
                if other.local_header_offset > mine && other.local_header_offset < bound {
                    bound = other.local_header_offset;
                }
            }
            entries[i].payload_bound = bound;
        }

        Ok(Archive { bytes, entries })
    }

    /// Decompress one entry.
    pub fn read(&self, entry: &Entry) -> Result<Vec<u8>, ZipError> {
        let at = entry.local_header_offset as usize;
        if u32(self.bytes, at)? != 0x0403_4b50 {
            return bad(format!("`{}` has no local file header", entry.name));
        }
        // The local header's own name and extra lengths are authoritative for
        // where the data starts; they may differ from the central directory's.
        let name_len = u16(self.bytes, at + 26)? as usize;
        let extra_len = u16(self.bytes, at + 28)? as usize;
        let data_at = at + 30 + name_len + extra_len;
        if data_at as u64 > entry.payload_bound {
            return bad(format!("`{}` starts past where its payload must end", entry.name));
        }
        // The declared size is a CEILING. See `Entry::payload_bound`.
        let available = entry.payload_bound - data_at as u64;
        let take = entry.compressed_size.min(available);
        let data = slice(self.bytes, data_at, take as usize)?;

        let out = match entry.method {
            0 => {
                if entry.compressed_size != entry.uncompressed_size {
                    return bad(format!(
                        "`{}` is stored, but its two sizes disagree",
                        entry.name
                    ));
                }
                data.to_vec()
            }
            8 => inflate(data, entry.uncompressed_size as usize)
                .map_err(|e| ZipError(format!("`{}`: {e}", entry.name)))?,
            method => {
                return bad(format!(
                    "`{}` uses compression method {method}",
                    entry.name
                ))
            }
        };

        let crc = crc32(&out);
        if crc != entry.crc32 {
            return bad(format!(
                "`{}` fails its CRC check: the archive says {:08x} and the bytes give {crc:08x}",
                entry.name, entry.crc32
            ));
        }
        Ok(out)
    }
}

// ---------------------------------------------------------------------------

/// Every read of the archive goes through here, and the addition is CHECKED.
///
/// `at` comes from the file: a ZIP64 extended information field can name a
/// local-header offset anywhere in a `u64`, so `at + len` can overflow a
/// `usize` before the bounds check ever runs. In a release build it wraps and
/// `get` returns `None`, so the archive is refused and nobody notices; in a
/// debug build it panics. "Correct in release" is not an answer for a library,
/// and a panic is not a refusal.
///
/// Found 2026-09-10 by `tests/hostile.rs`, which had to build a ZIP64 archive
/// to reach it — no truncation or bit-flip of a committed fixture can, because
/// none of them carries a ZIP64 extra field.
fn slice(bytes: &[u8], at: usize, len: usize) -> Result<&[u8], ZipError> {
    let end = at
        .checked_add(len)
        .ok_or_else(|| ZipError("an offset in the archive is out of range".into()))?;
    bytes
        .get(at..end)
        .ok_or_else(|| ZipError("the archive is truncated".into()))
}

fn u16(bytes: &[u8], at: usize) -> Result<u16, ZipError> {
    let b = slice(bytes, at, 2)?;
    Ok(u16::from_le_bytes([b[0], b[1]]))
}

fn u32(bytes: &[u8], at: usize) -> Result<u32, ZipError> {
    let b = slice(bytes, at, 4)?;
    Ok(u32::from_le_bytes([b[0], b[1], b[2], b[3]]))
}

fn u64le(bytes: &[u8], at: usize) -> Result<u64, ZipError> {
    let b = slice(bytes, at, 8)?;
    Ok(u64::from_le_bytes([
        b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
    ]))
}

/// The end of central directory record is at the end of the archive, behind a
/// comment of up to 65535 bytes, so it is found by scanning backwards.
fn find_eocd(bytes: &[u8]) -> Result<usize, ZipError> {
    const EOCD_LEN: usize = 22;
    if bytes.len() < EOCD_LEN {
        return bad("too short to be a ZIP archive");
    }
    let earliest = bytes.len().saturating_sub(EOCD_LEN + 0xffff);
    for at in (earliest..=bytes.len() - EOCD_LEN).rev() {
        if u32(bytes, at)? == 0x0605_4b50 {
            let comment_len = u16(bytes, at + 20)? as usize;
            if at + EOCD_LEN + comment_len == bytes.len() {
                return Ok(at);
            }
        }
    }
    bad("no end of central directory record")
}

/// [APPNOTE] 4.3.15 and 4.3.14.
fn read_zip64_eocd(bytes: &[u8], eocd: usize) -> Result<(u64, u64, u64), ZipError> {
    if eocd < 20 {
        return bad("a saturated field with no ZIP64 locator before it");
    }
    let locator = eocd - 20;
    if u32(bytes, locator)? != 0x0706_4b50 {
        return bad("a saturated field with no ZIP64 locator before it");
    }
    let record = u64le(bytes, locator + 8)? as usize;
    if u32(bytes, record)? != 0x0606_4b50 {
        return bad("the ZIP64 locator does not point at a ZIP64 end of central directory");
    }
    let count = u64le(bytes, record + 32)?;
    let size = u64le(bytes, record + 40)?;
    let offset = u64le(bytes, record + 48)?;
    Ok((count, offset, size))
}

/// [APPNOTE] 4.5.3: the ZIP64 extended information extra field. Each value is
/// present only when the field it replaces is saturated, and in this order.
fn read_zip64_extra(
    extra: &[u8],
    uncompressed: &mut u64,
    compressed: &mut u64,
    local_offset: &mut u64,
) -> Result<(), ZipError> {
    let mut at = 0usize;
    while at + 4 <= extra.len() {
        let id = u16(extra, at)?;
        let len = u16(extra, at + 2)? as usize;
        let body_at = at + 4;
        if body_at + len > extra.len() {
            return bad("an extra field runs past the end of the block");
        }
        if id == 0x0001 {
            let mut cursor = body_at;
            let mut next = |slot: &mut u64| -> Result<(), ZipError> {
                if cursor + 8 <= body_at + len {
                    *slot = u64le(extra, cursor)?;
                    cursor += 8;
                }
                Ok(())
            };
            if *uncompressed == 0xffff_ffff {
                next(uncompressed)?;
            }
            if *compressed == 0xffff_ffff {
                next(compressed)?;
            }
            if *local_offset == 0xffff_ffff {
                next(local_offset)?;
            }
            return Ok(());
        }
        at = body_at + len;
    }
    Ok(())
}

/// CRC-32 as [APPNOTE] uses it, computed with the bitwise algorithm rather
/// than a table, because a table would be more code than the loop saves.
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

/// Build an archive whose entries are all STORED, with the UTF-8 filename
/// flag set (SPEC 7).
///
/// Decompress a raw DEFLATE stream, with the declared uncompressed size as a
/// CEILING on the output rather than as a promise.
///
/// ⚠ THE BOUND IS APPLIED WHILE THE OUTPUT IS PRODUCED, not after. A declared
/// size is a number in a hostile file, and inflating first and truncating
/// afterwards lets an archive name a small entry and expand to any size it
/// likes. `flate2` cannot see past the ceiling because it is never given more
/// room than that.
pub fn inflate(data: &[u8], declared: usize) -> Result<Vec<u8>, String> {
    use std::io::Read as _;
    // One byte past the ceiling, so a stream that over-produces is STOPPED
    // there. `saturating_add`, because a hostile archive may declare
    // `usize::MAX`.
    let ceiling = (declared as u64).saturating_add(1);
    let mut out = Vec::new();
    flate2::read::DeflateDecoder::new(data)
        .take(ceiling)
        .read_to_end(&mut out)
        .map_err(|e| format!("the deflate stream will not inflate: {e}"))?;
    // ⚠ THE TWO REFUSALS ARE TOLD APART BY THE CEILING, NOT BY A COMPARISON
    // WITH `declared`. Reaching the ceiling is the only way this can hold more
    // than the entry declared, so it is the RUNNING bound that produced this
    // message and no after-the-fact check could. A reader that inflated first
    // and measured afterwards would take the second branch instead, which is
    // what makes the difference visible to a test at all.
    if out.len() as u64 == ceiling {
        return Err("the deflate stream produced more bytes than the archive declared".to_string());
    }
    if out.len() != declared {
        return Err(format!(
            "the deflate stream gives {} bytes and the archive declares {declared}",
            out.len()
        ));
    }
    Ok(out)
}

/// Compress to a raw DEFLATE stream.
fn deflate(data: &[u8]) -> Vec<u8> {
    let mut encoder = DeflateEncoder::new(Vec::new(), Compression::default());
    encoder
        .write_all(data)
        .expect("writing to a Vec cannot fail");
    encoder.finish().expect("writing to a Vec cannot fail")
}

/// DEFLATE for every entry. SPEC 7.1 lets a writer choose stored or deflate
/// per entry, and a container that is larger than the document it holds is not
/// worth being a container.
///
/// The bytes of an archive are not constrained. SPEC 7.1 names five rules and
/// leaves every other field to the writer, so two conforming writers are not
/// expected to agree byte for byte — which is why nothing compares a `.vnz`
/// against a stored fixture any more.
///
/// No ZIP64 records are written. SPEC 7 makes that asymmetry deliberate: a
/// reader MUST accept ZIP64, a writer MAY leave it out, so a small export stays
/// readable by the widest range of tools.
pub(crate) fn write_archive(files: &[crate::vnz::Attachment]) -> Vec<u8> {
    /// Bit 11: the name and comment are UTF-8.
    const UTF8_FLAG: u16 = 1 << 11;
    /// Version 2.0, the minimum APPNOTE states for DEFLATE.
    const DEFLATE_VERSION: u16 = 20;
    /// A fixed timestamp, so container output is byte-deterministic: MS-DOS
    /// 1980-01-01 00:00:00, which is what `makevnz` writes. The DOS date packs
    /// as (year - 1980) << 9 | month << 5 | day.
    const DOS_TIME: u16 = 0;
    const DOS_DATE: u16 = (1 << 5) | 1;

    let mut out: Vec<u8> = Vec::new();
    let mut directory: Vec<u8> = Vec::new();

    for file in files {
        let name = file.path.as_bytes();
        let crc = crc32(&file.bytes);
        let size = file.bytes.len() as u32;
        let compressed = deflate(&file.bytes);
        let compressed_size = compressed.len() as u32;
        let offset = out.len() as u32;

        out.extend_from_slice(&0x0403_4b50u32.to_le_bytes());
        out.extend_from_slice(&DEFLATE_VERSION.to_le_bytes()); // version needed
        out.extend_from_slice(&UTF8_FLAG.to_le_bytes());
        out.extend_from_slice(&8u16.to_le_bytes()); // method: deflate
        out.extend_from_slice(&DOS_TIME.to_le_bytes());
        out.extend_from_slice(&DOS_DATE.to_le_bytes());
        out.extend_from_slice(&crc.to_le_bytes());
        out.extend_from_slice(&compressed_size.to_le_bytes());
        out.extend_from_slice(&size.to_le_bytes());
        out.extend_from_slice(&(name.len() as u16).to_le_bytes());
        out.extend_from_slice(&0u16.to_le_bytes()); // extra length
        out.extend_from_slice(name);
        out.extend_from_slice(&compressed);

        directory.extend_from_slice(&0x0201_4b50u32.to_le_bytes());
        directory.extend_from_slice(&DEFLATE_VERSION.to_le_bytes()); // version made by
        directory.extend_from_slice(&DEFLATE_VERSION.to_le_bytes()); // version needed
        directory.extend_from_slice(&UTF8_FLAG.to_le_bytes());
        directory.extend_from_slice(&8u16.to_le_bytes()); // method: deflate
        directory.extend_from_slice(&DOS_TIME.to_le_bytes());
        directory.extend_from_slice(&DOS_DATE.to_le_bytes());
        directory.extend_from_slice(&crc.to_le_bytes());
        directory.extend_from_slice(&compressed_size.to_le_bytes());
        directory.extend_from_slice(&size.to_le_bytes());
        directory.extend_from_slice(&(name.len() as u16).to_le_bytes());
        directory.extend_from_slice(&0u16.to_le_bytes()); // extra length
        directory.extend_from_slice(&0u16.to_le_bytes()); // comment length
        directory.extend_from_slice(&0u16.to_le_bytes()); // disk number
        directory.extend_from_slice(&0u16.to_le_bytes()); // internal attributes
        directory.extend_from_slice(&0u32.to_le_bytes()); // external attributes
        directory.extend_from_slice(&offset.to_le_bytes());
        directory.extend_from_slice(name);
    }

    let cd_offset = out.len() as u32;
    let cd_size = directory.len() as u32;
    out.extend_from_slice(&directory);
    out.extend_from_slice(&0x0605_4b50u32.to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes()); // this disk
    out.extend_from_slice(&0u16.to_le_bytes()); // disk with the directory
    out.extend_from_slice(&(files.len() as u16).to_le_bytes());
    out.extend_from_slice(&(files.len() as u16).to_le_bytes());
    out.extend_from_slice(&cd_size.to_le_bytes());
    out.extend_from_slice(&cd_offset.to_le_bytes());
    out.extend_from_slice(&0u16.to_le_bytes()); // comment length
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn crc32_matches_the_published_check_value() {
        // The check value APPNOTE and RFC 1952 both quote.
        assert_eq!(crc32(b"123456789"), 0xcbf4_3926);
        assert_eq!(crc32(b""), 0);
    }

    /// Container output is byte-deterministic: the same input gives the same
    /// archive, and every entry carries the fixed 1980-01-01 timestamp. That
    /// is no longer a conformance rule — SPEC 7.1 does not constrain the bytes
    /// — but a writer whose output moved between runs would make every
    /// container diff unreadable.
    #[test]
    fn container_output_is_byte_deterministic() {
        let files = vec![
            crate::vnz::Attachment {
                path: "doc.vnx".into(),
                bytes: b"{}\n".to_vec(),
            },
            crate::vnz::Attachment {
                path: "attachments/photo.txt".into(),
                bytes: b"not really a photo\n".to_vec(),
            },
        ];
        assert_eq!(write_archive(&files), write_archive(&files));

        // 1980-01-01 00:00:00 in the local header of the first entry.
        let bytes = write_archive(&files);
        assert_eq!(&bytes[10..12], &0u16.to_le_bytes(), "time");
        assert_eq!(&bytes[12..14], &0x0021u16.to_le_bytes(), "date");
    }

    #[test]
    fn rubbish_is_refused_rather_than_read() {
        assert!(Archive::open(b"").is_err());
        assert!(Archive::open(b"not a zip file at all, not even close").is_err());
    }
}

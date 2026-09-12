//! The VNZ container (SPEC 7): a ZIP archive carrying a VNX document and its
//! attachments.

use crate::decode::{decode, Reading};
use crate::diag::{escape_token, Cause, Diagnostic};
use crate::error::Refusal;
use crate::zip::{Archive, Entry};

/// A file carried alongside the document.
#[derive(Debug, Clone, PartialEq)]
pub struct Attachment {
    pub path: String,
    pub bytes: Vec<u8>,
}

/// A container that was read.
#[derive(Debug, Clone, PartialEq)]
pub struct Container {
    /// The name of the archive entry the document came from.
    pub document_entry: String,
    pub reading: Reading,
    pub attachments: Vec<Attachment>,
}

/// Read a `.vnz` container.
///
/// SPEC 7's refusal causes are separate from SPEC 6.2's.
///
/// ⚠ TWO ATTACHMENT FAULTS, TWO REMEDIES, AND ONLY ONE OF THEM IS SPECIFIED.
///
/// * An UNUSABLE COMPRESSION METHOD is the case SPEC 7 rules on: an attachment
///   compressed by a method other than stored or deflate is dropped, a
///   diagnostic is recorded and the document is returned, because refusing a
///   whole library over one photograph would be disproportionate. That is the
///   `method_is_readable` branch below.
/// * A CORRUPT STREAM — an attachment whose bytes fail to inflate, or whose
///   CRC disagrees with the bytes present — is REFUSED here. SPEC 7 does not
///   cover it: its drop-with-a-diagnostic rule is written about the method,
///   and corruption appears in Section 7 only in the rule forbidding a refusal
///   "where the CRC over those bytes agrees", which says nothing about a CRC
///   that does not. So this is THIS READER'S CHOICE, not a rule of the format,
///   and another conforming implementation may differ. It is refused because a
///   bad method says an attachment is merely beyond this reader while bad
///   bytes say the archive is not what it claims to be, and returning content
///   the archive's own checksum disowns is worse than returning nothing.
///   Raised for the spec to settle 2026-09-12.
pub fn read(bytes: &[u8]) -> Result<Container, Refusal> {
    let archive = Archive::open(bytes).map_err(|e| Refusal::ContainerMalformed {
        detail: e.to_string(),
    })?;

    check_paths(archive.entries())?;
    check_duplicates(archive.entries())?;

    let document_entry = choose_document(archive.entries())?;
    if !document_entry.method_is_readable() {
        return Err(Refusal::ContainerRootMethod {
            method: document_entry.method,
        });
    }
    let document_bytes = archive
        .read(document_entry)
        .map_err(|e| Refusal::ContainerMalformed {
            detail: e.to_string(),
        })?;
    let mut reading = decode(&document_bytes)?;

    let mut attachments = Vec::new();
    for entry in archive.entries() {
        if entry.name == document_entry.name || entry.is_directory() {
            continue;
        }
        if !entry.method_is_readable() {
            reading.diagnostics.push(Diagnostic::new(
                attachment_pointer(&entry.name),
                Cause::AttachmentUnreadable {
                    method: entry.method,
                },
            ));
            continue;
        }
        match archive.read(entry) {
            Ok(bytes) => attachments.push(Attachment {
                path: entry.name.clone(),
                bytes,
            }),
            Err(e) => {
                return Err(Refusal::ContainerMalformed {
                    detail: e.to_string(),
                })
            }
        }
    }

    Ok(Container {
        document_entry: document_entry.name.clone(),
        reading,
        attachments,
    })
}

/// SPEC 6.3 puts a container diagnostic in the separate `#/entries/` pointer
/// space, because an archive entry is not part of the document and no pointer
/// into the document can locate one.
///
/// ⚠ THE MEMBER NAME IS ONE REFERENCE TOKEN, so a `/` inside it is escaped as
/// `~1` and a `~` as `~0`, `~` first. An attachment name is the one place in
/// this format where a member name routinely holds a slash.
fn attachment_pointer(name: &str) -> String {
    format!("#/entries/{}", escape_token(name))
}

/// SPEC 7. A path must be relative and forward-slash separated; an absolute
/// path, a drive letter, or a `..` SEGMENT is refused. `..` is a segment, not
/// a substring: `a..b/photo.jpg` is a legal name and escapes nothing.
///
/// A backslash anywhere in a name is refused UNCONDITIONALLY, and that check
/// fires BEFORE the `..` segment hunt. Ruled 2026-09-09, deliberately stricter
/// than the ZIP appnote.
///
/// The ordering is the point rather than a detail. SPEC 7 says a `..` SEGMENT
/// is refused and that `..` is a segment rather than a substring, but it never
/// said whether `\` counts as a separator while hunting for that segment — so
/// `a\..\b` has two defensible readings. Refusing the backslash first means
/// that question never has to be answered.
fn check_paths(entries: &[Entry]) -> Result<(), Refusal> {
    for entry in entries {
        let name = &entry.name;
        let refuse = || {
            Err(Refusal::ContainerPathEscapes {
                name: name.clone(),
            })
        };
        if name.contains('\\') {
            return refuse();
        }
        if name.starts_with('/') || has_drive_letter(name) {
            return refuse();
        }
        // Only now, with no backslash left in the name, is a `/` split the
        // whole story.
        if name.split('/').any(|segment| segment == "..") {
            return refuse();
        }
    }
    Ok(())
}

fn has_drive_letter(name: &str) -> bool {
    let bytes = name.as_bytes();
    bytes.len() >= 2 && bytes[0].is_ascii_alphabetic() && bytes[1] == b':'
}

/// SPEC 7: duplicate entry names are refused, compared without regard to case.
/// Language runtimes resolve a repeated name differently, so one archive would
/// otherwise yield different documents in different languages — and that
/// applies to `doc.vnx` beside `DOC.VNX` exactly as it applies to an exact
/// repeat.
fn check_duplicates(entries: &[Entry]) -> Result<(), Refusal> {
    let mut seen: Vec<String> = Vec::with_capacity(entries.len());
    for entry in entries {
        let folded = entry.name.to_lowercase();
        if seen.contains(&folded) {
            return Err(Refusal::ContainerDuplicateEntry {
                name: entry.name.clone(),
            });
        }
        seen.push(folded);
    }
    Ok(())
}

/// SPEC 7: prefer an entry named `doc.vnx` at the archive root. Where there is
/// none, take the FIRST entry with a `.vnx` extension in archive order. Names
/// are compared without regard to case.
///
/// The match is on the full entry name, not on the base name: matching on the
/// base name alone would match at any depth, and letting the last such match
/// win rather than the first is the defect SPEC 7's note calls out.
fn choose_document(entries: &[Entry]) -> Result<&Entry, Refusal> {
    if let Some(entry) = entries
        .iter()
        .find(|e| e.name.eq_ignore_ascii_case("doc.vnx"))
    {
        return Ok(entry);
    }
    entries
        .iter()
        .find(|e| !e.is_directory() && has_vnx_extension(&e.name))
        .ok_or(Refusal::ContainerNoDocument)
}

fn has_vnx_extension(name: &str) -> bool {
    name.len() > 4 && name[name.len() - 4..].eq_ignore_ascii_case(".vnx")
}

// ---------------------------------------------------------------------------
// Writing
// ---------------------------------------------------------------------------

/// Write a `.vnz` container: `doc.vnx` at the archive root, then the
/// attachments.
///
/// Entries are DEFLATED. SPEC 7.1 lets a writer choose stored or deflate per
/// entry, and a container that is larger than the document it holds is not
/// worth being a container.
///
/// # Errors
///
/// An attachment path that SPEC 7 would make a reader refuse is refused here
/// rather than written, so this writer cannot produce an archive its own
/// reader would reject.
pub fn write(document_bytes: &[u8], attachments: &[Attachment]) -> Result<Vec<u8>, Refusal> {
    let mut names = vec![Attachment {
        path: "doc.vnx".to_string(),
        bytes: document_bytes.to_vec(),
    }];
    names.extend_from_slice(attachments);

    let as_entries: Vec<Entry> = names
        .iter()
        .map(|a| Entry {
            name: a.path.clone(),
            method: 0,
            crc32: 0,
            compressed_size: 0,
            uncompressed_size: 0,
            local_header_offset: 0,
            payload_bound: 0,
        })
        .collect();
    check_paths(&as_entries)?;
    check_duplicates(&as_entries)?;

    // The root document first, which SPEC 7.1 requires, then the attachments in
    // a fixed order of this writer's own choosing. 7.1 no longer constrains the
    // order of the entries after the first, so this is not conformance: it is so
    // that two exporters over one library — one iterating a hash map, one a
    // sorted list — emit one archive for one document. Rust compares `String` by
    // UTF-8 byte, which orders by code point.
    names[1..].sort_by(|a, b| a.path.cmp(&b.path));

    Ok(crate::zip::write_archive(&names))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn entry(name: &str) -> Entry {
        Entry {
            name: name.to_string(),
            method: 0,
            crc32: 0,
            compressed_size: 0,
            uncompressed_size: 0,
            local_header_offset: 0,
            payload_bound: 0,
        }
    }

    #[test]
    fn a_dot_dot_segment_is_refused_and_a_dot_dot_substring_is_not() {
        assert!(check_paths(&[entry("../../etc/passwd")]).is_err());
        assert!(check_paths(&[entry("photos/../secret")]).is_err());
        assert!(check_paths(&[entry("/etc/passwd")]).is_err());
        assert!(check_paths(&[entry("C:/windows/system32")]).is_err());
        assert!(check_paths(&[entry("a\\b")]).is_err());
        // The backslash check fires FIRST, so a name where `\` would have to
        // be read as a separator to find a `..` segment is refused without
        // that question arising.
        assert!(check_paths(&[entry("a\\..\\b")]).is_err());
        assert!(check_paths(&[entry("photos\\summit.jpg")]).is_err());
        // `..` is a SEGMENT, not a substring.
        assert!(check_paths(&[entry("a..b/photo.jpg")]).is_ok());
        assert!(check_paths(&[entry("attachments/photo.txt")]).is_ok());
    }

    #[test]
    fn duplicates_are_compared_without_regard_to_case() {
        assert!(check_duplicates(&[entry("doc.vnx"), entry("doc.vnx")]).is_err());
        assert!(check_duplicates(&[entry("doc.vnx"), entry("DOC.VNX")]).is_err());
        assert!(check_duplicates(&[entry("doc.vnx"), entry("other.vnx")]).is_ok());
    }

    #[test]
    fn the_root_document_is_preferred_and_the_fallback_takes_the_first() {
        let entries = [entry("a.vnx"), entry("doc.vnx"), entry("z.vnx")];
        assert_eq!(choose_document(&entries).expect("chosen").name, "doc.vnx");

        let entries = [entry("some-export.vnx"), entry("later.vnx")];
        assert_eq!(
            choose_document(&entries).expect("chosen").name,
            "some-export.vnx"
        );

        // The match is on the FULL name: a nested doc.vnx is not the root one,
        // so the fallback applies and takes the first .vnx in archive order.
        let entries = [entry("first.vnx"), entry("nested/doc.vnx")];
        assert_eq!(choose_document(&entries).expect("chosen").name, "first.vnx");

        assert!(choose_document(&[entry("photo.jpg")]).is_err());
    }

    /// SPEC 7 selects the root by its two rules and every OTHER entry is an
    /// attachment, whatever its name ends in. No extension-sniffing.
    #[test]
    fn a_second_vnx_in_an_archive_is_an_attachment() {
        let document = b"{\n  \"vnx\": \"0.1.0\"\n}\n";
        let bytes = write(
            document,
            &[Attachment {
                path: "an-earlier-export.vnx".into(),
                bytes: b"not read as a document\n".to_vec(),
            }],
        )
        .expect("written");
        let container = read(&bytes).expect("read back");
        assert_eq!(container.document_entry, "doc.vnx");
        assert_eq!(container.attachments.len(), 1);
        assert_eq!(container.attachments[0].path, "an-earlier-export.vnx");
        // Its bytes are not even JSON, and that is not an error.
        assert_eq!(container.reading.diagnostics, vec![]);
    }

    #[test]
    fn a_container_this_crate_writes_is_one_it_reads() {
        let document = b"{\n  \"vnx\": \"0.1.0\"\n}\n";
        let bytes = write(
            document,
            &[Attachment {
                path: "attachments/photo.txt".into(),
                bytes: b"not really a photo\n".to_vec(),
            }],
        )
        .expect("written");
        let container = read(&bytes).expect("read back");
        assert_eq!(container.document_entry, "doc.vnx");
        assert_eq!(container.attachments.len(), 1);
        assert_eq!(container.attachments[0].path, "attachments/photo.txt");
        assert_eq!(container.attachments[0].bytes, b"not really a photo\n");
        assert_eq!(container.reading.diagnostics, vec![]);
    }

    #[test]
    fn this_writer_will_not_produce_an_archive_its_reader_refuses() {
        let refused = write(
            b"{}",
            &[Attachment {
                path: "../escape.txt".into(),
                bytes: vec![],
            }],
        );
        assert!(matches!(
            refused,
            Err(Refusal::ContainerPathEscapes { .. })
        ));
    }
}

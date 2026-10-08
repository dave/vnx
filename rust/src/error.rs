//! Refusals (SPEC 6.2 and SPEC 7).
//!
//! A refusal is not a diagnostic, and SPEC 6.3 requires the two to reach a
//! caller through separate channels. Here that is `Result::Err` against the
//! `diagnostics` list returned beside a document.
//!
//! A plain document is refused for exactly four causes and a reader MUST NOT
//! refuse one for any other. The container causes of SPEC 7 are separate.
//! Every variant below is one of those causes; there is no catch-all.

/// Why a document or a container was refused.
#[derive(Debug, Clone, PartialEq)]
pub enum Refusal {
    // ---- SPEC 6.2, the four plain-document causes ----
    /// The bytes are not JSON.
    ///
    /// This also carries SPEC 4.2's non-finite cause. `NaN` and `Infinity` are
    /// not JSON tokens and a decimal literal too large for a binary64 is out of
    /// range, so the parser refuses all three and no later stage can meet one.
    MalformedJson { detail: String },
    /// The top level is not a JSON object, or carries no `vnx` key.
    NotAVnxDocument { detail: String },
    /// `vnx` is not three digit-runs separated by full stops.
    VersionMalformed { found: String },
    /// `vnx` names a MAJOR.MINOR series this reader does not implement.
    ///
    /// Through 0.x each MINOR is its own series (SPEC 2.2), so this covers
    /// BOTH DIRECTIONS: a lower minor is refused for the same reason as a
    /// higher one. `major` and `minor` are the document's own, carried so the
    /// message can name the series a reader would need.
    VersionUnsupportedSeries {
        found: String,
        major: u64,
        minor: u64,
    },
    // ---- SPEC 7, the container causes ----
    /// The bytes are not a ZIP archive this reader can walk.
    ContainerMalformed { detail: String },
    /// An entry name is absolute, carries a drive letter, or has a `..`
    /// segment. A security requirement, not a tidiness one (SPEC 9).
    ContainerPathEscapes { name: String },
    /// Two entries share a name, compared without regard to case.
    ContainerDuplicateEntry { name: String },
    /// The archive holds no `.vnx` entry, so there is no document to return.
    ContainerNoDocument,
    /// The root document uses a compression method other than stored or
    /// deflate, so it cannot be read. An ATTACHMENT in that state is dropped
    /// with a diagnostic instead.
    ContainerRootMethod { method: u16 },
}

impl std::fmt::Display for Refusal {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Refusal::MalformedJson { detail } => write!(f, "malformed JSON: {detail}"),
            Refusal::NotAVnxDocument { detail } => write!(
                f,
                "not a VNX document: {detail}; a VNX document is a JSON object \
                 carrying a `vnx` version key"
            ),
            Refusal::VersionMalformed { found } => write!(
                f,
                "the version `{found}` is not MAJOR.MINOR.PATCH — three runs of \
                 decimal digits separated by full stops, as in `0.10.0`"
            ),
            Refusal::VersionUnsupportedSeries {
                found,
                major,
                minor,
            } => {
                let (ours_major, ours_minor) = crate::model::SUPPORTED;
                // SPEC 2.2 requires the refusal to NAME THE VERSION. The tail
                // differs by direction only so a user is told what to do: a
                // newer document needs a newer reader, while an older series
                // needs a reader for that series, which is not this one and
                // not a newer one either.
                if (*major, *minor) > (ours_major, ours_minor) {
                    write!(
                        f,
                        "this document declares version {found}, from the \
                         {major}.{minor} series, and this reader implements the \
                         {ours_major}.{ours_minor} series, so a newer reader is needed"
                    )
                } else {
                    write!(
                        f,
                        "this document declares version {found}, from the \
                         {major}.{minor} series, and this reader implements the \
                         {ours_major}.{ours_minor} series, so a reader for the \
                         {major}.{minor} series is needed"
                    )
                }
            }
            Refusal::ContainerMalformed { detail } => {
                write!(f, "not a readable ZIP archive: {detail}")
            }
            Refusal::ContainerPathEscapes { name } => write!(
                f,
                "the entry `{name}` escapes the archive: an entry path must be \
                 relative, forward-slash separated, and free of a `..` segment"
            ),
            Refusal::ContainerDuplicateEntry { name } => write!(
                f,
                "duplicate entry `{name}`: two entries share a name, compared \
                 without regard to case, and which one wins differs between languages"
            ),
            Refusal::ContainerNoDocument => write!(
                f,
                "the archive holds no `.vnx` entry, so there is no document to return"
            ),
            Refusal::ContainerRootMethod { method } => write!(
                f,
                "the root document uses compression method {method}; \
                 a VNX container uses stored or deflate"
            ),
        }
    }
}

impl std::error::Error for Refusal {}

/// Why a document could not be encoded.
///
/// A writer never emits what its own reader would drop or repair. The model's
/// types rule out most of that, such as a time on a node; the rest is checked
/// here, and each variant names where the fault is.
#[derive(Debug, Clone, PartialEq)]
pub enum EncodeError {
    /// SPEC 3.9: an application key's name contains a full stop. One without
    /// would be read back as an unrecognised key, or as a key of the format.
    AppKeyWithoutFullStop { key: String },
    /// The same application key twice. Which of two equal members a reader
    /// keeps is its JSON parser's choice (SPEC 6.4), so one would be lost.
    AppKeyRepeated { key: String },
    /// A number that is not finite, at `path` (a JSON Pointer into the
    /// document). JSON cannot spell one (SPEC 4.2), so writing it would put a
    /// value in the file that no reader accepts.
    NonFiniteNumber { path: String },
    /// SPEC 3.3.2: an arc that is a self-loop, or that names a node its owner
    /// does not have. `path` is the arc's JSON Pointer.
    Arc { path: String, detail: String },
    /// SPEC 3.3.3, 3.4: a walk a reader would drop. `path` and `cause` are the
    /// ones the reader's diagnostic would carry.
    Walk {
        path: String,
        cause: crate::diag::Cause,
    },
    /// SPEC 3.7: a ring that is closed, or that holds fewer than three
    /// distinct positions. The writer does not open a closed ring itself:
    /// that would guess that its caller meant the repeat, which is the
    /// caller's to fix. `path` is the `polygon`'s JSON Pointer.
    Ring { path: String, detail: String },
}

impl std::fmt::Display for EncodeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            EncodeError::AppKeyWithoutFullStop { key } => write!(
                f,
                "application key {key:?} contains no full stop (SPEC 3.9)"
            ),
            EncodeError::AppKeyRepeated { key } => {
                write!(f, "application key {key:?} appears twice")
            }
            EncodeError::NonFiniteNumber { path } => {
                write!(
                    f,
                    "the number at {path:?} is not finite, and JSON cannot spell it (SPEC 4.2)"
                )
            }
            EncodeError::Arc { path, detail } => write!(f, "{path}: {detail} (SPEC 3.3.2)"),
            EncodeError::Walk { path, cause } => write!(
                f,
                "{path}: a reader would drop this walk under `{}` (SPEC 3.3.3): {cause}",
                cause.code()
            ),
            EncodeError::Ring { path, detail } => write!(f, "{path}: {detail} (SPEC 3.7)"),
        }
    }
}

impl std::error::Error for EncodeError {}

#[cfg(test)]
mod tests {
    use super::*;

    /// The corpus matches a refusal as a substring, so these substrings are
    /// load-bearing until reason codes land.
    #[test]
    fn refusals_carry_the_substrings_the_corpus_expects() {
        let cases: Vec<(Refusal, &str)> = vec![
            (
                Refusal::MalformedJson {
                    detail: "`NaN` is not a JSON value".into(),
                },
                "malformed JSON",
            ),
            (
                Refusal::NotAVnxDocument {
                    detail: "no `vnx` key".into(),
                },
                "not a VNX document",
            ),
            (
                Refusal::VersionMalformed {
                    found: "0.1".into(),
                },
                "MAJOR.MINOR.PATCH",
            ),
            (
                Refusal::VersionUnsupportedSeries {
                    found: "9.9.9".into(),
                    major: 9,
                    minor: 9,
                },
                "a newer reader is needed",
            ),
            (
                Refusal::VersionUnsupportedSeries {
                    found: "0.0.9".into(),
                    major: 0,
                    minor: 0,
                },
                "0.0 series",
            ),
            (
                Refusal::ContainerDuplicateEntry {
                    name: "doc.vnx".into(),
                },
                "duplicate entry",
            ),
            (
                Refusal::ContainerPathEscapes {
                    name: "../../etc/passwd".into(),
                },
                "escapes the archive",
            ),
        ];
        for (refusal, expected) in cases {
            let text = refusal.to_string();
            assert!(text.contains(expected), "{text:?} lacks {expected:?}");
        }
    }

    /// SPEC 2.2: a series refusal must name the version, in EITHER direction,
    /// or the user is told only that something is wrong.
    #[test]
    fn a_series_refusal_names_the_version_in_both_directions() {
        let newer = Refusal::VersionUnsupportedSeries {
            found: "9.9.9".into(),
            major: 9,
            minor: 9,
        }
        .to_string();
        assert!(newer.contains("9.9.9"), "{newer}");

        let older = Refusal::VersionUnsupportedSeries {
            found: "0.0.9".into(),
            major: 0,
            minor: 0,
        }
        .to_string();
        assert!(older.contains("0.0.9"), "{older}");
        // The two directions must not collapse into one sentence: telling a
        // user of a 0.0 document that a NEWER reader is needed sends them the
        // wrong way.
        assert!(!older.contains("a newer reader is needed"), "{older}");
    }
}

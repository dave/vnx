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
    /// The bytes are not JSON, including the `NaN` and `Infinity` literals,
    /// which are not JSON at all.
    MalformedJson { detail: String },
    /// The top level is not a JSON object, or carries no `vnx` key.
    NotAVnxDocument { detail: String },
    /// `vnx` is not three digit-runs separated by full stops.
    VersionMalformed { found: String },
    /// `vnx` names a MAJOR or MINOR above the reader's.
    VersionTooNew { found: String },
    /// A number in the document is not finite.
    NotFinite { detail: String },

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
                 decimal digits separated by full stops, as in `0.1.0`"
            ),
            Refusal::VersionTooNew { found } => write!(
                f,
                "this document declares version {found} and this reader implements \
                 {}.{}.x, so a newer reader is needed",
                crate::model::SUPPORTED.0,
                crate::model::SUPPORTED.1
            ),
            Refusal::NotFinite { detail } => {
                write!(f, "a number in the document is not finite: {detail}")
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
                Refusal::VersionTooNew {
                    found: "9.9.9".into(),
                },
                "a newer reader is needed",
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

    /// SPEC 2.2: the refusal for a version above the reader's must name the
    /// version needed, or the user is told only that something is wrong.
    #[test]
    fn a_too_new_refusal_names_the_version() {
        let text = Refusal::VersionTooNew {
            found: "9.9.9".into(),
        }
        .to_string();
        assert!(text.contains("9.9.9"), "{text}");
    }
}

//! Diagnostics (SPEC 6.3).
//!
//! A diagnostic carries a JSON Pointer, a stable code, and a short English
//! description. SPEC 6.3 makes the CODE the thing a conformance case compares
//! and forbids comparing the English, so the code is the wire commitment and
//! the prose is free to be reworded. Both come off one [`Cause`] variant, so a
//! diagnostic cannot carry a code that disagrees with its message.

/// Why a reader could not use something as written.
#[derive(Debug, Clone, PartialEq)]
pub enum Cause {
    /// SPEC 6.2: an unrecognised key on an object this format defines. The key
    /// is skipped and the item is kept.
    UnknownKey,
    /// SPEC 6.2: a value of the wrong JSON type in an OPTIONAL key. The field
    /// is dropped and the item is kept.
    WrongTypeOptional {
        expected: &'static str,
        found: &'static str,
    },
    /// SPEC 6.2: a value of the wrong JSON type in a REQUIRED key. The ITEM is
    /// dropped, because dropping the field would leave a waypoint that is
    /// nowhere, a link that traverses nothing, or an arc with no geometry.
    ///
    /// Separate from [`Cause::WrongTypeOptional`] because the remedies differ
    /// in whether the rest of the item survives, and a consumer told only that
    /// a type was wrong would have to re-derive required-ness from SPEC 3.10's
    /// table to find out which happened. That lookup is what a code exists to
    /// save.
    ///
    /// Not the same fact as [`Cause::MissingRequiredKey`], which takes the same
    /// remedy: absent and present-but-wrong-typed are different documents.
    WrongTypeRequired {
        expected: &'static str,
        found: &'static str,
    },
    /// SPEC 3.10: a REQUIRED key is absent, so the object cannot exist.
    MissingRequiredKey { key: &'static str },
    /// SPEC 4.1: a position that will not parse.
    ///
    /// `dropped` names what goes, because that is the fact a caller needs and
    /// the remedy is not the same everywhere: inside `pips` the ARC or SEGMENT
    /// holding it is dropped, and inside a `polygon` ring the ITEM is.
    ///
    /// SPEC 6.2: "GEOMETRY IS CONTINUOUS AND STRUCTURE IS NOT. A missing vertex
    /// changes the shape of a line and leaves nothing to indicate it — a
    /// shorter, plausible, wrong line."
    PositionUnusable {
        detail: String,
        dropped: &'static str,
    },
    /// SPEC 6.2: a link naming an arc absent from its network.
    LinkArcAbsent { arc: String },
    /// SPEC 6.2: a link endpoint naming a node absent from its network. The
    /// link is KEPT; that end takes no part in the continuity comparison.
    LinkNodeAbsent { node: String },
    /// SPEC 3.3.3: an inline endpoint that does not project onto its arc
    /// within 50 m.
    InlineEndpointBeyondTolerance { metres: f64 },
    /// SPEC 3.3.3: an inline endpoint on an arc with no pips at all, which has
    /// no distance to measure.
    InlineEndpointOnEmptyArc,
    /// SPEC 6.2: consecutive links in a chain that do not meet.
    ChainNotConnected,
    /// SPEC 3.7: a `polygon` holding rings rather than positions.
    PolygonNestedRings,
    /// SPEC 3.7: a ring that does not close, or holds fewer than four
    /// positions.
    PolygonNotARing { detail: String },
    /// SPEC 4.3: a record-level timestamp carrying an offset other than `Z`.
    TimestampNotUtc,
    /// SPEC 6.2: a `folder` naming a folder absent from the document. The key
    /// is treated as absent and the item sits at the top level.
    FolderAbsent { id: String },
    /// SPEC 6.2: an arc naming a node absent from its network. The arc is
    /// kept; its geometry is real.
    ArcNodeAbsent { node: String },
    /// SPEC 3.3.2: a terminal pip and the node it states disagree by more than
    /// SPEC 3.3.3's 50 m. BOTH are kept and nothing is corrected — the pips
    /// are authoritative, so there is nothing to fix, only something to say.
    ///
    /// The same 50 metres as the projection tolerance, deliberately: the same
    /// question about a different pair of objects, and two thresholds for one
    /// question is worse than one slightly loose one.
    TerminalPipOffNode {
        node: String,
        /// `"first"` or `"last"`, which is also which of `from` and `to` the
        /// pointer addresses.
        which: &'static str,
        metres: f64,
    },
    /// SPEC 5: a data record with no `@type`, or one that is not a string.
    RecordTypeMissing,
    /// SPEC 3.6: an `importance` outside the range 1 to 5, or not an integer.
    ImportanceOutOfRange,
    /// SPEC 7: an ATTACHMENT this reader could not produce bytes for. The
    /// attachment is dropped and the document is returned; refusing a whole
    /// library over one photograph would be disproportionate.
    ///
    /// Two faults reach it, and `fault` says which. They share a code because
    /// they share a remedy, and because what a caller does about a missing
    /// attachment does not depend on why it is missing.
    AttachmentUnreadable { fault: AttachmentFault },
}

/// Why an attachment could not be read (SPEC 7).
#[derive(Debug, Clone, PartialEq)]
pub enum AttachmentFault {
    /// A compression method other than stored or deflate. SPEC 7 rules on this
    /// case in its own words, and the method is refused BEFORE any attempt to
    /// decompress, so the bytes are never looked at.
    UnusableMethod(u16),
    /// A usable method whose stream will not decompress, or whose bytes
    /// disagree with the entry's CRC.
    ///
    /// ⚠ SPEC 7 DOES NOT RULE ON THIS CASE. Its drop-with-a-diagnostic rule is
    /// written about the METHOD, and corruption appears in Section 7 only in
    /// the rule forbidding a refusal "where the CRC over those bytes agrees",
    /// which says nothing about a CRC that does not. Dropping was ruled on
    /// SPEC 7, by the reasoning it gives for the method case: the
    /// document is intact, refusing a whole library over one damaged
    /// photograph is disproportionate, and the diagnostic is what stops the
    /// loss being silent. Rust refused until that ruling and was the outlier.
    CorruptStream(String),
}

impl Cause {
    /// The stable token SPEC 6.3 requires on any diagnostic a corpus case
    /// pins, and which is what the case compares. The English is never
    /// compared: a corpus matching prose across implementations would be
    /// pinning translations of a rule rather than the rule.
    ///
    /// Seven of these are pinned by `corpus/diagnose/` today and are wire
    /// commitments. The rest are this crate's own, offered because a caller
    /// switching on a cause is better served by a token than by a string
    /// search, and SPEC 6.3 permits a code on any diagnostic.
    pub fn code(&self) -> &'static str {
        match self {
            // Pinned by the corpus.
            Cause::UnknownKey => "unknown-key",
            Cause::LinkArcAbsent { .. } => "link-arc-absent",
            Cause::ChainNotConnected => "chain-discontinuity",
            Cause::PolygonNestedRings => "polygon-nested-rings",
            Cause::TimestampNotUtc => "timestamp-not-utc",
            Cause::InlineEndpointBeyondTolerance { .. } => "endpoint-off-arc",
            Cause::AttachmentUnreadable { .. } => "attachment-unreadable",
            // Not pinned anywhere; this crate's own tokens.
            Cause::WrongTypeOptional { .. } => "wrong-type-optional",
            Cause::WrongTypeRequired { .. } => "wrong-type-required",
            Cause::MissingRequiredKey { .. } => "missing-required-key",
            Cause::PositionUnusable { .. } => "position-unusable",
            Cause::LinkNodeAbsent { .. } => "link-node-absent",
            Cause::InlineEndpointOnEmptyArc => "endpoint-on-empty-arc",
            Cause::PolygonNotARing { .. } => "polygon-not-a-ring",
            Cause::FolderAbsent { .. } => "folder-absent",
            Cause::ArcNodeAbsent { .. } => "arc-node-absent",
            Cause::TerminalPipOffNode { .. } => "terminal-pip-off-node",
            Cause::RecordTypeMissing => "record-type-missing",
            Cause::ImportanceOutOfRange => "importance-out-of-range",
        }
    }
}

impl std::fmt::Display for Cause {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Cause::UnknownKey => write!(f, "unknown key, skipped"),
            Cause::WrongTypeOptional { expected, found } => write!(
                f,
                "expected {expected} but found {found}; the field is dropped and \
                 the item is kept"
            ),
            Cause::WrongTypeRequired { expected, found } => write!(
                f,
                "expected {expected} but found {found}; the item is dropped, \
                 because it cannot exist without this key"
            ),
            Cause::MissingRequiredKey { key } => {
                write!(f, "the required key `{key}` is absent, so the item is dropped")
            }
            Cause::PositionUnusable { detail, dropped } => {
                write!(f, "not a position: {detail}; {dropped} is dropped")
            }
            Cause::LinkArcAbsent { arc } => {
                write!(f, "the arc `{arc}` is not in this network, so the link is dropped")
            }
            Cause::LinkNodeAbsent { node } => write!(
                f,
                "the node `{node}` is not in this network; the link is kept, and \
                 that end takes no part in the continuity comparison"
            ),
            Cause::InlineEndpointBeyondTolerance { metres } => write!(
                f,
                "the inline endpoint is {metres:.0} m from its arc, beyond the 50 m tolerance, \
                 so the link is dropped"
            ),
            Cause::InlineEndpointOnEmptyArc => write!(
                f,
                "the arc has no pips, so the inline endpoint has no distance to it; \
                 the link is dropped"
            ),
            Cause::ChainNotConnected => write!(
                f,
                "this link does not begin where the one before it ended, \
                 so the chain is not one connected walk"
            ),
            Cause::PolygonNestedRings => write!(
                f,
                "a polygon holds one ring of positions; nested rings are not expressible, \
                 so the item is dropped"
            ),
            Cause::PolygonNotARing { detail } => {
                write!(f, "not a closed ring: {detail}; the item is dropped")
            }
            Cause::TimestampNotUtc => write!(
                f,
                "a record-level timestamp must be UTC with a Z suffix, so the field is dropped"
            ),
            Cause::FolderAbsent { id } => write!(
                f,
                "the folder `{id}` is not in this document, so the item sits at the top level"
            ),
            Cause::ArcNodeAbsent { node } => {
                write!(f, "the node `{node}` is not in this network; the arc is kept")
            }
            Cause::TerminalPipOffNode {
                node,
                which,
                metres,
            } => write!(
                f,
                "node `{node}` is {metres:.0} m from the {which} pip, beyond the 50 m \
                 tolerance; both are kept and the pips are authoritative"
            ),
            Cause::RecordTypeMissing => write!(
                f,
                "a data record needs a string `@type`, so the record is dropped"
            ),
            Cause::ImportanceOutOfRange => write!(
                f,
                "`importance` is an integer from 1 to 5, so the field is dropped"
            ),
            Cause::AttachmentUnreadable { fault } => match fault {
                AttachmentFault::UnusableMethod(method) => write!(
                    f,
                    "the attachment uses compression method {method}, which is neither \
                     stored nor deflate, so it is dropped"
                ),
                AttachmentFault::CorruptStream(detail) => write!(
                    f,
                    "the attachment will not decompress, so it is dropped: {detail}"
                ),
            },
        }
    }
}

/// One thing a reader could not use as written, and where it was.
#[derive(Debug, Clone, PartialEq)]
pub struct Diagnostic {
    /// A JSON Pointer ([RFC 6901]) into the document, in the wire's own key
    /// names.
    pub path: String,
    pub cause: Cause,
}

impl Diagnostic {
    pub fn new(path: impl Into<String>, cause: Cause) -> Self {
        Diagnostic {
            path: path.into(),
            cause,
        }
    }

    /// The short English description SPEC 6.3 requires. Never compared by a
    /// conformance runner — see [`Diagnostic::code`].
    pub fn message(&self) -> String {
        self.cause.to_string()
    }

    /// The stable token a conformance case compares (SPEC 6.3).
    pub fn code(&self) -> &'static str {
        self.cause.code()
    }
}

impl std::fmt::Display for Diagnostic {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.path, self.cause)
    }
}

/// Escape one JSON Pointer reference token ([RFC 6901] section 3). A node key
/// or a folder id may carry `~` or `/`.
pub fn escape_token(token: &str) -> String {
    if !token.contains('~') && !token.contains('/') {
        return token.to_string();
    }
    token.replace('~', "~0").replace('/', "~1")
}

/// A JSON Pointer built one token at a time.
#[derive(Debug, Clone, Default)]
pub struct Path(String);

impl Path {
    pub fn root() -> Self {
        Path(String::new())
    }

    /// Extend with one reference token, escaping it.
    pub fn key(&self, token: &str) -> Self {
        Path(format!("{}/{}", self.0, escape_token(token)))
    }

    /// Extend with an array index.
    pub fn index(&self, index: usize) -> Self {
        Path(format!("{}/{index}", self.0))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl std::fmt::Display for Path {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_pointer_escapes_its_tokens() {
        assert_eq!(escape_token("node-1"), "node-1");
        assert_eq!(escape_token("a/b"), "a~1b");
        assert_eq!(escape_token("a~b"), "a~0b");
        // RFC 6901: `~` is escaped first, so `~1` does not decode to `/`.
        assert_eq!(escape_token("~1"), "~01");
    }

    #[test]
    fn a_path_builds_the_shapes_the_corpus_uses() {
        let p = Path::root()
            .key("networks")
            .index(0)
            .key("arcs")
            .key("arc-1")
            .key("pips")
            .index(0);
        assert_eq!(p.as_str(), "/networks/0/arcs/arc-1/pips/0");
    }

    /// The six codes `corpus/diagnose/` pins. These are wire commitments: a
    /// corpus case compares the code and never the English, so a typo here
    /// fails a tier while a reworded message does not.
    #[test]
    fn the_pinned_codes_are_spelled_as_the_corpus_spells_them() {
        let cases: Vec<(Cause, &str)> = vec![
            (Cause::UnknownKey, "unknown-key"),
            (
                Cause::WrongTypeOptional {
                    expected: "a boolean",
                    found: "a string",
                },
                "wrong-type-optional",
            ),
            (
                Cause::WrongTypeRequired {
                    expected: "a position array",
                    found: "a string",
                },
                "wrong-type-required",
            ),
            (
                Cause::LinkArcAbsent {
                    arc: "ghost".into(),
                },
                "link-arc-absent",
            ),
            (Cause::ChainNotConnected, "chain-discontinuity"),
            (Cause::PolygonNestedRings, "polygon-nested-rings"),
            (Cause::TimestampNotUtc, "timestamp-not-utc"),
            (
                Cause::InlineEndpointBeyondTolerance { metres: 55.0 },
                "endpoint-off-arc",
            ),
        ];
        for (cause, expected) in cases {
            assert_eq!(cause.code(), expected);
        }
    }

    /// Every cause has its own code. Two causes sharing one would make a
    /// corpus case pass on the wrong diagnostic.
    #[test]
    fn no_two_causes_share_a_code() {
        let all = [
            Cause::UnknownKey,
            Cause::WrongTypeOptional { expected: "a", found: "b" },
            Cause::WrongTypeRequired { expected: "a", found: "b" },
            Cause::MissingRequiredKey { key: "at" },
            Cause::PositionUnusable {
                detail: String::new(),
                dropped: "the arc",
            },
            Cause::LinkArcAbsent { arc: String::new() },
            Cause::LinkNodeAbsent { node: String::new() },
            Cause::InlineEndpointBeyondTolerance { metres: 0.0 },
            Cause::InlineEndpointOnEmptyArc,
            Cause::ChainNotConnected,
            Cause::PolygonNestedRings,
            Cause::PolygonNotARing { detail: String::new() },
            Cause::TimestampNotUtc,
            Cause::FolderAbsent { id: String::new() },
            Cause::ArcNodeAbsent { node: String::new() },
            Cause::TerminalPipOffNode {
                node: String::new(),
                which: "first",
                metres: 0.0,
            },
            Cause::RecordTypeMissing,
            Cause::ImportanceOutOfRange,
            Cause::AttachmentUnreadable {
                fault: AttachmentFault::UnusableMethod(99),
            },
        ];
        let mut codes: Vec<&str> = all.iter().map(Cause::code).collect();
        let total = codes.len();
        codes.sort_unstable();
        codes.dedup();
        assert_eq!(codes.len(), total, "two causes share a code");
        // And every code is a kebab-case token, as SPEC 6.3 describes.
        for code in codes {
            assert!(
                !code.is_empty()
                    && code
                        .bytes()
                        .all(|b| b.is_ascii_lowercase() || b == b'-'),
                "{code:?} is not a short stable token"
            );
        }
    }
}

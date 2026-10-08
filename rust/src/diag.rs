//! Diagnostics (SPEC 6.3).
//!
//! A diagnostic carries a JSON Pointer, a stable code, and a short English
//! description. SPEC 6.3 makes the CODE the thing a conformance case compares
//! and forbids comparing the English, so the code is the wire commitment and
//! the prose is free to be reworded. Both come off one [`Cause`] variant, so a
//! diagnostic cannot carry a code that disagrees with its message.

/// Why a reader could not use something as written.
///
/// SPEC 6.2's remedy table gives each row a code, and SPEC 6.3 makes that code
/// the one a reader MUST put on the diagnostic. Where two causes here share a
/// code, it is because the table puts both in one row; they stay two causes
/// because their English differs.
#[derive(Debug, Clone, PartialEq)]
pub enum Cause {
    /// SPEC 6.2: an unrecognised key on an object this format defines. The key
    /// is skipped and the object is kept.
    UnknownKey,
    /// SPEC 6.2: a value of the wrong JSON type in an OPTIONAL key. The field
    /// is dropped and the object is kept.
    WrongTypeOptional {
        expected: &'static str,
        found: &'static str,
    },
    /// SPEC 6.2: a value of the wrong JSON type in a REQUIRED key. The OBJECT
    /// is dropped, because it cannot exist without the key.
    ///
    /// Separate from [`Cause::WrongTypeOptional`] because "a field is gone"
    /// and "the object is gone" are different outcomes for whoever holds the
    /// file, and a caller must tell them apart without parsing English.
    WrongTypeRequired {
        expected: &'static str,
        found: &'static str,
    },
    /// SPEC 3.10: a REQUIRED key is absent, so the object is dropped. SPEC
    /// 6.2's table puts this in the row of [`Cause::WrongTypeRequired`], so
    /// the two share a code: absent and wrong-typed are two documents with one
    /// outcome.
    MissingRequiredKey { key: &'static str },
    /// SPEC 4.1: a position that will not parse.
    ///
    /// `dropped` names what goes, because the remedy is not the same
    /// everywhere: SPEC 6.2's table drops the node, the arc, the segment, the
    /// area or the highlight that owns the position.
    PositionUnusable {
        detail: String,
        dropped: &'static str,
    },
    /// SPEC 3.3.1, 3.3.3: a time on a node, or an elevation or a time on a
    /// walk's `from`, `to` or `turn`. The channel is dropped and the node or
    /// the pass is kept.
    ExtraChannel { what: &'static str },
    /// SPEC 3.3.2: a time on an arc pip. Every time channel on the arc is
    /// dropped and its pips kept. Reported ONCE for the arc, however many
    /// pips carried a time.
    ArcPipTime,
    /// SPEC 3.3.2: an arc whose `from` and `to` name the same node. The arc is
    /// dropped, and every highlight walking it with it.
    SelfLoopArc { node: String },
    /// SPEC 3.3.2: an arc naming a node its owner does not have. The arc is
    /// dropped, and every highlight walking it with it.
    ArcNodeAbsent { node: String },
    /// SPEC 3.3.3: a pass that is not an object, or that names no arc of its
    /// owner, including an arc another rule dropped. The highlight is dropped.
    PassArcAbsent { detail: String },
    /// SPEC 3.3.3: a walk that breaks the rule for where `from` and `to`
    /// appear. The highlight is dropped.
    WalkSpelling { detail: String },
    /// SPEC 3.3.3: valid-walk rules 3, 4 and 5. The highlight is dropped.
    WalkNotConnected { detail: String },
    /// SPEC 3.3.3: a pass carrying both `turn` and `pin`. The highlight is
    /// dropped.
    PinnedTurn,
    /// SPEC 3.4: a position, a `turn` or a `pin` in a plan's walk. The plan
    /// is dropped.
    PlanPinOrPosition { detail: String },
    /// SPEC 3.7: a `polygon` holding rings rather than positions. The area is
    /// dropped.
    PolygonNestedRings,
    /// SPEC 3.7: a ring whose last position repeats its first. The repeated
    /// position is dropped and the area kept.
    RingClosed,
    /// SPEC 3.7: a ring with fewer than three distinct positions. The area is
    /// dropped.
    RingTooFewPoints { distinct: usize },
    /// SPEC 4.3: a record-level timestamp carrying an offset other than `Z`.
    TimestampNotUtc,
    /// SPEC 6.2: a `folder` naming a folder absent from the document. The key
    /// is treated as absent and the item sits at the top level.
    FolderAbsent { id: String },
    /// SPEC 5: a data record with no `@type`, or one that is not a string.
    /// SPEC 6.2's table gives it the code of [`Cause::WrongTypeRequired`].
    RecordTypeMissing,
    /// SPEC 3.6: an `importance` outside the range 1 to 5, or not an integer.
    /// SPEC 6.2 makes it an unusable value in an optional key, so it takes the
    /// code of [`Cause::WrongTypeOptional`].
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
    /// SPEC 7 gives this the same remedy as an unusable method, in its own
    /// sentence: drop the attachment, record a diagnostic, return the
    /// document, and never hand the attachment back with empty or partial
    /// content. The reasoning is the method case's — the document is intact,
    /// refusing a whole library over one damaged photograph is
    /// disproportionate, and the diagnostic is what stops the loss being
    /// silent.
    CorruptStream(String),
}

impl Cause {
    /// The stable token SPEC 6.3 requires, and which is what a corpus case
    /// compares. The English is never compared.
    ///
    /// Every code but one is a row of SPEC 6.2's remedy table. `folder-absent`
    /// is this crate's own: SPEC 6.2 states that remedy outside the table, and
    /// SPEC 6.3 permits a code on any diagnostic.
    pub fn code(&self) -> &'static str {
        match self {
            Cause::UnknownKey => "unknown-key",
            Cause::WrongTypeOptional { .. } | Cause::ImportanceOutOfRange => "wrong-type-optional",
            Cause::WrongTypeRequired { .. }
            | Cause::MissingRequiredKey { .. }
            | Cause::RecordTypeMissing => "wrong-type-required",
            Cause::PositionUnusable { .. } => "position-unusable",
            Cause::ExtraChannel { .. } => "extra-channel",
            Cause::ArcPipTime => "arc-pip-time",
            Cause::SelfLoopArc { .. } => "self-loop-arc",
            Cause::ArcNodeAbsent { .. } => "arc-node-absent",
            Cause::PassArcAbsent { .. } => "pass-arc-absent",
            Cause::WalkSpelling { .. } => "walk-spelling",
            Cause::WalkNotConnected { .. } => "walk-not-connected",
            Cause::PinnedTurn => "pinned-turn",
            Cause::PlanPinOrPosition { .. } => "plan-pin-or-position",
            Cause::PolygonNestedRings => "polygon-nested-rings",
            Cause::RingClosed => "ring-closed",
            Cause::RingTooFewPoints { .. } => "ring-too-few-points",
            Cause::TimestampNotUtc => "timestamp-not-utc",
            Cause::AttachmentUnreadable { .. } => "attachment-unreadable",
            Cause::FolderAbsent { .. } => "folder-absent",
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
                 the object is kept"
            ),
            Cause::WrongTypeRequired { expected, found } => write!(
                f,
                "expected {expected} but found {found}; the object is dropped, \
                 because it cannot exist without this key"
            ),
            Cause::MissingRequiredKey { key } => {
                write!(
                    f,
                    "the required key `{key}` is absent, so the object is dropped"
                )
            }
            Cause::PositionUnusable { detail, dropped } => {
                write!(f, "not a position: {detail}; {dropped} is dropped")
            }
            Cause::ExtraChannel { what } => write!(
                f,
                "{what}; the extra channel is dropped and the rest is kept"
            ),
            Cause::ArcPipTime => write!(
                f,
                "an arc pip carries no time; every time channel on this arc is \
                 dropped and its pips are kept"
            ),
            Cause::SelfLoopArc { node } => write!(
                f,
                "the arc runs from node `{node}` to itself, and a self-loop cannot \
                 be walked; the arc is dropped"
            ),
            Cause::ArcNodeAbsent { node } => write!(
                f,
                "the node `{node}` is not in this network or plan, so the arc is dropped"
            ),
            Cause::PassArcAbsent { detail } => write!(f, "{detail}; the highlight is dropped"),
            Cause::WalkSpelling { detail } => write!(f, "{detail}; the highlight is dropped"),
            Cause::WalkNotConnected { detail } => write!(f, "{detail}; the highlight is dropped"),
            Cause::PinnedTurn => write!(
                f,
                "a pass with a turn carries no pin, because the turn already fixes it; \
                 the highlight is dropped"
            ),
            Cause::PlanPinOrPosition { detail } => {
                write!(f, "{detail}; the plan is dropped")
            }
            Cause::PolygonNestedRings => write!(
                f,
                "a polygon holds one ring of positions; nested rings are not expressible, \
                 so the area is dropped"
            ),
            Cause::RingClosed => write!(
                f,
                "a ring is stored open, and this last position repeats the first; \
                 the repeat is dropped"
            ),
            Cause::RingTooFewPoints { distinct } => write!(
                f,
                "the ring holds {distinct} distinct position(s) and needs at least 3, \
                 so the area is dropped"
            ),
            Cause::TimestampNotUtc => write!(
                f,
                "a record-level timestamp must be UTC with a Z suffix, so the field is dropped"
            ),
            Cause::FolderAbsent { id } => write!(
                f,
                "the folder `{id}` is not in this document, so the item sits at the top level"
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

    /// One cause of each kind, so the tests below see every code.
    fn every_cause() -> Vec<Cause> {
        let text = String::new;
        vec![
            Cause::UnknownKey,
            Cause::WrongTypeOptional {
                expected: "a",
                found: "b",
            },
            Cause::WrongTypeRequired {
                expected: "a",
                found: "b",
            },
            Cause::MissingRequiredKey { key: "at" },
            Cause::PositionUnusable {
                detail: text(),
                dropped: "the arc",
            },
            Cause::ExtraChannel {
                what: "a node carries no time",
            },
            Cause::ArcPipTime,
            Cause::SelfLoopArc { node: text() },
            Cause::ArcNodeAbsent { node: text() },
            Cause::PassArcAbsent { detail: text() },
            Cause::WalkSpelling { detail: text() },
            Cause::WalkNotConnected { detail: text() },
            Cause::PinnedTurn,
            Cause::PlanPinOrPosition { detail: text() },
            Cause::PolygonNestedRings,
            Cause::RingClosed,
            Cause::RingTooFewPoints { distinct: 2 },
            Cause::TimestampNotUtc,
            Cause::FolderAbsent { id: text() },
            Cause::RecordTypeMissing,
            Cause::ImportanceOutOfRange,
            Cause::AttachmentUnreadable {
                fault: AttachmentFault::UnusableMethod(99),
            },
        ]
    }

    /// SPEC 6.2's remedy table, one code per row. These are wire commitments:
    /// a corpus case compares the code and never the English.
    const TABLE: &[&str] = &[
        "unknown-key",
        "wrong-type-optional",
        "wrong-type-required",
        "timestamp-not-utc",
        "position-unusable",
        "extra-channel",
        "arc-pip-time",
        "self-loop-arc",
        "arc-node-absent",
        "pass-arc-absent",
        "walk-spelling",
        "walk-not-connected",
        "pinned-turn",
        "plan-pin-or-position",
        "polygon-nested-rings",
        "ring-closed",
        "ring-too-few-points",
        "attachment-unreadable",
    ];

    /// Every row of the table has a cause that carries its code, and every
    /// code a cause carries is a row of the table or this crate's one own code.
    #[test]
    fn the_codes_are_the_rows_of_the_remedy_table() {
        let codes: std::collections::BTreeSet<&str> =
            every_cause().iter().map(Cause::code).collect();
        for row in TABLE {
            assert!(codes.contains(row), "no cause carries the code {row:?}");
        }
        for code in &codes {
            assert!(
                TABLE.contains(code) || *code == "folder-absent",
                "{code:?} is neither a row of SPEC 6.2's table nor this crate's own code"
            );
        }
    }

    /// Two causes share a code only where SPEC 6.2's table puts them in one
    /// row. A share nobody listed here would let a corpus case pass on the
    /// wrong diagnostic.
    #[test]
    fn only_the_table_s_own_rows_share_a_code() {
        let shared: &[(&str, &[&str])] = &[
            (
                "wrong-type-optional",
                &["WrongTypeOptional", "ImportanceOutOfRange"],
            ),
            (
                "wrong-type-required",
                &[
                    "WrongTypeRequired",
                    "MissingRequiredKey",
                    "RecordTypeMissing",
                ],
            ),
        ];
        let mut by_code: std::collections::BTreeMap<&str, Vec<String>> = Default::default();
        for cause in every_cause() {
            let name = format!("{cause:?}");
            let name = name
                .split(|c: char| !c.is_alphanumeric())
                .next()
                .expect("a variant name")
                .to_string();
            by_code.entry(cause.code()).or_default().push(name);
        }
        for (code, causes) in by_code {
            let allowed = shared.iter().find(|(c, _)| *c == code).map(|(_, v)| *v);
            match allowed {
                Some(names) => assert_eq!(causes, names, "the causes sharing {code:?}"),
                None => assert_eq!(causes.len(), 1, "{causes:?} share {code:?}"),
            }
        }
    }

    #[test]
    fn every_code_is_a_short_stable_token() {
        for cause in every_cause() {
            let code = cause.code();
            assert!(
                !code.is_empty() && code.bytes().all(|b| b.is_ascii_lowercase() || b == b'-'),
                "{code:?} is not a short stable token"
            );
        }
    }
}

//! The decoded document.
//!
//! Two properties of this model are load-bearing and neither is the shape a
//! Rust type system pushes you towards:
//!
//! * A [`Position`] keeps its channel count, so that an interior absence keeps
//!   its place: `[lon, lat, null, t]` cannot be shortened without moving the
//!   time into the elevation channel (SPEC 4.1). An `Option<f64>` elevation
//!   field — the first shape most implementations reach for — cannot express
//!   that.
//! * A [`Record`] body, a `style` object and an application key's value are
//!   kept as raw [`Json`]. They are opaque by ruling (SPEC 5.1, 3.10, 3.9) and
//!   must survive a read-modify-write at value level.

use crate::json::Json;

/// The `vnx` version string, and the numbers read out of it.
///
/// The declared text is kept because SPEC 2.2 requires a reader that
/// re-encodes a document to write the version the document declared, not its
/// own: `00.08.0000` is a legal spelling of 0.8.0 and is written back as it
/// was read.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Version {
    pub text: String,
    pub major: u64,
    pub minor: u64,
    pub patch: u64,
}

/// The version this crate implements.
pub const SUPPORTED: (u64, u64) = (0, 8);

/// A position: `[lon, lat, ele, time_ms]`, longitude first (SPEC 4.1).
///
/// The channel count is state, not a detail of spelling. `channels` is at
/// least two long, and its first two entries are always present.
#[derive(Debug, Clone, PartialEq)]
pub struct Position {
    channels: Vec<Option<f64>>,
}

impl Position {
    /// Build a position from its channels.
    ///
    /// # Panics
    ///
    /// Panics if fewer than two channels are given, or if longitude or
    /// latitude is absent. SPEC 4.1 permits `null` only from index 2 upward,
    /// and the decoder rejects a document that breaks that rule before this is
    /// reached.
    pub fn new(channels: Vec<Option<f64>>) -> Self {
        assert!(
            channels.len() >= 2,
            "a position carries at least longitude and latitude"
        );
        assert!(
            channels[0].is_some() && channels[1].is_some(),
            "longitude and latitude are never absent"
        );
        Position { channels }
    }

    /// A two-channel position.
    pub fn lon_lat(lon: f64, lat: f64) -> Self {
        Position::new(vec![Some(lon), Some(lat)])
    }

    pub fn lon(&self) -> f64 {
        self.channels[0].expect("longitude is never absent")
    }

    pub fn lat(&self) -> f64 {
        self.channels[1].expect("latitude is never absent")
    }

    /// Elevation in metres, absent when the channel is `null` or omitted.
    pub fn ele(&self) -> Option<f64> {
        self.channels.get(2).copied().flatten()
    }

    /// Unix milliseconds, absent when the channel is `null` or omitted.
    pub fn time_ms(&self) -> Option<f64> {
        self.channels.get(3).copied().flatten()
    }

    /// How many channels this position was written with.
    pub fn arity(&self) -> usize {
        self.channels.len()
    }

    pub fn channels(&self) -> &[Option<f64>] {
        &self.channels
    }

    /// Two positions are the same point when their longitude and latitude are
    /// equal. No other channel takes part: SPEC 3.7 counts a ring's distinct
    /// positions this way, and finds a closed ring this way.
    pub fn same_point(&self, other: &Position) -> bool {
        self.lon() == other.lon() && self.lat() == other.lat()
    }
}

/// A data record (SPEC 5). The body is opaque apart from `@type`.
#[derive(Debug, Clone, PartialEq)]
pub struct Record {
    /// The whole record, `@type` included, exactly as it was read.
    entries: Vec<(String, Json)>,
}

impl Record {
    /// # Panics
    ///
    /// Panics unless the entries carry a string `@type`. The decoder drops a
    /// record without one (SPEC 6.2) rather than constructing this.
    pub fn new(entries: Vec<(String, Json)>) -> Self {
        assert!(
            entries
                .iter()
                .any(|(k, v)| k == "@type" && matches!(v, Json::String(_))),
            "a record carries a string @type"
        );
        Record { entries }
    }

    pub fn type_name(&self) -> &str {
        self.entries
            .iter()
            .find(|(k, _)| k == "@type")
            .and_then(|(_, v)| v.as_str())
            .expect("a record carries a string @type")
    }

    pub fn entries(&self) -> &[(String, Json)] {
        &self.entries
    }
}

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Document {
    pub vnx: Option<Version>,
    pub generator: Option<String>,
    pub created: Option<String>,
    /// An absent collection and an empty one carry the same content and a
    /// reader treats them alike (SPEC 3.1). `Option` keeps the distinction the
    /// document made anyway, because it costs nothing to and because a writer
    /// that spells either stays conforming.
    pub folders: Option<Vec<Folder>>,
    pub networks: Option<Vec<Network>>,
    pub routes: Option<Vec<Route>>,
    pub tracks: Option<Vec<Track>>,
    pub waypoints: Option<Vec<Waypoint>>,
    pub areas: Option<Vec<Area>>,
    /// The application keys (SPEC 3.9): every top-level key containing a full
    /// stop, each value kept verbatim and interpreted not at all. A JSON
    /// `null` is a present key holding [`Json::Null`], and an empty value
    /// stays empty: SPEC 3.9 exempts these keys from absent-equals-empty.
    ///
    /// A list, not a map, and sorted by key on decode. Key order is not
    /// significant (SPEC 4.4); sorted is the order the Go reference keeps.
    /// [`encode`](crate::encode) writes the list in the order it is given.
    pub app_keys: Vec<AppKey>,
}

/// One application key (SPEC 3.9).
#[derive(Debug, Clone, PartialEq)]
pub struct AppKey {
    pub key: String,
    pub value: Json,
}

/// Whether a top-level key is an application key: whether it contains a full
/// stop (SPEC 3.9). No key the format defines contains one.
pub fn is_app_key(key: &str) -> bool {
    key.contains('.')
}

#[derive(Debug, Clone, PartialEq)]
pub struct Folder {
    pub id: String,
    pub parent: Option<String>,
    pub name: Option<String>,
    pub notes: Option<String>,
    pub hidden: Option<bool>,
    pub data: Option<Vec<Record>>,
}

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Network {
    pub name: Option<String>,
    pub notes: Option<String>,
    pub folder: Option<String>,
    pub hidden: Option<bool>,
    /// Keyed by identifier, scoped to this network (SPEC 4.4). A `Vec` rather
    /// than a map: entries are few, the key order is not significant, and the
    /// crate takes no dependency on a hasher.
    pub nodes: Option<Vec<(String, Vertex)>>,
    pub arcs: Option<Vec<(String, Arc)>>,
    pub highlights: Option<Vec<Highlight>>,
    pub points: Option<Vec<Waypoint>>,
    pub areas: Option<Vec<Area>>,
    pub data: Option<Vec<Record>>,
}

impl Network {
    pub fn node(&self, key: &str) -> Option<&Vertex> {
        find(&self.nodes, key)
    }

    pub fn arc(&self, key: &str) -> Option<&Arc> {
        find(&self.arcs, key)
    }
}

fn find<'a, T>(list: &'a Option<Vec<(String, T)>>, key: &str) -> Option<&'a T> {
    list.as_ref()?
        .iter()
        .find(|(k, _)| k == key)
        .map(|(_, v)| v)
}

/// A node or an arc pip: a point that carries no time (SPEC 3.3.1, 3.3.2).
///
/// A type of its own rather than a [`Position`], so that a time on a node or an
/// arc pip cannot be built and so cannot be written. The reader drops such a
/// time with a diagnostic, and nothing downstream has to remember to.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Vertex {
    pub lon: f64,
    pub lat: f64,
    /// Elevation in metres, `None` when unknown.
    pub ele: Option<f64>,
}

/// A position in a walk: a pass's `from`, `to` or `turn` (SPEC 3.3.3).
///
/// Longitude and latitude only. Its elevation is the arc line's at the point it
/// identifies, so a stored one would be a second copy of a fact the arc holds.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct LonLat {
    pub lon: f64,
    pub lat: f64,
}

/// A line between two DIFFERENT nodes of its owner, a network or a route.
///
/// `pips` holds the INTERIOR points only (SPEC 3.3.2): the line runs from the
/// `from` node's position, through `pips`, to the `to` node's. An arc with no
/// pips is the straight line between its nodes.
#[derive(Debug, Clone, PartialEq)]
pub struct Arc {
    pub from: String,
    pub to: String,
    pub pips: Vec<Vertex>,
    pub data: Option<Vec<Record>>,
}

/// A highlight walks arcs its network owns. Its geometry is by reference: two
/// highlights on one trail name the same arc.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Highlight {
    pub name: Option<String>,
    pub notes: Option<String>,
    pub hidden: Option<bool>,
    pub style: Option<Json>,
    pub walk: Vec<Pass>,
    pub data: Option<Vec<Record>>,
}

/// One entry of a walk (SPEC 3.3.3): the arc it covers, and where on it the
/// walk enters, turns and leaves.
///
/// ⚠ `from` AND `to` ARE PRESENT ONLY WHERE SPEC 3.3.3's RULE PUTS THEM: on the
/// first pass, on the last pass, and on a pass whose `from` is a position.
/// Everywhere else the entry is the node the walk is at and the exit is
/// derived. `None` is that absence, not "unknown".
#[derive(Debug, Clone, PartialEq)]
pub struct Pass {
    pub arc: String,
    pub from: Option<Endpoint>,
    pub to: Option<Endpoint>,
    pub turn: Option<LonLat>,
    /// `false` is written as nothing: SPEC 3.3.3 has a writer emit `pinned`
    /// only when it is `true`, and a reader treat `false` as absent.
    pub pinned: bool,
}

impl Pass {
    /// A pass over `arc` with no `from`, `to`, `turn` or pin.
    pub fn over(arc: &str) -> Self {
        Pass {
            arc: arc.to_string(),
            from: None,
            to: None,
            turn: None,
            pinned: false,
        }
    }
}

/// Where a pass enters or leaves its arc: a node key, or a position on the arc
/// (SPEC 3.3.3).
#[derive(Debug, Clone, PartialEq)]
pub enum Endpoint {
    Node(String),
    At(LonLat),
}

/// A top-level route OWNS its graph (SPEC 3.4): nodes and arcs of a network's
/// shape, and one walk over them.
///
/// Nothing outside the route can reference its nodes or arcs. Its walk runs
/// node to node, with no position, turn or pin. Where a reader would drop a
/// highlight, it drops the route: the route is its own highlight.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Route {
    pub name: Option<String>,
    pub notes: Option<String>,
    pub folder: Option<String>,
    pub hidden: Option<bool>,
    pub style: Option<Json>,
    pub nodes: Option<Vec<(String, Vertex)>>,
    pub arcs: Option<Vec<(String, Arc)>>,
    pub walk: Vec<Pass>,
    pub data: Option<Vec<Record>>,
}

impl Route {
    pub fn node(&self, key: &str) -> Option<&Vertex> {
        find(&self.nodes, key)
    }

    pub fn arc(&self, key: &str) -> Option<&Arc> {
        find(&self.arcs, key)
    }
}

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Track {
    pub name: Option<String>,
    pub notes: Option<String>,
    pub folder: Option<String>,
    pub hidden: Option<bool>,
    pub style: Option<Json>,
    pub started: Option<String>,
    pub ended: Option<String>,
    pub segments: Vec<Segment>,
    pub data: Option<Vec<Record>>,
}

impl Track {
    /// Moving time in milliseconds, or `None` for unknown (SPEC 3.5).
    ///
    /// The sum over segments of the difference between the last and the first
    /// pip time in that segment. Deliberately not the wall-clock span between
    /// `started` and `ended`, which a pause lengthens.
    ///
    /// A track that is only PARTLY timed is unknown for the whole track, not
    /// the sum of the segments that are timed. Ruled 2026-09-09, against this
    /// crate's first reading and against the JavaScript seat's, which were the
    /// same reading and were both wrong.
    ///
    /// The reasoning is the one that makes zero wrong for an untimed track: a
    /// partial sum reports a number for a span it cannot measure, and a user
    /// comparing two tracks gets a wrong answer with nothing to say anything
    /// is missing. Unknown is honest. No corpus tier could see the divergence,
    /// because every corpus track is fully timed or fully untimed.
    pub fn moving_time_ms(&self) -> Option<f64> {
        let mut total = 0.0;
        let mut measured = 0;
        for segment in &self.segments {
            // An EMPTY segment has no span to measure, so it neither
            // contributes nor makes the track unknown.
            let (Some(first), Some(last)) = (segment.pips.first(), segment.pips.last()) else {
                continue;
            };
            // Any segment that does have a span must have a time at both ends.
            // Not "its first and last TIMED pip" — a span measured between two
            // pips that are not the segment's ends is a different span.
            total += last.time_ms()? - first.time_ms()?;
            measured += 1;
        }
        (measured > 0).then_some(total)
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct Segment {
    pub pips: Vec<Position>,
}

/// A top-level waypoint, or a network's point. One shape (SPEC 3.6).
///
/// `folder` is carried only by a top-level waypoint; a network's point is
/// placed by the network that contains it, and the decoder reports `folder` on
/// one as an unrecognised key.
#[derive(Debug, Clone, PartialEq)]
pub struct Waypoint {
    pub name: Option<String>,
    pub notes: Option<String>,
    pub folder: Option<String>,
    pub hidden: Option<bool>,
    pub style: Option<Json>,
    pub at: Position,
    pub data: Option<Vec<Record>>,
}

/// A top-level area, or a network's area. `folder` is carried only by the
/// top-level one, as for [`Waypoint`].
#[derive(Debug, Clone, PartialEq)]
pub struct Area {
    pub name: Option<String>,
    pub notes: Option<String>,
    pub folder: Option<String>,
    pub hidden: Option<bool>,
    pub style: Option<Json>,
    /// One OPEN ring of at least three distinct positions (SPEC 3.7): the last
    /// position does not repeat the first, and the boundary runs from the last
    /// back to the first. Holes and multiple rings are not expressible.
    pub polygon: Vec<Position>,
    pub data: Option<Vec<Record>>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn arity_is_kept() {
        let short = Position::lon_lat(-3.0, 57.0);
        let long = Position::new(vec![Some(-3.0), Some(57.0), None]);
        assert_eq!(short.arity(), 2);
        assert_eq!(long.arity(), 3);
        assert_eq!(short.ele(), None);
        assert_eq!(long.ele(), None);
        // The same point, differently spelled, and NOT the same model value:
        // arity is what keeps an interior absence in its own channel.
        assert!(short.same_point(&long));
        assert_ne!(short, long);
    }

    #[test]
    fn an_interior_absence_is_not_a_trailing_one() {
        let p = Position::new(vec![Some(-3.0), Some(57.0), None, Some(1778914991000.0)]);
        assert_eq!(p.ele(), None);
        assert_eq!(p.time_ms(), Some(1778914991000.0));
    }

    #[test]
    fn a_type_differing_only_by_case_is_a_different_type() {
        let record =
            |t: &str| Record::new(vec![("@type".to_string(), Json::String(t.to_string()))]);
        // SPEC 5: `@type` is compared exactly, including case.
        assert_ne!(
            record("App.viatrail.tag").type_name(),
            record("app.viatrail.tag").type_name()
        );
    }

    #[test]
    fn moving_time_skips_a_pause_and_admits_when_it_cannot_know() {
        let timed = |t0: f64, t1: f64| Segment {
            pips: vec![
                Position::new(vec![Some(-3.0), Some(57.0), None, Some(t0)]),
                Position::new(vec![Some(-3.0), Some(57.1), None, Some(t1)]),
            ],
        };
        let track = Track {
            segments: vec![timed(0.0, 103_000.0), timed(1_889_000.0, 1_953_000.0)],
            ..Track::default()
        };
        assert_eq!(track.moving_time_ms(), Some(167_000.0));

        let untimed = Track {
            segments: vec![Segment {
                pips: vec![Position::lon_lat(-3.0, 57.0), Position::lon_lat(-3.0, 57.1)],
            }],
            ..Track::default()
        };
        assert_eq!(untimed.moving_time_ms(), None, "unknown is not zero");
    }

    /// SPEC 3.5, ruled 2026-09-09: a PARTLY timed track is unknown for the
    /// whole track. Summing only its timed segments reports a number for a
    /// span that cannot be measured, which is the shape SPEC 3.5 forbids for
    /// the all-untimed case.
    ///
    /// No corpus case reaches this — every corpus track is fully timed or
    /// fully untimed — so this test is the only thing holding the rule.
    #[test]
    fn a_partly_timed_track_is_unknown_for_the_whole_track() {
        let timed = Segment {
            pips: vec![
                Position::new(vec![Some(-3.0), Some(57.0), None, Some(0.0)]),
                Position::new(vec![Some(-3.0), Some(57.1), None, Some(103_000.0)]),
            ],
        };
        let untimed = Segment {
            pips: vec![Position::lon_lat(-3.0, 57.2), Position::lon_lat(-3.0, 57.3)],
        };
        let track = Track {
            segments: vec![timed.clone(), untimed],
            ..Track::default()
        };
        assert_eq!(
            track.moving_time_ms(),
            None,
            "a partial sum answers a question the document cannot answer"
        );
        // And the fully timed prefix on its own is still known, so this is not
        // a guard that has stopped measuring anything.
        assert_eq!(
            Track {
                segments: vec![timed],
                ..Track::default()
            }
            .moving_time_ms(),
            Some(103_000.0)
        );
    }

    /// A track with no segments has nothing to measure either. Zero would be
    /// the same wrong answer in a smaller costume.
    #[test]
    fn a_track_with_no_segments_is_unknown() {
        assert_eq!(Track::default().moving_time_ms(), None);
    }

    /// An EMPTY segment has no span, so it neither contributes nor makes the
    /// track unknown. The distinction matters: "every segment must be timed"
    /// and "every segment that HAS a span must be timed" differ exactly here.
    #[test]
    fn an_empty_segment_neither_contributes_nor_makes_a_track_unknown() {
        let timed = Segment {
            pips: vec![
                Position::new(vec![Some(-3.0), Some(57.0), None, Some(0.0)]),
                Position::new(vec![Some(-3.0), Some(57.1), None, Some(103_000.0)]),
            ],
        };
        let track = Track {
            segments: vec![Segment { pips: vec![] }, timed, Segment { pips: vec![] }],
            ..Track::default()
        };
        assert_eq!(track.moving_time_ms(), Some(103_000.0));

        // But a track of nothing BUT empty segments has measured nothing.
        assert_eq!(
            Track {
                segments: vec![Segment { pips: vec![] }],
                ..Track::default()
            }
            .moving_time_ms(),
            None
        );
    }

    /// A one-pip segment is a span of zero, not an absence. Its single pip is
    /// both the first and the last, so it needs a time.
    #[test]
    fn a_one_pip_segment_is_a_span_of_zero() {
        let at = |t: Option<f64>| Segment {
            pips: vec![Position::new(vec![Some(-3.0), Some(57.0), None, t])],
        };
        assert_eq!(
            Track {
                segments: vec![at(Some(5.0))],
                ..Track::default()
            }
            .moving_time_ms(),
            Some(0.0)
        );
        assert_eq!(
            Track {
                segments: vec![at(None)],
                ..Track::default()
            }
            .moving_time_ms(),
            None
        );
    }
}

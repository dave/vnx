//! Reading a document (SPEC 6).
//!
//! "A reader is tolerant of damage and silent about nothing." Every tolerance
//! here records a diagnostic, and the only paths out of this module that are
//! not a document plus a diagnostic list are SPEC 6.2's four refusal causes.
//!
//! There are no fallbacks. A malformed value never becomes a default: it is
//! dropped, with its cause and its JSON Pointer named.

use crate::diag::{Cause, Diagnostic, Path};
use crate::error::Refusal;
use crate::json::{self, Json};
use crate::model::*;
use crate::walk::{Cursor, WalkFault};

/// What a reader returns: the document, and the list of everything it could
/// not use as written (SPEC 6.3).
#[derive(Debug, Clone, PartialEq)]
pub struct Reading {
    pub document: Document,
    pub diagnostics: Vec<Diagnostic>,
}

/// Read a `.vnx` document from bytes.
pub fn decode(bytes: &[u8]) -> Result<Reading, Refusal> {
    let value = json::parse(bytes).map_err(|detail| Refusal::MalformedJson { detail })?;
    decode_json(&value)
}

/// Read a document from an already-parsed JSON tree.
pub fn decode_json(value: &Json) -> Result<Reading, Refusal> {
    let mut d = Decoder {
        diagnostics: Vec::new(),
    };
    let document = d.document(value)?;
    Ok(Reading {
        document,
        diagnostics: d.diagnostics,
    })
}

struct Decoder {
    diagnostics: Vec<Diagnostic>,
}

/// Where a position sits, which decides both how a value of the wrong JSON type
/// is reported and what gets dropped when the position will not parse (SPEC
/// 6.2).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Slot {
    /// A required key, such as a waypoint's `at`, or a node's value. A value
    /// that is not a position array at all is a wrong TYPE here, and the item
    /// cannot exist without it.
    RequiredKey,
    /// A position inside an array — a pip, or a ring vertex. A value that is
    /// not an array is a position that will not parse rather than a wrong type,
    /// because the array itself is perfectly usable and one element inside it
    /// is not. `dropped` names what goes with it.
    Element { dropped: &'static str },
}

// ---------------------------------------------------------------------------
// Recognised keys (SPEC 3.10). Section 6.2's unrecognised-key rule cannot be
// implemented without this table, which is why the specification carries it.
// ---------------------------------------------------------------------------

const DOCUMENT_KEYS: &[&str] = &[
    "vnx",
    "generator",
    "created",
    "folders",
    "networks",
    "routes",
    "tracks",
    "waypoints",
    "areas",
];
const FOLDER_KEYS: &[&str] = &["id", "parent", "name", "notes", "hidden", "data"];
const NETWORK_KEYS: &[&str] = &[
    "name", "notes", "folder", "hidden", "nodes", "arcs", "chains", "points", "areas", "data",
];
/// An arc, in a network or a route.
const ARC_KEYS: &[&str] = &["from", "to", "pips", "data"];
const CHAIN_KEYS: &[&str] = &["walk", "name", "notes", "hidden", "style", "data"];
const PASS_KEYS: &[&str] = &["arc", "from", "to", "turn", "pinned"];
const ROUTE_KEYS: &[&str] = &[
    "walk", "name", "notes", "folder", "hidden", "style", "nodes", "arcs", "data",
];
const TRACK_KEYS: &[&str] = &[
    "segments", "name", "notes", "folder", "hidden", "style", "started", "ended", "data",
];
const SEGMENT_KEYS: &[&str] = &["pips"];
const WAYPOINT_KEYS: &[&str] = &["at", "name", "notes", "folder", "hidden", "style", "data"];
/// A network's point is placed by the network that contains it, so it carries
/// no `folder` (SPEC 3.10).
const POINT_KEYS: &[&str] = &["at", "name", "notes", "hidden", "style", "data"];
const AREA_KEYS: &[&str] = &[
    "polygon", "name", "notes", "folder", "hidden", "style", "data",
];
const NETWORK_AREA_KEYS: &[&str] = &["polygon", "name", "notes", "hidden", "style", "data"];

impl Decoder {
    fn report(&mut self, path: &Path, cause: Cause) {
        self.diagnostics.push(Diagnostic::new(path.as_str(), cause));
    }

    // -----------------------------------------------------------------------
    // Generic field readers. Each reports and returns `None` rather than
    // substituting anything.
    // -----------------------------------------------------------------------

    /// SPEC 6.2: an unrecognised key is diagnosed, skipped, and the item kept.
    ///
    /// Applied only to objects this format defines. It is NOT applied inside a
    /// data record, a style object, an application key's value, or the
    /// identifier-keyed `nodes` and `arcs` maps. The document itself is checked
    /// by [`Decoder::check_document_keys`], because its key set is open.
    fn check_keys(&mut self, entries: &[(String, Json)], known: &[&str], path: &Path) {
        for (key, _) in entries {
            if !known.contains(&key.as_str()) {
                self.report(&path.key(key), Cause::UnknownKey);
            }
        }
    }

    /// SPEC 3.9: at the TOP LEVEL ONLY, a key containing a full stop is an
    /// application key and is recognised. Every other object keeps its fixed
    /// table, so a dotted key on an item is reported as unknown.
    fn check_document_keys(&mut self, entries: &[(String, Json)], path: &Path) {
        for (key, _) in entries {
            if !DOCUMENT_KEYS.contains(&key.as_str()) && !is_app_key(key) {
                self.report(&path.key(key), Cause::UnknownKey);
            }
        }
    }

    fn field<'a>(&self, entries: &'a [(String, Json)], key: &str) -> Option<&'a Json> {
        entries.iter().find(|(k, _)| k == key).map(|(_, v)| v)
    }

    /// A string-valued optional key.
    fn opt_string(
        &mut self,
        entries: &[(String, Json)],
        key: &str,
        path: &Path,
    ) -> Option<String> {
        let value = self.field(entries, key)?;
        match value {
            Json::String(s) => Some(s.clone()),
            other => {
                self.report(
                    &path.key(key),
                    Cause::WrongTypeOptional {
                        expected: "a string",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    fn opt_bool(&mut self, entries: &[(String, Json)], key: &str, path: &Path) -> Option<bool> {
        let value = self.field(entries, key)?;
        match value {
            Json::Bool(b) => Some(*b),
            other => {
                self.report(
                    &path.key(key),
                    Cause::WrongTypeOptional {
                        expected: "a boolean",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    fn opt_array<'a>(
        &mut self,
        entries: &'a [(String, Json)],
        key: &str,
        path: &Path,
    ) -> Option<&'a [Json]> {
        let value = self.field(entries, key)?;
        match value {
            Json::Array(items) => Some(items),
            other => {
                self.report(
                    &path.key(key),
                    Cause::WrongTypeOptional {
                        expected: "an array",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    fn opt_object<'a>(
        &mut self,
        entries: &'a [(String, Json)],
        key: &str,
        path: &Path,
    ) -> Option<&'a [(String, Json)]> {
        let value = self.field(entries, key)?;
        match value {
            Json::Object(members) => Some(members),
            other => {
                self.report(
                    &path.key(key),
                    Cause::WrongTypeOptional {
                        expected: "an object",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    /// A REQUIRED array. Absence or the wrong type takes the object with it
    /// (SPEC 6.2), so the caller drops on `None`. Both are reported at the
    /// key, present or not, under one code: SPEC 6.2's table puts them in one
    /// row.
    fn req_array<'a>(
        &mut self,
        entries: &'a [(String, Json)],
        key: &'static str,
        path: &Path,
    ) -> Option<&'a [Json]> {
        match self.field(entries, key) {
            None => {
                self.report(&path.key(key), Cause::MissingRequiredKey { key });
                None
            }
            Some(Json::Array(items)) => Some(items),
            Some(other) => {
                self.report(
                    &path.key(key),
                    Cause::WrongTypeRequired {
                        expected: "an array",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    fn req_string(
        &mut self,
        entries: &[(String, Json)],
        key: &'static str,
        path: &Path,
    ) -> Option<String> {
        match self.field(entries, key) {
            None => {
                self.report(&path.key(key), Cause::MissingRequiredKey { key });
                None
            }
            Some(Json::String(s)) => Some(s.clone()),
            Some(other) => {
                self.report(
                    &path.key(key),
                    Cause::WrongTypeRequired {
                        expected: "a string",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    // -----------------------------------------------------------------------
    // Values
    // -----------------------------------------------------------------------

    /// SPEC 4.1. Every channel is kept, including one this reader does not
    /// know, so that a future channel is additive and a read-modify-write keeps
    /// the spelling the document carried.
    fn position(&mut self, value: &Json, path: &Path, dropped: &'static str) -> Option<Position> {
        self.position_in(value, path, Slot::Element { dropped })
    }

    /// A position in a REQUIRED key, where a value that is not an array at all
    /// is a wrong JSON TYPE rather than a position that will not parse. SPEC
    /// 6.2 gives those two clauses separately, and the corpus pins the
    /// distinction at `/waypoints/1/at`.
    fn required_position(&mut self, value: &Json, path: &Path) -> Option<Position> {
        self.position_in(value, path, Slot::RequiredKey)
    }

    fn position_in(&mut self, value: &Json, path: &Path, slot: Slot) -> Option<Position> {
        let Json::Array(items) = value else {
            match slot {
                Slot::RequiredKey => self.report(
                    path,
                    Cause::WrongTypeRequired {
                        expected: "a position array",
                        found: value.type_name(),
                    },
                ),
                Slot::Element { dropped } => self.report(
                    path,
                    Cause::PositionUnusable {
                        detail: format!("expected an array but found {}", value.type_name()),
                        dropped,
                    },
                ),
            }
            return None;
        };
        let dropped = match slot {
            Slot::RequiredKey => "the item",
            Slot::Element { dropped } => dropped,
        };
        match parse_position(items) {
            Ok(position) => Some(position),
            Err(detail) => {
                self.report(path, Cause::PositionUnusable { detail, dropped });
                None
            }
        }
    }

    /// A `pips` array. One position that will not parse drops the ARC or the
    /// SEGMENT that holds it, NOT the position (SPEC 6.2): a missing vertex
    /// would leave a shorter, plausible, wrong line with nothing to indicate
    /// it.
    ///
    /// This is not the required-key rule: `pips` is an array and is perfectly
    /// usable; one element inside it is not.
    fn pips(&mut self, items: &[Json], path: &Path, dropped: &'static str) -> Option<Vec<Position>> {
        let mut out = Vec::with_capacity(items.len());
        for (i, item) in items.iter().enumerate() {
            out.push(self.position(item, &path.index(i), dropped)?);
        }
        Some(out)
    }

    /// SPEC 4.3: a record-level timestamp is ISO 8601 in UTC, `Z` suffix,
    /// OPTIONAL fractional seconds. Anything else is dropped with a
    /// diagnostic.
    fn timestamp(&mut self, entries: &[(String, Json)], key: &str, path: &Path) -> Option<String> {
        let text = self.opt_string(entries, key, path)?;
        if is_utc_timestamp(&text) {
            Some(text)
        } else {
            self.report(&path.key(key), Cause::TimestampNotUtc);
            None
        }
    }

    /// SPEC 5. A record body is opaque apart from `@type`, so nothing here
    /// reaches inside it: no key check, no field validation. A record with no
    /// string `@type` is dropped (SPEC 6.2); the rest of the bag survives.
    fn records(&mut self, entries: &[(String, Json)], path: &Path) -> Option<Vec<Record>> {
        let items = self.opt_array(entries, "data", path)?;
        let data_path = path.key("data");
        let mut out = Vec::with_capacity(items.len());
        for (i, item) in items.iter().enumerate() {
            let item_path = data_path.index(i);
            let Json::Object(members) = item else {
                self.report(
                    &item_path,
                    Cause::WrongTypeRequired {
                        expected: "an object",
                        found: item.type_name(),
                    },
                );
                continue;
            };
            match members.iter().find(|(k, _)| k == "@type").map(|(_, v)| v) {
                Some(Json::String(_)) => out.push(Record::new(members.clone())),
                _ => self.report(&item_path, Cause::RecordTypeMissing),
            }
        }
        Some(out)
    }

    /// SPEC 3.10 makes a style object opaque and SPEC 3.6 makes `importance`
    /// an integer from 1 to 5, routing an out-of-range value to SPEC 6.2. The
    /// two rules meet here: every key is carried through untouched except
    /// `importance`, which is the one key inside an opaque object this reader
    /// is required to inspect and may drop. Reported as a spec tension.
    fn style(&mut self, entries: &[(String, Json)], path: &Path) -> Option<Json> {
        let members = self.opt_object(entries, "style", path)?;
        let importance = members.iter().find(|(k, _)| k == "importance");
        let acceptable = match importance {
            None => true,
            Some((_, Json::Number(n))) => n.fract() == 0.0 && (1.0..=5.0).contains(n),
            Some(_) => false,
        };
        if acceptable {
            return Some(Json::Object(members.to_vec()));
        }
        self.report(
            &path.key("style").key("importance"),
            Cause::ImportanceOutOfRange,
        );
        Some(Json::Object(
            members
                .iter()
                .filter(|(k, _)| k != "importance")
                .cloned()
                .collect(),
        ))
    }

    // -----------------------------------------------------------------------
    // The document
    // -----------------------------------------------------------------------

    fn document(&mut self, value: &Json) -> Result<Document, Refusal> {
        let Json::Object(entries) = value else {
            return Err(Refusal::NotAVnxDocument {
                detail: format!("the top level is {}", value.type_name()),
            });
        };
        let root = Path::root();

        // SPEC 2.2, and the first three of SPEC 6.2's four refusal causes.
        let version = match entries.iter().find(|(k, _)| k == "vnx") {
            None => {
                return Err(Refusal::NotAVnxDocument {
                    detail: "the top level carries no `vnx` key".to_string(),
                })
            }
            Some((_, Json::String(text))) => parse_version(text)?,
            Some((_, other)) => {
                return Err(Refusal::VersionMalformed {
                    found: format!("a {} value", other.type_name().trim_start_matches("a ")),
                })
            }
        };

        self.check_document_keys(entries, &root);

        let mut document = Document {
            vnx: Some(version),
            generator: self.opt_string(entries, "generator", &root),
            created: self.timestamp(entries, "created", &root),
            ..Document::default()
        };

        if let Some(items) = self.opt_array(entries, "folders", &root) {
            let path = root.key("folders");
            document.folders = Some(
                items
                    .iter()
                    .enumerate()
                    .filter_map(|(i, item)| self.folder(item, &path.index(i)))
                    .collect(),
            );
        }
        if let Some(items) = self.opt_array(entries, "networks", &root) {
            let path = root.key("networks");
            document.networks = Some(
                items
                    .iter()
                    .enumerate()
                    .filter_map(|(i, item)| self.network(item, &path.index(i)))
                    .collect(),
            );
        }
        if let Some(items) = self.opt_array(entries, "routes", &root) {
            let path = root.key("routes");
            document.routes = Some(
                items
                    .iter()
                    .enumerate()
                    .filter_map(|(i, item)| self.route(item, &path.index(i)))
                    .collect(),
            );
        }
        if let Some(items) = self.opt_array(entries, "tracks", &root) {
            let path = root.key("tracks");
            document.tracks = Some(
                items
                    .iter()
                    .enumerate()
                    .filter_map(|(i, item)| self.track(item, &path.index(i)))
                    .collect(),
            );
        }
        if let Some(items) = self.opt_array(entries, "waypoints", &root) {
            let path = root.key("waypoints");
            document.waypoints = Some(
                items
                    .iter()
                    .enumerate()
                    .filter_map(|(i, item)| self.waypoint(item, &path.index(i), WAYPOINT_KEYS))
                    .collect(),
            );
        }
        if let Some(items) = self.opt_array(entries, "areas", &root) {
            let path = root.key("areas");
            document.areas = Some(
                items
                    .iter()
                    .enumerate()
                    .filter_map(|(i, item)| self.area(item, &path.index(i), AREA_KEYS))
                    .collect(),
            );
        }
        // SPEC 3.9: every application key, whatever its value — `null` and
        // empty included — preserved verbatim and interpreted not at all.
        let mut app_keys: Vec<AppKey> = entries
            .iter()
            .filter(|(k, _)| is_app_key(k))
            .map(|(k, v)| AppKey {
                key: k.clone(),
                value: v.clone(),
            })
            .collect();
        app_keys.sort_by(|a, b| a.key.cmp(&b.key));
        document.app_keys = app_keys;

        self.resolve_folder_references(&mut document);
        Ok(document)
    }

    fn folder(&mut self, value: &Json, path: &Path) -> Option<Folder> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, FOLDER_KEYS, path);
        // SPEC 6.2: a folder with no `id` is dropped. Items that named it then
        // sit at the top level, so a folder's contents survive its loss.
        let id = self.req_string(entries, "id", path)?;
        Some(Folder {
            id,
            parent: self.opt_string(entries, "parent", path),
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            hidden: self.opt_bool(entries, "hidden", path),
            data: self.records(entries, path),
        })
    }

    fn network(&mut self, value: &Json, path: &Path) -> Option<Network> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, NETWORK_KEYS, path);

        let mut network = Network {
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            folder: self.opt_string(entries, "folder", path),
            hidden: self.opt_bool(entries, "hidden", path),
            data: self.records(entries, path),
            ..Network::default()
        };
        network.nodes = self.nodes(entries, path);
        network.arcs = self.arcs(entries, path, &network.nodes);

        if let Some(items) = self.opt_array(entries, "points", path) {
            let points_path = path.key("points");
            network.points = Some(
                items
                    .iter()
                    .enumerate()
                    .filter_map(|(i, item)| {
                        self.waypoint(item, &points_path.index(i), POINT_KEYS)
                    })
                    .collect(),
            );
        }
        if let Some(items) = self.opt_array(entries, "areas", path) {
            let areas_path = path.key("areas");
            network.areas = Some(
                items
                    .iter()
                    .enumerate()
                    .filter_map(|(i, item)| {
                        self.area(item, &areas_path.index(i), NETWORK_AREA_KEYS)
                    })
                    .collect(),
            );
        }

        // A walk can only be checked against the arcs that survived, so the
        // chains are read once the graph is in hand.
        if let Some(items) = self.opt_array(entries, "chains", path) {
            let chains_path = path.key("chains");
            let chains: Vec<Chain> = items
                .iter()
                .enumerate()
                .filter_map(|(i, item)| self.chain(item, &chains_path.index(i), &network.arcs))
                .collect();
            network.chains = Some(chains);
        }
        Some(network)
    }

    /// A network's or a route's `nodes`. The keys are identifiers rather than
    /// format vocabulary, so the unrecognised-key rule does not apply to them.
    ///
    /// A node that will not parse is dropped (SPEC 6.2). An arc naming it is
    /// then dropped under `arc-node-absent`, so the loss shows on everything
    /// that depended on it.
    fn nodes(&mut self, entries: &[(String, Json)], path: &Path) -> Option<Vec<(String, Vertex)>> {
        let members = self.opt_object(entries, "nodes", path)?;
        let nodes_path = path.key("nodes");
        let mut out = Vec::with_capacity(members.len());
        for (key, value) in members {
            let at = nodes_path.key(key);
            let Some(position) = self.position(value, &at, "the node") else {
                continue;
            };
            // SPEC 3.3.1: a node carries no time. The channel goes and the
            // node stays.
            if position.time_ms().is_some() {
                self.report(
                    &at,
                    Cause::ExtraChannel {
                        what: "a node carries no time",
                    },
                );
            }
            out.push((key.clone(), vertex(&position)));
        }
        Some(out)
    }

    /// A network's or a route's `arcs`.
    ///
    /// ⚠ AN ARC THAT IS DROPPED TAKES EVERY CHAIN WALKING IT WITH IT, but not
    /// here: the chain goes when its walk is read, under `pass-arc-absent`,
    /// with a diagnostic of its own (SPEC 6.2).
    fn arcs(
        &mut self,
        entries: &[(String, Json)],
        path: &Path,
        nodes: &Option<Vec<(String, Vertex)>>,
    ) -> Option<Vec<(String, Arc)>> {
        let members = self.opt_object(entries, "arcs", path)?;
        let arcs_path = path.key("arcs");
        Some(
            members
                .iter()
                .filter_map(|(key, value)| {
                    let arc = self.arc(value, &arcs_path.key(key), nodes)?;
                    Some((key.clone(), arc))
                })
                .collect(),
        )
    }

    fn arc(&mut self, value: &Json, path: &Path, nodes: &Option<Vec<(String, Vertex)>>) -> Option<Arc> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, ARC_KEYS, path);
        let from = self.req_string(entries, "from", path)?;
        let to = self.req_string(entries, "to", path)?;
        let pip_items = self.req_array(entries, "pips", path)?.to_vec();
        let pips_path = path.key("pips");
        let positions = self.pips(&pip_items, &pips_path, "the arc")?;
        // SPEC 3.3.2: an arc pip carries no time. One diagnostic for the arc,
        // however many pips carried one.
        if positions.iter().any(|p| p.time_ms().is_some()) {
            self.report(&pips_path, Cause::ArcPipTime);
        }
        let data = self.records(entries, path);
        if from == to {
            self.report(path, Cause::SelfLoopArc { node: from });
            return None;
        }
        // Both ends are checked, so an arc naming two absent nodes reports
        // both.
        let mut absent = false;
        for (key, node) in [("from", &from), ("to", &to)] {
            if !nodes.iter().flatten().any(|(k, _)| k == node) {
                self.report(&path.key(key), Cause::ArcNodeAbsent { node: node.clone() });
                absent = true;
            }
        }
        if absent {
            return None;
        }
        Some(Arc {
            from,
            to,
            pips: positions.iter().map(vertex).collect(),
            data,
        })
    }

    fn chain(&mut self, value: &Json, path: &Path, arcs: &Option<Vec<(String, Arc)>>) -> Option<Chain> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, CHAIN_KEYS, path);
        let chain = Chain {
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            hidden: self.opt_bool(entries, "hidden", path),
            style: self.style(entries, path),
            data: self.records(entries, path),
            walk: Vec::new(),
        };
        let walk = self.walk(entries, path, arcs, false)?;
        Some(Chain { walk, ..chain })
    }

    /// A chain's or a route's REQUIRED `walk`, checked in SPEC 6.2's order.
    /// `None` drops the owner.
    ///
    /// ⚠ A WALK IS USED WHOLE OR NOT AT ALL (SPEC 6.2). A walk with one pass
    /// removed is a different walk, not a shorter one: two arcs joined that do
    /// not meet, a direction reversed by a lost turn, a start moved to a node.
    /// So the first fault drops the chain, it alone is reported, and nothing
    /// after it in the walk is examined. Diagnostics that do not drop the
    /// chain are recorded as they are met.
    fn walk(
        &mut self,
        entries: &[(String, Json)],
        owner: &Path,
        arcs: &Option<Vec<(String, Arc)>>,
        route: bool,
    ) -> Option<Vec<Pass>> {
        let items = self.req_array(entries, "walk", owner)?.to_vec();
        let walk_path = owner.key("walk");
        let mut cursor = Cursor::new(items.len(), route);
        let mut walk = Vec::with_capacity(items.len());
        for (i, item) in items.iter().enumerate() {
            let pass = match self.pass(item, i, &walk_path, arcs) {
                Ok((pass, arc)) => cursor.step(i, &pass, arc).map(|_| pass),
                Err(fault) => Err(fault),
            };
            match pass {
                Ok(pass) => walk.push(pass),
                Err(fault) => {
                    self.report(&fault.path(&walk_path), fault.cause);
                    return None;
                }
            }
        }
        Some(walk)
    }

    /// Read one pass: SPEC 6.2's steps 1 to 4. Steps 5 to 8 need the walk
    /// around it and are the [`Cursor`]'s.
    fn pass<'a>(
        &mut self,
        item: &Json,
        i: usize,
        walk_path: &Path,
        arcs: &'a Option<Vec<(String, Arc)>>,
    ) -> Result<(Pass, &'a Arc), WalkFault> {
        let at = walk_path.index(i);
        let fault = |key: Option<&'static str>, cause: Cause| WalkFault { pass: i, key, cause };

        // 1. The pass is an object naming an arc of its owner. A pass whose
        // arc is absent resolves in no part, so the pass itself is named.
        let Json::Object(members) = item else {
            return Err(fault(
                None,
                Cause::PassArcAbsent {
                    detail: format!("the pass is {}, not an object", item.type_name()),
                },
            ));
        };
        let named = members.iter().find(|(k, _)| k == "arc").map(|(_, v)| v);
        let arc = match named {
            Some(Json::String(key)) => arcs
                .iter()
                .flatten()
                .find(|(k, _)| k == key)
                .map(|(k, a)| (k.clone(), a)),
            _ => None,
        };
        let Some((arc_key, arc)) = arc else {
            let what = match named {
                None => "no arc".to_string(),
                Some(Json::String(key)) => format!("arc `{key}`, which is not here"),
                Some(other) => format!("{} for its arc", other.type_name()),
            };
            return Err(fault(
                None,
                Cause::PassArcAbsent {
                    detail: format!("the pass names {what}"),
                },
            ));
        };

        // 2. Unrecognised keys. 3. `pinned`, which reads `false` as absent.
        self.check_keys(members, PASS_KEYS, &at);
        let mut pass = Pass::over(&arc_key);
        pass.pinned = self.opt_bool(members, "pinned", &at).unwrap_or(false);

        // 4. `from`, `to` and `turn`, in that order: each parses.
        for key in ["from", "to", "turn"] {
            let Some(value) = self.field(members, key) else {
                continue;
            };
            let endpoint = match value {
                Json::String(node) if key != "turn" => Endpoint::Node(node.clone()),
                Json::Array(channels) => {
                    let position = parse_position(channels).map_err(|detail| {
                        fault(
                            Some(key),
                            Cause::PositionUnusable {
                                detail,
                                dropped: "the chain",
                            },
                        )
                    })?;
                    // SPEC 3.3.3: longitude and latitude only. The rest goes
                    // and the pass stays.
                    if position.ele().is_some() || position.time_ms().is_some() {
                        self.report(
                            &at.key(key),
                            Cause::ExtraChannel {
                                what: "a walk position carries longitude and latitude only",
                            },
                        );
                    }
                    Endpoint::At(LonLat {
                        lon: position.lon(),
                        lat: position.lat(),
                    })
                }
                other => {
                    let expected = if key == "turn" {
                        "a position"
                    } else {
                        "a node key or a position"
                    };
                    return Err(fault(
                        Some(key),
                        Cause::PositionUnusable {
                            detail: format!("expected {expected} but found {}", other.type_name()),
                            dropped: "the chain",
                        },
                    ));
                }
            };
            match (key, endpoint) {
                ("from", e) => pass.from = Some(e),
                ("to", e) => pass.to = Some(e),
                (_, Endpoint::At(p)) => pass.turn = Some(p),
                (_, Endpoint::Node(_)) => unreachable!("a turn is never read as a node key"),
            }
        }
        Ok((pass, arc))
    }

    fn route(&mut self, value: &Json, path: &Path) -> Option<Route> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, ROUTE_KEYS, path);
        let nodes = self.nodes(entries, path);
        let arcs = self.arcs(entries, path, &nodes);
        let route = Route {
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            folder: self.opt_string(entries, "folder", path),
            hidden: self.opt_bool(entries, "hidden", path),
            style: self.style(entries, path),
            data: self.records(entries, path),
            walk: Vec::new(),
            nodes,
            arcs,
        };
        // Where a chain would be dropped, the route is: it IS the chain
        // (SPEC 3.4).
        let walk = self.walk(entries, path, &route.arcs, true)?;
        Some(Route { walk, ..route })
    }

    fn track(&mut self, value: &Json, path: &Path) -> Option<Track> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, TRACK_KEYS, path);
        let segment_items = self.req_array(entries, "segments", path)?.to_vec();
        let segments_path = path.key("segments");
        let segments = segment_items
            .iter()
            .enumerate()
            .filter_map(|(i, item)| self.segment(item, &segments_path.index(i)))
            .collect();
        Some(Track {
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            folder: self.opt_string(entries, "folder", path),
            hidden: self.opt_bool(entries, "hidden", path),
            style: self.style(entries, path),
            started: self.timestamp(entries, "started", path),
            ended: self.timestamp(entries, "ended", path),
            segments,
            data: self.records(entries, path),
        })
    }

    fn segment(&mut self, value: &Json, path: &Path) -> Option<Segment> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, SEGMENT_KEYS, path);
        let pip_items = self.req_array(entries, "pips", path)?.to_vec();
        // A track has segments rather than arcs, and a segment is the unit of
        // continuous geometry: segments are already discontinuous from each
        // other (a pause), so dropping one leaves a gap a consumer can see,
        // where dropping a pip inside one would not. SPEC 6.2 names the arc;
        // this is that rule applied to the shape a track has.
        Some(Segment {
            pips: self.pips(&pip_items, &path.key("pips"), "the segment")?,
        })
    }

    fn waypoint(&mut self, value: &Json, path: &Path, known: &[&str]) -> Option<Waypoint> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, known, path);
        let at = match self.field(entries, "at") {
            None => {
                self.report(&path.key("at"), Cause::MissingRequiredKey { key: "at" });
                return None;
            }
            Some(value) => self.required_position(value, &path.key("at"))?,
        };
        Some(Waypoint {
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            folder: self.opt_string(entries, "folder", path),
            hidden: self.opt_bool(entries, "hidden", path),
            style: self.style(entries, path),
            at,
            data: self.records(entries, path),
        })
    }

    fn area(&mut self, value: &Json, path: &Path, known: &[&str]) -> Option<Area> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, known, path);
        let items = self.req_array(entries, "polygon", path)?.to_vec();
        let polygon = self.ring(&items, &path.key("polygon"))?;
        Some(Area {
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            folder: self.opt_string(entries, "folder", path),
            hidden: self.opt_bool(entries, "hidden", path),
            style: self.style(entries, path),
            polygon,
            data: self.records(entries, path),
        })
    }

    /// SPEC 3.7: one OPEN ring of at least three distinct positions.
    ///
    /// A nested or additional ring drops the AREA, not the extra ring, which
    /// would keep an area whose hole had vanished with nothing to say so.
    ///
    /// ⚠ A CLOSED RING IS REPAIRED, NOT DROPPED. A last position repeating the
    /// first is one fact written twice, so dropping the repeat loses nothing;
    /// the diagnostic is what makes a writer that closes rings visible. The
    /// distinct positions are counted after that, so `[A, B, A]` is two points
    /// and its area goes.
    fn ring(&mut self, items: &[Json], path: &Path) -> Option<Vec<Position>> {
        let nested = items
            .first()
            .and_then(Json::as_array)
            .and_then(|first| first.first())
            .is_some_and(|c| matches!(c, Json::Array(_)));
        if nested {
            self.report(path, Cause::PolygonNestedRings);
            return None;
        }
        let mut ring = Vec::with_capacity(items.len());
        for (i, item) in items.iter().enumerate() {
            // A ring position that will not parse drops the AREA: a dropped
            // vertex would move the boundary with nothing to say so.
            ring.push(self.position(item, &path.index(i), "the area")?);
        }
        if ring.len() >= 2 && ring[0].same_point(&ring[ring.len() - 1]) {
            self.report(&path.index(ring.len() - 1), Cause::RingClosed);
            ring.pop();
        }
        let distinct = distinct_points(&ring, 3);
        if distinct < 3 {
            self.report(path, Cause::RingTooFewPoints { distinct });
            return None;
        }
        Some(ring)
    }

    fn object<'a>(&mut self, value: &'a Json, path: &Path) -> Option<&'a [(String, Json)]> {
        match value {
            Json::Object(entries) => Some(entries),
            other => {
                self.report(
                    path,
                    Cause::WrongTypeRequired {
                        expected: "an object",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    /// SPEC 6.2: a `folder` naming a folder absent from the document is not an
    /// unusable value. The key is treated as absent, so the item sits at the
    /// top level, and a diagnostic is recorded. Dropping the item would lose
    /// content over a lost grouping.
    ///
    /// The same treatment is applied to a folder's own `parent`, which SPEC
    /// 6.2 does not name. That is this crate's reading, reported as such.
    fn resolve_folder_references(&mut self, document: &mut Document) {
        let known: Vec<String> = document
            .folders
            .iter()
            .flatten()
            .map(|f| f.id.clone())
            .collect();
        let root = Path::root();
        let mut unresolved: Vec<(Path, String)> = Vec::new();

        {
            let mut sweep = |slot: &mut Option<String>, path: Path, key: &str| {
                let Some(id) = slot.as_ref() else { return };
                if known.iter().any(|k| k == id) {
                    return;
                }
                unresolved.push((path.key(key), id.clone()));
                *slot = None;
            };

            for (i, folder) in document.folders.iter_mut().flatten().enumerate() {
                sweep(&mut folder.parent, root.key("folders").index(i), "parent");
            }
            for (i, item) in document.networks.iter_mut().flatten().enumerate() {
                sweep(&mut item.folder, root.key("networks").index(i), "folder");
            }
            for (i, item) in document.routes.iter_mut().flatten().enumerate() {
                sweep(&mut item.folder, root.key("routes").index(i), "folder");
            }
            for (i, item) in document.tracks.iter_mut().flatten().enumerate() {
                sweep(&mut item.folder, root.key("tracks").index(i), "folder");
            }
            for (i, item) in document.waypoints.iter_mut().flatten().enumerate() {
                sweep(&mut item.folder, root.key("waypoints").index(i), "folder");
            }
            for (i, item) in document.areas.iter_mut().flatten().enumerate() {
                sweep(&mut item.folder, root.key("areas").index(i), "folder");
            }
        }

        for (path, id) in unresolved {
            self.report(&path, Cause::FolderAbsent { id });
        }
    }
}

/// SPEC 4.1. Every channel is kept, including one this reader does not know,
/// so that a future channel is additive. `Err` carries why the array is not a
/// position.
fn parse_position(items: &[Json]) -> Result<Position, String> {
    if items.len() < 2 {
        return Err(format!(
            "a position carries at least longitude and latitude, found {} channel(s)",
            items.len()
        ));
    }
    let mut channels = Vec::with_capacity(items.len());
    for (index, item) in items.iter().enumerate() {
        match item {
            Json::Number(n) => channels.push(Some(*n)),
            // SPEC 4.1: `null` marks an absent value from index 2 upward.
            Json::Null if index >= 2 => channels.push(None),
            Json::Null => {
                return Err(format!(
                    "channel {index} is null, and {} is never absent",
                    if index == 0 { "longitude" } else { "latitude" }
                ))
            }
            other => return Err(format!("channel {index} is {}", other.type_name())),
        }
    }
    let (lon, lat) = (
        channels[0].expect("channel 0 is a number"),
        channels[1].expect("channel 1 is a number"),
    );
    if let Some(problem) = off_the_globe(lon, lat) {
        return Err(problem);
    }
    Ok(Position::new(channels))
}

/// SPEC 4.1's normative ranges, both bounds included. `None` when in range.
pub(crate) fn off_the_globe(lon: f64, lat: f64) -> Option<String> {
    if !(-180.0..=180.0).contains(&lon) {
        return Some(format!("longitude {lon} is off the globe"));
    }
    if !(-90.0..=90.0).contains(&lat) {
        return Some(format!("latitude {lat} is off the globe"));
    }
    None
}

/// A node or an arc pip: longitude, latitude and elevation. A time channel,
/// and any channel after it, is not carried (SPEC 3.3.1, 3.3.2); the caller
/// reports a time.
fn vertex(position: &Position) -> Vertex {
    Vertex {
        lon: position.lon(),
        lat: position.lat(),
        ele: position.ele(),
    }
}

/// How many of `ring`'s positions differ in longitude or latitude, counting no
/// further than `enough`. SPEC 3.7 lets no other channel take part.
///
/// The count stops at `enough` because the only question asked is whether a
/// ring reaches three, and a long ring would otherwise cost a quadratic scan.
fn distinct_points(ring: &[Position], enough: usize) -> usize {
    let mut seen: Vec<&Position> = Vec::with_capacity(enough);
    for p in ring {
        if seen.len() == enough {
            break;
        }
        if !seen.iter().any(|q| q.same_point(p)) {
            seen.push(p);
        }
    }
    seen.len()
}

/// SPEC 2.2. Three runs of decimal digits separated by full stops. A leading
/// zero is legal and the comparison is numeric, so `00.02.0000` is 0.2.0.
fn parse_version(text: &str) -> Result<Version, Refusal> {
    let malformed = || Refusal::VersionMalformed {
        found: text.to_string(),
    };
    let mut parts = text.split('.');
    let mut numbers = [0u64; 3];
    for slot in &mut numbers {
        let part = parts.next().ok_or_else(malformed)?;
        if part.is_empty() || !part.bytes().all(|b| b.is_ascii_digit()) {
            return Err(malformed());
        }
        // A component has no bound on length, so a run of digits longer than a
        // u64 saturates rather than wrapping. Saturating high can only make a
        // document look newer, which refuses rather than half-reads it.
        *slot = part.parse().unwrap_or(u64::MAX);
    }
    if parts.next().is_some() {
        return Err(malformed());
    }
    let version = Version {
        text: text.to_string(),
        major: numbers[0],
        minor: numbers[1],
        patch: numbers[2],
    };
    // SPEC 2.2, through 0.x: EACH MINOR IS ITS OWN SERIES, so the test is
    // "not this reader's series", NOT "above it". 0.0 is not an older spelling
    // of 0.1 and this reader has no rules for it, so `0.0.9` is refused for
    // the same reason `0.2.0` is. PATCH never refuses, at any version.
    if (version.major, version.minor) != SUPPORTED {
        return Err(Refusal::VersionUnsupportedSeries {
            found: text.to_string(),
            major: version.major,
            minor: version.minor,
        });
    }
    Ok(version)
}

/// SPEC 4.3: `YYYY-MM-DDTHH:MM:SS` with OPTIONAL fractional seconds and a
/// literal `Z`. An offset other than `Z` is not permitted.
///
/// SHAPE ONLY. `2026-13-45T99:99:99Z` is accepted, and a reader MUST NOT
/// refuse it on calendar grounds. Ruled 2026-09-09 by consistency rather than
/// on the merits: SPEC 6.2 has a reader check a value only where this document
/// states a normative range or enumeration, which is the same rule that leaves
/// `icon-size` unchecked. A reader validating calendars would refuse documents
/// every other library accepts.
fn is_utc_timestamp(text: &str) -> bool {
    let Some(body) = text.strip_suffix('Z') else {
        return false;
    };
    let bytes = body.as_bytes();
    if bytes.len() < 19 {
        return false;
    }
    let digits = |range: std::ops::Range<usize>| bytes[range].iter().all(u8::is_ascii_digit);
    if !(digits(0..4)
        && bytes[4] == b'-'
        && digits(5..7)
        && bytes[7] == b'-'
        && digits(8..10)
        && bytes[10] == b'T'
        && digits(11..13)
        && bytes[13] == b':'
        && digits(14..16)
        && bytes[16] == b':'
        && digits(17..19))
    {
        return false;
    }
    match bytes.len() {
        19 => true,
        _ => bytes[19] == b'.' && bytes.len() > 20 && digits(20..bytes.len()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn read(text: &str) -> Reading {
        decode(text.as_bytes()).expect("accepted")
    }

    fn refuse(text: &str) -> Refusal {
        decode(text.as_bytes()).expect_err("refused")
    }

    #[test]
    fn versions_that_refuse_and_versions_that_do_not() {
        // Above this reader's series.
        assert!(matches!(
            refuse(r#"{"vnx": "0.3.0"}"#),
            Refusal::VersionUnsupportedSeries { .. }
        ));
        assert!(matches!(
            refuse(r#"{"vnx": "1.0.0"}"#),
            Refusal::VersionUnsupportedSeries { .. }
        ));
        // And BELOW it, which is the same refusal for the same reason: a
        // reader comparing with `>` reads these and passes everything above.
        // A 0.2 reader reads the 0.2 series only (SPEC 2.2), so 0.1 is refused.
        for text in ["0.1.0", "0.1.9", "0.0.9", "0.0.0"] {
            let document = format!(r#"{{"vnx": "{text}"}}"#);
            assert!(
                matches!(
                    refuse(&document),
                    Refusal::VersionUnsupportedSeries { .. }
                ),
                "{text:?} was not refused as a series this reader does not implement"
            );
        }
        for text in ["0.2", "0.2.0.0", "0.2.x", "0.2.+1", "", "0.2.", ".2.0", "0.2.0 "] {
            let document = format!(r#"{{"vnx": "{text}"}}"#);
            assert!(
                matches!(refuse(&document), Refusal::VersionMalformed { .. }),
                "{text:?} was not refused as malformed"
            );
        }
        // PATCH never refuses, and a leading zero is a legal spelling.
        for text in ["0.2.99", "0.2.4294967296", "00.02.0000", "0.2.0"] {
            let document = format!(r#"{{"vnx": "{text}"}}"#);
            let reading = read(&document);
            assert_eq!(
                reading.document.vnx.expect("a version").text,
                text,
                "the declared spelling is kept"
            );
        }
    }

    #[test]
    fn a_document_that_is_not_one() {
        assert!(matches!(
            refuse(r#"{"generator": "x"}"#),
            Refusal::NotAVnxDocument { .. }
        ));
        assert!(matches!(refuse("[]"), Refusal::NotAVnxDocument { .. }));
        assert!(matches!(refuse("7"), Refusal::NotAVnxDocument { .. }));
    }

    #[test]
    fn an_unknown_key_is_diagnosed_and_the_item_kept() {
        let reading = read(
            r#"{"vnx":"0.2.0","waypoints":[{"nmae":"typo","name":"Car park","at":[-3.675,57.1201]}]}"#,
        );
        assert_eq!(reading.diagnostics.len(), 1);
        assert_eq!(reading.diagnostics[0].path, "/waypoints/0/nmae");
        assert_eq!(reading.diagnostics[0].cause, Cause::UnknownKey);
        assert_eq!(reading.document.waypoints.expect("kept").len(), 1);
    }

    #[test]
    fn a_wrong_type_drops_an_optional_field_and_takes_a_required_one() {
        let reading = read(r#"{"vnx":"0.2.0","waypoints":[{"name":7,"at":[-3.6,57.1]}]}"#);
        assert_eq!(reading.document.waypoints.as_ref().expect("kept").len(), 1);
        assert_eq!(reading.document.waypoints.expect("kept")[0].name, None);
        assert_eq!(reading.diagnostics.len(), 1);

        let reading = read(r#"{"vnx":"0.2.0","waypoints":[{"name":"x","at":"here"}]}"#);
        assert_eq!(reading.document.waypoints.expect("kept").len(), 0);
        assert_eq!(reading.diagnostics.len(), 1);
    }

    #[test]
    fn an_absent_collection_is_not_an_empty_one() {
        assert_eq!(read(r#"{"vnx":"0.2.0"}"#).document.waypoints, None);
        assert_eq!(
            read(r#"{"vnx":"0.2.0","waypoints":[]}"#).document.waypoints,
            Some(vec![])
        );
    }

    #[test]
    fn a_position_keeps_every_channel_it_was_given() {
        let reading = read(r#"{"vnx":"0.2.0","waypoints":[{"at":[-3.6,57.1,null,5,99]}]}"#);
        let at = &reading.document.waypoints.expect("kept")[0].at;
        assert_eq!(at.arity(), 5, "an unknown channel is kept, not truncated");
        assert_eq!(at.ele(), None);
        assert_eq!(at.time_ms(), Some(5.0));
    }

    #[test]
    fn longitude_and_latitude_are_never_null() {
        let reading = read(r#"{"vnx":"0.2.0","waypoints":[{"at":[null,57.1]}]}"#);
        assert_eq!(reading.document.waypoints.expect("kept").len(), 0);
        assert!(matches!(
            reading.diagnostics[0].cause,
            Cause::PositionUnusable { .. }
        ));
    }

    #[test]
    fn a_timestamp_must_be_utc() {
        for good in [
            "2026-05-12T07:03:11Z",
            "2026-05-12T07:03:11.123Z",
            "2026-05-12T07:03:60Z",
        ] {
            assert!(is_utc_timestamp(good), "{good}");
        }
        for bad in [
            "2026-05-12T08:03:11+01:00",
            "2026-05-12T08:03:11-00:00",
            "2026-05-12T08:03:11z",
            "2026-05-12T08:03:11",
            "2026-05-12",
            "2026-05-12T08:03:11.Z",
            "2026-05-12 08:03:11Z",
            "not a date",
        ] {
            assert!(!is_utc_timestamp(bad), "{bad}");
        }
    }

    /// SPEC 4.3 is checked for SHAPE ONLY (ruled 2026-09-09). A calendar that
    /// cannot exist is still a well-shaped timestamp, and refusing it would
    /// refuse documents every other library accepts.
    #[test]
    fn a_timestamp_is_not_checked_against_a_calendar() {
        for impossible in [
            "2026-13-45T99:99:99Z",
            "2026-00-00T00:00:00Z",
            "0000-99-99T99:99:99.999999Z",
        ] {
            assert!(is_utc_timestamp(impossible), "{impossible}");
        }
    }

    #[test]
    fn an_out_of_range_importance_is_dropped_and_the_rest_of_the_style_kept() {
        let reading = read(
            r#"{"vnx":"0.2.0","waypoints":[{"at":[-3.6,57.1],"style":{"importance":9,"color":"x"}}]}"#,
        );
        let style = reading.document.waypoints.expect("kept")[0]
            .style
            .clone()
            .expect("a style");
        assert!(style.get("importance").is_none());
        assert!(style.get("color").is_some(), "other style keys are opaque");
        assert_eq!(reading.diagnostics[0].cause, Cause::ImportanceOutOfRange);
    }

    /// SPEC 5.1 rules 2 and 3: a record body is opaque and an unrecognised
    /// type is inert on any host. Red-proof for the reading that keeps
    /// SPEC 6.2's `size` example unimplemented.
    #[test]
    fn a_record_body_is_never_inspected() {
        let reading = read(
            r#"{"vnx":"0.2.0","waypoints":[{"at":[-3.6,57.1],"data":[
                 {"@type":"app.viatrail.settlement","size":"metropolis"},
                 {"@type":"com.example.x","nmae":1,"deep":{"a":[1,2,{"b":null}]}}]}]}"#,
        );
        assert_eq!(reading.diagnostics, vec![], "a record body raises nothing");
        let data = reading.document.waypoints.expect("kept")[0]
            .data
            .clone()
            .expect("data");
        assert_eq!(data.len(), 2);
        assert_eq!(data[1].entries().len(), 3, "the body survives whole");
    }

    #[test]
    fn a_record_with_no_type_is_dropped_and_the_bag_survives() {
        let reading = read(
            r#"{"vnx":"0.2.0","waypoints":[{"at":[-3.6,57.1],"data":[{"value":1},{"@type":"a.b"}]}]}"#,
        );
        assert_eq!(reading.diagnostics[0].cause, Cause::RecordTypeMissing);
        assert_eq!(
            reading.document.waypoints.expect("kept")[0]
                .data
                .as_ref()
                .expect("data")
                .len(),
            1
        );
    }

    #[test]
    fn an_absent_folder_leaves_the_item_at_the_top_level() {
        let reading = read(r#"{"vnx":"0.2.0","waypoints":[{"at":[-3.6,57.1],"folder":"ghost"}]}"#);
        assert_eq!(
            reading.diagnostics[0].cause,
            Cause::FolderAbsent {
                id: "ghost".into()
            }
        );
        assert_eq!(reading.document.waypoints.expect("kept")[0].folder, None);
    }

    #[test]
    fn a_folder_with_no_id_is_dropped_and_its_contents_survive() {
        let reading = read(
            r#"{"vnx":"0.2.0","folders":[{"name":"Nameless"}],"waypoints":[{"at":[-3.6,57.1],"folder":"gone"}]}"#,
        );
        assert_eq!(reading.document.folders.as_ref().expect("kept").len(), 0);
        assert_eq!(reading.document.waypoints.as_ref().expect("kept").len(), 1);
        assert_eq!(
            pairs(&reading),
            vec![
                ("/folders/0/id".into(), "wrong-type-required"),
                ("/waypoints/0/folder".into(), "folder-absent"),
            ]
        );
    }

    /// The diagnostics as `(path, code)` pairs, sorted, which is all SPEC 6.3
    /// lets a caller compare.
    fn pairs(reading: &Reading) -> Vec<(String, &'static str)> {
        let mut out: Vec<(String, &'static str)> = reading
            .diagnostics
            .iter()
            .map(|d| (d.path.clone(), d.code()))
            .collect();
        out.sort();
        out
    }

    /// SPEC 3.7: a ring is stored open. A closed one loses its repeat and
    /// keeps its area, and the diagnostic names the repeat by its index.
    #[test]
    fn a_closed_ring_loses_its_repeat_and_keeps_its_area() {
        let reading = read(
            r#"{"vnx":"0.2.0","areas":[{"polygon":[[-3.7,57.09],[-3.6,57.09],[-3.6,57.0],[-3.7,57.09]]}]}"#,
        );
        assert_eq!(pairs(&reading), vec![("/areas/0/polygon/3".into(), "ring-closed")]);
        let areas = reading.document.areas.expect("kept");
        assert_eq!(areas[0].polygon.len(), 3, "only the repeat goes");
    }

    /// SPEC 3.7: fewer than three distinct positions drops the area. Distinct
    /// is longitude and latitude only, and it is counted after a closing
    /// repeat is gone, so `[A, B, A]` is two points and takes both codes.
    #[test]
    fn a_ring_of_fewer_than_three_distinct_positions_drops_its_area() {
        let reading = read(
            r#"{"vnx":"0.2.0","areas":[
                 {"polygon":[[-3.7,57.09],[-3.6,57.09],[-3.7,57.09]]},
                 {"polygon":[[-3.7,57.09,1],[-3.7,57.09,2],[-3.6,57.09]]},
                 {"polygon":[[-3.7,57.09],[-3.6,57.09],[-3.6,57.0]]}]}"#,
        );
        assert_eq!(
            pairs(&reading),
            vec![
                ("/areas/0/polygon".into(), "ring-too-few-points"),
                ("/areas/0/polygon/2".into(), "ring-closed"),
                ("/areas/1/polygon".into(), "ring-too-few-points"),
            ]
        );
        assert_eq!(reading.document.areas.expect("kept").len(), 1);
    }

    fn network_with(chains: &str) -> String {
        format!(
            r#"{{"vnx":"0.2.0","networks":[{{
                 "nodes":{{"a":[-3.60,57.10],"b":[-3.59,57.10],"c":[-3.58,57.10],"d":[-3.57,57.10]}},
                 "arcs":{{"ab":{{"from":"a","to":"b","pips":[[-3.595,57.101]]}},
                          "cb":{{"from":"c","to":"b","pips":[]}},
                          "cd":{{"from":"c","to":"d","pips":[]}}}},
                 "chains":{chains}}}]}}"#
        )
    }

    fn walk_verdict(walk: &str) -> Vec<(String, &'static str)> {
        let reading = read(&network_with(&format!(r#"[{{"walk":{walk}}}]"#)));
        let kept = reading.document.networks.as_ref().expect("kept")[0]
            .chains
            .as_ref()
            .expect("chains")
            .len();
        let found = pairs(&reading);
        // A walk is used whole or not at all: a fault that drops the chain
        // leaves no chain, and a clean walk leaves one.
        let drops = found.iter().any(|(_, c)| {
            !matches!(*c, "unknown-key" | "wrong-type-optional" | "extra-channel")
        });
        assert_eq!(kept, usize::from(!drops), "{walk} kept {kept} chain(s) with {found:?}");
        found
    }

    /// SPEC 6.2's check order for a walk, one rule at a time. Each walk breaks
    /// exactly one rule, and the path says which part broke it.
    #[test]
    fn each_walk_rule_takes_its_own_code_at_its_own_path() {
        let at = |path: &str, code: &'static str| vec![(format!("/networks/0/chains/0/walk/{path}"), code)];
        let cases: Vec<(&str, Vec<(String, &'static str)>)> = vec![
            // Valid: every form in one walk.
            (
                r#"[{"arc":"ab","from":[-3.597,57.1005],"to":"b"},{"arc":"cb","pinned":true},
                    {"arc":"cd","turn":[-3.574,57.1]},{"arc":"cb"},{"arc":"ab","to":[-3.598,57.1]}]"#,
                vec![],
            ),
            (r#"[]"#, vec![]),
            // 1. pass-arc-absent, at the pass.
            (r#"[{"arc":"ghost","from":"a","to":"b"}]"#, at("0", "pass-arc-absent")),
            (r#"[7]"#, at("0", "pass-arc-absent")),
            (r#"[{"from":"a","to":"b"}]"#, at("0", "pass-arc-absent")),
            // 2. an unknown key is reported and the chain kept.
            (r#"[{"arc":"ab","from":"a","to":"b","via":1}]"#, at("0/via", "unknown-key")),
            // 3. `pinned` of the wrong type drops the field only.
            (r#"[{"arc":"ab","from":"a","to":"b","pinned":"yes"}]"#, at("0/pinned", "wrong-type-optional")),
            // 4. a position that will not parse, or a node key as a turn.
            (r#"[{"arc":"ab","from":"a","to":"b","turn":"b"}]"#, at("0/turn", "position-unusable")),
            (r#"[{"arc":"ab","from":[null,57.1],"to":"b"}]"#, at("0/from", "position-unusable")),
            // 4. an elevation on a walk position is dropped, the pass kept.
            (r#"[{"arc":"ab","from":[-3.597,57.1005,410],"to":"b"}]"#, at("0/from", "extra-channel")),
            // 6. a turn and a pin together.
            (r#"[{"arc":"ab","from":"a","to":"a","turn":[-3.595,57.101],"pinned":true}]"#, at("0", "pinned-turn")),
            // 7. the rule for `from` and `to`.
            (r#"[{"arc":"ab","to":"b"}]"#, at("0", "walk-spelling")),
            (r#"[{"arc":"ab","from":"a"}]"#, at("0", "walk-spelling")),
            (r#"[{"arc":"ab","from":"a","to":"b"},{"arc":"cb","to":"c"}]"#, at("0/to", "walk-spelling")),
            (r#"[{"arc":"ab","from":"a"},{"arc":"cb","from":"b","to":"c"}]"#, at("1/from", "walk-spelling")),
            (r#"[{"arc":"ab","from":[-3.597,57.1005]},{"arc":"cb","to":"c"}]"#, at("0", "walk-spelling")),
            // 8. the walk is connected.
            (r#"[{"arc":"ab","from":"c","to":"b"}]"#, at("0/from", "walk-not-connected")),
            (r#"[{"arc":"ab","from":"a","to":"d"}]"#, at("0/to", "walk-not-connected")),
            (r#"[{"arc":"ab","from":"a"},{"arc":"cd","to":"d"}]"#, at("1", "walk-not-connected")),
            (
                r#"[{"arc":"ab","from":[-3.597,57.1005],"to":[-3.596,57.1005]},{"arc":"cb","to":"c"}]"#,
                at("0/to", "walk-not-connected"),
            ),
        ];
        for (walk, want) in cases {
            assert_eq!(walk_verdict(walk), want, "{walk}");
        }
    }

    /// SPEC 6.2: the first fault drops the chain, and NOTHING after it in the
    /// walk is examined, so a second fault further on is not reported.
    #[test]
    fn only_the_first_fault_in_a_walk_is_reported() {
        assert_eq!(
            walk_verdict(r#"[{"arc":"ab","to":"b"},{"arc":"ghost"},{"arc":"cd","turn":"x"}]"#),
            vec![("/networks/0/chains/0/walk/0".into(), "walk-spelling")]
        );
        // Within one pass, the steps run in order: an absent arc is found
        // before the unknown key on the same pass, and the key is not reported.
        assert_eq!(
            walk_verdict(r#"[{"arc":"ghost","via":1}]"#),
            vec![("/networks/0/chains/0/walk/0".into(), "pass-arc-absent")]
        );
    }

    /// SPEC 3.3.3: a pass without `to` leaves by the other end of its arc, or
    /// by the node it entered when it has a turn. The next pass must enter
    /// there.
    #[test]
    fn a_pass_leaves_where_its_turn_and_arc_say() {
        // Enter ab at a, no turn: leave at b, so cb is next and cd is not.
        assert_eq!(walk_verdict(r#"[{"arc":"ab","from":"a"},{"arc":"cb","to":"c"}]"#), vec![]);
        // Enter ab at a and turn: leave at a again, so cb (b-c) does not join.
        assert_eq!(
            walk_verdict(r#"[{"arc":"ab","from":"a","turn":[-3.595,57.101]},{"arc":"cb","to":"c"}]"#),
            vec![("/networks/0/chains/0/walk/1".into(), "walk-not-connected")]
        );
    }

    /// SPEC 3.4: a route's walk runs node to node. A position, a turn or a
    /// pin drops the ROUTE, at the key that carries it.
    #[test]
    fn a_route_walk_carries_no_position_turn_or_pin() {
        let route = |walk: &str| {
            read(&format!(
                r#"{{"vnx":"0.2.0","routes":[{{
                     "nodes":{{"a":[-3.60,57.10],"b":[-3.59,57.10]}},
                     "arcs":{{"ab":{{"from":"a","to":"b","pips":[]}}}},
                     "walk":{walk}}}]}}"#
            ))
        };
        let clean = route(r#"[{"arc":"ab","from":"b","to":"a"}]"#);
        assert_eq!(pairs(&clean), vec![]);
        assert_eq!(clean.document.routes.expect("kept").len(), 1);
        for (walk, key) in [
            (r#"[{"arc":"ab","from":[-3.6,57.1],"to":"b"}]"#, "from"),
            (r#"[{"arc":"ab","from":"a","to":[-3.595,57.1]}]"#, "to"),
            (r#"[{"arc":"ab","from":"a","to":"a","turn":[-3.595,57.1]}]"#, "turn"),
            (r#"[{"arc":"ab","from":"a","to":"b","pinned":true}]"#, "pinned"),
        ] {
            let reading = route(walk);
            assert_eq!(
                pairs(&reading),
                vec![(format!("/routes/0/walk/0/{key}"), "route-pin-or-position")],
                "{walk}"
            );
            assert_eq!(reading.document.routes.expect("kept").len(), 0, "{walk}");
        }
        // `"pinned": false` is absent (SPEC 3.3.3), so it is no pin.
        assert_eq!(pairs(&route(r#"[{"arc":"ab","from":"a","to":"b","pinned":false}]"#)), vec![]);
    }

    /// SPEC 3.3.1, 3.3.2: a node and an arc pip carry no time. The time goes,
    /// the node or pip stays, and an arc is reported ONCE however many of its
    /// pips carried one.
    #[test]
    fn a_time_on_a_node_or_an_arc_pip_is_dropped_and_reported_once() {
        let reading = read(
            r#"{"vnx":"0.2.0","networks":[{
                 "nodes":{"a":[-3.60,57.10,410,1778914991000],"b":[-3.59,57.10,null,5]},
                 "arcs":{"ab":{"from":"a","to":"b","pips":[[-3.597,57.1,1,2],[-3.595,57.1,null,3]]}}}]}"#,
        );
        assert_eq!(
            pairs(&reading),
            vec![
                ("/networks/0/arcs/ab/pips".into(), "arc-pip-time"),
                ("/networks/0/nodes/a".into(), "extra-channel"),
                ("/networks/0/nodes/b".into(), "extra-channel"),
            ]
        );
        let network = &reading.document.networks.expect("kept")[0];
        assert_eq!(
            network.node("a"),
            Some(&Vertex { lon: -3.60, lat: 57.10, ele: Some(410.0) }),
            "the elevation stays"
        );
        assert_eq!(network.arc("ab").expect("kept").pips.len(), 2);
    }

    /// SPEC 3.3.2, 6.2: a self-loop, or an arc naming an absent node, drops
    /// the ARC now, and each chain walking it goes under `pass-arc-absent`.
    #[test]
    fn a_dropped_arc_takes_the_chains_that_walk_it() {
        let reading = read(
            r#"{"vnx":"0.2.0","networks":[{
                 "nodes":{"a":[-3.60,57.10],"b":[-3.59,57.10]},
                 "arcs":{"ab":{"from":"a","to":"b","pips":[]},
                         "loop":{"from":"a","to":"a","pips":[[-3.6,57.2]]},
                         "gone":{"from":"x","to":"y","pips":[]}},
                 "chains":[{"walk":[{"arc":"ab","from":"a","to":"b"}]},
                           {"walk":[{"arc":"loop","from":"a","to":"a"}]},
                           {"walk":[{"arc":"ab","from":"a"},{"arc":"gone","to":"y"}]}]}]}"#,
        );
        assert_eq!(
            pairs(&reading),
            vec![
                ("/networks/0/arcs/gone/from".into(), "arc-node-absent"),
                ("/networks/0/arcs/gone/to".into(), "arc-node-absent"),
                ("/networks/0/arcs/loop".into(), "self-loop-arc"),
                ("/networks/0/chains/1/walk/0".into(), "pass-arc-absent"),
                ("/networks/0/chains/2/walk/1".into(), "pass-arc-absent"),
            ]
        );
        let network = &reading.document.networks.expect("kept")[0];
        assert_eq!(network.arcs.as_ref().expect("arcs").len(), 1);
        assert_eq!(network.chains.as_ref().expect("chains").len(), 1, "only the sound chain stays");
    }

    /// A node that will not parse is dropped, and the arc naming it goes with
    /// it under `arc-node-absent`.
    #[test]
    fn a_dropped_node_takes_the_arcs_that_name_it() {
        let reading = read(
            r#"{"vnx":"0.2.0","networks":[{
                 "nodes":{"a":[-3.60,57.10],"b":"here"},
                 "arcs":{"ab":{"from":"a","to":"b","pips":[]}}}]}"#,
        );
        assert_eq!(
            pairs(&reading),
            vec![
                ("/networks/0/arcs/ab/to".into(), "arc-node-absent"),
                ("/networks/0/nodes/b".into(), "position-unusable"),
            ]
        );
    }

    /// SPEC 6.2's table puts an absent REQUIRED key in the row of a wrong-typed
    /// one, and the diagnostic names the key.
    #[test]
    fn an_absent_required_key_is_reported_at_the_key() {
        let reading = read(r#"{"vnx":"0.2.0","waypoints":[{"name":"x"}],"networks":[{"chains":[{}]}]}"#);
        assert_eq!(
            pairs(&reading),
            vec![
                ("/networks/0/chains/0/walk".into(), "wrong-type-required"),
                ("/waypoints/0/at".into(), "wrong-type-required"),
            ]
        );
    }

    /// SPEC 6.2: a position inside `pips` that will not parse drops the ARC or
    /// SEGMENT that holds it, NOT the position. "GEOMETRY IS CONTINUOUS AND
    /// STRUCTURE IS NOT" — keeping the other pips leaves a shorter, plausible,
    /// wrong line with nothing to indicate it.
    ///
    /// This crate kept the other pips until 2026-09-10, which is the reading
    /// SPEC 6.2 explicitly rules out.
    #[test]
    fn one_unusable_pip_drops_the_segment_that_holds_it() {
        let reading = read(
            r#"{"vnx":"0.2.0","tracks":[{"segments":[
                 {"pips":[[-3.6,57.1],["x",1],[-3.5,57.2]]},
                 {"pips":[[-3.4,57.3],[-3.3,57.4]]}]}]}"#,
        );
        let track = &reading.document.tracks.expect("kept")[0];
        assert_eq!(track.segments.len(), 1, "the bad segment goes, whole");
        assert_eq!(track.segments[0].pips.len(), 2, "the good segment stays");
        assert_eq!(reading.diagnostics.len(), 1);
        assert_eq!(reading.diagnostics[0].code(), "position-unusable");
        assert!(
            reading.diagnostics[0].message().contains("the segment is dropped"),
            "the message must name WHAT was dropped: {}",
            reading.diagnostics[0].message()
        );
    }

    /// The same rule on an arc, where SPEC 6.2 states it in those words.
    #[test]
    fn one_unusable_pip_drops_the_arc_that_holds_it() {
        let reading = read(
            r#"{"vnx":"0.2.0","networks":[{
                 "nodes":{"n1":[-3.68,57.12],"n2":[-3.66,57.1]},
                 "arcs":{"bad":{"from":"n1","to":"n2","pips":[[-3.67,57.11],[null,57.11]]},
                         "good":{"from":"n1","to":"n2","pips":[[-3.67,57.11]]}}}]}"#,
        );
        let network = &reading.document.networks.expect("kept")[0];
        let arcs = network.arcs.as_ref().expect("arcs");
        assert_eq!(arcs.len(), 1, "the bad arc goes, whole");
        assert_eq!(arcs[0].0, "good");
        assert!(reading
            .diagnostics
            .iter()
            .any(|d| d.code() == "position-unusable"
                && d.message().contains("the arc is dropped")));
    }

    /// A ring vertex that will not parse drops the AREA, for the same reason:
    /// dropping the vertex would move the boundary with nothing to say so.
    #[test]
    fn one_unusable_ring_vertex_drops_the_area() {
        let reading = read(
            r#"{"vnx":"0.2.0","areas":[
                 {"polygon":[[-3.7,57.09],[-3.6],[-3.6,57.0]]},
                 {"polygon":[[-3.5,57.09],[-3.4,57.09],[-3.4,57.0]]}]}"#,
        );
        let areas = reading.document.areas.as_ref().expect("kept");
        assert_eq!(areas.len(), 1, "the drop is scoped to the offending area");
        assert_eq!(pairs(&reading), vec![("/areas/0/polygon/1".into(), "position-unusable")]);
        assert!(reading.diagnostics[0].message().contains("the area is dropped"));
    }
}

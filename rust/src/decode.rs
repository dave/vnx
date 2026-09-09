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
use crate::geo::{distance_to_polyline, TOLERANCE_M};
use crate::json::{self, Json, ParseErrorKind};
use crate::model::*;

/// What a reader returns: the document, and the list of everything it could
/// not use as written (SPEC 6.3).
#[derive(Debug, Clone, PartialEq)]
pub struct Reading {
    pub document: Document,
    pub diagnostics: Vec<Diagnostic>,
}

/// Read a `.vnx` document from bytes.
pub fn decode(bytes: &[u8]) -> Result<Reading, Refusal> {
    let value = json::parse(bytes).map_err(|e| match e.kind {
        ParseErrorKind::NotFinite => Refusal::NotFinite { detail: e.detail },
        _ => Refusal::MalformedJson {
            detail: e.to_string(),
        },
    })?;
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

// ---------------------------------------------------------------------------
// Recognised keys (SPEC 3.10). Section 6.2's unrecognised-key rule cannot be
// implemented without this table, which is why the specification carries it.
// ---------------------------------------------------------------------------

const DOCUMENT_KEYS: &[&str] = &[
    "vnx",
    "profile",
    "generator",
    "created",
    "folders",
    "networks",
    "routes",
    "tracks",
    "waypoints",
    "areas",
    "rulesets",
];
const FOLDER_KEYS: &[&str] = &["id", "parent", "name", "notes", "hidden", "data"];
const NETWORK_KEYS: &[&str] = &[
    "name", "notes", "folder", "hidden", "nodes", "arcs", "chains", "points", "areas", "data",
];
const NETWORK_ARC_KEYS: &[&str] = &["from", "to", "pips", "data"];
const CHAIN_KEYS: &[&str] = &["links", "name", "notes", "hidden", "style", "data"];
const LINK_KEYS: &[&str] = &["arc", "from", "to"];
const ROUTE_KEYS: &[&str] = &[
    "arcs", "name", "notes", "folder", "hidden", "style", "data",
];
const ROUTE_ARC_KEYS: &[&str] = &["pips", "data"];
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
    /// data record, a style object, `rulesets`, or the identifier-keyed
    /// `nodes` and `arcs` maps.
    fn check_keys(&mut self, entries: &[(String, Json)], known: &[&str], path: &Path) {
        for (key, _) in entries {
            if !known.contains(&key.as_str()) {
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
                    Cause::WrongType {
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
                    Cause::WrongType {
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
                    Cause::WrongType {
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
                    Cause::WrongType {
                        expected: "an object",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    /// A REQUIRED array. Absence or the wrong type takes the item with it
    /// (SPEC 6.2), so the caller drops on `None`.
    fn req_array<'a>(
        &mut self,
        entries: &'a [(String, Json)],
        key: &'static str,
        path: &Path,
    ) -> Option<&'a [Json]> {
        match self.field(entries, key) {
            None => {
                self.report(path, Cause::MissingRequiredKey { key });
                None
            }
            Some(Json::Array(items)) => Some(items),
            Some(other) => {
                self.report(
                    &path.key(key),
                    Cause::WrongType {
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
                self.report(path, Cause::MissingRequiredKey { key });
                None
            }
            Some(Json::String(s)) => Some(s.clone()),
            Some(other) => {
                self.report(
                    &path.key(key),
                    Cause::WrongType {
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
    /// know, so that a future channel is additive and a re-render reproduces
    /// the spelling the document carried.
    fn position(&mut self, value: &Json, path: &Path) -> Option<Position> {
        let Json::Array(items) = value else {
            self.report(
                path,
                Cause::BadPosition {
                    detail: format!("expected an array but found {}", value.type_name()),
                },
            );
            return None;
        };
        if items.len() < 2 {
            self.report(
                path,
                Cause::BadPosition {
                    detail: format!(
                        "a position carries at least longitude and latitude, found {} channel(s)",
                        items.len()
                    ),
                },
            );
            return None;
        }
        let mut channels = Vec::with_capacity(items.len());
        for (index, item) in items.iter().enumerate() {
            match item {
                Json::Number(n) => channels.push(Some(*n)),
                // SPEC 4.1: `null` marks an absent value from index 2 upward.
                Json::Null if index >= 2 => channels.push(None),
                Json::Null => {
                    self.report(
                        path,
                        Cause::BadPosition {
                            detail: format!(
                                "channel {index} is null, and {} is never absent",
                                if index == 0 { "longitude" } else { "latitude" }
                            ),
                        },
                    );
                    return None;
                }
                other => {
                    self.report(
                        path,
                        Cause::BadPosition {
                            detail: format!("channel {index} is {}", other.type_name()),
                        },
                    );
                    return None;
                }
            }
        }
        Some(Position::new(channels))
    }

    /// A `pips` array. A pip that will not parse is dropped, with the rest of
    /// the array kept (SPEC 6.2: drop the offending ELEMENT).
    fn pips(&mut self, items: &[Json], path: &Path) -> Vec<Position> {
        items
            .iter()
            .enumerate()
            .filter_map(|(i, item)| self.position(item, &path.index(i)))
            .collect()
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
                    Cause::WrongType {
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

        self.check_keys(entries, DOCUMENT_KEYS, &root);

        let mut document = Document {
            vnx: Some(version),
            profile: self.opt_string(entries, "profile", &root),
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
        // SPEC 3.9: opaque, preserved verbatim, interpreted not at all.
        if let Some(items) = self.opt_array(entries, "rulesets", &root) {
            document.rulesets = Some(Json::Array(items.to_vec()));
        }

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

        // `nodes` and `arcs` are keyed by identifier, so their keys are data
        // rather than format vocabulary and the unrecognised-key rule of
        // SPEC 6.2 does not apply to them.
        if let Some(members) = self.opt_object(entries, "nodes", path) {
            let nodes_path = path.key("nodes");
            network.nodes = Some(
                members
                    .iter()
                    .filter_map(|(key, value)| {
                        let position = self.position(value, &nodes_path.key(key))?;
                        Some((key.clone(), position))
                    })
                    .collect(),
            );
        }
        if let Some(members) = self.opt_object(entries, "arcs", path) {
            let arcs_path = path.key("arcs");
            network.arcs = Some(
                members
                    .iter()
                    .filter_map(|(key, value)| {
                        let arc = self.network_arc(value, &arcs_path.key(key))?;
                        Some((key.clone(), arc))
                    })
                    .collect(),
            );
        }
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

        // Chains reference arcs and nodes, so they are decoded once the graph
        // is in hand.
        if let Some(items) = self.opt_array(entries, "chains", path) {
            let chains_path = path.key("chains");
            let chains: Vec<Chain> = items
                .iter()
                .enumerate()
                .filter_map(|(i, item)| self.chain(item, &chains_path.index(i), &network))
                .collect();
            network.chains = Some(chains);
        }

        self.check_arc_endpoints(&network, path);
        Some(network)
    }

    /// SPEC 6.2: an arc naming a node that is not in its network keeps the
    /// arc — its geometry is real — but the unresolved reference is still a
    /// relationship the reader could not resolve, so it is reported. No corpus
    /// case pins this; reported as a spec gap.
    fn check_arc_endpoints(&mut self, network: &Network, path: &Path) {
        let Some(arcs) = network.arcs.as_ref() else {
            return;
        };
        let arcs_path = path.key("arcs");
        let mut missing = Vec::new();
        for (key, arc) in arcs {
            for (field, node) in [("from", &arc.from), ("to", &arc.to)] {
                if network.node(node).is_none() {
                    missing.push((arcs_path.key(key).key(field), node.clone()));
                }
            }
        }
        for (where_, node) in missing {
            self.report(&where_, Cause::ArcNodeAbsent { node });
        }
    }

    fn network_arc(&mut self, value: &Json, path: &Path) -> Option<NetworkArc> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, NETWORK_ARC_KEYS, path);
        let from = self.req_string(entries, "from", path)?;
        let to = self.req_string(entries, "to", path)?;
        let pip_items = self.req_array(entries, "pips", path)?.to_vec();
        let pips = self.pips(&pip_items, &path.key("pips"));
        Some(NetworkArc {
            from,
            to,
            pips,
            data: self.records(entries, path),
        })
    }

    fn chain(&mut self, value: &Json, path: &Path, network: &Network) -> Option<Chain> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, CHAIN_KEYS, path);
        let link_items = self.req_array(entries, "links", path)?.to_vec();
        let links_path = path.key("links");

        // Decoded positionally, so a link that is dropped leaves a hole. The
        // continuity check below then compares only pairs that were adjacent
        // in the document AND both survived: dropping one link must not turn
        // one defect into two by reporting a break that the drop created.
        let mut slots: Vec<Option<Link>> = Vec::with_capacity(link_items.len());
        for (i, item) in link_items.iter().enumerate() {
            slots.push(self.link(item, &links_path.index(i), network));
        }
        self.check_continuity(&slots, &links_path, network);

        Some(Chain {
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            hidden: self.opt_bool(entries, "hidden", path),
            style: self.style(entries, path),
            links: slots.into_iter().flatten().collect(),
            data: self.records(entries, path),
        })
    }

    fn link(&mut self, value: &Json, path: &Path, network: &Network) -> Option<Link> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, LINK_KEYS, path);
        let arc_key = self.req_string(entries, "arc", path)?;

        // SPEC 6.2: a link naming an arc absent from the document is dropped.
        // Checked before the endpoints, so one defect stays one diagnostic.
        let Some(arc) = network.arc(&arc_key) else {
            self.report(path, Cause::LinkArcAbsent { arc: arc_key });
            return None;
        };

        let from = self.endpoint(entries, "from", path, arc, network)?;
        let to = self.endpoint(entries, "to", path, arc, network)?;
        Some(Link {
            arc: arc_key,
            from,
            to,
        })
    }

    /// A link endpoint is a node key or an inline position (SPEC 3.3.3).
    /// `None` drops the whole link, which is SPEC 6.2's remedy — dropping the
    /// endpoint alone would leave a malformed link.
    fn endpoint(
        &mut self,
        entries: &[(String, Json)],
        key: &'static str,
        path: &Path,
        arc: &NetworkArc,
        network: &Network,
    ) -> Option<Endpoint> {
        let field_path = path.key(key);
        match self.field(entries, key) {
            None => {
                self.report(path, Cause::MissingRequiredKey { key });
                None
            }
            Some(Json::String(node)) => {
                // SPEC 6.2: KEEP the link, and exclude that end from the
                // continuity comparison. Dropping it would be a second defect
                // manufactured out of the first.
                if network.node(node).is_none() {
                    self.report(
                        &field_path,
                        Cause::LinkNodeAbsent {
                            node: node.clone(),
                        },
                    );
                }
                Some(Endpoint::Node(node.clone()))
            }
            Some(array @ Json::Array(_)) => {
                let position = self.position(array, &field_path)?;
                match distance_to_polyline(&position, &arc.pips) {
                    None => {
                        self.report(&field_path, Cause::InlineEndpointOnEmptyArc);
                        None
                    }
                    Some(metres) if metres > TOLERANCE_M => {
                        self.report(
                            &field_path,
                            Cause::InlineEndpointBeyondTolerance { metres },
                        );
                        None
                    }
                    Some(_) => Some(Endpoint::Inline(position)),
                }
            }
            Some(other) => {
                self.report(
                    &field_path,
                    Cause::WrongType {
                        expected: "a node key or an inline position",
                        found: other.type_name(),
                    },
                );
                None
            }
        }
    }

    /// SPEC 6.2: a chain's links must form one connected walk. The diagnostic
    /// addresses the SECOND link of the pair.
    fn check_continuity(&mut self, slots: &[Option<Link>], links_path: &Path, network: &Network) {
        let mut breaks = Vec::new();
        for i in 1..slots.len() {
            let (Some(before), Some(after)) = (&slots[i - 1], &slots[i]) else {
                continue;
            };
            if !endpoints_meet(&before.to, &after.from, network) {
                breaks.push(i);
            }
        }
        for i in breaks {
            self.report(&links_path.index(i), Cause::ChainNotConnected);
        }
    }

    fn route(&mut self, value: &Json, path: &Path) -> Option<Route> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, ROUTE_KEYS, path);
        let arc_items = self.req_array(entries, "arcs", path)?.to_vec();
        let arcs_path = path.key("arcs");
        let arcs = arc_items
            .iter()
            .enumerate()
            .filter_map(|(i, item)| self.route_arc(item, &arcs_path.index(i)))
            .collect();
        Some(Route {
            name: self.opt_string(entries, "name", path),
            notes: self.opt_string(entries, "notes", path),
            folder: self.opt_string(entries, "folder", path),
            hidden: self.opt_bool(entries, "hidden", path),
            style: self.style(entries, path),
            arcs,
            data: self.records(entries, path),
        })
    }

    fn route_arc(&mut self, value: &Json, path: &Path) -> Option<RouteArc> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, ROUTE_ARC_KEYS, path);
        let pip_items = self.req_array(entries, "pips", path)?.to_vec();
        let pips = self.pips(&pip_items, &path.key("pips"));
        Some(RouteArc {
            pips,
            data: self.records(entries, path),
        })
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
        Some(Segment {
            pips: self.pips(&pip_items, &path.key("pips")),
        })
    }

    fn waypoint(&mut self, value: &Json, path: &Path, known: &[&str]) -> Option<Waypoint> {
        let entries = self.object(value, path)?;
        self.check_keys(entries, known, path);
        let at = match self.field(entries, "at") {
            None => {
                self.report(path, Cause::MissingRequiredKey { key: "at" });
                return None;
            }
            Some(value) => self.position(value, &path.key("at"))?,
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

    /// SPEC 3.7: one closed ring of at least four positions. A nested or
    /// additional ring drops the ITEM — not the extra ring, which would keep
    /// an area whose hole had vanished with nothing anywhere to say so.
    fn ring(&mut self, items: &[Json], path: &Path) -> Option<Vec<Position>> {
        let nested = items.iter().any(|item| {
            item.as_array()
                .is_some_and(|inner| inner.iter().any(|c| matches!(c, Json::Array(_))))
        });
        if nested {
            self.report(path, Cause::PolygonNestedRings);
            return None;
        }
        if items.len() < 4 {
            self.report(
                path,
                Cause::PolygonNotARing {
                    detail: format!("a ring holds at least four positions, found {}", items.len()),
                },
            );
            return None;
        }
        let mut ring = Vec::with_capacity(items.len());
        for (i, item) in items.iter().enumerate() {
            ring.push(self.position(item, &path.index(i))?);
        }
        if !ring[0].same_point(ring.last().expect("the ring is non-empty")) {
            self.report(
                path,
                Cause::PolygonNotARing {
                    detail: "the last position does not repeat the first".to_string(),
                },
            );
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
                    Cause::WrongType {
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

/// Two node keys meet when the keys are equal; otherwise the endpoints are
/// compared by resolved position. Comparing a node key by its position rather
/// than refusing to compare it is what stops a walk that is continuous being
/// reported as broken.
fn endpoints_meet(a: &Endpoint, b: &Endpoint, network: &Network) -> bool {
    if let (Endpoint::Node(x), Endpoint::Node(y)) = (a, b) {
        if x == y {
            return true;
        }
    }
    let resolve = |e: &Endpoint| match e {
        Endpoint::Inline(p) => Some(p.clone()),
        Endpoint::Node(k) => network.node(k).cloned(),
    };
    match (resolve(a), resolve(b)) {
        (Some(x), Some(y)) => x.same_point(&y),
        // A node this reader could not resolve already carries its own
        // diagnostic; it does not also make the chain broken.
        _ => true,
    }
}

/// SPEC 2.2. Three runs of decimal digits separated by full stops. A leading
/// zero is legal and the comparison is numeric, so `00.01.0000` is 0.1.0.
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
    // SPEC 2.2, through 0.x: refuse a MAJOR or MINOR above this reader's.
    // PATCH never refuses, at any version.
    if (version.major, version.minor) > SUPPORTED {
        return Err(Refusal::VersionTooNew {
            found: text.to_string(),
        });
    }
    Ok(version)
}

/// SPEC 4.3: `YYYY-MM-DDTHH:MM:SS` with OPTIONAL fractional seconds and a
/// literal `Z`. An offset other than `Z` is not permitted.
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
    let number = |range: std::ops::Range<usize>| {
        body[range].parse::<u32>().expect("checked to be digits")
    };
    if !(1..=12).contains(&number(5..7))
        || !(1..=31).contains(&number(8..10))
        || number(11..13) > 23
        || number(14..16) > 59
        // A leap second is 60.
        || number(17..19) > 60
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
        assert!(matches!(
            refuse(r#"{"vnx": "0.2.0"}"#),
            Refusal::VersionTooNew { .. }
        ));
        assert!(matches!(
            refuse(r#"{"vnx": "1.0.0"}"#),
            Refusal::VersionTooNew { .. }
        ));
        for text in ["0.1", "0.1.0.0", "0.1.x", "0.1.+1", "", "0.1.", ".1.0", "0.1.0 "] {
            let document = format!(r#"{{"vnx": "{text}"}}"#);
            assert!(
                matches!(refuse(&document), Refusal::VersionMalformed { .. }),
                "{text:?} was not refused as malformed"
            );
        }
        // PATCH never refuses, and a leading zero is a legal spelling.
        for text in ["0.1.99", "0.0.9", "00.01.0000", "0.1.0"] {
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
            refuse(r#"{"profile": "full"}"#),
            Refusal::NotAVnxDocument { .. }
        ));
        assert!(matches!(refuse("[]"), Refusal::NotAVnxDocument { .. }));
        assert!(matches!(refuse("7"), Refusal::NotAVnxDocument { .. }));
    }

    #[test]
    fn an_unknown_key_is_diagnosed_and_the_item_kept() {
        let reading = read(
            r#"{"vnx":"0.1.0","waypoints":[{"nmae":"typo","name":"Car park","at":[-3.675,57.1201]}]}"#,
        );
        assert_eq!(reading.diagnostics.len(), 1);
        assert_eq!(reading.diagnostics[0].path, "/waypoints/0/nmae");
        assert_eq!(reading.diagnostics[0].cause, Cause::UnknownKey);
        assert_eq!(reading.document.waypoints.expect("kept").len(), 1);
    }

    #[test]
    fn a_wrong_type_drops_an_optional_field_and_takes_a_required_one() {
        let reading = read(r#"{"vnx":"0.1.0","waypoints":[{"name":7,"at":[-3.6,57.1]}]}"#);
        assert_eq!(reading.document.waypoints.as_ref().expect("kept").len(), 1);
        assert_eq!(reading.document.waypoints.expect("kept")[0].name, None);
        assert_eq!(reading.diagnostics.len(), 1);

        let reading = read(r#"{"vnx":"0.1.0","waypoints":[{"name":"x","at":"here"}]}"#);
        assert_eq!(reading.document.waypoints.expect("kept").len(), 0);
        assert_eq!(reading.diagnostics.len(), 1);
    }

    #[test]
    fn an_absent_collection_is_not_an_empty_one() {
        assert_eq!(read(r#"{"vnx":"0.1.0"}"#).document.waypoints, None);
        assert_eq!(
            read(r#"{"vnx":"0.1.0","waypoints":[]}"#).document.waypoints,
            Some(vec![])
        );
    }

    #[test]
    fn a_position_keeps_every_channel_it_was_given() {
        let reading = read(r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6,57.1,null,5,99]}]}"#);
        let at = &reading.document.waypoints.expect("kept")[0].at;
        assert_eq!(at.arity(), 5, "an unknown channel is kept, not truncated");
        assert_eq!(at.ele(), None);
        assert_eq!(at.time_ms(), Some(5.0));
    }

    #[test]
    fn longitude_and_latitude_are_never_null() {
        let reading = read(r#"{"vnx":"0.1.0","waypoints":[{"at":[null,57.1]}]}"#);
        assert_eq!(reading.document.waypoints.expect("kept").len(), 0);
        assert!(matches!(
            reading.diagnostics[0].cause,
            Cause::BadPosition { .. }
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
            "2026-05-12T08:03:11",
            "2026-05-12",
            "2026-13-12T08:03:11Z",
            "2026-05-12T24:03:11Z",
            "2026-05-12T08:03:11.Z",
            "not a date",
        ] {
            assert!(!is_utc_timestamp(bad), "{bad}");
        }
    }

    #[test]
    fn an_out_of_range_importance_is_dropped_and_the_rest_of_the_style_kept() {
        let reading = read(
            r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6,57.1],"style":{"importance":9,"color":"x"}}]}"#,
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
            r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6,57.1],"data":[
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
            r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6,57.1],"data":[{"value":1},{"@type":"a.b"}]}]}"#,
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
        let reading = read(r#"{"vnx":"0.1.0","waypoints":[{"at":[-3.6,57.1],"folder":"ghost"}]}"#);
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
            r#"{"vnx":"0.1.0","folders":[{"name":"Nameless"}],"waypoints":[{"at":[-3.6,57.1],"folder":"gone"}]}"#,
        );
        assert_eq!(reading.document.folders.expect("kept").len(), 0);
        assert_eq!(reading.document.waypoints.expect("kept").len(), 1);
        assert_eq!(reading.diagnostics.len(), 2);
    }

    #[test]
    fn a_ring_must_close_and_must_hold_four_positions() {
        let short = read(
            r#"{"vnx":"0.1.0","areas":[{"polygon":[[-3.7,57.09],[-3.6,57.09],[-3.7,57.09]]}]}"#,
        );
        assert_eq!(short.document.areas.expect("kept").len(), 0);
        let open = read(
            r#"{"vnx":"0.1.0","areas":[{"polygon":[[-3.7,57.09],[-3.6,57.09],[-3.6,57.0],[-3.65,57.05]]}]}"#,
        );
        assert_eq!(open.document.areas.expect("kept").len(), 0);
        assert!(matches!(
            open.diagnostics[0].cause,
            Cause::PolygonNotARing { .. }
        ));
    }

    #[test]
    fn a_pip_that_will_not_parse_is_dropped_and_the_rest_of_the_array_kept() {
        let reading = read(
            r#"{"vnx":"0.1.0","tracks":[{"segments":[{"pips":[[-3.6,57.1],["x",1],[-3.5,57.2]]}]}]}"#,
        );
        let track = &reading.document.tracks.expect("kept")[0];
        assert_eq!(track.segments[0].pips.len(), 2);
        assert_eq!(reading.diagnostics.len(), 1);
    }
}

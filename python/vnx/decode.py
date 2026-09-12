"""Reading a VNX document (SPEC 6).

"A reader is tolerant of damage and silent about nothing" (SPEC 6.2).  Three
things can happen to a piece of a document here and no fourth:

* an unrecognised key is skipped, the item is kept, and a diagnostic is
  recorded;
* an unusable value is dropped, together with the smallest element it makes
  unusable, and a diagnostic is recorded;
* the whole document is refused, which raises.

Nothing becomes a default.  A malformed value never turns into 0, "" or false.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Any, Union

from . import keys as K
from .errors import Diagnostic, VNXRefusal
from .geo import distance_between, distance_to_polyline
from .jsonio import parse_json
from .model import (
    Area, Arc, Chain, DataRecord, Document, Endpoint, Folder, JSONValue, Link,
    Network, Position, Route, RouteArc, Segment, Style, Track, Waypoint,
)
from .pointer import join

__all__ = ["ReadResult", "read_bytes", "decode_document", "SUPPORTED_VERSION"]

# The MAJOR.MINOR this reader implements (SPEC 2.2).
SUPPORTED_VERSION = (0, 1)

# SPEC 3.3.3.  An inline chain endpoint must project onto its arc within this.
PROJECTION_TOLERANCE_M = 50.0

_VERSION = re.compile(r"\A(\d+)\.(\d+)\.(\d+)\Z")
# SPEC 4.3: ISO 8601 in UTC, a literal Z suffix, optional fractional seconds.
_TIMESTAMP = re.compile(r"\A\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z\Z")


@dataclass(slots=True)
class ReadResult:
    """A document and the diagnostics raised while reading it (SPEC 6.3).

    Both, always.  "A reader that returns a document alone has discarded the
    only evidence that anything was wrong."
    """

    document: Document
    diagnostics: list[Diagnostic]


def read_bytes(data: bytes) -> ReadResult:
    """Read a plain ``.vnx`` document.  Raises :class:`VNXRefusal`."""
    return decode_document(parse_json(data))


def decode_document(value: JSONValue) -> ReadResult:
    reader = _Reader()
    document = reader.document(value)
    return ReadResult(document, reader.diagnostics)


class _Reader:
    def __init__(self) -> None:
        self.diagnostics: list[Diagnostic] = []

    # -- diagnostics ---------------------------------------------------

    def note(self, path: str, message: str, code: Union[str, None] = None) -> None:
        self.diagnostics.append(Diagnostic(path, message, code))

    # -- primitives ----------------------------------------------------

    def _object(
        self, value: JSONValue, path: str, known: frozenset[str], what: str
    ) -> Union[dict[str, JSONValue], None]:
        """Type-check an object and diagnose every key outside *known*."""
        if not isinstance(value, dict):
            self.note(path, f"{what} must be an object")
            return None
        for key in value:
            if key not in known:
                self.note(join(path, key), "unknown key", "unknown-key")
        return value

    @staticmethod
    def _type_code(key: str, required: frozenset[str]) -> str:
        """SPEC 6.2 gives a wrong-typed value two remedies, so two codes.

        An optional key is dropped and the item kept; a required key drops the
        item, because dropping the field would leave a waypoint that is nowhere.
        A library that always drops the item, and one that always drops the
        field, each pass a document carrying only one of the two.
        """
        return "wrong-type-required" if key in required else "wrong-type-optional"

    def _complete(
        self,
        obj: dict[str, JSONValue],
        path: str,
        what: str,
        fields: dict[str, object],
    ) -> bool:
        """SPEC 3.10: a REQUIRED key must be present for the object to exist.

        Returns False when the item must be dropped.  A key that was present but
        unusable has already been diagnosed where it was read, so only a key
        genuinely ABSENT from the source object is reported here -- otherwise one
        defect would produce two diagnostics.
        """
        missing = [key for key, value in fields.items() if value is None]
        for key in missing:
            if key not in obj:
                self.note(join(path, key), f"{what} must carry `{key}`")
        return not missing

    def _string(
        self,
        obj: dict[str, JSONValue],
        key: str,
        path: str,
        required: frozenset[str] = frozenset(),
    ) -> Union[str, None]:
        if key not in obj:
            return None
        value = obj[key]
        if not isinstance(value, str):
            self.note(
                join(path, key),
                f"`{key}` is not a string",
                self._type_code(key, required),
            )
            return None
        return value

    def _bool(
        self,
        obj: dict[str, JSONValue],
        key: str,
        path: str,
        required: frozenset[str] = frozenset(),
    ) -> Union[bool, None]:
        if key not in obj:
            return None
        value = obj[key]
        if not isinstance(value, bool):
            self.note(
                join(path, key),
                f"`{key}` is not a boolean",
                self._type_code(key, required),
            )
            return None
        return value

    def _timestamp(
        self, obj: dict[str, JSONValue], key: str, path: str
    ) -> Union[str, None]:
        value = self._string(obj, key, path)
        if value is None:
            return None
        if not _TIMESTAMP.match(value):
            # SPEC 6.2 lists a record-level timestamp carrying an offset other
            # than Z as an unusable value: drop it, diagnose, continue.  The
            # element dropped is the timestamp, not the item that carries it.
            self.note(join(path, key), "must be UTC with a Z suffix", "timestamp-not-utc")
            return None
        return value

    def _array(
        self,
        obj: dict[str, JSONValue],
        key: str,
        path: str,
        required: frozenset[str] = frozenset(),
    ) -> Union[list[JSONValue], None]:
        if key not in obj:
            return None
        value = obj[key]
        if not isinstance(value, list):
            self.note(
                join(path, key),
                f"`{key}` is not an array",
                self._type_code(key, required),
            )
            return None
        return value

    def _style(self, obj: dict[str, JSONValue], path: str) -> Union[Style, None]:
        """A style body is opaque (ruled 2026-09-09; see `keys.py`).

        Its shape is checked and its contents are not: no key inside a style is
        ever unknown, and every key survives a read-modify-write.
        """
        if "style" not in obj:
            return None
        value = obj["style"]
        if not isinstance(value, dict):
            self.note(join(path, "style"), "a style must be an object")
            return None
        return dict(value)

    def _check_importance(self, style: Union[Style, None], path: str) -> None:
        """SPEC 3.6's `importance` range, ruled normative on 2026-09-09.

        `icon-size`'s list of the sizes Viatrail uses was ruled informative in
        the same breath and is not checked.  An out-of-range `importance` is an
        unusable value under SPEC 6.2: the key is dropped, the style and the
        item are kept.  That remedy is this implementation's reading -- SPEC 6.2
        gives none for a value outside a stated range.
        """
        if style is None or "importance" not in style:
            return
        if not _is_importance(style["importance"]):
            self.note(
                join(path, "style", "importance"), "must be an integer from 1 to 5"
            )
            del style["importance"]

    def _data(
        self, obj: dict[str, JSONValue], path: str
    ) -> Union[list[DataRecord], None]:
        items = self._array(obj, "data", path)
        if items is None:
            return None
        records: list[DataRecord] = []
        for index, item in enumerate(items):
            at = join(path, "data", index)
            if not isinstance(item, dict):
                self.note(at, "a data record must be an object")
                continue
            type_name = item.get("@type")
            if not isinstance(type_name, str):
                # SPEC 5: every record carries an @type.  Without one the record
                # has no meaning at all, so it is dropped rather than kept as an
                # anonymous body.  A record body is otherwise opaque, so no key
                # inside it is ever unknown.
                self.note(at, "a data record must carry a string @type")
                continue
            body = {k: v for k, v in item.items() if k != "@type"}
            records.append(DataRecord(type_name, body))
        return records

    def _position(
        self, value: JSONValue, path: str, code: Union[str, None] = None
    ) -> Union[Position, None]:
        """SPEC 4.1.  Returns None for a position that will not parse.

        Every way of failing is the same reason -- this is not a position --
        so they share one code.  A caller passes ``code`` only where the value
        fails as a TYPE before it fails as a position: SPEC 6.2's two clauses
        divide at IS IT AN ARRAY AT ALL, and the diagnostic follows the clause.
        The path is the position's, never a channel's: a reader that pointed at
        the channel would report a pointer no other reader reports, and the
        offending element under SPEC 6.2 is the position.
        """
        unusable = code or "position-unusable"
        if not isinstance(value, list):
            self.note(path, "a position must be an array of numbers", code)
            return None
        if len(value) < 2:
            self.note(
                path,
                f"found {len(value)} channel{'' if len(value) == 1 else 's'}, "
                "needs at least 2 (longitude and latitude)",
                unusable,
            )
            return None
        channels: list[Union[float, None]] = []
        for index, channel in enumerate(value):
            if channel is None:
                if index < 2:
                    self.note(
                        path,
                        f"channel {index} is null, expected a number",
                        unusable,
                    )
                    return None
                channels.append(None)
                continue
            if isinstance(channel, bool) or not isinstance(channel, (int, float)):
                self.note(
                    path,
                    f"channel {index} is {_json_type(channel)}, "
                    "expected a number",
                    unusable,
                )
                return None
            channels.append(float(channel))
        # SPEC 4.1: a channel this version does not define is ignored.  The
        # loop above still checks it, because an array holding a string is not
        # a position whichever index the string sits at.
        lon, lat = channels[0], channels[1]
        assert lon is not None and lat is not None
        return Position(
            lon,
            lat,
            channels[2] if len(channels) > 2 else None,
            channels[3] if len(channels) > 3 else None,
        )

    def _positions(
        self, obj: dict[str, JSONValue], key: str, path: str
    ) -> Union[list[Position], None]:
        items = self._array(obj, key, path)
        if items is None:
            return None
        out: list[Position] = []
        for index, item in enumerate(items):
            position = self._position(item, join(path, key, index))
            if position is None:
                # SPEC 6.2: the container goes, not the position.  Geometry is
                # continuous -- dropping one vertex leaves a shorter line that
                # is plausible, wrong, and carries nothing to say so.
                return None
            out.append(position)
        return out

    # -- document ------------------------------------------------------

    def document(self, value: JSONValue) -> Document:
        if not isinstance(value, dict):
            raise VNXRefusal("not a VNX document: the top level is not a JSON object")
        if "vnx" not in value:
            raise VNXRefusal("not a VNX document: no `vnx` key at the top level")
        version = value["vnx"]
        if not isinstance(version, str):
            raise VNXRefusal(
                "not a VNX document: `vnx` must be a version string of the shape "
                "MAJOR.MINOR.PATCH"
            )
        self._check_version(version)

        obj = self._object(value, "", K.DOCUMENT, "a document")
        assert obj is not None

        document = Document(vnx=version)
        document.profile = self._string(obj, "profile", "")
        document.generator = self._string(obj, "generator", "")
        document.created = self._timestamp(obj, "created", "")

        document.folders = self._collection(obj, "folders", self._folder)
        document.networks = self._collection(obj, "networks", self._network)
        document.routes = self._collection(obj, "routes", self._route)
        document.tracks = self._collection(obj, "tracks", self._track)
        document.waypoints = self._collection(
            obj, "waypoints", lambda v, p: self._waypoint(v, p)
        )
        document.areas = self._collection(obj, "areas", self._area)

        # SPEC 3.9: rulesets are opaque in 0.1 and MUST survive a
        # read-modify-write.  Nothing inside them is inspected, so nothing
        # inside them can be unknown.
        rulesets = self._array(obj, "rulesets", "")
        document.rulesets = rulesets
        self._check_folder_references(document)
        return document

    def _check_folder_references(self, document: Document) -> None:
        """SPEC 6.2: a `folder` naming a folder that is not present.

        Not an unusable value: the reader treats the key as absent, so the item
        sits at the top level, and records a diagnostic.  Dropping the item
        would lose content over a lost grouping.

        This runs after every collection is decoded, because a folder may be
        written after the items that name it -- key and element order carry no
        meaning (SPEC 4.4).
        """
        known = {
            folder.id for folder in (document.folders or []) if folder.id is not None
        }
        hosts: list[tuple[str, list[Any]]] = [
            ("networks", list(document.networks or [])),
            ("routes", list(document.routes or [])),
            ("tracks", list(document.tracks or [])),
            ("waypoints", list(document.waypoints or [])),
            ("areas", list(document.areas or [])),
        ]
        for collection, items in hosts:
            for index, item in enumerate(items):
                if item.folder is not None and item.folder not in known:
                    self.note(
                        join("", collection, index, "folder"),
                        f"no folder `{item.folder}` in this document",
                    )
                    item.folder = None

    def _check_version(self, version: str) -> None:
        match = _VERSION.match(version)
        if match is None:
            raise VNXRefusal(
                f"unreadable version {version!r}: a VNX version is three runs of "
                "decimal digits separated by full stops, MAJOR.MINOR.PATCH"
            )
        major, minor = int(match.group(1)), int(match.group(2))
        # SPEC 2.2: PATCH never refuses.  Through 0.x, MAJOR and MINOR are both
        # breaking axes, and each MINOR is ITS OWN SERIES -- so the test is "not
        # the series this reader implements", not "above it".  0.0 is not an
        # older spelling of 0.1; this reader has no rules for it and refuses it
        # for the same reason it refuses 0.2.  Written as `>` -- the shape five
        # of the six implementations had -- a 0.0.9 document reads as though it
        # were 0.1, under rules it was never written to.
        if (major, minor) == SUPPORTED_VERSION:
            return
        supported = f"{SUPPORTED_VERSION[0]}.{SUPPORTED_VERSION[1]}"
        if (major, minor) > SUPPORTED_VERSION:
            raise VNXRefusal(
                f"this document is version {version} and a newer reader is needed: "
                f"this one reads {supported}.x"
            )
        raise VNXRefusal(
            f"this document is version {version} and this reader does not "
            f"support the {major}.{minor} series: it reads {supported}.x"
        )

    def _collection(
        self, obj: dict[str, JSONValue], key: str, decode: Any
    ) -> Union[list[Any], None]:
        items = self._array(obj, key, "")
        if items is None:
            return None
        out = []
        for index, item in enumerate(items):
            decoded = decode(item, join("", key, index))
            if decoded is not None:
                out.append(decoded)
        return out

    # -- items ---------------------------------------------------------

    def _folder(self, value: JSONValue, path: str) -> Union[Folder, None]:
        obj = self._object(value, path, K.FOLDER, "a folder")
        if obj is None:
            return None
        folder = Folder(
            id=self._string(obj, "id", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            parent=self._string(obj, "parent", path),
            hidden=self._bool(obj, "hidden", path),
            data=self._data(obj, path),
        )
        if not self._complete(obj, path, "a folder", {"id": folder.id}):
            # SPEC 6.2: items that named it are then covered by the dangling
            # `folder` rule and sit at the top level, so a folder's contents
            # survive the loss of the folder.
            return None
        return folder

    def _waypoint(
        self,
        value: JSONValue,
        path: str,
        known: frozenset[str] = K.WAYPOINT,
    ) -> Union[Waypoint, None]:
        obj = self._object(value, path, known, "a waypoint")
        if obj is None:
            return None
        at = None
        if "at" in obj:
            at = self._position(
                obj["at"], join(path, "at"), "wrong-type-required"
            )
        style = self._style(obj, path)
        self._check_importance(style, path)
        if not self._complete(obj, path, "a waypoint", {"at": at}):
            return None
        return Waypoint(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path, K.REQUIRED_WAYPOINT),
            at=at,
            style=style,
            data=self._data(obj, path),
        )

    def _area(
        self,
        value: JSONValue,
        path: str,
        known: frozenset[str] = K.AREA,
    ) -> Union[Area, None]:
        obj = self._object(value, path, known, "an area")
        if obj is None:
            return None
        polygon = None
        if "polygon" in obj:
            polygon = self._polygon(obj["polygon"], join(path, "polygon"))
            if polygon is None:
                # SPEC 3.7: a reader that meets a nested or additional ring MUST
                # report a diagnostic and MUST drop the item.  It must not keep
                # the outer ring and must not merge the rings.
                return None
        if not self._complete(obj, path, "an area", {"polygon": polygon}):
            return None
        return Area(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path),
            polygon=polygon,
            style=self._style(obj, path),
            data=self._data(obj, path),
        )

    def _polygon(self, value: JSONValue, path: str) -> Union[list[Position], None]:
        """One closed ring (SPEC 3.7).  None means the area must be dropped."""
        if not isinstance(value, list):
            self.note(path, "a polygon must be an array of positions")
            return None
        if any(
            isinstance(item, list) and item and isinstance(item[0], list)
            for item in value
        ):
            self.note(path, "nested rings: this format carries one closed ring",
                      "polygon-nested-rings")
            return None
        ring: list[Position] = []
        for index, item in enumerate(value):
            position = self._position(item, join(path, index))
            if position is None:
                return None
            ring.append(position)
        if len(ring) < 4:
            self.note(path, "a ring must hold at least four positions")
            return None
        if not ring[0].same_point(ring[-1]):
            self.note(path, "a ring must close: its first and last positions differ")
            return None
        return ring

    def _track(self, value: JSONValue, path: str) -> Union[Track, None]:
        obj = self._object(value, path, K.TRACK, "a track")
        if obj is None:
            return None
        segments = None
        items = self._array(obj, "segments", path)
        if items is not None:
            segments = []
            for index, item in enumerate(items):
                at = join(path, "segments", index)
                seg = self._object(item, at, K.SEGMENT, "a segment")
                if seg is None:
                    continue
                pips = self._positions(seg, "pips", at)
                if pips is None and "pips" in seg:
                    # SPEC 6.2: an unusable position drops the container that
                    # holds the geometry, and the diagnostic is already
                    # reported at the position.  Reporting `pips` absent here
                    # too would turn one defect into two.
                    continue
                if not self._complete(seg, at, "a segment", {"pips": pips}):
                    continue
                segments.append(Segment(pips=pips))
        if not self._complete(obj, path, "a track", {"segments": segments}):
            return None
        return Track(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            started=self._timestamp(obj, "started", path),
            ended=self._timestamp(obj, "ended", path),
            hidden=self._bool(obj, "hidden", path),
            style=self._style(obj, path),
            segments=segments,
            data=self._data(obj, path),
        )

    def _route(self, value: JSONValue, path: str) -> Union[Route, None]:
        obj = self._object(value, path, K.ROUTE, "a route")
        if obj is None:
            return None
        arcs = None
        items = self._array(obj, "arcs", path)
        if items is not None:
            arcs = []
            for index, item in enumerate(items):
                at = join(path, "arcs", index)
                arc = self._object(item, at, K.ROUTE_ARC, "a route arc")
                if arc is None:
                    continue
                pips = self._positions(arc, "pips", at)
                if pips is None and "pips" in arc:
                    # SPEC 6.2: an unusable position drops the container that
                    # holds the geometry, and the diagnostic is already
                    # reported at the position.  Reporting `pips` absent here
                    # too would turn one defect into two.
                    continue
                if not self._complete(arc, at, "a route arc", {"pips": pips}):
                    continue
                arcs.append(RouteArc(pips=pips, data=self._data(arc, at)))
        if not self._complete(obj, path, "a route", {"arcs": arcs}):
            return None
        return Route(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path),
            style=self._style(obj, path),
            arcs=arcs,
            data=self._data(obj, path),
        )

    # -- networks ------------------------------------------------------

    def _network(self, value: JSONValue, path: str) -> Union[Network, None]:
        obj = self._object(value, path, K.NETWORK, "a network")
        if obj is None:
            return None
        network = Network(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path),
        )
        network.nodes = self._nodes(obj, path)
        network.arcs = self._arcs(obj, path, network.nodes or {})
        network.data = self._data(obj, path)

        points = self._array(obj, "points", path)
        if points is not None:
            network.points = []
            for index, item in enumerate(points):
                point = self._waypoint(
                    item, join(path, "points", index), K.WAYPOINT_IN_NETWORK
                )
                if point is not None:
                    network.points.append(point)

        areas = self._array(obj, "areas", path)
        if areas is not None:
            network.areas = []
            for index, item in enumerate(areas):
                area = self._area(
                    item, join(path, "areas", index), K.AREA_IN_NETWORK
                )
                if area is not None:
                    network.areas.append(area)

        network.chains = self._chains(obj, path, network)
        return network

    def _nodes(
        self, obj: dict[str, JSONValue], path: str
    ) -> Union[dict[str, Position], None]:
        if "nodes" not in obj:
            return None
        value = obj["nodes"]
        at = join(path, "nodes")
        if not isinstance(value, dict):
            self.note(at, "`nodes` must be an object keyed by node identifier")
            return None
        nodes: dict[str, Position] = {}
        for key, item in value.items():
            position = self._position(item, join(at, key))
            if position is not None:
                nodes[key] = position
        return nodes

    def _arcs(
        self, obj: dict[str, JSONValue], path: str, nodes: dict[str, Position]
    ) -> Union[dict[str, Arc], None]:
        if "arcs" not in obj:
            return None
        value = obj["arcs"]
        at = join(path, "arcs")
        if not isinstance(value, dict):
            self.note(at, "`arcs` must be an object keyed by arc identifier")
            return None
        arcs: dict[str, Arc] = {}
        for key, item in value.items():
            here = join(at, key)
            entry = self._object(item, here, K.ARC, "an arc")
            if entry is None:
                continue
            pips = self._positions(entry, "pips", here)
            if pips is None and "pips" in entry:
                # SPEC 6.2: an unusable pip drops the ARC.  The arc then leaves
                # the network, and a link naming it reports `link-arc-absent` --
                # a gap the continuity rule can see, where a shorter polyline
                # would have looked right.
                continue
            arc = Arc(
                from_=self._string(entry, "from", here),
                to=self._string(entry, "to", here),
                pips=pips,
                data=self._data(entry, here),
            )
            # An arc's endpoints name nodes in the same network (SPEC 3.3.2).
            # A name that is not there is an unusable value under SPEC 6.2, but
            # the arc's geometry is still real, so the arc is kept.
            for field_name, node_key in (("from", arc.from_), ("to", arc.to)):
                if node_key is not None and node_key not in nodes:
                    self.note(
                        join(here, field_name), "not a node in this network"
                    )
            self._check_terminal_pips(arc, here, nodes)
            if not self._complete(
                entry, here, "an arc",
                {"from": arc.from_, "to": arc.to, "pips": arc.pips},
            ):
                continue
            arcs[key] = arc
        return arcs

    def _check_terminal_pips(
        self, arc: Arc, path: str, nodes: dict[str, Position]
    ) -> None:
        """SPEC 3.3.2: a terminal pip and its node are one fact written twice.

        The pips are authoritative, so nothing is corrected and both are kept --
        but a reader MUST compare them and MUST record a diagnostic where they
        are further apart than SPEC 3.3.3's tolerance.
        """
        pips = arc.pips
        if not pips:
            return
        for field_name, node_key, pip, which in (
            ("from", arc.from_, pips[0], "first"),
            ("to", arc.to, pips[-1], "last"),
        ):
            if node_key is None:
                continue
            node = nodes.get(node_key)
            if node is None:
                # An absent node is already reported as an unusable value, and
                # there is no second position to compare against.
                continue
            distance = distance_between((pip.lon, pip.lat), (node.lon, node.lat))
            if distance > PROJECTION_TOLERANCE_M:
                # `diagnose/terminal-pip-off-node.vnx` pins the code, so the
                # spelling is the corpus's rather than this reader's.  The
                # metres are NOT pinned: what a reader reports depends on its
                # distance model, and only the code and the path are compared.
                self.note(
                    join(path, field_name),
                    f"node `{node_key}` is {distance:.0f} m from the "
                    f"{which} pip, beyond the "
                    f"{PROJECTION_TOLERANCE_M:.0f} m tolerance",
                    "terminal-pip-off-node",
                )

    def _chains(
        self, obj: dict[str, JSONValue], path: str, network: Network
    ) -> Union[list[Chain], None]:
        items = self._array(obj, "chains", path)
        if items is None:
            return None
        chains: list[Chain] = []
        for index, item in enumerate(items):
            chain = self._chain(item, join(path, "chains", index), network)
            if chain is not None:
                chains.append(chain)
        return chains

    def _chain(
        self, value: JSONValue, path: str, network: Network
    ) -> Union[Chain, None]:
        obj = self._object(value, path, K.CHAIN, "a chain")
        if obj is None:
            return None
        chain = Chain(
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path),
            style=self._style(obj, path),
            data=self._data(obj, path),
        )
        items = self._array(obj, "links", path)
        if items is None:
            if not self._complete(obj, path, "a chain", {"links": None}):
                return None
            return chain
        chain.links = []
        # `kept` records which source indices survived, so that a break is only
        # reported between links that were adjacent in the document.  Reporting
        # one across a dropped link would turn one defect into two.
        kept: list[tuple[int, Link]] = []
        for index, item in enumerate(items):
            at = join(path, "links", index)
            link = self._link(item, at, network)
            if link is not None:
                chain.links.append(link)
                kept.append((index, link))
        self._check_continuity(kept, path, network)
        return chain

    def _link(
        self, value: JSONValue, path: str, network: Network
    ) -> Union[Link, None]:
        obj = self._object(value, path, K.LINK, "a link")
        if obj is None:
            return None
        arc_key = self._string(obj, "arc", path)
        arcs = network.arcs or {}
        if arc_key is None or arc_key not in arcs:
            # SPEC 6.2 names this exactly: a link naming an arc that is absent
            # from the document is an unusable value.  A link's whole meaning is
            # the arc it traverses, so there is no geometry to keep.
            self.note(
            path,
            "not in this network: this link names no arc it can traverse",
            "link-arc-absent",
        )
            return None
        arc = arcs[arc_key]
        link = Link(arc=arc_key)
        for field_name in ("from", "to"):
            endpoint = self._endpoint(obj, field_name, path, arc, network)
            if field_name == "from":
                link.from_ = endpoint
            else:
                link.to = endpoint
        if not self._complete(
            obj, path, "a link", {"from": link.from_, "to": link.to}
        ):
            return None
        return link

    def _endpoint(
        self,
        obj: dict[str, JSONValue],
        key: str,
        path: str,
        arc: Arc,
        network: Network,
    ) -> Union[Endpoint, None]:
        if key not in obj:
            return None
        value = obj[key]
        at = join(path, key)
        if isinstance(value, str):
            if value not in (network.nodes or {}):
                self.note(at, "not a node in this network")
            return value
        position = self._position(value, at)
        if position is None:
            return None
        # SPEC 3.3.3: an inline position MUST lie on the named arc, within a
        # projection tolerance of 50 metres.  One that does not is an unusable
        # value under SPEC 6.2.
        polyline = [(pip.lon, pip.lat) for pip in (arc.pips or [])]
        if not polyline:
            self.note(at, "the named arc carries no pips to project onto")
            return None
        distance = distance_to_polyline((position.lon, position.lat), polyline)
        if distance > PROJECTION_TOLERANCE_M:
            # The corpus pins the substring "beyond the 50 m tolerance" and
            # deliberately does NOT pin the measured distance, since the metres
            # a reader reports depend on its distance model.  The distance is
            # still worth reporting -- it is what tells a holder whether the
            # document is nearly right or nowhere near.
            self.note(
                at,
                f"does not lie on arc `{arc_name(network, arc)}`: "
                f"{distance:.0f} m, beyond the "
                f"{PROJECTION_TOLERANCE_M:.0f} m tolerance",
                "endpoint-off-arc",
            )
            return None
        return position

    def _check_continuity(
        self, kept: list[tuple[int, Link]], path: str, network: Network
    ) -> None:
        """Consecutive links must meet (SPEC 8, diagnose tier).

        Endpoints are compared by value, never by identity: SPEC 3.3.3 requires
        value-equal inline positions to be treated as the same endpoint, and
        without that a turnaround handle reads as a break that is not there.
        """
        for (index_a, first), (index_b, second) in zip(kept, kept[1:]):
            if index_b != index_a + 1:
                continue
            if first.to is None or second.from_ is None:
                continue
            if not self._same_endpoint(first.to, second.from_, network):
                self.note(
                    join(path, "links", index_b),
                    "not one connected walk: this link does not start where the "
                    "previous one ended",
                    "chain-discontinuity",
                )

    def _same_endpoint(
        self, first: Endpoint, second: Endpoint, network: Network
    ) -> bool:
        if isinstance(first, str) and isinstance(second, str) and first == second:
            return True
        a = self._resolve(first, network)
        b = self._resolve(second, network)
        if a is None or b is None:
            return False
        return a.same_point(b)

    @staticmethod
    def _resolve(endpoint: Endpoint, network: Network) -> Union[Position, None]:
        if isinstance(endpoint, Position):
            return endpoint
        return (network.nodes or {}).get(endpoint)


def _json_type(value: JSONValue) -> str:
    """The JSON type name for a value, for a diagnostic that names what it saw."""
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "a boolean"
    if isinstance(value, (int, float)):
        return "a number"
    if isinstance(value, str):
        return "a string"
    if isinstance(value, list):
        return "an array"
    return "an object"


def _is_importance(value: JSONValue) -> bool:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return False
    return float(value).is_integer() and 1 <= value <= 5


def arc_name(network: Network, arc: Arc) -> str:
    for key, candidate in (network.arcs or {}).items():
        if candidate is arc:
            return key
    return "?"

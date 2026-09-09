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
from .geo import distance_to_polyline
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

    def note(self, path: str, message: str) -> None:
        self.diagnostics.append(Diagnostic(path, message))

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
                self.note(join(path, key), "unknown key")
        return value

    def _string(
        self, obj: dict[str, JSONValue], key: str, path: str
    ) -> Union[str, None]:
        if key not in obj:
            return None
        value = obj[key]
        if not isinstance(value, str):
            self.note(join(path, key), "must be a string")
            return None
        return value

    def _bool(
        self, obj: dict[str, JSONValue], key: str, path: str
    ) -> Union[bool, None]:
        if key not in obj:
            return None
        value = obj[key]
        if not isinstance(value, bool):
            self.note(join(path, key), "must be a boolean")
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
            self.note(join(path, key), "must be UTC with a Z suffix")
            return None
        return value

    def _array(
        self, obj: dict[str, JSONValue], key: str, path: str
    ) -> Union[list[JSONValue], None]:
        if key not in obj:
            return None
        value = obj[key]
        if not isinstance(value, list):
            self.note(join(path, key), "must be an array")
            return None
        return value

    def _style(
        self, obj: dict[str, JSONValue], path: str, known: frozenset[str]
    ) -> Union[Style, None]:
        if "style" not in obj:
            return None
        style = self._object(obj["style"], join(path, "style"), known, "style")
        if style is None:
            return None
        return {k: v for k, v in style.items() if k in known}

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

    def _position(self, value: JSONValue, path: str) -> Union[Position, None]:
        """SPEC 4.1.  Returns None for a position that will not parse."""
        if not isinstance(value, list):
            self.note(path, "a position must be an array")
            return None
        if len(value) < 2:
            self.note(path, "a position must carry at least longitude and latitude")
            return None
        channels: list[Union[float, None]] = []
        for index, channel in enumerate(value):
            if channel is None:
                if index < 2:
                    self.note(path, "longitude and latitude must not be null")
                    return None
                channels.append(None)
                continue
            if isinstance(channel, bool) or not isinstance(channel, (int, float)):
                self.note(join(path, index), "a position channel must be a number")
                return None
            channels.append(float(channel))
        return Position(tuple(channels))

    def _positions(
        self, obj: dict[str, JSONValue], key: str, path: str
    ) -> Union[list[Position], None]:
        items = self._array(obj, key, path)
        if items is None:
            return None
        out: list[Position] = []
        for index, item in enumerate(items):
            position = self._position(item, join(path, key, index))
            if position is not None:
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
        return document

    def _check_version(self, version: str) -> None:
        match = _VERSION.match(version)
        if match is None:
            raise VNXRefusal(
                f"unreadable version {version!r}: a VNX version is three runs of "
                "decimal digits separated by full stops, MAJOR.MINOR.PATCH"
            )
        major, minor = int(match.group(1)), int(match.group(2))
        # SPEC 2.2: PATCH never refuses.  Through 0.x, MAJOR and MINOR are both
        # breaking axes, so anything above this reader's own is refused.
        if (major, minor) > SUPPORTED_VERSION:
            supported = f"{SUPPORTED_VERSION[0]}.{SUPPORTED_VERSION[1]}"
            raise VNXRefusal(
                f"this document is version {version} and a newer reader is needed: "
                f"this one reads {supported}.x"
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
        return Folder(
            id=self._string(obj, "id", path),
            name=self._string(obj, "name", path),
            parent=self._string(obj, "parent", path),
            hidden=self._bool(obj, "hidden", path),
        )

    def _waypoint(self, value: JSONValue, path: str) -> Union[Waypoint, None]:
        obj = self._object(value, path, K.WAYPOINT, "a waypoint")
        if obj is None:
            return None
        at = None
        if "at" in obj:
            at = self._position(obj["at"], join(path, "at"))
        return Waypoint(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path),
            at=at,
            style=self._style(obj, path, K.POINT_STYLE),
            data=self._data(obj, path),
        )

    def _area(self, value: JSONValue, path: str) -> Union[Area, None]:
        obj = self._object(value, path, K.AREA, "an area")
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
        return Area(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path),
            polygon=polygon,
            style=self._style(obj, path, K.AREA_STYLE),
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
            self.note(path, "nested rings: this format carries one closed ring")
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
                segments.append(Segment(pips=self._positions(seg, "pips", at)))
        return Track(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            started=self._timestamp(obj, "started", path),
            ended=self._timestamp(obj, "ended", path),
            hidden=self._bool(obj, "hidden", path),
            style=self._style(obj, path, K.LINE_STYLE),
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
                arcs.append(
                    RouteArc(
                        pips=self._positions(arc, "pips", at),
                        data=self._data(arc, at),
                    )
                )
        return Route(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path),
            style=self._style(obj, path, K.LINE_STYLE),
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
                point = self._waypoint(item, join(path, "points", index))
                if point is not None:
                    network.points.append(point)

        areas = self._array(obj, "areas", path)
        if areas is not None:
            network.areas = []
            for index, item in enumerate(areas):
                area = self._area(item, join(path, "areas", index))
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
            arc = Arc(
                from_=self._string(entry, "from", here),
                to=self._string(entry, "to", here),
                pips=self._positions(entry, "pips", here),
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
            arcs[key] = arc
        return arcs

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
            style=self._style(obj, path, K.LINE_STYLE),
            data=self._data(obj, path),
        )
        items = self._array(obj, "links", path)
        if items is None:
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
            self.note(path, "not in this network: this link names no arc it can traverse")
            return None
        arc = arcs[arc_key]
        link = Link(arc=arc_key)
        for field_name in ("from", "to"):
            endpoint = self._endpoint(obj, field_name, path, arc, network)
            if field_name == "from":
                link.from_ = endpoint
            else:
                link.to = endpoint
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
            self.note(
                at,
                f"does not lie on arc {arc_name(network, arc)}: "
                f"{distance:.0f} m from it, tolerance is "
                f"{PROJECTION_TOLERANCE_M:.0f} m",
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


def arc_name(network: Network, arc: Arc) -> str:
    for key, candidate in (network.arcs or {}).items():
        if candidate is arc:
            return key
    return "?"

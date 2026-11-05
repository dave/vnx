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
from . import walk as W
from .errors import Diagnostic, VNXRefusal
from .jsonio import parse_json
from .model import (
    Area,
    Arc,
    Highlight,
    DataRecord,
    Document,
    Endpoint,
    Folder,
    JSONValue,
    Network,
    Pass,
    Position,
    Plan,
    Segment,
    Style,
    Track,
    Waypoint,
    AppKey,
    is_app_key,
)
from .pointer import join

__all__ = ["ReadResult", "read_bytes", "decode_document", "SUPPORTED_VERSION"]

# The MAJOR.MINOR this reader implements (SPEC 2.2).
SUPPORTED_VERSION = (0, 10)

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


def off_the_globe(lon: float, lat: float) -> Union[str, None]:
    """SPEC 4.1's normative ranges, both bounds included.  None when in range."""
    if not -180 <= lon <= 180:
        return f"longitude {lon!r} is off the globe"
    if not -90 <= lat <= 90:
        return f"latitude {lat!r} is off the globe"
    return None


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
        defect would produce two diagnostics.  SPEC 6.2 gives an absent REQUIRED
        key the same code as a wrong-typed one: the object is gone either way.
        """
        missing = [key for key, value in fields.items() if value is None]
        for key in missing:
            if key not in obj:
                self.note(
                    join(path, key), f"{what} must carry `{key}`", "wrong-type-required"
                )
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
            self.note(
                join(path, key), "must be UTC with a Z suffix", "timestamp-not-utc"
            )
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
                self.note(
                    at, "a data record must carry a string @type", "wrong-type-required"
                )
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
        # A value that is not an array fails as a TYPE and takes the caller's
        # code; one that is an array and still will not parse fails as a
        # position (SPEC 4.1) and takes position-unusable wherever it sits.
        unusable = "position-unusable"
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
                    f"channel {index} is {_json_type(channel)}, expected a number",
                    unusable,
                )
                return None
            channels.append(float(channel))
        # SPEC 4.1: a channel this version does not define is ignored.  The
        # loop above still checks it, because an array holding a string is not
        # a position whichever index the string sits at.
        lon, lat = channels[0], channels[1]
        assert lon is not None and lat is not None
        problem = off_the_globe(lon, lat)
        if problem is not None:
            self.note(path, problem, unusable)
            return None
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
            position = self._position(item, join(path, key, index), "position-unusable")
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

        # SPEC 3.9: an application key is recognised at the top level only,
        # so the document's known set is widened here and nowhere else.
        known = K.DOCUMENT | {key for key in value if is_app_key(key)}
        obj = self._object(value, "", known, "a document")
        assert obj is not None

        document = Document(vnx=version)
        document.generator = self._string(obj, "generator", "")
        document.created = self._timestamp(obj, "created", "")

        document.folders = self._collection(obj, "folders", self._folder)
        document.networks = self._collection(obj, "networks", self._network)
        document.plans = self._collection(obj, "plans", self._plan)
        document.tracks = self._collection(obj, "tracks", self._track)
        document.waypoints = self._collection(
            obj, "waypoints", lambda v, p: self._waypoint(v, p)
        )
        document.areas = self._collection(obj, "areas", self._area)

        # SPEC 3.9: application keys are opaque and MUST survive a
        # read-modify-write, a null and an empty value included.  Nothing
        # inside them is inspected, so nothing inside them can be unknown.
        # Sorted by key, as the reference implementation keeps them.
        document.app_keys = [
            AppKey(key, obj[key]) for key in sorted(obj) if is_app_key(key)
        ]
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
            ("plans", list(document.plans or [])),
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
        # the series this reader implements", not "above it".  0.1 is not an
        # older spelling of 0.10; this reader has no rules for it and refuses it
        # for the same reason it refuses 0.3.  Written as `>` -- the shape five
        # of the six implementations once had -- a 0.1.9 document reads as
        # though it were 0.10, under rules it was never written to.
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
            at = self._position(obj["at"], join(path, "at"), "wrong-type-required")
        style = self._style(obj, path)
        self._check_importance(style, path)
        if not self._complete(obj, path, "a waypoint", {"at": at}):
            return None
        return Waypoint(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path, K.REQUIRED_WAYPOINT),
            pinned=self._timestamp(obj, "pinned", path),
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
            pinned=self._timestamp(obj, "pinned", path),
            polygon=polygon,
            style=self._style(obj, path),
            data=self._data(obj, path),
        )

    def _polygon(self, value: JSONValue, path: str) -> Union[list[Position], None]:
        """One OPEN ring (SPEC 3.7).  None means the area must be dropped.

        ⚠ A CLOSED RING IS REPAIRED, NOT DROPPED.  Its last position repeating
        its first is one fact written twice, so dropping the repeat loses
        nothing; the diagnostic is kept so that a writer that closes rings can
        be seen.  The distinct positions are counted after that, so
        ``[A, B, A]`` is two points and its area goes, with both diagnostics.
        """
        if not isinstance(value, list):
            self.note(
                path, "a polygon must be an array of positions", "wrong-type-required"
            )
            return None
        if any(
            isinstance(item, list) and item and isinstance(item[0], list)
            for item in value
        ):
            self.note(
                path,
                "nested rings: this format carries one ring",
                "polygon-nested-rings",
            )
            return None
        ring: list[Position] = []
        for index, item in enumerate(value):
            position = self._position(item, join(path, index), "position-unusable")
            if position is None:
                return None
            ring.append(position)
        if W.ring_is_closed(ring):
            self.note(
                join(path, len(ring) - 1),
                "a ring is stored open, and this last position repeats the first",
                "ring-closed",
            )
            ring.pop()
        count = W.distinct_lon_lat(ring)
        if count < 3:
            self.note(
                path,
                f"a ring holds {count} distinct position(s), and needs at least 3",
                "ring-too-few-points",
            )
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
            pinned=self._timestamp(obj, "pinned", path),
            style=self._style(obj, path),
            segments=segments,
            data=self._data(obj, path),
        )

    def _plan(self, value: JSONValue, path: str) -> Union[Plan, None]:
        """SPEC 3.4.  A plan is its own highlight: where a highlight would be dropped,
        the whole plan is."""
        obj = self._object(value, path, K.PLAN, "a plan")
        if obj is None:
            return None
        plan = Plan(
            folder=self._string(obj, "folder", path),
            name=self._string(obj, "name", path),
            notes=self._string(obj, "notes", path),
            hidden=self._bool(obj, "hidden", path),
            pinned=self._timestamp(obj, "pinned", path),
            style=self._style(obj, path),
        )
        plan.nodes = self._nodes(obj, path)
        plan.arcs = self._arcs(obj, path, plan.nodes or {})
        plan.data = self._data(obj, path)
        walk = self._walk(obj, path, plan.arcs or {}, plan=True)
        if walk is None:
            return None
        plan.walk = walk
        return plan

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
            pinned=self._timestamp(obj, "pinned", path),
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
                area = self._area(item, join(path, "areas", index), K.AREA_IN_NETWORK)
                if area is not None:
                    network.areas.append(area)

        network.highlights = self._highlights(obj, path, network.arcs or {})
        return network

    def _nodes(
        self, obj: dict[str, JSONValue], path: str
    ) -> Union[dict[str, Position], None]:
        """A network's or a plan's `nodes` (SPEC 3.3.1).

        A node that will not parse is dropped, and an arc naming it is then
        dropped under `arc-node-absent`, so the loss surfaces on everything
        that depended on it.  A node carries no time: the time is dropped and
        the node kept.
        """
        if "nodes" not in obj:
            return None
        value = obj["nodes"]
        at = join(path, "nodes")
        if not isinstance(value, dict):
            self.note(at, "`nodes` must be an object keyed by node identifier")
            return None
        nodes: dict[str, Position] = {}
        for key, item in value.items():
            here = join(at, key)
            position = self._position(item, here, "position-unusable")
            if position is None:
                continue
            nodes[key] = self._extra_channel(position, here, keep_ele=True)
        return nodes

    def _arcs(
        self, obj: dict[str, JSONValue], path: str, nodes: dict[str, Position]
    ) -> Union[dict[str, Arc], None]:
        """A network's or a plan's `arcs` (SPEC 3.3.2).

        ⚠ A DROPPED ARC TAKES EVERY HIGHLIGHT WALKING IT WITH IT, but not here: the
        highlight is dropped when its walk is read, under `pass-arc-absent`, with a
        diagnostic of its own (SPEC 6.2).  That is why nodes and arcs are read
        before any walk.
        """
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
            arc = self._arc(entry, here, nodes)
            if arc is not None:
                arcs[key] = arc
        return arcs

    def _arc(
        self, entry: dict[str, JSONValue], here: str, nodes: dict[str, Position]
    ) -> Union[Arc, None]:
        ends: list[str] = []
        for end in ("from", "to"):
            name = entry.get(end)
            if not isinstance(name, str):
                what = "is missing" if end not in entry else "is not a node key"
                self.note(join(here, end), f"arc `{end}` {what}", "wrong-type-required")
                return None
            ends.append(name)
        if not isinstance(entry.get("pips"), list):
            what = "is missing" if "pips" not in entry else "is not an array"
            self.note(join(here, "pips"), f"arc `pips` {what}", "wrong-type-required")
            return None
        pips = self._positions(entry, "pips", here)
        if pips is None:
            # SPEC 6.2: an unusable pip drops the ARC, reported at the pip.
            return None
        arc = Arc(
            from_=ends[0],
            to=ends[1],
            pips=self._pip_times(pips, here),
            data=self._data(entry, here),
        )
        if W.is_self_loop(arc):
            self.note(
                here,
                f"arc runs from node {arc.from_!r} to itself; a self-loop cannot "
                "be walked",
                "self-loop-arc",
            )
            return None
        return arc if self._ends_present(arc, here, nodes) else None

    def _ends_present(self, arc: Arc, here: str, nodes: dict[str, Position]) -> bool:
        """SPEC 3.3.2: an arc naming a node its owner does not have is dropped.
        Each absent end is its own diagnostic, at that end."""
        present = True
        for end, name in (("from", arc.from_), ("to", arc.to)):
            if name not in nodes:
                present = False
                self.note(
                    join(here, end),
                    f"arc names {end} node {name!r}, which is not here",
                    "arc-node-absent",
                )
        return present

    def _pip_times(self, pips: list[Position], here: str) -> list[Position]:
        """SPEC 3.3.2: an arc pip carries no time.  Every time on the arc is
        dropped and the pips kept, with ONE diagnostic at the arc's `pips`."""
        if all(pip.time_ms is None for pip in pips):
            return pips
        self.note(
            join(here, "pips"),
            "an arc pip carries no time: every time on this arc is dropped",
            "arc-pip-time",
        )
        return [Position(pip.lon, pip.lat, pip.ele) for pip in pips]

    def _highlights(
        self, obj: dict[str, JSONValue], path: str, arcs: dict[str, Arc]
    ) -> Union[list[Highlight], None]:
        items = self._array(obj, "highlights", path)
        if items is None:
            return None
        highlights: list[Highlight] = []
        for index, item in enumerate(items):
            highlight = self._highlight(item, join(path, "highlights", index), arcs)
            if highlight is not None:
                highlights.append(highlight)
        return highlights

    def _highlight(
        self, value: JSONValue, path: str, arcs: dict[str, Arc]
    ) -> Union[Highlight, None]:
        obj = self._object(value, path, K.HIGHLIGHT, "a highlight")
        if obj is None:
            return None
        highlight = Highlight(
            name=self._string(obj, "name", path),
            hidden=self._bool(obj, "hidden", path),
            pinned=self._timestamp(obj, "pinned", path),
            data=self._data(obj, path),
        )
        walk = self._walk(obj, path, arcs, plan=False)
        if walk is None:
            return None
        highlight.walk = walk
        return highlight

    def _walk(
        self,
        obj: dict[str, JSONValue],
        owner: str,
        arcs: dict[str, Arc],
        plan: bool,
    ) -> Union[list[Pass], None]:
        """A highlight's or a plan's REQUIRED `walk`, checked in SPEC 6.2's order.

        None means the owner is dropped: a missing or wrong-typed `walk`, or the
        first pass that breaks a rule, which is reported alone -- nothing after
        it in the walk is examined.

        ⚠ A WALK IS USED WHOLE OR NOT AT ALL (SPEC 6.2).  Dropping a pass would
        hand the caller a different walk that reads as valid -- two arcs joined
        that do not meet, a direction reversed by a lost turn, a start moved to
        a node -- so any fault drops the owner.
        """
        path = join(owner, "walk")
        items = obj.get("walk")
        if not isinstance(items, list):
            what = "is missing" if "walk" not in obj else "is not an array"
            self.note(path, f"`walk` {what}", "wrong-type-required")
            return None
        walk: list[Pass] = []
        cursor = W.Cursor(last=len(items) - 1, plan=plan)
        for index, item in enumerate(items):
            at = join(path, index)
            # 1. The pass is an object naming an arc of its owner.
            arc_key = self._pass_arc(item, at, arcs)
            if arc_key is None:
                return None
            assert isinstance(item, dict)
            # 2. Unrecognised keys.
            self._object(item, at, K.PASS, "a pass")
            p = Pass(arc=arc_key)
            # 3. `from`, `to`, `turn` and `pin` each parse.
            for key in ("from", "to", "turn", "pin"):
                if key not in item:
                    continue
                endpoint = self._walk_position(item[key], join(at, key), key)
                if endpoint is None:
                    return None
                if key == "from":
                    p.from_ = endpoint
                elif key == "to":
                    p.to = endpoint
                elif key == "turn":
                    assert isinstance(endpoint, Position)
                    p.turn = endpoint
                else:
                    assert isinstance(endpoint, Position)
                    p.pin = endpoint
            # 4 to 7: the plan rule, pin and turn, spelling, connection.
            stepped = cursor.step(index, p, arcs[arc_key])
            if isinstance(stepped, W.WalkFault):
                self.note(stepped.path(path), stepped.message, stepped.code)
                return None
            walk.append(p)
        return walk

    def _pass_arc(
        self, item: JSONValue, at: str, arcs: dict[str, Arc]
    ) -> Union[str, None]:
        """SPEC 6.2's first check on a pass: it is an object, and its `arc`
        names an arc of its owner -- including one another rule dropped.  When
        it does not, nothing about the pass resolves, so the pass is named."""
        if not isinstance(item, dict):
            self.note(at, "a pass must be an object", "pass-arc-absent")
            return None
        arc_key = item.get("arc")
        if not isinstance(arc_key, str) or arc_key not in arcs:
            self.note(
                at, f"pass names arc {arc_key!r}, which is not here", "pass-arc-absent"
            )
            return None
        return arc_key

    def _walk_position(
        self, value: JSONValue, path: str, key: str
    ) -> Union[Endpoint, None]:
        """A pass's `from`, `to`, `turn` or `pin`: a node key where one is
        allowed, or a position carrying longitude and latitude only (SPEC 3.3.3).

        A position identifies the nearest point of its arc's line, at ANY
        distance, so nothing here measures one against the arc.
        """
        if isinstance(value, str) and key in ("from", "to"):
            return value
        position = self._position(value, path, "position-unusable")
        if position is None:
            return None
        return self._extra_channel(position, path, keep_ele=False)

    def _extra_channel(self, position: Position, path: str, keep_ele: bool) -> Position:
        """A channel the element may not carry: a time on a node (SPEC 3.3.1),
        or an elevation or a time on a walk position (SPEC 3.3.3).  The channel
        is dropped, the element kept, and the diagnostic names the element."""
        ele = position.ele if keep_ele else None
        if position.ele == ele and position.time_ms is None:
            return position
        self.note(
            path,
            "this position carries a channel it may not: the channel is dropped",
            "extra-channel",
        )
        return Position(position.lon, position.lat, ele)


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

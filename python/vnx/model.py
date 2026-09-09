"""The VNX document model (SPEC 3).

Two properties of this model are load-bearing and neither is obvious.

**A position keeps its arity.**  ``[lon, lat]`` and ``[lon, lat, null]`` state
the same thing (SPEC 4.1) but they are different bytes, and every file in
``corpus/accept/`` is asserted to survive a decode and re-encode byte for byte
(SPEC 8.1).  ``accept/absent-elevation-and-time.vnx`` is stored canonical
carrying a trailing ``null``, so a model that normalised the two spellings
could not reproduce it.  Shortening is a rule for a *writer* at the
serialisation boundary, alongside rounding (SPEC 4.5.4), not a rule for a
formatter.

**Absent is not empty and not false.**  ``None`` means the key was not present.
An empty ``data`` array, a ``hidden`` written as ``false``, and an omitted key
are three different documents, so they are three different models.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Union

__all__ = [
    "Position", "DataRecord", "Folder", "Arc", "Link", "Chain", "Network",
    "RouteArc", "Route", "Segment", "Track", "Waypoint", "Area", "Document",
    "Endpoint", "Style", "JSONValue",
]

JSONValue = Any
Style = dict[str, JSONValue]


@dataclass(frozen=True, slots=True)
class Position:
    """A fixed-channel coordinate array: ``[lon, lat, ele, time_ms]``.

    Channels from index 2 upward may be ``None``.  ``channels`` holds exactly
    what the document carried, including a trailing ``None`` and including any
    channel beyond index 3, which a reader must ignore but must not lose
    (SPEC 4.1).
    """

    channels: tuple[Union[float, None], ...]

    @property
    def lon(self) -> float:
        value = self.channels[0]
        assert value is not None
        return value

    @property
    def lat(self) -> float:
        value = self.channels[1]
        assert value is not None
        return value

    @property
    def ele(self) -> Union[float, None]:
        return self.channels[2] if len(self.channels) > 2 else None

    @property
    def time_ms(self) -> Union[float, None]:
        return self.channels[3] if len(self.channels) > 3 else None

    def same_point(self, other: "Position") -> bool:
        """Value equality in longitude and latitude (SPEC 3.3.3)."""
        return self.lon == other.lon and self.lat == other.lat


@dataclass(slots=True)
class DataRecord:
    """A record on an item's ``data`` bag (SPEC 5).

    ``body`` is the record's fields with ``@type`` removed.  It is opaque: an
    unrecognised type is inert and must survive a read-modify-write at value
    level, so the body is kept as the JSON it was.
    """

    type: str
    body: dict[str, JSONValue] = field(default_factory=dict)


# A chain link endpoint is a node key or an inline position (SPEC 3.3.3).
Endpoint = Union[str, Position]


@dataclass(slots=True)
class Folder:
    """SPEC 3.2 and 3.10.  `id` is REQUIRED; a folder without one is dropped."""

    id: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    parent: Union[str, None] = None
    hidden: Union[bool, None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Arc:
    """An arc owned by a network's ``arcs`` object (SPEC 3.3.2)."""

    from_: Union[str, None] = None
    to: Union[str, None] = None
    pips: Union[list[Position], None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Link:
    arc: Union[str, None] = None
    from_: Union[Endpoint, None] = None
    to: Union[Endpoint, None] = None


@dataclass(slots=True)
class Chain:
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    style: Union[Style, None] = None
    links: Union[list[Link], None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Network:
    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    nodes: Union[dict[str, Position], None] = None
    arcs: Union[dict[str, Arc], None] = None
    chains: Union[list[Chain], None] = None
    points: Union[list["Waypoint"], None] = None
    areas: Union[list["Area"], None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class RouteArc:
    """An arc owned outright by a route (SPEC 3.4): no identifier, no endpoints."""

    pips: Union[list[Position], None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Route:
    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    style: Union[Style, None] = None
    arcs: Union[list[RouteArc], None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Segment:
    pips: Union[list[Position], None] = None


@dataclass(slots=True)
class Track:
    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    started: Union[str, None] = None
    ended: Union[str, None] = None
    hidden: Union[bool, None] = None
    style: Union[Style, None] = None
    segments: Union[list[Segment], None] = None
    data: Union[list[DataRecord], None] = None

    def moving_time_ms(self) -> Union[float, None]:
        """Moving time, or ``None`` for unknown (SPEC 3.5).

        The sum over segments of the difference between the last and first pip
        times in that segment.  Deliberately not the wall-clock span between
        ``started`` and ``ended``, which pauses lengthen.

        ``None`` when no pip in the track carries a time.  Unknown is not zero:
        zero is indistinguishable from a track on which nobody moved.
        """
        total = 0.0
        seen = False
        for segment in self.segments or []:
            times = [
                pip.time_ms for pip in (segment.pips or []) if pip.time_ms is not None
            ]
            if not times:
                continue
            seen = True
            total += times[-1] - times[0]
        return total if seen else None


@dataclass(slots=True)
class Waypoint:
    """A top-level waypoint or a network's point: one shape (SPEC 3.6)."""

    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    at: Union[Position, None] = None
    style: Union[Style, None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Area:
    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    polygon: Union[list[Position], None] = None
    style: Union[Style, None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Document:
    vnx: str = "0.1.0"
    profile: Union[str, None] = None
    generator: Union[str, None] = None
    created: Union[str, None] = None
    folders: Union[list[Folder], None] = None
    networks: Union[list[Network], None] = None
    routes: Union[list[Route], None] = None
    tracks: Union[list[Track], None] = None
    waypoints: Union[list[Waypoint], None] = None
    areas: Union[list[Area], None] = None
    # SPEC 3.9: rulesets are not defined in 0.1.  Their contents are opaque and
    # MUST survive a read-modify-write, so they are kept as raw JSON.
    rulesets: Union[list[JSONValue], None] = None

    def counts(self) -> dict[str, int]:
        """Item counts per top-level collection, as the corpus sidecar spells them."""
        return {
            "folders": len(self.folders or []),
            "networks": len(self.networks or []),
            "routes": len(self.routes or []),
            "tracks": len(self.tracks or []),
            "waypoints": len(self.waypoints or []),
            "areas": len(self.areas or []),
            "rulesets": len(self.rulesets or []),
        }

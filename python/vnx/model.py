"""The VNX document model (SPEC 3).

Two properties of this model are load-bearing and neither is obvious.

**A position does not keep the arity the document spelled.**  ``[lon, lat]``
and ``[lon, lat, null]`` state the same thing (SPEC 4.1), so they are one
model, and the encoder writes the shortest spelling of it.  The bytes of a
document are not a rule, and a comparison of two documents applies the same
normalisation (SPEC 8.1).

**Absent is not empty and not false.**  ``None`` means the key was not present.
An empty ``data`` array, a ``hidden`` written as ``false``, and an omitted key
are three different documents, so they are three different models.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Union

__all__ = [
    "Position",
    "DataRecord",
    "Folder",
    "Arc",
    "Pass",
    "Highlight",
    "Network",
    "Plan",
    "Segment",
    "Track",
    "Point",
    "Area",
    "Document",
    "Endpoint",
    "Style",
    "JSONValue",
    "AppKey",
    "is_app_key",
]

JSONValue = Any
Style = dict[str, JSONValue]


@dataclass(frozen=True, slots=True)
class Position:
    """A coordinate: longitude, latitude, an optional elevation and time.

    On the wire this is a fixed-channel array ``[lon, lat, ele, time_ms]``
    (SPEC 4.1), where a channel from index 2 upward may be ``null`` and a
    trailing channel may be left out.  A channel the format does not define is
    ignored on the way in and is not written back out.
    """

    lon: float
    lat: float
    ele: Union[float, None] = None
    time_ms: Union[float, None] = None

    def same_point(self, other: "Position") -> bool:
        """Value equality in longitude and latitude, the test SPEC 3.7 uses."""
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


# Where a pass enters or leaves its arc: a node key, or a position on the arc
# (SPEC 3.3.3).
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
    """An arc of a network or a plan (SPEC 3.3.2).

    Its line runs from the ``from_`` node's position, through ``pips``, to the
    ``to`` node's position.  ``pips`` holds the INTERIOR points only, and an
    empty list is the straight line between the two nodes.  A pip carries no
    time: the reader drops one with a diagnostic, and the writer refuses one.
    """

    from_: Union[str, None] = None
    to: Union[str, None] = None
    pips: Union[list[Position], None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Pass:
    """One entry of a walk (SPEC 3.3.3): the arc it covers, and where on it the
    walk enters, turns and leaves.

    ``from_`` and ``to`` are present only where SPEC 3.3.3's rule puts them: on
    the first pass, on the last pass, and on a pass whose ``from_`` is a
    position.  Everywhere else the entry is the node the walk is at and the
    exit is derived, so ``None`` there is that absence, not "unknown".

    A position here carries longitude and latitude only: its elevation is the
    arc line's.  A pass is pinned when ``pin`` is set: the user fixed it with a
    handle at that position, and the pin leaves what the pass covers unchanged.
    """

    arc: str
    from_: Union[Endpoint, None] = None
    to: Union[Endpoint, None] = None
    turn: Union[Position, None] = None
    pin: Union[Position, None] = None


@dataclass(slots=True)
class Highlight:
    """SPEC 3.3.3.  A lightweight container for data objects along a walk of its
    network's arcs.  It has an optional name, and no notes or style of its own."""

    name: Union[str, None] = None
    hidden: Union[bool, None] = None
    pinned: Union[str, None] = None
    walk: Union[list[Pass], None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Network:
    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    pinned: Union[str, None] = None
    nodes: Union[dict[str, Position], None] = None
    arcs: Union[dict[str, Arc], None] = None
    highlights: Union[list[Highlight], None] = None
    points: Union[list["Point"], None] = None
    areas: Union[list["Area"], None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Plan:
    """A plan owns its graph (SPEC 3.4): nodes and arcs of a network's shape,
    and one walk over them that runs node to node."""

    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    pinned: Union[str, None] = None
    style: Union[Style, None] = None
    nodes: Union[dict[str, Position], None] = None
    arcs: Union[dict[str, Arc], None] = None
    walk: Union[list[Pass], None] = None
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
    pinned: Union[str, None] = None
    style: Union[Style, None] = None
    segments: Union[list[Segment], None] = None
    data: Union[list[DataRecord], None] = None

    def moving_time_ms(self) -> Union[float, None]:
        """Moving time, or ``None`` for unknown (SPEC 3.5).

        The sum over segments of the difference between the last and first pip
        times in that segment.  Deliberately not the wall-clock span between
        ``started`` and ``ended``, which pauses lengthen.

        Derivable only when EVERY segment carries a time on its first and last
        pip.  A sum over the segments that happen to be timed is
        indistinguishable from a complete one, and no consumer can tell which it
        was given: zero is at least recognisable as suspicious, a partial sum is
        not.  This library summed what it had until that was ruled otherwise, as
        did two other implementations including the reference one, and every
        corpus tier stayed green for all three.

        ``None`` for a track with no segments as well: unknown, not zero.
        """
        segments = self.segments or []
        if not segments:
            return None
        total = 0.0
        for segment in segments:
            pips = segment.pips or []
            if not pips:
                return None
            first, last = pips[0].time_ms, pips[-1].time_ms
            if first is None or last is None:
                return None
            total += last - first
        return total


@dataclass(slots=True)
class Point:
    """A top-level point or a network's point: one shape (SPEC 3.6)."""

    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    pinned: Union[str, None] = None
    at: Union[Position, None] = None
    style: Union[Style, None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class Area:
    folder: Union[str, None] = None
    name: Union[str, None] = None
    notes: Union[str, None] = None
    hidden: Union[bool, None] = None
    pinned: Union[str, None] = None
    polygon: Union[list[Position], None] = None
    style: Union[Style, None] = None
    data: Union[list[DataRecord], None] = None


@dataclass(slots=True)
class AppKey:
    """One application key (SPEC 3.9): a top-level key with a full stop."""

    key: str
    # Required: a JSON `null` is written `None` on purpose, never by default.
    value: JSONValue


def is_app_key(key: str) -> bool:
    """Whether a top-level key is an application key: it contains a full stop.

    No key the format defines contains one (SPEC 3.9).
    """
    return "." in key


@dataclass(slots=True)
class Document:
    vnx: str = "0.10.6"
    generator: Union[str, None] = None
    created: Union[str, None] = None
    folders: Union[list[Folder], None] = None
    networks: Union[list[Network], None] = None
    plans: Union[list[Plan], None] = None
    tracks: Union[list[Track], None] = None
    points: Union[list[Point], None] = None
    areas: Union[list[Area], None] = None
    # SPEC 3.9: every top-level key containing a full stop, in key order.  A
    # list of pairs rather than a dict, matching the other implementations.
    # Each value is raw JSON, never inspected; a JSON null is a present key
    # whose value is None, not an absent key.
    app_keys: list[AppKey] = field(default_factory=list)

    def counts(self) -> dict[str, int]:
        """Item counts per top-level collection, as the corpus sidecar spells them."""
        return {
            "folders": len(self.folders or []),
            "networks": len(self.networks or []),
            "plans": len(self.plans or []),
            "tracks": len(self.tracks or []),
            "points": len(self.points or []),
            "areas": len(self.areas or []),
        }

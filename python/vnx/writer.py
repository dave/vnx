"""The writer emit path (SPEC 4.1).

A writer emits a model as a document and applies the emit-boundary rules on the
way: longitude and latitude to 6 decimal places, elevation to 1, and the
shortest spelling of every position.  The shortest spelling is ``encode.py``'s,
because it is a property of the tree; the rounding is here.

SPEC 4.1 says rounding MUST NOT happen in the model.  So this builds a NEW
model and encodes that; the caller's document is never touched.  Rounding in
place would change which positions an arc de-duplication or a ring winding
considers equal, which is a change to the geometry rather than to its spelling.

Two traps live here, and neither is reachable by any corpus tier, because every
tier re-encodes a document that already satisfies the rules:

* **Round by FORMATTING, never by scaling.**  ``round(v * 1e6) / 1e6`` is a
  different operation: the scaled product is itself a binary64 and is not
  always the exact scaled value, so the multiplication manufactures ties the
  true decimal never had and destroys ones it did.  Go took that route and
  disagreed with ``corpus/numbers/`` on 175 of 5,329 rows.
* **Rounding can CREATE a negative zero.**  ``-0.04`` at one place is a zero
  with its sign bit set.  It is the same value as ``0`` and carries no
  information, so it is normalised here, where rounding produced it.
"""

from __future__ import annotations

import json
from dataclasses import replace
from typing import Union

from .encode import encode_document
from .model import (
    Area, Arc, Chain, Document, Endpoint, Link, Network, Position, Route,
    RouteArc, Segment, Track, Waypoint,
)

__all__ = [
    "encode_bytes", "round_position", "rounded_document", "LONLAT_PLACES",
    "ELEVATION_PLACES",
]

# SPEC 4.1.  Six places of latitude is about 11 cm, and a consumer GPS
# receiver is accurate to 3 to 5 m, so digits below the sixth are the
# arithmetic's noise rather than the instrument's reading.
LONLAT_PLACES = 6
ELEVATION_PLACES = 1


def encode_bytes(document: Document) -> bytes:
    """Emit *document*: rounded, shortest-spelled, compact JSON.

    ``separators`` makes it compact.  Whitespace and key order are the writer's
    own business (SPEC 8.1), and the smallest spelling is the useful one.
    ``allow_nan`` is off because ``NaN`` and ``Infinity`` are not JSON and
    SPEC 4.2 forbids them throughout a document.
    """
    tree = encode_document(rounded_document(document))
    text = json.dumps(
        tree, separators=(",", ":"), ensure_ascii=False, allow_nan=False
    )
    return text.encode("utf-8")


def round_to(value: float, places: int) -> float:
    """The value with at most *places* decimals nearest to *value* (SPEC 4.1).

    By formatting and parsing back, which is the operation SPEC 4.1 defines.
    Python's formatting is correctly rounded and breaks a tie to even.
    """
    rounded = float(f"{value:.{places}f}")
    if rounded == 0.0:
        # Catches both zeros: rounding a small negative value produces a zero
        # with its sign bit set, which no document ever carried.
        return 0.0
    return rounded


def round_position(position: Position) -> Position:
    """Apply SPEC 4.1's precision to one position.

    Time is a count of milliseconds and is never rounded: there is nothing
    below its last digit to round away.  An absent elevation stays absent --
    rounding a missing elevation into 0.0 would put every unknown point at sea
    level.
    """
    return Position(
        round_to(position.lon, LONLAT_PLACES),
        round_to(position.lat, LONLAT_PLACES),
        None if position.ele is None else round_to(position.ele, ELEVATION_PLACES),
        position.time_ms,
    )


def _unhidden(item: object) -> None:
    """SPEC 3.8: a writer emits `hidden` only when it is true.

    Absence already means visible, so an explicit false carries nothing.
    """
    if getattr(item, "hidden", None) is False:
        setattr(item, "hidden", None)


def _positions(items: Union[list[Position], None]) -> Union[list[Position], None]:
    if items is None:
        return None
    return [round_position(item) for item in items]


def _endpoint(value: Union[Endpoint, None]) -> Union[Endpoint, None]:
    if isinstance(value, Position):
        return round_position(value)
    return value


def rounded_document(document: Document) -> Document:
    """A copy of *document* with every position at writer precision.

    The original is not modified: SPEC 4.1 keeps rounding out of the model.
    Everything that is not a position -- data records, styles, rulesets -- is
    shared rather than copied, because nothing here mutates it.
    """
    folders = None
    if document.folders is not None:
        folders = [replace(folder) for folder in document.folders]
        for folder in folders:
            _unhidden(folder)
    return replace(
        document,
        folders=folders,
        networks=_networks(document.networks),
        routes=_routes(document.routes),
        tracks=_tracks(document.tracks),
        waypoints=_waypoints(document.waypoints),
        areas=_areas(document.areas),
    )


def _networks(items: Union[list[Network], None]) -> Union[list[Network], None]:
    if items is None:
        return None
    out = []
    for network in items:
        nodes = (
            None
            if network.nodes is None
            else {key: round_position(value) for key, value in network.nodes.items()}
        )
        arcs = (
            None
            if network.arcs is None
            else {
                key: replace(arc, pips=_positions(arc.pips))
                for key, arc in network.arcs.items()
            }
        )
        rebuilt = replace(
                network,
                nodes=nodes,
                arcs=arcs,
                chains=_chains(network.chains),
                points=_waypoints(network.points),
                areas=_areas(network.areas),
        )
        _unhidden(rebuilt)
        out.append(rebuilt)
    return out


def _chains(items: Union[list[Chain], None]) -> Union[list[Chain], None]:
    if items is None:
        return None
    out = []
    for chain in items:
        rebuilt = replace(
            chain,
            links=None
            if chain.links is None
            else [
                replace(link, from_=_endpoint(link.from_), to=_endpoint(link.to))
                for link in chain.links
            ],
        )
        _unhidden(rebuilt)
        out.append(rebuilt)
    return out


def _routes(items: Union[list[Route], None]) -> Union[list[Route], None]:
    if items is None:
        return None
    out = []
    for route in items:
        rebuilt = replace(
            route,
            arcs=None
            if route.arcs is None
            else [replace(arc, pips=_positions(arc.pips)) for arc in route.arcs],
        )
        _unhidden(rebuilt)
        out.append(rebuilt)
    return out


def _tracks(items: Union[list[Track], None]) -> Union[list[Track], None]:
    if items is None:
        return None
    out = []
    for track in items:
        rebuilt = replace(
            track,
            segments=None
            if track.segments is None
            else [
                replace(segment, pips=_positions(segment.pips))
                for segment in track.segments
            ],
        )
        _unhidden(rebuilt)
        out.append(rebuilt)
    return out


def _waypoints(items: Union[list[Waypoint], None]) -> Union[list[Waypoint], None]:
    if items is None:
        return None
    out = []
    for point in items:
        rebuilt = replace(point, at=None if point.at is None else round_position(point.at))
        _unhidden(rebuilt)
        out.append(rebuilt)
    return out


def _areas(items: Union[list[Area], None]) -> Union[list[Area], None]:
    if items is None:
        return None
    out = []
    for area in items:
        rebuilt = replace(area, polygon=_positions(area.polygon))
        _unhidden(rebuilt)
        out.append(rebuilt)
    return out

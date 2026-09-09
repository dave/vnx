"""The writer emit path (SPEC 4.5.4 and 4.1).

The format has two emit paths and a library owes both.

* A **formatter** reproduces what a document carried: it does not round and it
  does not re-spell a position.  ``reformat_bytes``.  SPEC 4.5.4 is explicit --
  "A reader MUST NOT round, and neither MUST a formatter" -- and
  ``corpus/accept/full-precision-coordinate.vnx`` round-trips byte for byte
  only because of it.
* A **writer** emits a model as a document, applying the writer-boundary rules:
  longitude and latitude to 6 decimal places, elevation to 1, and the shortest
  spelling of every position.  ``encode_bytes``.

The same file cannot come out of both unchanged, and that is the point:
``full-precision-coordinate.vnx`` reformats to itself and encodes to
``-3.671235``, which its own sidecar says is correct.

SPEC 4.5.4 also says rounding MUST NOT happen in the model.  So this builds a
NEW model and renders that; the caller's document is never touched.  Rounding in
place would change which positions an arc de-duplication or a ring winding
considers equal, which is a change to the geometry rather than to its spelling.

Two traps live here rather than in the renderer, and neither is reachable by any
corpus tier, because every tier re-renders a document that already exists:

* **Rounding can CREATE a negative zero.** ``round(-0.04, 1)`` is ``-0.0`` with
  the sign bit set.  SPEC 4.5.3's negative-zero rule therefore applies to what
  rounding produced, not only to what the document carried.  It is applied by
  the renderer, which is the only thing here that decides what a number looks
  like -- never a formatting primitive: ``format(-0.04, '.1f')`` is ``-0.0`` and
  ``format(340.0, '.1f')`` is ``340.0``, and both are wrong bytes.
* **Ties round to even** (SPEC 4.5.3).  Python's ``round`` does this on the true
  binary value, measured rather than assumed; see ``tests/test_numbers.py``.
"""

from __future__ import annotations

from dataclasses import replace
from typing import Union

from .canonical import render_document
from .encode import encode_document
from .model import (
    Area, Arc, Chain, Document, Endpoint, Link, Network, Position, Route,
    RouteArc, Segment, Track, Waypoint,
)

__all__ = ["encode_bytes", "rounded_document", "LONLAT_PLACES", "ELEVATION_PLACES"]

# SPEC 4.5.4.  Six places of latitude is about 11 cm, and a consumer GPS
# receiver is accurate to 3 to 5 m, so digits below the sixth are the
# arithmetic's noise rather than the instrument's reading.
LONLAT_PLACES = 6
ELEVATION_PLACES = 1


def encode_bytes(document: Document) -> bytes:
    """Emit *document* as a writer: rounded, shortest-spelled, canonical."""
    return render_document(encode_document(rounded_document(document)))


def round_position(position: Position) -> Position:
    """Apply the writer-boundary rules to one position (SPEC 4.5.4, 4.1).

    Time is a count of milliseconds and is never rounded.  A channel beyond the
    fourth is carried through untouched: this format does not define one, and
    inventing a rounding rule for a channel whose units are unknown would be a
    guess.
    """
    channels: list[Union[float, None]] = list(position.channels)
    for index in (0, 1):
        value = channels[index]
        if value is not None:
            channels[index] = round(value, LONLAT_PLACES)
    if len(channels) > 2 and channels[2] is not None:
        channels[2] = round(channels[2], ELEVATION_PLACES)

    # SPEC 4.1: a writer emits the shortest spelling.  Only TRAILING absences
    # go: an interior `null` cannot be dropped, because doing so would move the
    # time into the elevation channel.
    while len(channels) > 2 and channels[-1] is None:
        channels.pop()
    return Position(tuple(channels))


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

    The original is not modified: SPEC 4.5.4 keeps rounding out of the model.
    Everything that is not a position -- data records, styles, rulesets -- is
    shared rather than copied, because nothing here mutates it.
    """
    return replace(
        document,
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
        out.append(
            replace(
                network,
                nodes=nodes,
                arcs=arcs,
                chains=_chains(network.chains),
                points=_waypoints(network.points),
                areas=_areas(network.areas),
            )
        )
    return out


def _chains(items: Union[list[Chain], None]) -> Union[list[Chain], None]:
    if items is None:
        return None
    return [
        replace(
            chain,
            links=None
            if chain.links is None
            else [
                replace(link, from_=_endpoint(link.from_), to=_endpoint(link.to))
                for link in chain.links
            ],
        )
        for chain in items
    ]


def _routes(items: Union[list[Route], None]) -> Union[list[Route], None]:
    if items is None:
        return None
    return [
        replace(
            route,
            arcs=None
            if route.arcs is None
            else [replace(arc, pips=_positions(arc.pips)) for arc in route.arcs],
        )
        for route in items
    ]


def _tracks(items: Union[list[Track], None]) -> Union[list[Track], None]:
    if items is None:
        return None
    return [
        replace(
            track,
            segments=None
            if track.segments is None
            else [
                replace(segment, pips=_positions(segment.pips))
                for segment in track.segments
            ],
        )
        for track in items
    ]


def _waypoints(items: Union[list[Waypoint], None]) -> Union[list[Waypoint], None]:
    if items is None:
        return None
    return [
        replace(point, at=None if point.at is None else round_position(point.at))
        for point in items
    ]


def _areas(items: Union[list[Area], None]) -> Union[list[Area], None]:
    if items is None:
        return None
    return [replace(area, polygon=_positions(area.polygon)) for area in items]

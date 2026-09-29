"""Turning the model into a JSON value tree.

``writer.py`` hands that tree to ``json`` for the bytes.  Key order is the key
order of each object built here, and it carries no meaning: SPEC 4.4 says a
reader MUST accept any order.

A key absent from the model is absent from the output.  Nothing is written as a
default: ``hidden`` is written only when the model carries it (SPEC 3.8), and an
empty ``data`` array only when the document had one.
"""

from __future__ import annotations

from typing import Any, Union

from . import walk as W
from .model import (
    Area, Arc, Chain, DataRecord, Document, Endpoint, Folder, JSONValue,
    Network, Pass, Position, Route, Segment, Track, Waypoint, is_app_key,
)

__all__ = ["encode_document"]


def _position(position: Position) -> list[JSONValue]:
    """The shortest spelling of *position* (SPEC 4.1).

    Only TRAILING absences go.  An interior ``null`` stays, because dropping it
    would move the time into the elevation channel.
    """
    channels: list[Union[float, None]] = [
        position.lon, position.lat, position.ele, position.time_ms
    ]
    while len(channels) > 2 and channels[-1] is None:
        channels.pop()
    return channels


def _put(obj: dict[str, JSONValue], key: str, value: Any) -> None:
    if value is not None:
        obj[key] = value


def _positions(items: Union[list[Position], None]) -> Union[list[JSONValue], None]:
    if items is None:
        return None
    return [_position(item) for item in items]


def _records(items: Union[list[DataRecord], None]) -> Union[list[JSONValue], None]:
    if items is None:
        return None
    return [{"@type": record.type, **record.body} for record in items]


def _endpoint(value: Union[Endpoint, None]) -> Union[JSONValue, None]:
    if value is None:
        return None
    if isinstance(value, str):
        return value
    return _position(value)


def _common(obj: dict[str, JSONValue], item: Any) -> None:
    """The keys SPEC 3.2 and 3.8 give to items generally."""
    _put(obj, "folder", getattr(item, "folder", None))
    _put(obj, "name", item.name)
    _put(obj, "notes", getattr(item, "notes", None))
    _put(obj, "hidden", item.hidden)


def _folder(folder: Folder) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _put(obj, "id", folder.id)
    _put(obj, "name", folder.name)
    _put(obj, "notes", folder.notes)
    _put(obj, "parent", folder.parent)
    _put(obj, "hidden", folder.hidden)
    _put(obj, "data", _records(folder.data))
    return obj


def _waypoint(point: Waypoint) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _common(obj, point)
    _put(obj, "at", _position(point.at) if point.at is not None else None)
    _put(obj, "style", point.style)
    _put(obj, "data", _records(point.data))
    return obj


def _area(area: Area) -> JSONValue:
    if area.polygon is not None:
        problem = W.check_ring(area.polygon)
        if problem is not None:
            raise ValueError(f"area {area.name!r}: {problem} (SPEC 3.7)")
    obj: dict[str, JSONValue] = {}
    _common(obj, area)
    _put(obj, "polygon", _positions(area.polygon))
    _put(obj, "style", area.style)
    _put(obj, "data", _records(area.data))
    return obj


def _arc(arc: Arc) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _put(obj, "from", arc.from_)
    _put(obj, "to", arc.to)
    _put(obj, "pips", _positions(arc.pips))
    _put(obj, "data", _records(arc.data))
    return obj


def _pass(p: Pass) -> JSONValue:
    obj: dict[str, JSONValue] = {"arc": p.arc}
    _put(obj, "from", _endpoint(p.from_))
    _put(obj, "to", _endpoint(p.to))
    _put(obj, "turn", _endpoint(p.turn))
    # SPEC 3.3.3: `pinned` is written only when it is true.
    if p.pinned:
        obj["pinned"] = True
    return obj


def _walk(walk: Union[list[Pass], None], arcs: dict[str, Arc], route: bool, owner: str) -> Union[list[JSONValue], None]:
    """A walk, checked first: a writer never emits one its own reader drops."""
    if walk is None:
        return None
    fault = W.check_walk(walk, arcs, route)
    if fault is not None:
        raise ValueError(f"{owner}: {fault.describe()} (SPEC 3.3.3)")
    return [_pass(p) for p in walk]


def _chain(chain: Chain, arcs: dict[str, Arc]) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _put(obj, "name", chain.name)
    _put(obj, "notes", chain.notes)
    _put(obj, "hidden", chain.hidden)
    _put(obj, "style", chain.style)
    _put(obj, "walk", _walk(chain.walk, arcs, False, f"chain {chain.name!r}"))
    _put(obj, "data", _records(chain.data))
    return obj


def _graph(
    obj: dict[str, JSONValue],
    nodes: Union[dict[str, Position], None],
    arcs: Union[dict[str, Arc], None],
    owner: str,
) -> None:
    """The `nodes` and `arcs` a network and a route share (SPEC 3.4)."""
    problem = W.check_arcs(nodes or {}, arcs or {})
    if problem is not None:
        raise ValueError(f"{owner}: {problem} (SPEC 3.3.2)")
    if nodes is not None:
        for key, position in nodes.items():
            if position.time_ms is not None:
                raise ValueError(
                    f"{owner}: node {key!r} carries a time, and a node carries "
                    "none (SPEC 3.3.1)"
                )
        obj["nodes"] = {key: _position(position) for key, position in nodes.items()}
    if arcs is not None:
        obj["arcs"] = {key: _arc(arc) for key, arc in arcs.items()}


def _network(network: Network) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _common(obj, network)
    _graph(obj, network.nodes, network.arcs, f"network {network.name!r}")
    if network.chains is not None:
        obj["chains"] = [_chain(chain, network.arcs or {}) for chain in network.chains]
    if network.points is not None:
        obj["points"] = [_waypoint(point) for point in network.points]
    if network.areas is not None:
        obj["areas"] = [_area(area) for area in network.areas]
    _put(obj, "data", _records(network.data))
    return obj


def _route(route: Route) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    owner = f"route {route.name!r}"
    _common(obj, route)
    _put(obj, "style", route.style)
    _graph(obj, route.nodes, route.arcs, owner)
    _put(obj, "walk", _walk(route.walk, route.arcs or {}, True, owner))
    _put(obj, "data", _records(route.data))
    return obj


def _segment(segment: Segment) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _put(obj, "pips", _positions(segment.pips))
    return obj


def _track(track: Track) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _common(obj, track)
    _put(obj, "started", track.started)
    _put(obj, "ended", track.ended)
    _put(obj, "style", track.style)
    if track.segments is not None:
        obj["segments"] = [_segment(segment) for segment in track.segments]
    _put(obj, "data", _records(track.data))
    return obj


def encode_document(document: Document) -> JSONValue:
    obj: dict[str, JSONValue] = {"vnx": document.vnx}
    _put(obj, "generator", document.generator)
    _put(obj, "created", document.created)
    if document.folders is not None:
        obj["folders"] = [_folder(item) for item in document.folders]
    if document.networks is not None:
        obj["networks"] = [_network(item) for item in document.networks]
    if document.routes is not None:
        obj["routes"] = [_route(item) for item in document.routes]
    if document.tracks is not None:
        obj["tracks"] = [_track(item) for item in document.tracks]
    if document.waypoints is not None:
        obj["waypoints"] = [_waypoint(item) for item in document.waypoints]
    if document.areas is not None:
        obj["areas"] = [_area(item) for item in document.areas]
    for app_key in document.app_keys:
        # SPEC 3.9.  A key without a full stop would be read back as an
        # unrecognised key and dropped, or as one of the format's own keys; a
        # key written twice would leave the reader's parser to pick one.
        if not is_app_key(app_key.key):
            raise ValueError(
                f"application key {app_key.key!r} contains no full stop (SPEC 3.9)"
            )
        if app_key.key in obj:
            raise ValueError(f"application key {app_key.key!r} appears twice")
        obj[app_key.key] = app_key.value
    return obj

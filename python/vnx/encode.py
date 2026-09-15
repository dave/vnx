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

from .model import (
    Area, Arc, Chain, DataRecord, Document, Endpoint, Folder, JSONValue, Link,
    Network, Position, Route, RouteArc, Segment, Track, Waypoint,
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


def _link(link: Link) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _put(obj, "arc", link.arc)
    _put(obj, "from", _endpoint(link.from_))
    _put(obj, "to", _endpoint(link.to))
    return obj


def _chain(chain: Chain) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _put(obj, "name", chain.name)
    _put(obj, "notes", chain.notes)
    _put(obj, "hidden", chain.hidden)
    _put(obj, "style", chain.style)
    if chain.links is not None:
        obj["links"] = [_link(link) for link in chain.links]
    _put(obj, "data", _records(chain.data))
    return obj


def _network(network: Network) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _common(obj, network)
    if network.nodes is not None:
        obj["nodes"] = {
            key: _position(position) for key, position in network.nodes.items()
        }
    if network.arcs is not None:
        obj["arcs"] = {key: _arc(arc) for key, arc in network.arcs.items()}
    if network.chains is not None:
        obj["chains"] = [_chain(chain) for chain in network.chains]
    if network.points is not None:
        obj["points"] = [_waypoint(point) for point in network.points]
    if network.areas is not None:
        obj["areas"] = [_area(area) for area in network.areas]
    _put(obj, "data", _records(network.data))
    return obj


def _route_arc(arc: RouteArc) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _put(obj, "pips", _positions(arc.pips))
    _put(obj, "data", _records(arc.data))
    return obj


def _route(route: Route) -> JSONValue:
    obj: dict[str, JSONValue] = {}
    _common(obj, route)
    _put(obj, "style", route.style)
    if route.arcs is not None:
        obj["arcs"] = [_route_arc(arc) for arc in route.arcs]
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
    if document.rulesets is not None:
        obj["rulesets"] = document.rulesets
    return obj

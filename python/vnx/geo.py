"""Distance from a point to a polyline, for the inline-endpoint tolerance.

SPEC 3.3.3 gives a projection tolerance of 50 metres and does not say which
distance function measures it.  This module uses a local equirectangular
projection about the query point -- longitude scaled by ``cos(lat)``, a mean
Earth radius of 6 371 008.8 m -- and an exact point-to-segment distance in that
plane.  Over the tens of metres the tolerance is about, the error of that
approximation is millimetres.

The choice is this implementation's, not the specification's.  See the report.
"""

from __future__ import annotations

import math

__all__ = ["EARTH_RADIUS_M", "distance_between", "distance_to_polyline"]

EARTH_RADIUS_M = 6_371_008.8


def _project(lon: float, lat: float, lat0: float) -> tuple[float, float]:
    scale = math.pi / 180.0 * EARTH_RADIUS_M
    return (
        math.radians(lon) * EARTH_RADIUS_M * math.cos(math.radians(lat0)),
        lat * scale,
    )


def _point_to_segment(
    p: tuple[float, float], a: tuple[float, float], b: tuple[float, float]
) -> float:
    abx, aby = b[0] - a[0], b[1] - a[1]
    apx, apy = p[0] - a[0], p[1] - a[1]
    length2 = abx * abx + aby * aby
    if length2 == 0.0:
        return math.hypot(apx, apy)
    t = (apx * abx + apy * aby) / length2
    t = max(0.0, min(1.0, t))
    return math.hypot(apx - t * abx, apy - t * aby)


def distance_to_polyline(
    point: tuple[float, float], polyline: list[tuple[float, float]]
) -> float:
    """Least distance in metres from ``(lon, lat)`` to a ``(lon, lat)`` polyline.

    A polyline of one vertex is a point.  An empty polyline has no distance and
    raises, because the caller has nothing to project onto and must say so
    rather than invent a number.
    """
    if not polyline:
        raise ValueError("cannot project onto an empty polyline")
    lat0 = point[1]
    p = _project(point[0], point[1], lat0)
    projected = [_project(lon, lat, lat0) for lon, lat in polyline]
    if len(projected) == 1:
        return math.hypot(p[0] - projected[0][0], p[1] - projected[0][1])
    return min(
        _point_to_segment(p, projected[i], projected[i + 1])
        for i in range(len(projected) - 1)
    )


def distance_between(
    first: tuple[float, float], second: tuple[float, float]
) -> float:
    """Distance in metres between two ``(lon, lat)`` points.

    SPEC 3.3.2 measures a terminal pip against its node with the same tolerance
    SPEC 3.3.3 uses, so it is measured with the same projection: the pair are
    one question about two kinds of object, and two distance models for one
    question would be worse than one approximate one.
    """
    return distance_to_polyline(first, [second])

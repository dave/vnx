"""VNX -- Viatrail Network eXchange.

A codec for the VNX interchange format: it reads and writes ``.vnx`` and
``.vnz``, and reports diagnostics.  That is what ``SPEC.md`` calls a VNX
library.  The GPX, KML and GeoJSON writers are conversion tools rather than
part of the format and are not here.

    import vnx

    result = vnx.read_file("trip.vnx")
    for diagnostic in result.diagnostics:
        print(diagnostic.path, diagnostic.message)

Writing applies the emit-boundary rules of SPEC 4.1 -- longitude and latitude
to 6 decimal places, elevation to 1, and the shortest spelling of every
position:

    vnx.encode_bytes(doc)

A document that cannot be read at all raises :class:`vnx.VNXRefusal`; a document
that could be read with pieces dropped comes back with diagnostics.  The two are
different channels on purpose (SPEC 6.3).
"""

from __future__ import annotations

import os
from typing import Union

from .container import (
    Attachment, ContainerReadResult, encode_container_bytes,
    read_container_bytes,
)
from .decode import ReadResult, SUPPORTED_VERSION, decode_document, read_bytes
from .encode import encode_document
from .errors import Diagnostic, VNXRefusal
from .jsonio import parse_json
from .model import (
    Area, Arc, Chain, DataRecord, Document, Folder, Link, Network, Position,
    Route, RouteArc, Segment, Track, Waypoint,
)
from .writer import encode_bytes, round_position, rounded_document

__all__ = [
    "Area", "Arc", "Attachment", "Chain", "ContainerReadResult", "DataRecord",
    "Diagnostic", "Document", "Folder", "Link", "Network", "Position",
    "ReadResult", "Route",
    "RouteArc", "SUPPORTED_VERSION", "Segment", "Track", "VNXRefusal",
    "Waypoint", "decode_document", "encode_document", "encode_bytes",
    "encode_container_bytes", "encode_file", "parse_json", "read_bytes",
    "read_container_bytes", "read_container_file", "read_file",
    "round_position", "rounded_document",
]

__version__ = "0.1.0"


def read_file(path: Union[str, os.PathLike[str]]) -> ReadResult:
    with open(path, "rb") as handle:
        return read_bytes(handle.read())


def read_container_file(path: Union[str, os.PathLike[str]]) -> ContainerReadResult:
    with open(path, "rb") as handle:
        return read_container_bytes(handle.read())


def encode_file(path: Union[str, os.PathLike[str]], document: Document) -> None:
    """Write *document* at writer precision — rounded and shortest-spelled."""
    with open(path, "wb") as handle:
        handle.write(encode_bytes(document))

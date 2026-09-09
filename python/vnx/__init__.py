"""VNX -- Viatrail Network eXchange.

A codec for the VNX interchange format: it reads and writes ``.vnx`` and
``.vnz``, renders canonical form, and reports diagnostics.  That is what
``SPEC.md`` calls a VNX library.  The GPX, KML and GeoJSON writers are
conversion tools rather than part of the format and are not here.

    import vnx

    result = vnx.read_file("trip.vnx")
    for diagnostic in result.diagnostics:
        print(diagnostic.path, diagnostic.message)

There are two emit paths and they are not interchangeable (SPEC 4.5.4):

    vnx.reformat_bytes(doc)   # formatter: preserves what the document carried
    vnx.encode_bytes(doc)     # writer: rounds to 6 and 1 places, shortest spelling

A document that cannot be read at all raises :class:`vnx.VNXRefusal`; a document
that could be read with pieces dropped comes back with diagnostics.  The two are
different channels on purpose (SPEC 6.3).
"""

from __future__ import annotations

import os
from typing import Union

from .canonical import render_document
from .container import (
    ContainerReadResult, read_container_bytes, write_container_bytes,
)
from .decode import ReadResult, SUPPORTED_VERSION, decode_document, read_bytes
from .encode import encode_document, reformat_bytes
from .errors import Diagnostic, VNXRefusal
from .jsonio import parse_json
from .model import (
    Area, Arc, Chain, DataRecord, Document, Folder, Link, Network, Position,
    Route, RouteArc, Segment, Track, Waypoint,
)
from .numbers import render_number
from .writer import encode_bytes, rounded_document

__all__ = [
    "Area", "Arc", "Chain", "ContainerReadResult", "DataRecord", "Diagnostic",
    "Document", "Folder", "Link", "Network", "Position", "ReadResult", "Route",
    "RouteArc", "SUPPORTED_VERSION", "Segment", "Track", "VNXRefusal",
    "Waypoint", "canonicalise", "decode_document", "encode_document",
    "encode_bytes", "encode_file", "read_bytes", "read_container_bytes",
    "read_container_file", "read_file", "reformat_bytes", "reformat_file",
    "render_number", "rounded_document", "write_container_bytes",
]

__version__ = "0.1.0"


def read_file(path: Union[str, os.PathLike[str]]) -> ReadResult:
    with open(path, "rb") as handle:
        return read_bytes(handle.read())


def read_container_file(path: Union[str, os.PathLike[str]]) -> ContainerReadResult:
    with open(path, "rb") as handle:
        return read_container_bytes(handle.read())


def reformat_file(path: Union[str, os.PathLike[str]], document: Document) -> None:
    """Write *document* preserving its values — the formatter path."""
    with open(path, "wb") as handle:
        handle.write(reformat_bytes(document))


def encode_file(path: Union[str, os.PathLike[str]], document: Document) -> None:
    """Write *document* at writer precision — rounded and shortest-spelled."""
    with open(path, "wb") as handle:
        handle.write(encode_bytes(document))


def canonicalise(data: bytes) -> bytes:
    """Re-render *data* in canonical form (SPEC 4.5).

    Bytes in, bytes out.  This is a layout operation over the JSON the document
    carries: it does not decode to the model, so it neither drops an
    unrecognised key nor rounds a coordinate (SPEC 4.5.4 is explicit that a
    formatter must not round).  A document that is not VNX at all is still
    refused, because the non-finite rule of SPEC 4.2 is about bytes too.
    """
    return render_document(parse_json(data))

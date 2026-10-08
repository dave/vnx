"""VNX -- Viatrail Network eXchange.

A codec for the VNX interchange format: it reads and writes ``.vnx`` and
``.vnz``, and reports diagnostics.  That is what ``SPEC.md`` calls a VNX
library.  The GPX, KML and GeoJSON writers are conversion tools rather than
part of the format and are not here.

    import vnx

    result = vnx.read_file("trip.vnx")
    for diagnostic in result.diagnostics:
        print(diagnostic.path, diagnostic.message)

Writing applies the emit-boundary rules of SPEC 4.1 -- the shortest spelling of
every position.  It does not change a value: the format does not constrain a
writer's precision, so what you put in a position is what comes out:

    vnx.encode_bytes(doc)

A document that cannot be read at all raises :class:`vnx.VNXRefusal`; a document
that could be read with pieces dropped comes back with diagnostics.  The two are
different channels on purpose (SPEC 6.3).
"""

from __future__ import annotations

import os
import tempfile
from typing import Union

from .container import (
    Attachment,
    ContainerReadResult,
    encode_container_bytes,
    read_container_bytes,
)
from .decode import ReadResult, SUPPORTED_VERSION, decode_document, read_bytes
from .encode import encode_document
from .errors import Diagnostic, VNXRefusal
from .jsonio import parse_json
from .model import (
    AppKey,
    Area,
    Arc,
    Highlight,
    DataRecord,
    Document,
    Folder,
    Network,
    Pass,
    Position,
    Plan,
    Segment,
    Track,
    Waypoint,
    is_app_key,
)
from .writer import encode_bytes, normalised_document

__all__ = [
    "AppKey",
    "Area",
    "Arc",
    "Attachment",
    "Highlight",
    "ContainerReadResult",
    "DataRecord",
    "Diagnostic",
    "Document",
    "Folder",
    "Network",
    "Pass",
    "Position",
    "ReadResult",
    "Plan",
    "SUPPORTED_VERSION",
    "Segment",
    "Track",
    "VNXRefusal",
    "Waypoint",
    "decode_document",
    "encode_document",
    "encode_bytes",
    "encode_container_bytes",
    "encode_file",
    "parse_json",
    "read_bytes",
    "read_container_bytes",
    "read_container_file",
    "read_file",
    "normalised_document",
    "is_app_key",
]

__version__ = "0.10.0"


def read_file(path: Union[str, os.PathLike[str]]) -> ReadResult:
    with open(path, "rb") as handle:
        return read_bytes(handle.read())


def read_container_file(path: Union[str, os.PathLike[str]]) -> ContainerReadResult:
    with open(path, "rb") as handle:
        return read_container_bytes(handle.read())


def encode_file(path: Union[str, os.PathLike[str]], document: Document) -> None:
    """Write *document*: shortest-spelled positions, values unchanged.

    The document is encoded in full before the file is touched, then written to
    a temporary file beside *path* and renamed over it.  A refused encode
    (``ValueError``) or a failed write leaves an existing file as it was.
    """
    data = encode_bytes(document)
    directory = os.path.dirname(os.path.abspath(path))
    fd, temp = tempfile.mkstemp(dir=directory, prefix=".vnx-", suffix=".tmp")
    try:
        with os.fdopen(fd, "wb") as handle:
            handle.write(data)
        os.replace(temp, path)
    except BaseException:
        os.unlink(temp)
        raise

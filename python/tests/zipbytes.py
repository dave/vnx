"""Reading a ZIP's headers as bytes, because a ZIP library will not.

Every ZIP library resolves an entry from the central directory, on purpose: it
is the authoritative index and the local header is a hint. So a library-based
reader cannot witness a rule about a LOCAL header, whoever wrote the library.
An independent reader is not automatically an independent instrument, because it
can still consult the same field the defect is not in.

This walks the archive forward, entry by entry, and returns both halves so a test
can assert they agree.
"""

from __future__ import annotations

import struct
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class Header:
    name: str
    version: int
    flags: int
    method: int
    crc: int
    compressed: int
    uncompressed: int
    extra_len: int


@dataclass(frozen=True, slots=True)
class CentralHeader(Header):
    made_by: int
    internal_attr: int
    external_attr: int
    disk: int
    comment_len: int


def local_headers(data: bytes) -> list[Header]:
    """Every local file header, in the order the archive stores them."""
    out: list[Header] = []
    offset = 0
    while data[offset : offset + 4] == b"PK\x03\x04":
        (version, flags, method, _t, _d, crc, csize, usize, nlen, elen) = (
            struct.unpack_from("<HHHHHIIIHH", data, offset + 4)
        )
        name = data[offset + 30 : offset + 30 + nlen].decode("utf-8")
        out.append(Header(name, version, flags, method, crc, csize, usize, elen))
        offset += 30 + nlen + elen + csize
    return out


def central_headers(data: bytes) -> list[CentralHeader]:
    """Every central directory entry."""
    offset = data.rindex(b"PK\x05\x06")
    start = struct.unpack_from("<I", data, offset + 16)[0]
    out: list[CentralHeader] = []
    while data[start : start + 4] == b"PK\x01\x02":
        (made_by, version, flags, method, _t, _d, crc, csize, usize,
         nlen, elen, clen, disk, iattr, eattr, _lho) = struct.unpack_from(
            "<HHHHHHIIIHHHHHII", data, start + 4
        )
        name = data[start + 46 : start + 46 + nlen].decode("utf-8")
        out.append(
            CentralHeader(name, version, flags, method, crc, csize, usize, elen,
                          made_by, iattr, eattr, disk, clen)
        )
        start += 46 + nlen + elen + clen
    return out

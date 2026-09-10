"""The VNZ container (SPEC 7).

A VNZ file is a ZIP archive carrying a VNX document and its attachments.  Two
of the rules here are security requirements rather than tidiness (SPEC 9): an
attachment path containing ``..`` or an absolute path can direct an extractor to
write outside its destination, and the check belongs at read time rather than
being left to whoever extracts later.
"""

from __future__ import annotations

import io
import re
import zipfile
import zlib
from dataclasses import dataclass, field
from typing import Union

from .decode import ReadResult, read_bytes
from .encode import reformat_bytes
from .writer import encode_bytes
from .errors import Diagnostic, VNXRefusal
from .model import Document

__all__ = [
    "ContainerReadResult", "read_container_bytes",
    "reformat_container_bytes", "encode_container_bytes",
]

_ROOT_DOCUMENT = "doc.vnx"
_DRIVE_LETTER = re.compile(r"\A[A-Za-z]:")


@dataclass(slots=True)
class ContainerReadResult:
    """A container's document, its diagnostics, and its attachment names."""

    document: Document
    diagnostics: list[Diagnostic]
    attachments: list[str] = field(default_factory=list)
    document_name: str = _ROOT_DOCUMENT


def _check_path(name: str) -> None:
    if name.startswith("/") or name.startswith("\\"):
        raise VNXRefusal(f"{name!r} escapes the archive: an absolute path")
    if _DRIVE_LETTER.match(name):
        raise VNXRefusal(f"{name!r} escapes the archive: a drive letter")
    # A backslash anywhere in a member name is refused, unconditionally, and
    # this check stands alone rather than feeding the `..` hunt below.  Ruled
    # 2026-09-09.  It is deliberately stricter than the ZIP appnote, which
    # permits the character: no VNX archive has a legitimate use for a backslash
    # in an entry name, and on a Windows-authored archive `..\x` is a real
    # escape.  Refusing the character outright means "is a backslash a separator
    # while hunting for the segment" never has to be answered.
    if "\\" in name:
        raise VNXRefusal(
            f"{name!r} escapes the archive: an entry name may not contain a "
            "backslash"
        )

    # SPEC 7: `..` is a path SEGMENT, not a substring, and names
    # `a..b/photo.jpg` as a legal entry that escapes nothing.
    if ".." in name.split("/"):
        raise VNXRefusal(f"{name!r} escapes the archive: a `..` path segment")


def read_container_bytes(data: bytes) -> ContainerReadResult:
    """Read a ``.vnz``.  Raises :class:`VNXRefusal`."""
    try:
        archive = zipfile.ZipFile(io.BytesIO(data))
    except zipfile.BadZipFile as exc:
        raise VNXRefusal(f"not a readable VNZ container: {exc}") from exc

    names = [info.filename for info in archive.infolist()]

    # SPEC 7: duplicate entry names MUST be refused, compared CASE-INSENSITIVELY.
    # Language runtimes resolve a repeated name differently, so one archive would
    # otherwise yield different documents in different languages -- and a
    # case-insensitive filesystem makes `doc.vnx` and `DOC.VNX` a repeat even
    # where the archive spells them apart.
    seen: dict[str, str] = {}
    for name in names:
        folded = name.lower()
        if folded in seen:
            first = seen[folded]
            detail = (
                f"duplicate entry {name!r} in the container"
                if first == name
                else f"duplicate entry {name!r} in the container, "
                f"differing from {first!r} only by case"
            )
            raise VNXRefusal(detail)
        seen[folded] = name

    for name in names:
        _check_path(name)

    # SPEC 7: prefer an entry named doc.vnx at the archive root; otherwise take
    # the first entry with a .vnx extension in archive order.  Names are
    # compared case-insensitively, and the match is on the full entry name, not
    # on its base name -- matching the base name would match at any depth.
    document_name: Union[str, None] = None
    for name in names:
        if name.lower() == _ROOT_DOCUMENT:
            document_name = name
            break
    if document_name is None:
        for name in names:
            if name.lower().endswith(".vnx"):
                document_name = name
                break
    if document_name is None:
        raise VNXRefusal("not a VNX document: the container holds no .vnx entry")

    # SPEC 7: compression must be stored or deflate, and the remedy for another
    # method depends on which entry carries it.  The root document leaves nothing
    # to return, so the archive is refused; an attachment is dropped and
    # reported, because refusing a whole library over one photograph compressed
    # by an unusual method would be disproportionate.
    unreadable: set[str] = set()
    for info in archive.infolist():
        if info.compress_type in (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED):
            continue
        if info.filename == document_name:
            raise VNXRefusal(
                f"the root document {info.filename!r} uses a compression method "
                "that is not stored or deflate"
            )
        unreadable.add(info.filename)

    # Decompressing is where a hostile archive bites: a corrupt deflate stream
    # raises `zlib.error`, and a wrong CRC or a short entry raises
    # `zipfile.BadZipFile` -- from the READ, not from the constructor this
    # function already guards.  Neither is a VNXRefusal, so both escaped every
    # caller as a crash.  A malformed container is refused, like every other
    # malformed input; it does not take the process down.
    try:
        body = archive.read(document_name)
    except (zipfile.BadZipFile, zlib.error, EOFError, ValueError) as exc:
        raise VNXRefusal(
            f"the root document {document_name!r} will not decompress: {exc}"
        ) from exc
    result: ReadResult = read_bytes(body)
    diagnostics = list(result.diagnostics)
    for name in sorted(unreadable):
        # SPEC 6.3 shapes a diagnostic's path as a JSON Pointer into the
        # document, and an archive entry is not in the document, so the pointer
        # addresses the document as a whole and the message names the entry.
        # No corpus case pins this diagnostic, so it carries no code: SPEC 6.3
        # permits that, and inventing a token no case pins would commit the wire
        # to one this format has not chosen.
        diagnostics.append(
            Diagnostic(
                "",
                f"attachment {name!r} uses a compression method that is not "
                "stored or deflate, and was dropped",
            )
        )
    attachments = [
        name for name in names if name != document_name and name not in unreadable
    ]
    return ContainerReadResult(
        document=result.document,
        diagnostics=diagnostics,
        attachments=attachments,
        document_name=document_name,
    )


def reformat_container_bytes(
    document: Document, attachments: Union[dict[str, bytes], None] = None
) -> bytes:
    """A ``.vnz`` whose document PRESERVES the values it carried.

    Repackaging a document must not silently lose precision, so a container
    gets the same two paths as a plain document (ruled 2026-09-09).  A
    container is not a special case; it is the same document inside a zip.
    """
    return _container(reformat_bytes(document), attachments)


def encode_container_bytes(
    document: Document, attachments: Union[dict[str, bytes], None] = None
) -> bytes:
    """A ``.vnz`` whose document is emitted at WRITER precision.

    Rounded to 6 and 1 places with the shortest spelling of every position,
    exactly as `encode_bytes`.
    """
    return _container(encode_bytes(document), attachments)


def _container(
    body: bytes, attachments: Union[dict[str, bytes], None]
) -> bytes:
    for name in attachments or {}:
        # A writer refuses to pack a name it would refuse to read.
        _check_path(name)
        if name.lower() == _ROOT_DOCUMENT:
            raise VNXRefusal(f"an attachment may not be named {name!r}")
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_STORED) as archive:
        archive.writestr(_zip_info(_ROOT_DOCUMENT), body)
        # SPEC 7.1: the root document first, then attachments SORTED by entry
        # name, compared by Unicode code point as in SPEC 4.5.1.  Caller order is
        # not a property of the content: two exporters using one library, one
        # iterating a hash map and one a sorted list, would otherwise emit
        # different archives for the same document while satisfying every other
        # field in that table.
        #
        # CPython's `sorted` already compares by code point, so this needs no
        # comparator where JavaScript and Kotlin do.  It is written out anyway,
        # because a default that happens to be right gives no signal if the names
        # are later routed through something that is not.
        for name in sorted((attachments or {}), key=_code_points):
            archive.writestr(_zip_info(name), (attachments or {})[name])
    return buffer.getvalue()


class _UTF8Info(zipfile.ZipInfo):
    """A ZipInfo whose UTF-8 filename flag cannot be cleared.

    SPEC 7 requires the flag to be set.  ``ZipFile._open_to_write`` assigns
    ``flag_bits = 0`` on the way past, so setting the bit before the write does
    not survive; shadowing the slot with a property does.
    """

    __slots__ = ("_flag_bits", "_external_attr")

    @property
    def flag_bits(self) -> int:
        return self._flag_bits | 0x800

    @flag_bits.setter
    def flag_bits(self, value: int) -> None:
        self._flag_bits = value

    @property
    def external_attr(self) -> int:
        """Always 0.

        ``ZipFile.writestr`` assigns file mode 0o600 here even when it is handed
        a ``ZipInfo`` that already set it, so an archive would otherwise carry
        the writing platform's permission bits.  Shadowing the slot is the same
        remedy the UTF-8 flag needs, and for the same reason.
        """
        return 0

    @external_attr.setter
    def external_attr(self, value: int) -> None:
        self._external_attr = value


def _code_points(name: str) -> tuple[int, ...]:
    """Sort key by Unicode code point (SPEC 4.5.1)."""
    return tuple(ord(character) for character in name)


def _zip_info(name: str) -> zipfile.ZipInfo:
    info = _UTF8Info(name, date_time=(1980, 1, 1, 0, 0, 0))
    # A writer emits STORED for every entry; a reader still accepts deflate.
    # Ruled 2026-09-09 because deflate has no canonical output: for one corpus
    # document Go's compress/flate emits 116 bytes and zlib emits 108, and no
    # zlib level from 0 to 9 reproduces the committed bytes.  A deflated archive
    # is byte-reproducible only by the language that wrote it, whatever it pins
    # in its headers, so fixing the timestamps was necessary and not sufficient.
    # The asymmetry matches canonical form: required of what a library writes,
    # never of what it will read.
    info.compress_type = zipfile.ZIP_STORED

    # Pin every header field that `zipfile` would otherwise fill from the
    # platform, so one document produces one archive everywhere.  Two of these
    # encode the writing MACHINE rather than the content: `create_system` is 3
    # (Unix) on macOS and Linux and 0 (MS-DOS) on Windows, and `writestr` sets
    # external attributes to file mode 0o600, so the same Python on two
    # platforms emitted two different archives for one document.  Fixing the
    # timestamp was necessary and not sufficient.
    #
    # The VALUES follow the only other implementation to have stated its
    # choices: version 1.0, which is what STORED requires, MS-DOS as the
    # creating system, and no external attributes.  Which values is not yet
    # ruled; that they must not come from the platform is not in doubt.
    info.create_system = 0
    info.create_version = 10
    info.extract_version = 10
    info.internal_attr = 0
    info.volume = 0
    return info

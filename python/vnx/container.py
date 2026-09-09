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
    # SPEC 7: `..` is a path SEGMENT, not a substring, and names
    # `a..b/photo.jpg` as a legal entry that escapes nothing.
    #
    # A backslash is separated out because SPEC 7 requires a path to be
    # forward-slash separated without saying what a reader does with one that is
    # not.  A Windows-authored archive can carry `..\x`, which the ZIP appnote
    # permits and which is a real escape on a Windows extractor, so `\` counts
    # as a separator for THIS check only.  A name that merely contains a
    # backslash, such as `weird\name.jpg`, is legal on a POSIX filesystem and is
    # not refused: the rule is about separators rather than about the character.
    #
    # Refusing `..\x` is this implementation's reading. The JavaScript seat
    # reached the same one independently; nothing in SPEC 7 states the remedy.
    if ".." in re.split(r"[/\\]", name):
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

    for info in archive.infolist():
        if info.compress_type not in (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED):
            raise VNXRefusal(
                f"entry {info.filename!r} uses a compression method that is not "
                "stored or deflate"
            )

    result: ReadResult = read_bytes(archive.read(document_name))
    attachments = [name for name in names if name != document_name]
    return ContainerReadResult(
        document=result.document,
        diagnostics=result.diagnostics,
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
        _check_path(name)
        if name.lower() == _ROOT_DOCUMENT:
            raise VNXRefusal(f"an attachment may not be named {name!r}")
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr(_zip_info(_ROOT_DOCUMENT), body)
        for name, body in (attachments or {}).items():
            archive.writestr(_zip_info(name), body)
    return buffer.getvalue()


class _UTF8Info(zipfile.ZipInfo):
    """A ZipInfo whose UTF-8 filename flag cannot be cleared.

    SPEC 7 requires the flag to be set.  ``ZipFile._open_to_write`` assigns
    ``flag_bits = 0`` on the way past, so setting the bit before the write does
    not survive; shadowing the slot with a property does.
    """

    __slots__ = ("_flag_bits",)

    @property
    def flag_bits(self) -> int:
        return self._flag_bits | 0x800

    @flag_bits.setter
    def flag_bits(self, value: int) -> None:
        self._flag_bits = value


def _zip_info(name: str) -> zipfile.ZipInfo:
    info = _UTF8Info(name, date_time=(1980, 1, 1, 0, 0, 0))
    info.compress_type = zipfile.ZIP_DEFLATED
    return info

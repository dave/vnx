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
from .pointer import escape as escape_token
from .writer import encode_bytes
from .errors import Diagnostic, VNXRefusal
from .model import Document

__all__ = [
    "Attachment", "ContainerReadResult", "read_container_bytes",
    "encode_container_bytes",
]

_ROOT_DOCUMENT = "doc.vnx"
_DRIVE_LETTER = re.compile(r"\A[A-Za-z]:")


@dataclass(frozen=True, slots=True)
class Attachment:
    """One file carried alongside the document (SPEC 7).

    The PATH and the BYTES travel together because a caller that cannot reach
    the bytes cannot do anything with the name: the container exists to carry
    attachments, and a reader that returned names alone made a ``.vnz`` half
    readable.  Nothing in this format interprets the bytes -- an attachment is
    opaque, so it is ``bytes`` and not a decoded anything.
    """

    path: str
    bytes: bytes


@dataclass(slots=True)
class ContainerReadResult:
    """A container's document, its diagnostics, and its attachments."""

    document: Document
    diagnostics: list[Diagnostic]
    attachments: list[Attachment] = field(default_factory=list)
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


def _entry_pointer(name: str) -> str:
    """SPEC 6.3's `#/entries/` space, where a diagnostic about an ENTRY lives.

    An archive entry is not part of the document, so no pointer into the
    document can locate one.

    The member name is ONE RFC 6901 reference token, and the escaping ORDER
    decides the answer: ``~`` becomes ``~0`` before ``/`` becomes ``~1``.
    Reversed, the second pass escapes the ``~1`` the first pass just wrote, so
    ``photos/a~b.jpg`` comes out ``photos~01a~0b.jpg``.  An attachment name is
    the one place in this format where a member name routinely holds a slash.
    """
    return "#/entries/" + escape_token(name)


def _bound_declared_sizes(data: bytes, archive: zipfile.ZipFile) -> None:
    """Hold every declared size down to the bytes that are actually there.

    SPEC 7 makes an entry's declared size a CEILING rather than a length: a
    reader MUST NOT refuse an entry that declares more bytes than it holds where
    the CRC over the bytes present agrees.  ``zipfile`` reads the declared count
    and refuses the overrun as ``Overlapped entries (possible zip bomb)`` -- it
    detects the discrepancy and refuses on it, which is what the rule forbids.

    The bound on a payload is the nearest FOLLOWING local header, or the central
    directory where there is none.  Both come from the archive itself, so the
    correction needs nothing from ``zipfile``'s internals.

    A size declared SHORT is left alone: the CRC over too few bytes disagrees,
    so the same check catches it and no size comparison is needed either way.
    """
    directory_at = _central_directory_offset(data)
    starts = sorted(info.header_offset for info in archive.infolist())
    for info in archive.infolist():
        payload_at = _payload_offset(data, info)
        if payload_at is None:
            continue
        following = [start for start in starts if start > info.header_offset]
        bound = following[0] if following else directory_at
        if bound is None or payload_at > bound:
            continue
        available = bound - payload_at
        if info.compress_size <= available:
            continue
        info.compress_size = available
        if info.compress_type == zipfile.ZIP_STORED:
            # A stored entry's two sizes are one quantity by definition.
            info.file_size = available


def _payload_offset(data: bytes, info: zipfile.ZipInfo) -> Union[int, None]:
    """Where an entry's payload starts, from its LOCAL header.

    The local header carries its own name and extra lengths, which are different
    fields from the central directory's and may legitimately disagree with them.
    """
    at = info.header_offset
    if len(data) < at + 30 or data[at:at + 4] != b"PK\x03\x04":
        return None
    name_len = int.from_bytes(data[at + 26:at + 28], "little")
    extra_len = int.from_bytes(data[at + 28:at + 30], "little")
    return at + 30 + name_len + extra_len


def _central_directory_offset(data: bytes) -> Union[int, None]:
    """The end-of-central-directory record's offset field.

    Found by scanning backwards for the signature and requiring the comment
    length to account for exactly the bytes after the record -- which is what
    tells a real record from the same four bytes inside a stored payload.
    """
    end = len(data) - 22
    earliest = max(0, len(data) - 22 - 0xFFFF)
    while end >= earliest:
        if data[end:end + 4] == b"PK\x05\x06":
            comment_len = int.from_bytes(data[end + 20:end + 22], "little")
            if end + 22 + comment_len == len(data):
                offset = int.from_bytes(data[end + 16:end + 20], "little")
                if offset != 0xFFFFFFFF:
                    return offset
                return None
        end -= 1
    return None


def read_container_bytes(data: bytes) -> ContainerReadResult:
    """Read a ``.vnz``.  Raises :class:`VNXRefusal`."""
    try:
        archive = zipfile.ZipFile(io.BytesIO(data))
    except zipfile.BadZipFile as exc:
        raise VNXRefusal(f"not a readable VNZ container: {exc}") from exc
    _bound_declared_sizes(data, archive)

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

    # SPEC 7 ACCEPTS an entry whose declared size exceeds what it holds.  The
    # declared size is bounded to what is there by `_bound_declared_sizes`
    # above, before any entry is read, and the CRC is left as the check with
    # teeth: the stream ended, the checksum over what arrived agrees, so the
    # bytes are complete.  A size declared SHORT needs nothing of its own --
    # the same CRC catches it.
    result: ReadResult = read_bytes(body)
    diagnostics = list(result.diagnostics)
    for name in sorted(unreadable):
        diagnostics.append(
            Diagnostic(
                _entry_pointer(name),
                f"attachment {name!r} uses a compression method that is not "
                "stored or deflate, and was dropped",
                "attachment-unreadable",
            )
        )
    # An attachment's BYTES come back with its name.  An entry this reader
    # cannot decompress was already dropped above, on its METHOD, before
    # anything was inflated; what is left failing here is a damaged archive --
    # a wrong CRC or a truncated stream -- and that is refused like every other
    # malformed container rather than dropped quietly.  A caller cannot tell a
    # silently missing attachment from one the archive never held.
    attachments = []
    for name in names:
        if name == document_name or name in unreadable:
            continue
        try:
            body = archive.read(name)
        except (zipfile.BadZipFile, zlib.error, EOFError, ValueError) as exc:
            raise VNXRefusal(
                f"the attachment {name!r} will not decompress: {exc}"
            ) from exc
        attachments.append(Attachment(name, body))
    return ContainerReadResult(
        document=result.document,
        diagnostics=diagnostics,
        attachments=attachments,
        document_name=document_name,
    )


def encode_container_bytes(
    document: Document, attachments: Union[dict[str, bytes], None] = None
) -> bytes:
    """A ``.vnz`` holding *document* and *attachments*.

    The document is emitted exactly as `encode_bytes` emits it: rounded to 6
    and 1 places, with the shortest spelling of every position.
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
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr(_zip_info(_ROOT_DOCUMENT), body)
        # SPEC 7.1 requires the root document FIRST and leaves every other
        # field of the archive to the writer, attachment order included.  So
        # the sort below is this library's own choice, not conformance: caller
        # order is not a property of the content, and two exporters using this
        # library -- one iterating a hash map, one a sorted list -- would
        # otherwise emit different archives for the same document.
        for name in sorted(attachments or {}):
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


def _zip_info(name: str) -> zipfile.ZipInfo:
    info = _UTF8Info(name, date_time=(1980, 1, 1, 0, 0, 0))
    info.compress_type = zipfile.ZIP_DEFLATED

    # Pin the header fields `zipfile` would otherwise fill from the platform, so
    # one document produces one archive wherever it is written.  Two of them
    # encode the writing MACHINE rather than the content: `create_system` is 3
    # (Unix) on macOS and Linux and 0 (MS-DOS) on Windows, and `writestr` sets
    # external attributes to file mode 0o600.  Nothing in SPEC 7.1 requires
    # this; it is what makes one input give one output.
    info.create_system = 0
    info.internal_attr = 0
    info.volume = 0
    return info

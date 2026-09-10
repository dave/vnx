"""Malformed archives that no corpus fixture reaches.

Every case here is bytes a hostile or damaged file could carry, and the
property under test is the same one throughout: a malformed container is
REFUSED.  It does not crash, and it does not quietly return a document.

The corpus cannot reach any of this.  A fixture is a file somebody committed,
and a committed file has a valid deflate stream and a matching CRC -- so the
decompression path is exercised only by archives built here, damaged on
purpose.  That gap is why this file exists: found by the JS seat, which
mutated its reader to trust a local header's zeroed sizes and watched
`zlib`'s own error type escape every format-error handler it had.
"""

from __future__ import annotations

import io
import struct
import unittest
import zipfile
import zlib
from typing import Any
from unittest import mock

import vnx
from vnx import decode, model

from . import test_corpus

_DOCUMENT = b'{\n  "vnx": "0.1.0",\n  "notes": "' + b"x" * 400 + b'"\n}\n'


def _deflated() -> bytes:
    """A valid one-entry `.vnz` whose document really is deflated.

    This implementation's WRITER only ever emits stored (SPEC 7.1), so an
    archive built by `encode_container_bytes` would not exercise inflate at
    all.  A reader MUST accept deflate, so the fixture is built here.
    """
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr("doc.vnx", _DOCUMENT)
    return buffer.getvalue()


def _payload(data: bytes) -> tuple[int, int]:
    """Where entry zero's compressed bytes start, and how many there are."""
    name_length, extra_length = struct.unpack("<HH", data[26:30])
    return 30 + name_length + extra_length, struct.unpack("<I", data[18:22])[0]


class DamagedStreams(unittest.TestCase):
    def setUp(self) -> None:
        self.good = _deflated()
        self.start, self.size = _payload(self.good)

    def test_the_fixture_itself_reads(self) -> None:
        """Without this, every test below could pass for the wrong reason.

        A refusal is the expected outcome of four of the five, so an archive
        that was already unreadable would satisfy them all while proving
        nothing about the damage.
        """
        result = vnx.read_container_bytes(self.good)
        self.assertEqual(result.document_name, "doc.vnx")
        with zipfile.ZipFile(io.BytesIO(self.good)) as archive:
            methods = {i.compress_type for i in archive.infolist()}
        self.assertEqual(methods, {zipfile.ZIP_DEFLATED})

    def damaged(self, data: bytes) -> str:
        with self.assertRaises(vnx.VNXRefusal) as caught:
            vnx.read_container_bytes(data)
        return str(caught.exception)

    def test_a_garbage_deflate_stream_is_refused(self) -> None:
        """`zlib.error`, which is not a subclass of anything `zipfile` raises."""
        self.damaged(
            self.good[: self.start]
            + b"\xff" * self.size
            + self.good[self.start + self.size :]
        )

    def test_a_truncated_deflate_stream_is_refused(self) -> None:
        tail = self.good[self.start : self.start + self.size][:5]
        self.damaged(
            self.good[: self.start]
            + tail
            + b"\x00" * (self.size - 5)
            + self.good[self.start + self.size :]
        )

    def test_a_single_flipped_byte_is_refused(self) -> None:
        """Reaches the CRC check rather than the inflater, from the same call."""
        index = self.start + 3
        self.damaged(
            self.good[:index]
            + bytes([self.good[index] ^ 0xFF])
            + self.good[index + 1 :]
        )

    def test_a_truncated_file_is_refused(self) -> None:
        """This one is caught by the constructor, which was already guarded."""
        self.damaged(self.good[: self.start + 4])

    def test_zeroed_local_sizes_are_ACCEPTED(self) -> None:
        """SPEC 7: sizes come from the CENTRAL DIRECTORY, not a local header.

        The counterweight to every test above.  A reader hardened until it
        refuses anything unusual would fail this, and an archive written with
        general purpose bit 3 -- which a reader MUST accept -- carries exactly
        this shape.
        """
        zeroed = self.good[:18] + b"\x00" * 8 + self.good[26:]
        result = vnx.read_container_bytes(zeroed)
        self.assertEqual(result.document_name, "doc.vnx")

    def test_the_guard_is_load_bearing(self) -> None:
        """Without the wrap, these bytes escape as something else entirely.

        The tests above assert a refusal, and a refusal is also what an
        already-broken fixture would produce -- so this pins that the damage
        really does reach `zipfile.read`, and really is not a VNXRefusal there.
        A guard nobody proved necessary is a guard nobody can remove safely.
        """
        damaged = (
            self.good[: self.start]
            + b"\xff" * self.size
            + self.good[self.start + self.size :]
        )
        with zipfile.ZipFile(io.BytesIO(damaged)) as archive:
            with self.assertRaises(zlib.error):
                archive.read("doc.vnx")


class RunnerContainment(unittest.TestCase):
    """A throw that is NOT a refusal must not take the other cases down.

    Every previous containment fix in this runner was about refusals, which is
    exactly why this is the case worth pinning: it is the one they were not
    about.  Found by the JS seat, whose runner lost 38 cases to one throw.
    """

    def test_one_exploding_case_does_not_silence_the_others(self) -> None:
        cases = [path.name for path in test_corpus._cases("accept")]
        victim = cases[len(cases) // 2]
        victim_bytes = (test_corpus.CORPUS / "accept" / victim).read_bytes()
        original = decode.read_bytes

        def explodes(data: bytes) -> Any:
            if data == victim_bytes:
                raise RuntimeError("not a refusal")
            return original(data)

        # The liveness half.  Counting error ENTRIES would not do: the question
        # is whether every OTHER case was still checked, so a second defect is
        # injected that every case must report, and the two are counted apart.
        with mock.patch.object(decode, "read_bytes", explodes), \
             mock.patch.object(vnx, "read_bytes", explodes), \
             mock.patch.object(model.Document, "counts", lambda self: {
                 key: 0 for key in test_corpus.COLLECTIONS + ("rulesets",)}):
            suite = unittest.TestLoader().loadTestsFromName(
                "AcceptTier.test_accept", test_corpus
            )
            result = unittest.TextTestRunner(
                stream=io.StringIO(), verbosity=0
            ).run(suite)

        reported = {
            str(test).split("case=")[-1].strip("')] ")
            for test, _ in result.failures + result.errors
        }
        self.assertEqual(sorted(reported), sorted(cases))
        self.assertEqual(len(result.errors), 1, "the throw is one case, not all")
        self.assertEqual(len(result.failures), len(cases) - 1)


if __name__ == "__main__":
    unittest.main()

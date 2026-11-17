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
import tracemalloc
import unittest
import zipfile
import zlib
from typing import Any
from unittest import mock

import vnx
from vnx import decode, model

from . import test_corpus, zipbytes

# Long enough that deflate has something to compress and a flipped byte lands
# inside the payload rather than in a header.  The bulk lives on a point's
# `notes`, which is a key this format defines: at the document level it would be
# an unknown key, and every archive here would then carry a diagnostic that has
# nothing to do with what these tests are about.
_NOTES = "x" * 400
_DOCUMENT = (
    b'{\n  "vnx": "0.11.0",\n  "points": [\n    {\n      "at": [-3.6, 57.1],\n'
    b'      "notes": "' + _NOTES.encode() + b'"\n    }\n  ]\n}\n'
)


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
        self.assertEqual(
            [(d.path, d.message) for d in result.diagnostics],
            [],
            "the fixture document must be clean, or every test here carries a "
            "diagnostic that has nothing to do with the damage",
        )
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


class LyingHeaders(unittest.TestCase):
    """Header fields that disagree with the bytes behind them.

    The Kotlin implementation found three hazards here and warned the others.
    All three are the same CLASS -- a header field trusted before the data that
    would justify it -- and none of the three MECHANISMS transferred to this
    language, which is the point worth keeping: take the class and find your own
    mechanism by building the inputs, because a warning about somebody else's
    call site is not a measurement of yours.

    These are absence-proofs, and an absence-proof goes stale the day the
    implementation underneath it changes.  They are here so that it fails.
    """

    LIE = 2_000_000_000

    def setUp(self) -> None:
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w", zipfile.ZIP_STORED) as archive:
            archive.writestr("doc.vnx", _DOCUMENT)
        self.good = buffer.getvalue()

    def rewritten(
        self, compressed: int | None = None, uncompressed: int | None = None
    ) -> bytes:
        """The same archive with a size field rewritten in BOTH headers.

        Rewriting one only would be a different test: SPEC 7 makes the central
        directory authoritative, so a lie told in the local header alone is a
        lie this reader is REQUIRED not to read.
        """
        out = bytearray(self.good)
        central = self.good.find(b"PK\x01\x02")
        for target, offset in ((compressed, 18), (uncompressed, 22)):
            if target is None:
                continue
            out[offset : offset + 4] = struct.pack("<I", target)
            here = central + (offset + 2)
            out[here : here + 4] = struct.pack("<I", target)
        return bytes(out)

    def test_a_two_gigabyte_claim_allocates_nothing_like_it(self) -> None:
        """The hazard: sizing a buffer from a header before reading the data.

        An `OutOfMemoryError` is not an exception a caller catches -- it takes
        the process rather than the document -- so the measurement is the peak
        allocation, and NOT the outcome.  Either outcome would satisfy an
        outcome-shaped test: a refusal is what an allocation failure looks like
        from outside, and a successful read is what no allocation looks like.
        Only the peak separates them.
        """
        lying = self.rewritten(uncompressed=self.LIE)
        tracemalloc.start()
        try:
            result = vnx.read_container_bytes(lying)
            peak = tracemalloc.get_traced_memory()[1]
        finally:
            tracemalloc.stop()
        self.assertEqual(result.document_name, "doc.vnx")
        self.assertLess(peak, 8 * 1024 * 1024, "a header field sized a buffer")
        self.assertGreater(
            self.LIE,
            8 * 1024 * 1024 * 100,
            "the lie is too small to prove anything",
        )

    def test_an_over_declared_compressed_size_is_READ(self) -> None:
        """SPEC 7 FORBIDS refusing this, and this test used to require it.

        The declared size is a ceiling, not a length: an entry declaring more
        bytes than it holds is read as the bytes that are there, bounded by the
        next local header or the central directory, and the CRC decides whether
        that was the whole payload.  `corpus/accept/container-over-declared-size`
        pins the rule; this pins the extreme of it, which no committed archive
        can carry.
        """
        result = vnx.read_container_bytes(self.rewritten(compressed=self.LIE))
        self.assertEqual(result.document_name, "doc.vnx")
        self.assertEqual(result.diagnostics, [])

    def test_a_short_declared_size_is_refused(self) -> None:
        with self.assertRaises(vnx.VNXRefusal):
            vnx.read_container_bytes(self.rewritten(uncompressed=4))

    def test_the_crc_is_verified_on_a_STORED_entry(self) -> None:
        """The corpus cannot show this: every committed archive is well formed.

        Stored is what this format's writers emit, so an unverified CRC here
        would mean silent corruption on the ordinary path rather than the
        unusual one.
        """
        name_length, extra_length = struct.unpack("<HH", self.good[26:30])
        index = 30 + name_length + extra_length + 40
        corrupt = (
            self.good[:index]
            + bytes([self.good[index] ^ 0x01])
            + self.good[index + 1 :]
        )
        with self.assertRaises(vnx.VNXRefusal):
            vnx.read_container_bytes(corrupt)

    def test_a_declared_size_LARGER_than_the_data_is_ACCEPTED(self) -> None:
        """Ruled 2026-09-10, and SPEC 7 says so rather than staying silent.

        The CRC is the check with teeth: the stream ended, the checksum over
        what arrived agrees, so the bytes are complete and a size field that
        disagrees adds nothing the CRC has not established.  Nor is there a
        size rule to be consistent with -- a size SHORTER than the data is
        refused by the CRC, not by any comparison of sizes.

        This test exists because silence is what produced the question. The
        next reader to harden this file will refuse it, believing robustness
        has improved while conformance has broken.
        """
        result = vnx.read_container_bytes(self.rewritten(uncompressed=self.LIE))
        assert result.document.points is not None
        self.assertEqual(result.document.points[0].notes, _NOTES)

    def test_an_archive_deferring_its_sizes_still_reads(self) -> None:
        """SPEC 7: the CENTRAL directory is authoritative, not a local header.

        General purpose bit 3 produces exactly this shape, and a reader MUST
        accept it.  Any size check written against the local header refuses a
        conforming archive, which is the trap the previous test guards from the
        other side.
        """
        local_only = self.good[:18] + b"\x00" * 8 + self.good[26:]
        # Read the two headers as bytes, because the whole point is that they
        # disagree and no ZIP library will show you the local one.
        self.assertEqual(zipbytes.local_headers(local_only)[0].uncompressed, 0)
        self.assertEqual(
            zipbytes.central_headers(local_only)[0].uncompressed, len(_DOCUMENT)
        )
        result = vnx.read_container_bytes(local_only)
        assert result.document.points is not None
        self.assertEqual(result.document.points[0].notes, _NOTES)


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
        with (
            mock.patch.object(decode, "read_bytes", explodes),
            mock.patch.object(vnx, "read_bytes", explodes),
            mock.patch.object(
                model.Document,
                "counts",
                lambda self: {key: 0 for key in test_corpus.COLLECTIONS},
            ),
        ):
            suite = unittest.TestLoader().loadTestsFromName(
                "AcceptTier.test_accept", test_corpus
            )
            result = unittest.TextTestRunner(stream=io.StringIO(), verbosity=0).run(
                suite
            )

        reported = {
            str(test).split("case=")[-1].strip("')] ")
            for test, _ in result.failures + result.errors
        }
        self.assertEqual(sorted(reported), sorted(cases))
        self.assertEqual(len(result.errors), 1, "the throw is one case, not all")
        self.assertEqual(len(result.failures), len(cases) - 1)


if __name__ == "__main__":
    unittest.main()

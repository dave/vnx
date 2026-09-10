#!/usr/bin/env python3
"""Build `accept/container-over-declared-size.vnz`, which no writer can produce.

SPEC.md section 7: a reader MUST NOT refuse an entry on the ground that its
declared size exceeds the bytes present, where the CRC over those bytes agrees.
Section 7.1 leaves a conforming writer no way to over-declare, so this archive
cannot come from one -- not ours, and not the stock library of any language,
since a general-purpose writer computes the size it writes. The bytes are laid
out here by hand instead.

That is also why this script shares no code with any VNX writer, the way
`makevnz` shares none: a generator built on a writer inherits the writer's
inability to produce the case.

    python3 corpus/tools/overdeclare/make.py            # verify, refuse to drift
    python3 corpus/tools/overdeclare/make.py --write     # write the fixture

⚠ IT VERIFIES THE ARCHIVE IT BUILT, NOT THE INTENT IT BUILT IT FROM. Every
property below is re-derived by parsing the finished bytes. A generator that
asserted its own inputs would keep passing after an edit that changed what those
inputs mean -- and an over-declaring archive whose arithmetic has been "fixed"
is a valid archive under a name promising an invalid one, which every
implementation passes.
"""

import argparse
import pathlib
import struct
import sys
import zlib

SIG_LOCAL = 0x04034B50
SIG_CENTRAL = 0x02014B50
SIG_END = 0x06054B50

# How far the document over-declares. Seven bytes: enough that a reader trusting
# the count reads into the next entry's local header signature, and few enough
# that it cannot be mistaken for a truncated file.
OVER_DECLARED_BY = 7

DOCUMENT = (
    b'{\n'
    b'  "generator": "corpus",\n'
    b'  "vnx": "0.1.0",\n'
    b'  "waypoints": [\n'
    b'    {\n'
    b'      "name": "Ben Macdui",\n'
    b'      "at": [-3.6689, 57.0704]\n'
    b'    }\n'
    b'  ]\n'
    b'}\n'
)

# The document is the SECOND of three entries, and both positions are load
# bearing. Second, so its local header is not at offset zero and a reader that
# loses an offset and defaults to zero does not still land on it. Not last, so
# the bound on its payload comes from a FOLLOWING LOCAL HEADER rather than from
# the central directory -- the two are different code paths in a reader and only
# one of them is reachable from an archive whose last entry is the document.
ENTRIES = [
    ("before.txt", b"an attachment ahead of the document", 0),
    ("doc.vnx", DOCUMENT, OVER_DECLARED_BY),
    ("after.txt", b"an attachment behind it", 0),
]


def build():
    """Lay out the archive. Fixed timestamps, stored, UTF-8 flag, no extra fields."""
    out = bytearray()
    directory = bytearray()
    for name, body, over in ENTRIES:
        raw = name.encode("utf-8")
        crc = zlib.crc32(body) & 0xFFFFFFFF
        declared = len(body) + over
        offset = len(out)
        out += struct.pack(
            "<IHHHHHIIIHH", SIG_LOCAL, 10, 0x0800, 0, 0, 0x0021, crc, declared, declared, len(raw), 0
        )
        out += raw
        out += body
        directory += struct.pack("<I", SIG_CENTRAL)
        directory += struct.pack("<HH", 10, 10)
        directory += struct.pack("<HH", 0x0800, 0)
        directory += struct.pack("<HH", 0, 0x0021)
        directory += struct.pack("<III", crc, declared, declared)
        directory += struct.pack("<HHH", len(raw), 0, 0)
        directory += struct.pack("<HHI", 0, 0, 0)
        directory += struct.pack("<I", offset)
        directory += raw
    directory_offset = len(out)
    out += directory
    out += struct.pack(
        "<IHHHHIIH", SIG_END, 0, 0, len(ENTRIES), len(ENTRIES), len(directory), directory_offset, 0
    )
    return bytes(out)


def parse_local_headers(data):
    """Walk the local headers forwards, the way a forward-only reader must."""
    headers = []
    p = 0
    while p + 30 <= len(data) and struct.unpack_from("<I", data, p)[0] == SIG_LOCAL:
        crc, declared, _ = struct.unpack_from("<III", data, p + 14)
        name_len, extra_len = struct.unpack_from("<HH", data, p + 26)
        name = data[p + 30 : p + 30 + name_len].decode("utf-8")
        start = p + 30 + name_len + extra_len
        headers.append(dict(name=name, crc=crc, declared=declared, offset=p, payload_at=start))
        # The next header is found by SEARCHING, not by trusting the declared
        # size -- the declared size is the thing under test.
        nxt = data.find(b"PK\x03\x04", start)
        if nxt < 0:
            headers[-1]["present"] = data.find(b"PK\x01\x02", start) - start
            break
        headers[-1]["present"] = nxt - start
        p = nxt
    return headers


def parse_central(data):
    end = data.rindex(struct.pack("<I", SIG_END))
    count, _, directory_offset = struct.unpack_from("<HII", data, end + 10)
    p = directory_offset
    entries = []
    for _ in range(count):
        crc, declared = struct.unpack_from("<II", data, p + 16)
        name_len, extra_len, comment_len = struct.unpack_from("<HHH", data, p + 28)
        name = data[p + 46 : p + 46 + name_len].decode("utf-8")
        entries.append(dict(name=name, crc=crc, declared=declared))
        p += 46 + name_len + extra_len + comment_len
    return entries


def verify(data):
    """Re-derive every property the case depends on, from the bytes."""
    failures = []
    locals_ = parse_local_headers(data)
    centrals = parse_central(data)
    names = [h["name"] for h in locals_]

    if names != [name for name, _, _ in ENTRIES]:
        failures.append(f"archive order is {names}")
    if len(locals_) != len(centrals):
        failures.append(f"{len(locals_)} local headers against {len(centrals)} directory entries")

    doc = next((h for h in locals_ if h["name"] == "doc.vnx"), None)
    if doc is None:
        return ["there is no doc.vnx in the archive"]

    # 1. The property the whole case exists for.
    if doc["declared"] <= doc["present"]:
        failures.append(
            f"doc.vnx declares {doc['declared']} and holds {doc['present']}: it does not "
            "over-declare, so every implementation passes this case"
        )
    # 2. The CRC must agree over the bytes PRESENT, or section 7 does not apply
    #    and the archive is simply corrupt.
    present = data[doc["payload_at"] : doc["payload_at"] + doc["present"]]
    if (zlib.crc32(present) & 0xFFFFFFFF) != doc["crc"]:
        failures.append("the CRC does not agree over the bytes present, so this is a corrupt archive")
    # 3. Both headers must tell the same lie, or a reader taking sizes from the
    #    central directory sees a different archive from one taking them locally.
    central_doc = next(e for e in centrals if e["name"] == "doc.vnx")
    if central_doc["declared"] != doc["declared"]:
        failures.append(
            f"the directory declares {central_doc['declared']} and the local header {doc['declared']}"
        )
    # 4 and 5. The two positional properties.
    if names.index("doc.vnx") == 0:
        failures.append("doc.vnx is first, so its local header is at offset 0")
    if names.index("doc.vnx") == len(names) - 1:
        failures.append("doc.vnx is last, so its payload is bounded by the central directory")
    if present != DOCUMENT:
        failures.append("the bytes present are not the document this script means to store")
    return failures


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--write", action="store_true", help="write the fixture rather than only verifying")
    ap.add_argument(
        "--out",
        type=pathlib.Path,
        default=pathlib.Path(__file__).resolve().parents[2] / "accept" / "container-over-declared-size.vnz",
    )
    args = ap.parse_args()

    data = build()
    failures = verify(data)
    if failures:
        print("REFUSING to write: the archive does not have the properties the case needs", file=sys.stderr)
        for f in failures:
            print(f"  - {f}", file=sys.stderr)
        return 1

    doc = next(h for h in parse_local_headers(data) if h["name"] == "doc.vnx")
    print(f"{args.out.name}: {len(data)} bytes; doc.vnx declares {doc['declared']} and holds {doc['present']}")

    if args.out.exists():
        committed = args.out.read_bytes()
        if committed == data:
            print("the committed fixture is byte-identical")
            return 0
        print(f"DRIFT: the committed fixture is {len(committed)} bytes and this script builds {len(data)}",
              file=sys.stderr)
        if not args.write:
            print("re-run with --write only if you mean to replace it, and say in the sidecar "
                  "which rule the old bytes were the witness for", file=sys.stderr)
            return 1
    if args.write:
        args.out.write_bytes(data)
        print(f"wrote {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

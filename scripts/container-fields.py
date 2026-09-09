#!/usr/bin/env python3
"""Compare .vnz archives field by field, against SPEC.md section 7.1.

Python 3, standard library only, no VNX implementation required — the same bar
the corpus itself is held to.

    scripts/container-fields.py a.vnz b.vnz c.vnz     # compare seats
    scripts/container-fields.py --spec a.vnz          # check WRITER OUTPUT against 7.1
    scripts/container-fields.py --fields a.vnz        # just print the table
    scripts/container-fields.py --self-test           # prove the checks can fail

Exits non-zero when any archive disagrees with another, when a local and
central header disagree with each other, or, under --spec, when a field departs
from what section 7.1 fixes.

`--spec` is for an archive a conforming WRITER produced. Section 7.1 constrains
writers, and section 7 has a reader accept far more — so an INPUT fixture, such
as `corpus/accept/container-zip64.vnz`, legitimately reports faults under
`--spec` and is not defective. Without the flag the tool checks only what is
malformed under any reading, which is what an input fixture should be held to.

ZIP64 is read, because section 7 requires a reader to understand it and lets a
writer omit it, so a legal input can carry it. The parse is cross-checked
against Python's own `zipfile` on the corpus's ZIP64 archive.

WHY THIS EXISTS, AND WHY IT IS NOT TEN LINES OF `zipfile`
--------------------------------------------------------

**It diffs fields, not hashes.** A hash mismatch says only "you differ", and
costs a round trip per field to localise. Reported 2026-09-09 by the seat that
did it, not measured here: a field list sent beside a hash let one implementation
find and fix two divergent fields in a single message and another find three.

**It parses raw, and never through a ZIP library.** Every ZIP library in every
language resolves an entry from the CENTRAL DIRECTORY, on purpose — that is what
the central directory is for. So no library-based reader can witness a rule
about a LOCAL header, and a local header that disagrees with its central twin is
structurally invisible to it. That is not hypothetical. Measured here: all four
of the corpus's deflate archives carry zeros for the CRC and both sizes in every
local header. Reported by another seat, not measured here: the same is true of a
live VNX writer, and the very test written to pin its known gaps could not see
it, because that test read the output back through a ZIP reader and asserted on
fields the central directory had right.

**So it asserts local and central agree** — on field values AND on entry
sequence, which can disagree independently. No library-based reader can make
either assertion, which is why nobody had them.

**And it checks entry ORDER**, which section 7.1 fixes: the root document first,
then attachments sorted by entry name under section 4.5.1's code-point
comparator. Caller order is not a property of the content, so two conforming
exporters — one iterating a hash map, one a sorted list — would otherwise emit
different archives while satisfying every other field in the table.

Three implementations were found emitting caller order on 2026-09-10 — two
reporting their own, one measured here — and this tool certified them clean
before it looked at sequence. THAT IS THREE FOUND, NOT THREE OF SIX: the other
implementations were never checked, and an earlier draft of this paragraph said
"three of the six", which another seat then cited as a partition. Run --spec
against a writer's output rather than trusting any count, including this one.

**Always-STORED is what makes any of this possible.** Under deflate there is no
canonical output — Go's compress/flate and zlib emit different bytes for the
same input, and no zlib level reproduces Go's — so no expected bytes could be
committed for anyone to compare against. Stored has no freedom, so a container's
bytes are fully determined by the field table in section 7.1, and any seat can
derive them rather than trust whoever generated them.
"""

import argparse
import os
import struct
import sys

LOCAL_SIGNATURE = 0x04034B50
CENTRAL_SIGNATURE = 0x02014B50
EOCD_SIGNATURE = 0x06054B50
ZIP64_LOCATOR_SIGNATURE = 0x07064B50
ZIP64_EOCD_SIGNATURE = 0x06064B50
SATURATED32 = 0xFFFFFFFF
SATURATED16 = 0xFFFF

# What SPEC.md section 7.1 fixes. `None` means the field is content-determined
# (a CRC, a size, a name) and is compared between archives rather than to a
# constant.
SPEC_7_1 = {
    "version_needed": 10,
    "version_made_by": 10,
    "create_system": 0,
    "flags": 0x0800,
    "method": 0,
    "dos_time": 0x0000,
    "dos_date": 0x0021,
    "extra_len": 0,
    "comment_len": 0,
    "disk_start": 0,
    "internal_attr": 0,
    "external_attr": 0,
}

# The fields a local and a central header both carry, which must agree.
SHARED = ["version_needed", "flags", "method", "dos_time", "dos_date", "crc", "csize", "usize", "name"]


class Fault(Exception):
    pass


def u16(b, i):
    return struct.unpack_from("<H", b, i)[0]


def u32(b, i):
    return struct.unpack_from("<I", b, i)[0]


def u64(b, i):
    return struct.unpack_from("<Q", b, i)[0]


def zip64_extra(data, offset, length, wanted):
    """The ZIP64 extra field's values, in presence order.

    Section 7 requires a READER to understand ZIP64 and allows a writer to omit
    it, so an archive carrying it is legal input that this tool must parse.
    `wanted` names which of uncompressed, compressed and local-offset were
    saturated, in that fixed order; the extra field carries only those, with no
    tags of their own.
    """
    p = offset
    end = offset + length
    while p + 4 <= end:
        tag = u16(data, p)
        size = u16(data, p + 2)
        if tag == 0x0001:
            q = p + 4
            values = {}
            for field in wanted:
                if q + 8 > end:
                    break
                values[field] = u64(data, q)
                q += 8
            return values
        p += 4 + size
    raise Fault("a field is saturated but the entry carries no ZIP64 extra field")


def find_eocd(data):
    earliest = max(0, len(data) - 22 - 0xFFFF)
    for p in range(len(data) - 22, earliest - 1, -1):
        if u32(data, p) == EOCD_SIGNATURE:
            return p
    raise Fault("no end-of-central-directory record: not a ZIP archive")


def read_central(data):
    """Every central directory entry, in order, as plain dicts."""
    eocd = find_eocd(data)
    count = u16(data, eocd + 10)
    offset = u32(data, eocd + 16)
    if count == SATURATED16 or offset == SATURATED32:
        locator = eocd - 20
        if locator < 0 or u32(data, locator) != ZIP64_LOCATOR_SIGNATURE:
            raise Fault("a 32-bit field is saturated but there is no ZIP64 locator")
        zip64 = u64(data, locator + 8)
        if u32(data, zip64) != ZIP64_EOCD_SIGNATURE:
            raise Fault("the ZIP64 locator does not point at a ZIP64 end-of-central-directory record")
        count = u64(data, zip64 + 32)
        offset = u64(data, zip64 + 48)
    entries = []
    p = offset
    for i in range(count):
        if u32(data, p) != CENTRAL_SIGNATURE:
            raise Fault(f"central directory entry {i + 1} has a bad signature")
        name_len = u16(data, p + 28)
        extra_len = u16(data, p + 30)
        comment_len = u16(data, p + 32)
        usize, csize = u32(data, p + 24), u32(data, p + 20)
        local_offset = u32(data, p + 42)
        saturated = [
            field
            for field, value in (("usize", usize), ("csize", csize), ("local_offset", local_offset))
            if value == SATURATED32
        ]
        if saturated:
            real = zip64_extra(data, p + 46 + name_len, extra_len, saturated)
            usize = real.get("usize", usize)
            csize = real.get("csize", csize)
            local_offset = real.get("local_offset", local_offset)
        entries.append(
            {
                "version_made_by": data[p + 4],
                "create_system": data[p + 5],
                "version_needed": u16(data, p + 6),
                "flags": u16(data, p + 8),
                "method": u16(data, p + 10),
                "dos_time": u16(data, p + 12),
                "dos_date": u16(data, p + 14),
                "crc": u32(data, p + 16),
                "csize": csize,
                "usize": usize,
                "extra_len": extra_len,
                "comment_len": comment_len,
                "disk_start": u16(data, p + 34),
                "internal_attr": u16(data, p + 36),
                "external_attr": u32(data, p + 38),
                "local_offset": local_offset,
                "name": data[p + 46 : p + 46 + name_len].decode("utf-8", "replace"),
            }
        )
        p += 46 + name_len + extra_len + comment_len
    return entries


def read_local(data, offset):
    if u32(data, offset) != LOCAL_SIGNATURE:
        raise Fault(f"no local header at offset {offset}")
    name_len = u16(data, offset + 26)
    extra_len = u16(data, offset + 28)
    csize, usize = u32(data, offset + 18), u32(data, offset + 22)
    saturated = [f for f, v in (("usize", usize), ("csize", csize)) if v == SATURATED32]
    if saturated:
        # The local header's ZIP64 extra carries uncompressed then compressed,
        # both always present, regardless of which was saturated.
        real = zip64_extra(data, offset + 30 + name_len, extra_len, ["usize", "csize"])
        usize = real.get("usize", usize)
        csize = real.get("csize", csize)
    return {
        "version_needed": u16(data, offset + 4),
        "flags": u16(data, offset + 6),
        "method": u16(data, offset + 8),
        "dos_time": u16(data, offset + 10),
        "dos_date": u16(data, offset + 12),
        "crc": u32(data, offset + 14),
        "csize": csize,
        "usize": usize,
        "extra_len": extra_len,
        "name": data[offset + 30 : offset + 30 + name_len].decode("utf-8", "replace"),
    }


def describe(path):
    """One archive as a list of {name, local, central} records."""
    with open(path, "rb") as f:
        data = f.read()
    out = []
    for central in read_central(data):
        out.append(
            {
                "name": central["name"],
                "central": central,
                "local": read_local(data, central["local_offset"]),
            }
        )
    return out, len(data)


def show(field, value):
    if value is None:
        return "-"
    if field in ("flags",):
        return f"0x{value:04x}"
    if field in ("dos_time", "dos_date"):
        return f"0x{value:04x}"
    if field in ("crc", "external_attr"):
        return f"0x{value:08x}"
    return str(value)


def check_local_matches_central(archives, faults):
    """The assertion no ZIP library can make.

    General purpose bit 3 defers the CRC and both sizes to a descriptor after the
    payload, so a local header that carries zeros for them is not disagreeing
    with its central twin — it is declining to state them, which section 7 has a
    reader accept. Comparing them anyway would report every bit-3 archive as
    malformed, which over-fires on legal input and is how an instrument stops
    being believed. Section 7.1 forbids a WRITER setting bit 3, and `--spec`
    reports that separately.
    """
    for path, (entries, _) in archives.items():
        for entry in entries:
            deferred = bool(entry["local"]["flags"] & 0x0008)
            for field in SHARED:
                if deferred and field in ("crc", "csize", "usize"):
                    continue
                a = entry["local"].get(field)
                b = entry["central"].get(field)
                if a != b:
                    faults.append(
                        f"{path}: entry {entry['name']!r}: local {field}="
                        f"{show(field, a)} but central {field}={show(field, b)}"
                    )


def utf16_order(name):
    """A sort key giving UTF-16 code unit order, which is NOT code point order."""
    return name.encode("utf-16-be", "surrogatepass")


def root_document(entries):
    """Section 7's resolution: `doc.vnx` at the root, else the first `.vnx`."""
    for entry in entries:
        if entry["name"].lower() == "doc.vnx":
            return entry
    for entry in entries:
        if entry["name"].lower().endswith(".vnx"):
            return entry
    return None


def check_order_consistency(archives, faults):
    """The two header lists against each other. Malformed under any reading."""
    for path, (entries, _) in archives.items():
        names = [e["name"] for e in entries]
        by_local = [e["name"] for e in sorted(entries, key=lambda e: e["central"]["local_offset"])]
        if by_local != names:
            faults.append(
                f"{path}: the local headers and the central directory disagree about entry order\n"
                f"         central directory: {names}\n"
                f"         local headers:     {by_local}"
            )


def check_spec_order(archives, faults):
    """Section 7.1's entry order. A writer rule, so it belongs under --spec."""
    for path, (entries, _) in archives.items():
        names = [e["name"] for e in entries]
        if not names:
            continue
        root = root_document(entries)
        if root is None:
            faults.append(f"{path}: no entry with a .vnx extension, so there is no root document")
            continue
        if names[0] != root["name"]:
            faults.append(
                f"{path}: the root document {root['name']!r} is at index "
                f"{names.index(root['name'])}, and section 7.1 puts it first — which is what makes "
                f"section 7's first-.vnx fallback agree with the preferred name"
            )

        attachments = [n for n in names if n != root["name"]]
        wanted = sorted(attachments)
        if attachments != wanted:
            note = ""
            if attachments == sorted(attachments, key=utf16_order):
                note = (
                    "\n         These are in UTF-16 CODE UNIT order. Section 4.5.1's comparator is by "
                    "CODE POINT, and the two disagree above U+FFFF: an astral character is a surrogate "
                    "pair beginning at U+D800, which sorts below a BMP character in U+E000..U+FFFF by "
                    "code unit and above it by code point. A default string sort is the code-unit one in "
                    "several languages, so this is the case where an implementer is most certain their "
                    "sort is right."
                )
            faults.append(
                f"{path}: attachments are not sorted by entry name\n"
                f"         emitted: {attachments}\n"
                f"         sorted:  {wanted}{note}"
            )


def check_spec(archives, faults):
    for path, (entries, _) in archives.items():
        for entry in entries:
            for field, expected in SPEC_7_1.items():
                for where in ("local", "central"):
                    if field not in entry[where]:
                        continue
                    actual = entry[where][field]
                    if actual != expected:
                        faults.append(
                            f"{path}: entry {entry['name']!r}: {where} {field}="
                            f"{show(field, actual)}, section 7.1 fixes it at {show(field, expected)}"
                        )
            if entry["local"]["flags"] & 0x0008:
                faults.append(
                    f"{path}: entry {entry['name']!r}: the data-descriptor bit is set. Section 7.1 "
                    "requires the CRC and both sizes in the local header: a STORED payload can itself "
                    "contain PK\\x03\\x04, so deferring them makes forward-only parsing ambiguous."
                )
            if not entry["local"]["flags"] & 0x0800:
                faults.append(
                    f"{path}: entry {entry['name']!r}: the UTF-8 filename flag is not set."
                )


def compare(archives, faults):
    """Every archive against the first, field by field."""
    paths = list(archives)
    reference = paths[0]
    ref_entries = archives[reference][0]
    for path in paths[1:]:
        entries = archives[path][0]
        if len(entries) != len(ref_entries):
            faults.append(
                f"{path}: {len(entries)} entries against {reference}'s {len(ref_entries)}"
            )
            continue
        for a, b in zip(ref_entries, entries):
            if a["name"] != b["name"]:
                faults.append(f"{path}: entry named {b['name']!r} where {reference} has {a['name']!r}")
                continue
            for where in ("local", "central"):
                for field in a[where]:
                    if field == "local_offset":
                        continue
                    if a[where][field] != b[where].get(field):
                        faults.append(
                            f"{path}: entry {a['name']!r}: {where} {field}="
                            f"{show(field, b[where].get(field))} where {reference} has "
                            f"{show(field, a[where][field])}"
                        )


def print_table(archives):
    paths = list(archives)
    width = max(len(p) for p in paths)
    for path in paths:
        entries, size = archives[path]
        print(f"{path:<{width}}  {size} bytes, {len(entries)} entries")
    print()
    names = [e["name"] for e in archives[paths[0]][0]]
    for index, name in enumerate(names):
        print(f"  entry {index}: {name!r}")
        fields = ["version_made_by", "create_system"] + SHARED + [
            "extra_len", "comment_len", "disk_start", "internal_attr", "external_attr",
        ]
        for field in fields:
            cells = []
            for path in paths:
                entries = archives[path][0]
                entry = entries[index] if index < len(entries) else None
                if entry is None:
                    cells.append("-")
                    continue
                local = entry["local"].get(field)
                central = entry["central"].get(field)
                if field in SHARED and local != central:
                    cells.append(f"L:{show(field, local)}/C:{show(field, central)}")
                else:
                    cells.append(show(field, central if central is not None else local))
            differ = len(set(cells)) > 1
            marker = "  <-- DIFFERS" if differ else ""
            print(f"    {field:<17} " + "  ".join(f"{c:<22}" for c in cells) + marker)
        print()


# ----------------------------------------------------------------- self test


def _build(entries, *, bit3=False, method=0, central_order=None):
    """A minimal ZIP builder, for archives that are deliberately wrong."""
    out = bytearray()
    directory = []
    for name, body in entries:
        n = name.encode("utf-8")
        crc = 0 if bit3 else struct.unpack("<I", struct.pack("<I", _crc32(body)))[0]
        size = 0 if bit3 else len(body)
        flags = 0x0800 | (0x0008 if bit3 else 0)
        offset = len(out)
        out += struct.pack(
            "<IHHHHHIIIHH", LOCAL_SIGNATURE, 10, flags, method, 0, 0x21, crc, size, size, len(n), 0
        ) + n + body
        directory.append((name, _crc32(body), len(body), offset, flags))
    order = central_order if central_order is not None else range(len(directory))
    cd = bytearray()
    for i in order:
        name, crc, size, offset, flags = directory[i]
        n = name.encode("utf-8")
        cd += struct.pack(
            "<IHHHHHHIIIHHHHHII", CENTRAL_SIGNATURE, 10, 10, flags, method, 0, 0x21,
            crc, size, size, len(n), 0, 0, 0, 0, 0, offset,
        ) + n
    cd_offset = len(out)
    out += cd
    out += struct.pack(
        "<IHHHHIIH", EOCD_SIGNATURE, 0, 0, len(directory), len(directory), len(cd), cd_offset, 0
    )
    return bytes(out)


def _crc32(body):
    import zlib

    return zlib.crc32(body) & 0xFFFFFFFF


def self_test():
    """Every check, shown failing on an archive built to break it.

    A checker nobody has watched fail is a green light with no bulb in it. These
    are here rather than in a shell history so that whoever inherits this file can
    re-establish in one command that it still discriminates.
    """
    import tempfile

    doc = b'{\n  "vnx": "0.1.0"\n}\n'
    emoji = "\U0001F600.txt"
    replacement = "\uFFFD.txt"

    cases = [
        # name, archive, expect_fault_without_spec, expect_fault_with_spec, substring
        ("conforming", _build([("doc.vnx", doc), ("aaa.txt", b"a"), ("zzz.txt", b"z")]), False, False, None),
        ("attachments in caller order",
         _build([("doc.vnx", doc), ("zzz.txt", b"z"), ("aaa.txt", b"a")]), False, True, "not sorted"),
        ("attachments in UTF-16 order",
         _build([("doc.vnx", doc), (emoji, b"e"), (replacement, b"r")]), False, True, "CODE UNIT"),
        ("root document not first",
         _build([("aaa.txt", b"a"), ("doc.vnx", doc)]), False, True, "section 7.1 puts it first"),
        ("deflate", _build([("doc.vnx", doc)], method=8), False, True, "method"),
        ("data descriptor bit", _build([("doc.vnx", doc)], bit3=True), False, True, "data-descriptor"),
        ("local and central order disagree",
         _build([("doc.vnx", doc), ("aaa.txt", b"a"), ("zzz.txt", b"z")], central_order=[0, 2, 1]),
         True, True, "disagree about entry order"),
    ]

    # A local CRC that disagrees with its central twin, bit 3 clear.
    tampered = bytearray(_build([("doc.vnx", doc)]))
    struct.pack_into("<I", tampered, 14, 0xDEADBEEF)
    cases.append(("local CRC disagrees with central", bytes(tampered), True, True, "local crc="))

    # A saturated field with no ZIP64 extra to resolve it from.
    saturated = bytearray(_build([("doc.vnx", doc)]))
    eocd = saturated.rfind(b"PK\x05\x06")
    cd_offset = u32(saturated, eocd + 16)
    struct.pack_into("<I", saturated, cd_offset + 20, SATURATED32)
    cases.append(("saturated field, no ZIP64 extra", bytes(saturated), None, None, "no ZIP64 extra field"))

    failures = []
    with tempfile.TemporaryDirectory() as tmp:
        for name, data, want_plain, want_spec, substring in cases:
            path = os.path.join(tmp, "case.vnz")
            with open(path, "wb") as f:
                f.write(data)
            for spec, want in (("", want_plain), ("--spec", want_spec)):
                if want is None:
                    # Expected to be unparseable, which is exit 2 either way.
                    try:
                        describe(path)
                        failures.append(f"{name}: parsed, and should not have")
                    except Fault as e:
                        if substring not in str(e):
                            failures.append(f"{name}: refused with {e!r}, expected {substring!r}")
                        else:
                            print(f"ok    {name} [unparseable, as expected]")
                    break
                archives = {path: describe(path)}
                faults = []
                check_local_matches_central(archives, faults)
                check_order_consistency(archives, faults)
                if spec:
                    check_spec(archives, faults)
                    check_spec_order(archives, faults)
                got = bool(faults)
                label = f"{name} [{spec or 'no flag'}]"
                if got != want:
                    failures.append(
                        f"{label}: expected {'a fault' if want else 'no fault'}, got {len(faults)}"
                        + ("" if not faults else f" — first: {faults[0].splitlines()[0]}")
                    )
                elif want and substring and not any(substring in f for f in faults):
                    failures.append(f"{label}: faulted, but none mentioned {substring!r}")
                else:
                    print(f"ok    {label}")

    for failure in failures:
        print(f"FAIL  {failure}")
    if failures:
        print(f"\n{len(failures)} self-test failure(s): this checker is not discriminating.")
        return 1
    print(f"\nall {len(cases)} cases discriminate")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("archives", nargs="*", help=".vnz files to compare")
    parser.add_argument("--spec", action="store_true", help="also check each against SPEC.md 7.1")
    parser.add_argument("--fields", action="store_true", help="print the field table even when all agree")
    parser.add_argument("--self-test", action="store_true", help="prove every check can fail")
    args = parser.parse_args()

    if args.self_test:
        return self_test()
    if not args.archives:
        parser.error("give at least one .vnz, or --self-test")

    archives = {}
    for path in args.archives:
        try:
            archives[path] = describe(path)
        except (Fault, OSError, struct.error) as e:
            print(f"{path}: {e}", file=sys.stderr)
            return 2

    faults = []
    check_local_matches_central(archives, faults)
    check_order_consistency(archives, faults)
    if args.spec:
        check_spec(archives, faults)
        check_spec_order(archives, faults)
    if len(archives) > 1:
        compare(archives, faults)

    if faults or args.fields:
        print_table(archives)
    for fault in faults:
        print(f"FAULT  {fault}")
    if faults:
        print(f"\n{len(faults)} fault(s).")
        if args.spec:
            print(
                "\nNote: --spec judges WRITER OUTPUT against section 7.1. An archive that is INPUT\n"
                "is not defective for failing it. Section 7 has a reader accept deflate, the\n"
                "data-descriptor bit, ZIP64 and a root document at any position, and the corpus's\n"
                "container fixtures are input that exists to exercise exactly those. Without\n"
                "--spec this tool checks only what is malformed under any reading."
            )
        return 1
    print(f"{len(archives)} archive(s) agree" if len(archives) > 1 else "no faults")
    return 0


if __name__ == "__main__":
    sys.exit(main())

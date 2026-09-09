#!/usr/bin/env python3
"""Compare .vnz archives field by field, against SPEC.md section 7.1.

Python 3, standard library only, no VNX implementation required — the same bar
the corpus itself is held to.

    scripts/container-fields.py a.vnz b.vnz c.vnz     # compare seats
    scripts/container-fields.py --spec a.vnz          # check one against 7.1
    scripts/container-fields.py --fields a.vnz        # just print the table

Exits non-zero when any archive disagrees with another, when a local and
central header disagree with each other, or, under --spec, when a field departs
from what section 7.1 fixes.

WHY THIS EXISTS, AND WHY IT IS NOT TEN LINES OF `zipfile`
--------------------------------------------------------

**It diffs fields, not hashes.** A hash mismatch says only "you differ", and
costs a round trip per field to localise. Measured 2026-09-09: a field list sent
beside a hash let one implementation find and fix two divergent fields in a
single message and another find three. That one choice turned a comparison into
a repair, three times in one evening.

**It parses raw, and never through a ZIP library.** Every ZIP library in every
language resolves an entry from the CENTRAL DIRECTORY, on purpose — that is what
the central directory is for. So no library-based reader can witness a rule
about a LOCAL header, and a local header that disagrees with its central twin is
structurally invisible to it. That is not hypothetical: a live VNX writer was
found on 2026-09-09 setting the data-descriptor bit and writing zeros for the
CRC and both sizes into its local headers, and the very test written to pin its
known gaps could not see it, because that test read the output back through a
ZIP reader and asserted on fields the central directory had right.

**So it asserts local and central agree.** No library-based reader can make that
assertion, which is why nobody had it.

**Always-STORED is what makes any of this possible.** Under deflate there is no
canonical output — Go's compress/flate and zlib emit different bytes for the
same input, and no zlib level reproduces Go's — so no expected bytes could be
committed for anyone to compare against. Stored has no freedom, so a container's
bytes are fully determined by the field table in section 7.1, and any seat can
derive them rather than trust whoever generated them.
"""

import argparse
import struct
import sys

LOCAL_SIGNATURE = 0x04034B50
CENTRAL_SIGNATURE = 0x02014B50
EOCD_SIGNATURE = 0x06054B50

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
    entries = []
    p = offset
    for i in range(count):
        if u32(data, p) != CENTRAL_SIGNATURE:
            raise Fault(f"central directory entry {i + 1} has a bad signature")
        name_len = u16(data, p + 28)
        extra_len = u16(data, p + 30)
        comment_len = u16(data, p + 32)
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
                "csize": u32(data, p + 20),
                "usize": u32(data, p + 24),
                "extra_len": extra_len,
                "comment_len": comment_len,
                "disk_start": u16(data, p + 34),
                "internal_attr": u16(data, p + 36),
                "external_attr": u32(data, p + 38),
                "local_offset": u32(data, p + 42),
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
    return {
        "version_needed": u16(data, offset + 4),
        "flags": u16(data, offset + 6),
        "method": u16(data, offset + 8),
        "dos_time": u16(data, offset + 10),
        "dos_date": u16(data, offset + 12),
        "crc": u32(data, offset + 14),
        "csize": u32(data, offset + 18),
        "usize": u32(data, offset + 22),
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
    """The assertion no ZIP library can make."""
    for path, (entries, _) in archives.items():
        for entry in entries:
            for field in SHARED:
                a = entry["local"].get(field)
                b = entry["central"].get(field)
                if a != b:
                    faults.append(
                        f"{path}: entry {entry['name']!r}: local {field}="
                        f"{show(field, a)} but central {field}={show(field, b)}"
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


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("archives", nargs="+", help=".vnz files to compare")
    parser.add_argument("--spec", action="store_true", help="also check each against SPEC.md 7.1")
    parser.add_argument("--fields", action="store_true", help="print the field table even when all agree")
    args = parser.parse_args()

    archives = {}
    for path in args.archives:
        try:
            archives[path] = describe(path)
        except (Fault, OSError, struct.error) as e:
            print(f"{path}: {e}", file=sys.stderr)
            return 2

    faults = []
    check_local_matches_central(archives, faults)
    if args.spec:
        check_spec(archives, faults)
    if len(archives) > 1:
        compare(archives, faults)

    if faults or args.fields:
        print_table(archives)
    for fault in faults:
        print(f"FAULT  {fault}")
    if faults:
        print(f"\n{len(faults)} fault(s).")
        return 1
    print(f"{len(archives)} archive(s) agree" if len(archives) > 1 else "no faults")
    return 0


if __name__ == "__main__":
    sys.exit(main())

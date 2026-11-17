"""The key vocabulary of the format.

SPEC 6.2 requires a diagnostic for "an unrecognised key on an object defined by
this format", but the specification never states a key set for any object: every
one arrives through a JSON example, and SPEC 1.3 says an example "does not
define a rule, and a rule that is not stated in prose is a gap".  These sets are
therefore read off the examples in SPEC 3, cross-checked against every file in
``corpus/accept/`` -- which must produce no diagnostics, so it constrains them
from below -- and they are this implementation's reading, not the format's.

Where a set is wider than any single example, the widening is noted.
"""

from __future__ import annotations

__all__ = [
    "DOCUMENT",
    "FOLDER",
    "NETWORK",
    "ARC",
    "HIGHLIGHT",
    "PASS",
    "PLAN",
    "TRACK",
    "SEGMENT",
    "POINT",
    "AREA",
    "POINT_IN_NETWORK",
    "AREA_IN_NETWORK",
]

# SPEC 3.10 is now the authority for both halves of this file.  Until 2026-09-09
# the format stated no key set at all -- every one arrived inside an example, and
# SPEC 1.3 says an example defines no rule -- so these sets were read off the
# examples and constrained from below by the accept corpus.  They are now the
# published table, checked against it key by key.

# SPEC 3.1.  The document's key set is open: every key containing a full stop
# is an application key (SPEC 3.9), which the decoder recognises separately, and
# at the top level only.
DOCUMENT = frozenset(
    {
        "vnx",
        "generator",
        "created",
        "folders",
        "networks",
        "plans",
        "tracks",
        "points",
        "areas",
    }
)

# SPEC 3.10.  `notes` and `data` were missing from this implementation's reading
# -- neither appears in SPEC 3.2's example -- and the published table has both.
# A folder places itself with `parent`, never with `folder`.
FOLDER = frozenset({"id", "parent", "name", "notes", "hidden", "data"})

# SPEC 3.3, plus `folder` (SPEC 3.2), and `hidden` and `pinned` (SPEC 3.8).
NETWORK = frozenset(
    {
        "folder",
        "name",
        "notes",
        "hidden",
        "pinned",
        "nodes",
        "arcs",
        "highlights",
        "points",
        "areas",
        "data",
    }
)

# SPEC 3.3.2.  One shape for an arc of a network and an arc of a plan.  An
# arc is not one of the items SPEC 3.8 lists as carrying `hidden`, and has no
# name of its own.
ARC = frozenset({"from", "to", "pips", "data"})

# SPEC 3.3.3.  A highlight is a lightweight container for data objects along a
# walk, with an optional name and no notes of its own.
HIGHLIGHT = frozenset({"name", "hidden", "pinned", "walk", "data"})

# SPEC 3.3.3.  A pass of a highlight's walk or of a plan's.
PASS = frozenset({"arc", "from", "to", "turn", "pin"})

# SPEC 3.4.  A plan owns a graph of a network's shape, and one walk over it.
PLAN = frozenset(
    {
        "folder",
        "name",
        "notes",
        "hidden",
        "pinned",
        "nodes",
        "arcs",
        "walk",
        "data",
    }
)

# SPEC 3.5.  `notes` is not in the section's example.  Dave ruled it legal on
# 2026-09-09, after this implementation had assumed it on no evidence at all.
TRACK = frozenset(
    {
        "folder",
        "name",
        "notes",
        "hidden",
        "pinned",
        "started",
        "ended",
        "segments",
        "data",
    }
)

SEGMENT = frozenset({"pips"})

# SPEC 3.6.  One shape for a top-level point and a network's point.  SPEC
# 3.8 lists "points" among the hosts of `hidden` and does not say whether that
# reaches a top-level point; Dave ruled on 2026-09-09 that it does, because
# "share one shape" governs.
POINT = frozenset({"folder", "name", "notes", "hidden", "pinned", "at", "data"})

# SPEC 3.7.
AREA = frozenset({"folder", "name", "notes", "hidden", "pinned", "polygon", "data"})

# SPEC 3.2 gives `folder` to "any item", which this implementation read as
# reaching a network's own points and areas too.  Ruled the other way on
# 2026-09-09: an item a network already contains does not also sit in a folder.
#
# The ruling is conditional -- it stands unless Viatrail's exporter is found to
# write one, in which case `folder` becomes legal there rather than have the
# format diagnose its own output.  If that happens, delete these two names and
# pass POINT and AREA from `decode._network` again; nothing else changes.
POINT_IN_NETWORK = POINT - {"folder"}
AREA_IN_NETWORK = AREA - {"folder"}

# SPEC 3.10's Required column.  "A key marked REQUIRED must be present for the
# object to exist", and SPEC 6.2: a REQUIRED key carrying a value of the wrong
# type means the reader MUST drop the ITEM, not the field, because dropping the
# field would leave a point that is nowhere or an arc with no geometry.
#
# Requiring a key does not require a non-empty value: `"walk": []` satisfies
# `walk`.  This implementation distinguishes them -- absent is None, present
# and empty is an empty list -- so the two cannot be confused.
#
# An absent required key is reported at the object that lacks it (SPEC 6.3);
# `diagnose/required-key-absent` and `diagnose/folder-duplicate-id` pin that.
REQUIRED_FOLDER = frozenset({"id"})
REQUIRED_ARC = frozenset({"from", "to", "pips"})
REQUIRED_HIGHLIGHT = frozenset({"walk"})
REQUIRED_PASS = frozenset({"arc"})
REQUIRED_PLAN = frozenset({"walk"})
REQUIRED_TRACK = frozenset({"segments"})
REQUIRED_SEGMENT = frozenset({"pips"})
REQUIRED_POINT = frozenset({"at"})
REQUIRED_AREA = frozenset({"polygon"})

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
    "DOCUMENT", "FOLDER", "NETWORK", "ARC", "CHAIN", "LINK", "ROUTE",
    "ROUTE_ARC", "TRACK", "SEGMENT", "WAYPOINT", "AREA",
    "WAYPOINT_IN_NETWORK", "AREA_IN_NETWORK",
]

# SPEC 3.1.
DOCUMENT = frozenset({
    "vnx", "profile", "generator", "created",
    "folders", "networks", "routes", "tracks", "waypoints", "areas", "rulesets",
})

# SPEC 3.2.  A folder places itself with `parent`, not with the `folder` key
# that SPEC 3.2 gives to "any item"; nothing in the format nests the two.
FOLDER = frozenset({"id", "name", "parent", "hidden"})

# SPEC 3.3, plus `folder` (SPEC 3.2) and `hidden` (SPEC 3.8).
NETWORK = frozenset({
    "folder", "name", "notes", "hidden",
    "nodes", "arcs", "chains", "points", "areas", "data",
})

# SPEC 3.3.2.  An arc inside a network's graph is not one of the items SPEC 3.8
# lists as carrying `hidden`, and has no name of its own.
ARC = frozenset({"from", "to", "pips", "data"})

# SPEC 3.3.3.  `notes` is not in the section's example; `example-network.vnx`
# carries one on the "Rock fall" chain and is an accept case, so it is legal.
CHAIN = frozenset({"name", "notes", "hidden", "style", "links", "data"})

LINK = frozenset({"arc", "from", "to"})

# SPEC 3.4.
ROUTE = frozenset({"folder", "name", "notes", "hidden", "style", "arcs", "data"})

ROUTE_ARC = frozenset({"pips", "data"})

# SPEC 3.5.  `notes` is not in the section's example.  Dave ruled it legal on
# 2026-09-09, after this implementation had assumed it on no evidence at all.
TRACK = frozenset({
    "folder", "name", "notes", "hidden", "started", "ended",
    "style", "segments", "data",
})

SEGMENT = frozenset({"pips"})

# SPEC 3.6.  One shape for a top-level waypoint and a network's point.  SPEC
# 3.8 lists "points" among the hosts of `hidden` and does not say whether that
# reaches a top-level waypoint; Dave ruled on 2026-09-09 that it does, because
# "share one shape" governs.
WAYPOINT = frozenset({"folder", "name", "notes", "hidden", "at", "style", "data"})

# SPEC 3.7.
AREA = frozenset({"folder", "name", "notes", "hidden", "polygon", "style", "data"})

# SPEC 3.2 gives `folder` to "any item", which this implementation read as
# reaching a network's own points and areas too.  Ruled the other way on
# 2026-09-09: an item a network already contains does not also sit in a folder.
#
# The ruling is conditional -- it stands unless Viatrail's exporter is found to
# write one, in which case `folder` becomes legal there rather than have the
# format diagnose its own output.  If that happens, delete these two names and
# pass WAYPOINT and AREA from `decode._network` again; nothing else changes.
WAYPOINT_IN_NETWORK = WAYPOINT - {"folder"}
AREA_IN_NETWORK = AREA - {"folder"}

# There is deliberately NO style key set here.
#
# This implementation had three, invented from the three examples in SPEC 3.3.3,
# 3.6 and 3.7, because a style is an object this format defines and SPEC 6.2
# therefore reaches it.  Ruled the other way on 2026-09-09, and the reasoning is
# worth keeping so nobody re-adds them as an obvious improvement:
#
#   A style vocabulary grows faster than a structural one.  Key sets would mean
#   every implementation diagnosing a valid document each time Viatrail adds a
#   style key, and that brittleness costs more than the typo detection buys.
#   A data record body is opaque for the same reason (SPEC 5.1.2), so making a
#   style opaque is the consistent answer rather than a special case.
#
# A style body is therefore opaque: no key inside one can be unknown, and every
# key survives a read-modify-write untouched.
#
# "Styles are opaque, so why is `importance` checked?"  Because opacity is a
# rule about the KEY SET and a normative range is a rule about a VALUE, and the
# two do not collide.  SPEC 5.2 already has exactly this shape: a data record
# body stays opaque while a recognised type carries a typed payload.  So a style
# whose keys are not enumerated can still have a stated range on one of them --
# SPEC 3.6's `importance`, 1 to 5, ruled normative on 2026-09-09 in the same
# breath as `icon-size`'s list of sizes being ruled informative.  The check is
# `decode._Reader._check_importance`.

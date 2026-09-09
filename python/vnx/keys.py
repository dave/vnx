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
    "LINE_STYLE", "POINT_STYLE", "AREA_STYLE",
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

# Style key sets.  SPEC gives one example per host and no rule.  The three sets
# below are by geometry rather than by host: a chain, a route and a track are
# all drawn as lines, and SPEC 3.5's track example showing only `color` and
# `width` is an example, not a restriction.
LINE_STYLE = frozenset({"color", "width", "opacity", "line-style"})
POINT_STYLE = frozenset({
    "color", "icon", "icon-size", "pin-shape",
    "label-position", "label-visible", "importance",
})
AREA_STYLE = frozenset({"fill-color", "fill-opacity", "stroke-color", "stroke-width"})

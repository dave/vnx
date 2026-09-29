"""The rules of a walk (SPEC 3.3.3), and the other rules the reader and the
writer share: an arc's two ends (SPEC 3.3.2) and an open ring (SPEC 3.7).

⚠ ONE COPY OF EACH RULE, AND TWO CALLERS.  The reader drops a chain whose walk
breaks a rule, and the writer refuses to emit one.  If the two held separate
copies, a writer could emit a walk its own reader drops, and nothing would say
so until another implementation read the file.

⚠ A PASS CANNOT BE CHECKED FROM ITS OWN KEYS.  Its entry is where the previous
pass left, and where the previous pass left depends on whether it had a `to`
and a `turn`.  So a :class:`Cursor` carries the node the walk is at from one
pass to the next, in walk order.

Each rule is its own function so that a red proof can switch off exactly one
of them (``tests/test_red_proof.py``).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Union

from .model import Arc, Endpoint, Pass, Position

__all__ = [
    "Broken", "WalkFault", "Cursor", "check_walk", "check_arcs", "check_ring",
    "is_self_loop", "ring_is_closed", "distinct_lon_lat",
    "route_rule", "pinned_turn", "spelling", "connection",
]


@dataclass(frozen=True, slots=True)
class Broken:
    """A rule the pass breaks: the key the diagnostic names, or ``None`` for
    the pass itself, and why."""

    key: Union[str, None]
    message: str


@dataclass(frozen=True, slots=True)
class WalkFault:
    """The first rule a walk breaks.

    ``key`` is ``None`` when the diagnostic names the pass itself, and else the
    pass key it names (SPEC 6.3, rule 2).
    """

    index: int
    key: Union[str, None]
    code: str
    message: str

    def path(self, walk_path: str) -> str:
        at = f"{walk_path}/{self.index}"
        return at if self.key is None else f"{at}/{self.key}"

    def describe(self) -> str:
        at = f"pass {self.index}"
        if self.key is not None:
            at += f" `{self.key}`"
        return f"{at}: {self.message} ({self.code})"


def _is_position(endpoint: Union[Endpoint, None]) -> bool:
    return isinstance(endpoint, Position)


def route_rule(p: Pass) -> Union[Broken, None]:
    """SPEC 3.4: a route's walk runs node to node, with no turn and no pin."""
    if _is_position(p.from_):
        return Broken("from", "a route's walk runs node to node, and this `from` is a position")
    if _is_position(p.to):
        return Broken("to", "a route's walk runs node to node, and this `to` is a position")
    if p.turn is not None:
        return Broken("turn", "a route's walk carries no turn")
    if p.pinned:
        return Broken("pinned", "a route's walk carries no pin")
    return None


def pinned_turn(p: Pass) -> Union[Broken, None]:
    """SPEC 3.3.3: a pass with a turn is never pinned."""
    if p.turn is not None and p.pinned:
        return Broken(None, "a pass with a turn is never pinned: the turn already fixes it")
    return None


def spelling(index: int, last: int, p: Pass) -> Union[Broken, None]:
    """SPEC 3.3.3's rule for `from` and `to`: first `from`, then `to`.

    A key the rule requires and the pass lacks names the pass (``None``); a key
    the pass carries and the rule forbids names that key.
    """
    if index == 0 and p.from_ is None:
        return Broken(None, "the first pass must carry `from`")
    if index > 0 and p.from_ is not None:
        return Broken("from", "only the first pass carries `from`")
    want_to = index == last or _is_position(p.from_)
    if want_to and p.to is None:
        if index == last:
            return Broken(None, "the last pass must carry `to`")
        return Broken(None, "a pass whose `from` is a position must carry `to`")
    if not want_to and p.to is not None:
        return Broken(
            "to",
            "`to` appears only on the last pass, or on a pass whose `from` is a "
            "position",
        )
    return None


def connection(
    at: Union[str, None], index: int, last: int, p: Pass, arc: Arc
) -> Union[Broken, tuple[Endpoint, Endpoint]]:
    """SPEC 3.3.3's valid-walk rules 3, 4 and 5, in SPEC 6.2's order.

    Returns what is broken, else the pass's ``(entry, exit)``.
    """

    def is_end(node: Union[str, None]) -> bool:
        return node is not None and node in (arc.from_, arc.to)

    entry: Endpoint
    if p.from_ is not None:
        if isinstance(p.from_, str) and not is_end(p.from_):
            return Broken("from", f"`from` node {p.from_!r} is not an end of arc {p.arc!r}")
        entry = p.from_
    else:
        if not is_end(at):
            return Broken(
                None, f"the walk is at node {at!r}, which is not an end of arc {p.arc!r}"
            )
        assert at is not None
        entry = at
    exit_: Endpoint
    if isinstance(p.to, str):
        if not is_end(p.to):
            return Broken("to", f"`to` node {p.to!r} is not an end of arc {p.arc!r}")
        exit_ = p.to
    elif p.to is not None:
        if index != last:
            return Broken("to", "only the last pass leaves at a position")
        exit_ = p.to
    elif p.turn is not None:
        # The entry is a node here: a position `from` requires a `to`, which
        # the spelling rule has already enforced.
        exit_ = entry
    elif entry == arc.from_:
        assert arc.to is not None
        exit_ = arc.to
    else:
        assert arc.from_ is not None
        exit_ = arc.from_
    return entry, exit_


@dataclass(slots=True)
class Cursor:
    """The node a walk is at, carried from one pass to the next."""

    last: int
    route: bool = False
    at: Union[str, None] = None

    def step(self, index: int, p: Pass, arc: Arc) -> Union[WalkFault, tuple[Endpoint, Endpoint]]:
        """Check pass *index* against the rules that span more than its own
        values -- SPEC 6.2's steps 5 to 8 -- and move to where it leaves."""
        if self.route:
            broken = route_rule(p)
            if broken is not None:
                return WalkFault(index, broken.key, "route-pin-or-position", broken.message)
        broken = pinned_turn(p)
        if broken is not None:
            return WalkFault(index, broken.key, "pinned-turn", broken.message)
        broken = spelling(index, self.last, p)
        if broken is not None:
            return WalkFault(index, broken.key, "walk-spelling", broken.message)
        leg = connection(self.at, index, self.last, p, arc)
        if isinstance(leg, Broken):
            return WalkFault(index, leg.key, "walk-not-connected", leg.message)
        entry, exit_ = leg
        self.at = exit_ if isinstance(exit_, str) else None
        return entry, exit_


def check_walk(walk: list[Pass], arcs: dict[str, Arc], route: bool) -> Union[WalkFault, None]:
    """The writer's check: every pass names an arc of its owner, and the walk
    follows SPEC 3.3.3.  A writer never emits a walk its own reader would drop.
    """
    cursor = Cursor(last=len(walk) - 1, route=route)
    for index, p in enumerate(walk):
        arc = arcs.get(p.arc)
        if arc is None:
            return WalkFault(
                index, None, "pass-arc-absent", f"pass names arc {p.arc!r}, which is not here"
            )
        for key, value in (("from", p.from_), ("to", p.to), ("turn", p.turn)):
            if isinstance(value, Position) and (
                value.ele is not None or value.time_ms is not None
            ):
                return WalkFault(
                    index, key, "extra-channel",
                    "a walk position carries longitude and latitude only",
                )
        stepped = cursor.step(index, p, arc)
        if isinstance(stepped, WalkFault):
            return stepped
    return None


def is_self_loop(arc: Arc) -> bool:
    """SPEC 3.3.2: an arc's `from` and `to` name two different nodes."""
    return arc.from_ == arc.to


def check_arcs(nodes: dict[str, Position], arcs: dict[str, Arc]) -> Union[str, None]:
    """The writer's check on an owner's arcs: two different nodes, both
    present, and no time on a pip.  Returns what is wrong, naming the arc."""
    for key in sorted(arcs):
        arc = arcs[key]
        if arc.from_ is None or arc.to is None:
            return f"arc {key!r} must name both its nodes"
        if is_self_loop(arc):
            return f"arc {key!r} runs from node {arc.from_!r} to itself, and a self-loop cannot be written"
        for node in (arc.from_, arc.to):
            if node not in nodes:
                return f"arc {key!r} names node {node!r}, which is not in its owner's nodes"
        if any(pip.time_ms is not None for pip in arc.pips or []):
            return f"arc {key!r} has a pip carrying a time, and an arc pip carries none"
    return None


def ring_is_closed(ring: list[Position]) -> bool:
    """SPEC 3.7: the last position repeats the first, in lon and lat."""
    return len(ring) >= 2 and ring[0].same_point(ring[-1])


def distinct_lon_lat(ring: list[Position]) -> int:
    """How many positions differ in longitude or latitude (SPEC 3.7)."""
    return len({(p.lon, p.lat) for p in ring})


def check_ring(ring: list[Position]) -> Union[str, None]:
    """The writer's check on a ring: open, with three distinct positions.

    It does not open a closed ring itself.  That would be the writer guessing
    that its caller meant the repeat, and the repeat is the caller's to fix.
    """
    if ring_is_closed(ring):
        return "a ring is written open, and this one's last position repeats its first"
    count = distinct_lon_lat(ring)
    if count < 3:
        return f"a ring needs at least 3 distinct positions, this one has {count}"
    return None

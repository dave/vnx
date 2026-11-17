"""JSON-tree comparison, which is what the corpus compares (SPEC 8.1).

The bytes of a document are not constrained: key order, number spelling and
whitespace are the writer's own business.  So two documents are compared as the
trees they parse to.

⚠ IT IS NEVER MODEL-VERSUS-MODEL, and that is the finding the accept tier rests
on.  A reader that discarded every data record, every application key, every style and
every ``notes`` produces a model that compares equal to itself, and passes an
accept tier built that way green -- that is the whole extensibility mechanism,
all styling and all user prose.  Comparing the tree parsed from the ORIGINAL
FILE against the tree parsed from encode(decode(file)) is what proves a reader
kept what it read.  `test_red_proof.py` freezes the proof.

Exactly two normalisations are permitted (SPEC 8.1) and there is no third: on a
position, trailing ``null`` channels are dropped; and an absent container and an
empty one normalise to the same value.  A VALUE IS NEVER NORMALISED.  SPEC 4.1
does not constrain a writer's precision, so rounding one side to meet the other
would hide a writer that changed a coordinate.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Callable

__all__ = ["Options", "compare_documents", "parse_tree", "tree_diff"]


@dataclass(frozen=True, slots=True)
class Options:
    """Which normalisations a comparison applies.

    ⚠ THE TWO TIERS NORMALISE DIFFERENTLY, AND MUST.  The accept tier compares a
    document against a re-encoding of itself, so SPEC 4.1's shortest spelling is
    applied to both sides -- otherwise it would require the file to have been
    written by this library.  The write tier compares one writer's output
    against another's, where the shortest spelling is the very rule under test:
    normalising it there would make the tier prove nothing.
    """

    emit_boundary: bool = False


def parse_tree(data: bytes) -> Any:
    """Parse JSON to a plain tree.

    Numbers land as ``float``, so the comparison is between values rather than
    between spellings, which is what SPEC 4.2 makes the round-trip guarantee.
    """
    return json.loads(data.decode("utf-8"), parse_int=float, parse_float=float)


def compare_documents(got: bytes, want: bytes, options: Options) -> str:
    """The first difference between two documents, or ``""`` when equal."""
    return tree_diff(
        _normalise_document(parse_tree(got), options),
        _normalise_document(parse_tree(want), options),
        "",
    )


# -- normalisation -------------------------------------------------------
#
# It is SCHEMA-AWARE rather than a heuristic over shapes, because the two
# regions the format declares opaque -- a data record's body and a style -- may
# hold arrays of numbers that are not positions and must not be shortened, and
# may hold empty containers that a third party means to keep.

Each = Callable[[dict[str, Any], Options], None]


def _normalise_document(value: Any, options: Options) -> Any:
    if not isinstance(value, dict):
        return value
    out = dict(value)
    _items(out, "folders", options, _folder)
    _items(out, "points", options, _point)
    _items(out, "areas", options, _area)
    _items(out, "tracks", options, _track)
    _items(out, "plans", options, _plan)
    _items(out, "networks", options, _network)
    # Application keys are left alone: SPEC 3.9 exempts them from SPEC 8.1's
    # absent-equals-empty normalisation, so a key dropped for holding null or
    # an empty value is a difference.
    return out


def _folder(obj: dict[str, Any], options: Options) -> None:
    _drop_empty(obj, "data")


def _point(obj: dict[str, Any], options: Options) -> None:
    if "at" in obj:
        obj["at"] = _position(obj["at"], options)
    _drop_empty(obj, "style")
    _drop_empty(obj, "data")


def _area(obj: dict[str, Any], options: Options) -> None:
    _position_list(obj, "polygon", options)
    _drop_empty(obj, "style")
    _drop_empty(obj, "data")


def _track(obj: dict[str, Any], options: Options) -> None:
    def segment(seg: dict[str, Any], options: Options) -> None:
        _position_list(seg, "pips", options)

    _items(obj, "segments", options, segment)
    _drop_empty(obj, "style")
    _drop_empty(obj, "data")


def _plan(obj: dict[str, Any], options: Options) -> None:
    _graph(obj, options)
    _walk(obj, options)
    _drop_empty(obj, "style")
    _drop_empty(obj, "data")


def _arc(obj: dict[str, Any], options: Options) -> None:
    _position_list(obj, "pips", options)
    _drop_empty(obj, "data")


def _graph(obj: dict[str, Any], options: Options) -> None:
    """The `nodes` and `arcs` a network and a plan share (SPEC 3.4)."""
    nodes = obj.get("nodes")
    if isinstance(nodes, dict):
        obj["nodes"] = {key: _position(value, options) for key, value in nodes.items()}
    arcs = obj.get("arcs")
    if isinstance(arcs, dict):
        out = {}
        for key, value in arcs.items():
            if isinstance(value, dict):
                value = dict(value)
                _arc(value, options)
            out[key] = value
        obj["arcs"] = out
    _drop_empty(obj, "nodes")
    _drop_empty(obj, "arcs")


def _walk(obj: dict[str, Any], options: Options) -> None:
    """A walk's positions.  `from` and `to` are a node key or a position
    (SPEC 3.3.3); only the second is a position, and `_position` leaves a
    string alone.  `turn` is always a position.

    ⚠ AN EMPTY WALK IS KEPT.  `walk` is REQUIRED (SPEC 3.10), so `"walk": []`
    and an absent walk are different documents, and folding one into the other
    would hide a writer that drops the key.
    """
    items = obj.get("walk")
    if not isinstance(items, list):
        return
    out = []
    for item in items:
        if isinstance(item, dict):
            item = dict(item)
            for key in ("from", "to", "turn"):
                if key in item:
                    item[key] = _position(item[key], options)
        out.append(item)
    obj["walk"] = out


def _network(obj: dict[str, Any], options: Options) -> None:
    _graph(obj, options)
    _items(obj, "highlights", options, _highlight)
    _items(obj, "points", options, _point)
    _items(obj, "areas", options, _area)
    _drop_empty(obj, "data")


def _highlight(obj: dict[str, Any], options: Options) -> None:
    _walk(obj, options)
    _drop_empty(obj, "style")
    _drop_empty(obj, "data")


def _items(obj: dict[str, Any], key: str, options: Options, each: Each) -> None:
    """Normalise an array of objects, dropping the key when it is empty."""
    items = obj.get(key)
    if not isinstance(items, list):
        return
    if not items:
        del obj[key]
        return
    out = []
    for item in items:
        if isinstance(item, dict):
            item = dict(item)
            each(item, options)
        out.append(item)
    obj[key] = out


def _position_list(obj: dict[str, Any], key: str, options: Options) -> None:
    items = obj.get(key)
    if not isinstance(items, list):
        return
    if not items:
        del obj[key]
        return
    obj[key] = [_position(item, options) for item in items]


def _position(value: Any, options: Options) -> Any:
    """SPEC 8.1's first normalisation: a trailing ``null``.

    It is an emit-boundary rule and is applied only where `Options` says so.
    No channel VALUE is touched -- SPEC 4.1 leaves precision to the writer, so
    the comparison must show a value that changed rather than absorb it.

    ⚠ A CHANNEL THIS VERSION DOES NOT NAME IS NOT NORMALISED AWAY, and no corpus
    file carries one.  A fixture asking a writer to reproduce a fifth channel
    would be asking for a rule the format does not state -- the fixture is what
    changes, not the comparison.
    """
    if not isinstance(value, list):
        return value
    out = list(value)
    if options.emit_boundary:
        while len(out) > 2 and out[-1] is None:
            out.pop()
    return out


def _drop_empty(obj: dict[str, Any], key: str) -> None:
    """SPEC 8.1's second normalisation, which SPEC 3.1 requires of a reader.

    An absent collection and an empty one carry the same content, so the
    comparison treats them alike.
    """
    value = obj.get(key)
    if isinstance(value, (list, dict)) and not value:
        del obj[key]


# -- the difference ------------------------------------------------------


def tree_diff(got: Any, want: Any, path: str) -> str:
    """The first place two trees differ, in JSON Pointer form.

    A PATH rather than a boolean, because a failing corpus case has to say
    which rule broke to be actionable.
    """
    where = path or "the document"
    if isinstance(want, dict):
        if not isinstance(got, dict):
            return f"{where}: got {_describe(got)}, want an object"
        for key in sorted(want):
            if key not in got:
                return f"{where}: key {key!r} is missing"
            difference = tree_diff(got[key], want[key], f"{path}/{_escape(key)}")
            if difference:
                return difference
        extra = sorted(key for key in got if key not in want)
        if extra:
            return f"{where}: unexpected key(s) {extra}"
        return ""
    if isinstance(want, list):
        if not isinstance(got, list):
            return f"{where}: got {_describe(got)}, want an array"
        if len(got) != len(want):
            return f"{where}: got {len(got)} element(s), want {len(want)}"
        for index in range(len(want)):
            difference = tree_diff(got[index], want[index], f"{path}/{index}")
            if difference:
                return difference
        return ""
    if isinstance(want, float) and not isinstance(want, bool):
        # A number compares by VALUE.  -0 and 0 are the same value and compare
        # equal here, exactly as they do in Python.
        if isinstance(got, float) and not isinstance(got, bool) and got == want:
            return ""
        return f"{where}: got {_describe(got)}, want {want!r}"
    if got is not want and got != want:
        return f"{where}: got {_describe(got)}, want {_describe(want)}"
    if type(got) is not type(want):
        return f"{where}: got {_describe(got)}, want {_describe(want)}"
    return ""


def _escape(key: str) -> str:
    return key.replace("~", "~0").replace("/", "~1")


def _describe(value: Any) -> str:
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, str):
        return repr(value)
    if isinstance(value, dict):
        return "an object"
    if isinstance(value, list):
        return "an array"
    return repr(value)

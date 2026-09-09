"""Canonical form (SPEC 4.5): the byte layout every conforming writer emits.

The module works over a plain JSON value tree -- ``dict``, ``list``, ``str``,
``float``, ``bool``, ``None`` -- rather than over the VNX model, because
canonical form constrains bytes and says nothing about meaning.  ``encode.py``
builds a tree from a model and hands it here.

No stock JSON encoder in Python emits this layout (SPEC 4.5.6), so this is a
serialiser rather than a wrapper around one.
"""

from __future__ import annotations

from typing import Any

from .numbers import render_number

__all__ = ["render_document", "render_value", "JSONValue"]

JSONValue = Any  # dict | list | str | float | bool | None

INDENT = "  "

# RFC 8259 requires escaping only the quote, the reverse solidus, and the
# control characters below U+0020.  SPEC 4.5 does not name any further
# escaping, so none is applied: no HTML escaping, and non-ASCII is emitted as
# UTF-8 rather than as \u escapes.  See the report -- this is unspecified and
# no corpus file exercises it.
_ESCAPES = {
    '"': '\\"',
    "\\": "\\\\",
    "\b": "\\b",
    "\f": "\\f",
    "\n": "\\n",
    "\r": "\\r",
    "\t": "\\t",
}


def render_document(value: JSONValue) -> bytes:
    """Render a whole document: UTF-8, no BOM, newline at end (SPEC 4.5.5)."""
    return (render_value(value, 0) + "\n").encode("utf-8")


def render_value(value: JSONValue, depth: int) -> str:
    if value is None:
        return "null"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if isinstance(value, str):
        return render_string(value)
    if isinstance(value, (int, float)):
        return render_number(value)
    if isinstance(value, list):
        return _render_array(value, depth)
    if isinstance(value, dict):
        return _render_object(value, depth)
    raise TypeError(f"not a JSON value: {value!r}")


def render_string(value: str) -> str:
    out = ['"']
    for ch in value:
        escape = _ESCAPES.get(ch)
        if escape is not None:
            out.append(escape)
        elif ch < " ":
            out.append(f"\\u{ord(ch):04x}")
        else:
            out.append(ch)
    out.append('"')
    return "".join(out)


def is_scalar(value: JSONValue) -> bool:
    """SPEC 4.5.1: strings, numbers, booleans and nulls are scalars."""
    return not isinstance(value, (list, dict))


def _is_number_or_null(value: JSONValue) -> bool:
    if value is None:
        return True
    if isinstance(value, bool):
        return False
    return isinstance(value, (int, float))


def _render_array(items: list[JSONValue], depth: int) -> str:
    # SPEC 4.5.2: an array holding only numbers and null is written on one
    # line.  `null` counts, so a position does not change shape when an
    # elevation goes missing.  An empty array satisfies the rule vacuously and
    # is written `[]`; the specification does not say, see the report.
    if all(_is_number_or_null(item) for item in items):
        return "[" + ", ".join(render_value(item, depth) for item in items) + "]"

    inner = INDENT * (depth + 1)
    lines = [
        inner + render_value(item, depth + 1) for item in items
    ]
    return "[\n" + ",\n".join(lines) + "\n" + INDENT * depth + "]"


def _render_object(entries: dict[str, JSONValue], depth: int) -> str:
    if not entries:
        return "{}"
    scalars = sorted(k for k, v in entries.items() if is_scalar(v))
    containers = sorted(k for k, v in entries.items() if not is_scalar(v))
    inner = INDENT * (depth + 1)
    lines = [
        f"{inner}{render_string(key)}: {render_value(entries[key], depth + 1)}"
        for key in scalars + containers
    ]
    return "{\n" + ",\n".join(lines) + "\n" + INDENT * depth + "}"

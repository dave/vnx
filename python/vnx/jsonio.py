"""Parsing VNX's JSON, strictly (SPEC 4.2).

Python's ``json`` accepts ``NaN``, ``Infinity`` and ``-Infinity`` by default.
They are not JSON, and SPEC 4.2 forbids them throughout a document, so they are
turned into refusals here rather than reaching the model.

Numbers become ``float`` unconditionally.  SPEC 4.2 says numbers are IEEE 754
binary64, so a JSON integer literal is a binary64 too and rendering it as one
is what makes ``1778914991000.0`` and ``1778914991000`` the same document.
"""

from __future__ import annotations

import json
import math
from typing import Any

from .errors import VNXRefusal

__all__ = ["parse_json"]


def _refuse_constant(name: str) -> Any:
    raise VNXRefusal(
        f"malformed JSON: {name} is not a JSON value and is forbidden by VNX"
    )


def _parse_number(text: str) -> float:
    value = float(text)
    if math.isinf(value) or math.isnan(value):
        # A literal like 1e999 is well-formed JSON whose value is not finite in
        # binary64.  SPEC 4.2 forbids non-finite numbers throughout a document.
        raise VNXRefusal(f"non-finite number: {text} overflows binary64")
    return value


def parse_json(data: bytes) -> Any:
    """Parse *data* as UTF-8 JSON, refusing anything VNX forbids."""
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise VNXRefusal(f"malformed JSON: not valid UTF-8 ({exc})") from exc
    # A byte order mark is left to fail as malformed JSON.  RFC 8259 forbids
    # one at the start of a JSON text and SPEC 4.5.5 forbids one in canonical
    # form, but no VNX rule says a reader strips it -- and stripping would be a
    # fallback that hides a writer whose encoding is not what it claims.
    try:
        return json.loads(
            text,
            parse_int=_parse_number,
            parse_float=_parse_number,
            parse_constant=_refuse_constant,
        )
    except VNXRefusal:
        raise
    except json.JSONDecodeError as exc:
        raise VNXRefusal(f"malformed JSON: {exc}") from exc

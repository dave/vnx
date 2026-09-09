"""Resolving a corpus sidecar's ``positions[].at`` against the decoded model.

The pointer is written in the wire's own key names, and it MUST be resolved
against the model rather than against the raw JSON.  Resolving it against the
file compares the file to itself and cannot see a decoder that swapped two
channels, which is the entire point of the check.
"""

from __future__ import annotations

from dataclasses import fields, is_dataclass
from typing import Any

from vnx.pointer import parse

__all__ = ["resolve"]

# `from` and `to` are Python keywords on the wire but not in the model.
_RENAMED = {"from": "from_"}


def resolve(root: Any, pointer: str) -> Any:
    node = root
    for token in parse(pointer):
        node = _step(node, token, pointer)
    return node


def _step(node: Any, token: str, pointer: str) -> Any:
    if isinstance(node, list):
        return node[int(token)]
    if isinstance(node, dict):
        return node[token]
    if is_dataclass(node):
        name = _RENAMED.get(token, token)
        known = {f.name for f in fields(node)}
        if name not in known:
            raise KeyError(f"{pointer}: {type(node).__name__} has no {token!r}")
        return getattr(node, name)
    raise KeyError(f"{pointer}: cannot step {token!r} through {type(node).__name__}")

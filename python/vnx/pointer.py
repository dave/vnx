"""JSON Pointer (RFC 6901) construction and resolution.

Used for diagnostic paths (SPEC 6.3) and for resolving a corpus sidecar's
``positions[].at`` against the decoded model.
"""

from __future__ import annotations

__all__ = ["join", "escape", "parse"]


def escape(token: str) -> str:
    return token.replace("~", "~0").replace("/", "~1")


def unescape(token: str) -> str:
    return token.replace("~1", "/").replace("~0", "~")


def join(prefix: str, *tokens: object) -> str:
    """Extend a pointer by one or more reference tokens."""
    out = prefix
    for token in tokens:
        out += "/" + escape(str(token))
    return out


def parse(pointer: str) -> list[str]:
    """Split a pointer into its unescaped reference tokens."""
    if pointer == "":
        return []
    if not pointer.startswith("/"):
        raise ValueError(f"not a JSON Pointer: {pointer!r}")
    return [unescape(token) for token in pointer.split("/")[1:]]

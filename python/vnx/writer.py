"""The writer emit path (SPEC 4.1).

A writer emits a model as a document and applies the emit-boundary rules on the
way.  Two rules reach here:

* **The shortest spelling of every position.**  That one is ``encode.py``'s,
  because it is a property of the tree: only TRAILING absences go, and
  ``[lon, lat, null, time]`` keeps its interior ``null``.
* **``hidden`` is written only when it is true** (SPEC 3.8).  Absence already
  means visible, so an explicit ``false`` carries nothing.  That one is here,
  because it is a property of the model.

Precision is NOT an emit-boundary rule.  SPEC 4.1 does not constrain how
precisely a writer states a coordinate: a value passes through this path
unchanged, and ``corpus/write/emit-boundary`` asserts exactly that.  Rounding to
six places is a size choice a producer may make somewhere this path does not
reach, and a reader MUST NOT round at all.

The ``hidden`` normalisation builds a NEW model and encodes that; the caller's
document is never touched.
"""

from __future__ import annotations

import json
from dataclasses import replace
from typing import Any, Union

from .encode import encode_document
from .model import Document, Network

__all__ = ["encode_bytes", "normalised_document"]


def encode_bytes(document: Document) -> bytes:
    """Emit *document*: shortest-spelled positions, compact JSON.

    ``separators`` makes it compact.  Whitespace and key order are the writer's
    own business (SPEC 8.1), and the smallest spelling is the useful one.
    ``allow_nan`` is off because ``NaN`` and ``Infinity`` are not JSON and
    SPEC 4.2 forbids them throughout a document.
    """
    tree = encode_document(normalised_document(document))
    text = json.dumps(tree, separators=(",", ":"), ensure_ascii=False, allow_nan=False)
    return text.encode("utf-8")


def _unhidden(item: Any) -> Any:
    """A copy of *item* with a ``hidden`` of ``false`` dropped (SPEC 3.8)."""
    if getattr(item, "hidden", None) is False:
        return replace(item, hidden=None)
    return item


def _unhidden_all(items: Union[list[Any], None]) -> Union[list[Any], None]:
    if items is None:
        return None
    return [_unhidden(item) for item in items]


def normalised_document(document: Document) -> Document:
    """A copy of *document* with the model-side emit rules applied.

    Today that is SPEC 3.8's ``hidden`` only.  The original is not modified.
    Everything untouched -- positions, data records, styles, application keys -- is
    shared rather than copied, because nothing here mutates it.
    """
    return replace(
        document,
        folders=_unhidden_all(document.folders),
        networks=_networks(document.networks),
        plans=_unhidden_all(document.plans),
        tracks=_unhidden_all(document.tracks),
        waypoints=_unhidden_all(document.waypoints),
        areas=_unhidden_all(document.areas),
    )


def _networks(items: Union[list[Network], None]) -> Union[list[Network], None]:
    if items is None:
        return None
    return [
        _unhidden(
            replace(
                network,
                highlights=_unhidden_all(network.highlights),
                points=_unhidden_all(network.points),
                areas=_unhidden_all(network.areas),
            )
        )
        for network in items
    ]

"""Refusals and diagnostics (SPEC 6.2, 6.3).

A refusal and a diagnostic are different channels on purpose.  SPEC 6.3: "A
refusal is not a diagnostic, and a reader MUST report it through a separate
channel, so that a caller can distinguish a document that was refused from one
that was read with elements dropped."  Here a refusal is an exception and a
diagnostic is a value.
"""

from __future__ import annotations

from dataclasses import dataclass

__all__ = ["VNXRefusal", "Diagnostic"]


class VNXRefusal(Exception):
    """The document or container was refused.

    The message names the cause.  SPEC 8.1 matches a refusal as a substring, so
    the wording is this implementation's business but the cause is not.
    """


@dataclass(frozen=True, slots=True)
class Diagnostic:
    """One thing the reader could not use as written (SPEC 6.3).

    ``path`` is a JSON Pointer (RFC 6901) into the document as it was written,
    not into the model after elements were dropped.  ``message`` is short
    English.  There is deliberately no severity: every diagnostic this format
    defines reports the same kind of fact.
    """

    path: str
    message: str

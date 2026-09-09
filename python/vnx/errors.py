"""Refusals and diagnostics (SPEC 6.2, 6.3).

A refusal and a diagnostic are different channels on purpose.  SPEC 6.3: "A
refusal is not a diagnostic, and a reader MUST report it through a separate
channel, so that a caller can distinguish a document that was refused from one
that was read with elements dropped."  Here a refusal is an exception and a
diagnostic is a value.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Union

__all__ = ["VNXRefusal", "Diagnostic"]


class VNXRefusal(Exception):
    """The document or container was refused.

    The message names the cause.  SPEC 8.1 matches a refusal as a substring of
    the reader's English, so the wording is *not* this implementation's
    business today, whatever SPEC 8.1 says: five implementations have to emit
    the same English or fail the corpus.  Dave ruled on 2026-09-09 that
    ``reject/`` sidecars gain a stable reason code and the English becomes
    advisory; until that lands, these six substrings are load-bearing and are
    the only reason the corpus passes.

    ==========================  ===========================  ================
    Substring the corpus wants  Where this library emits it  Cause
    ==========================  ===========================  ================
    ``malformed JSON``          ``jsonio.parse_json``        not JSON at all,
                                                             incl. NaN and
                                                             Infinity literals
    ``non-finite``              ``jsonio._parse_number``     a literal that
                                                             overflows
                                                             binary64 (no
                                                             corpus case)
    ``not a VNX document``      ``decode._Reader.document``  no ``vnx`` key,
                                                             or a top level
                                                             that is not an
                                                             object
    ``MAJOR.MINOR.PATCH``       ``decode._check_version``    a version that is
                                                             not three digit
                                                             runs
    ``a newer reader is        ``decode._check_version``     MAJOR or MINOR
    needed``                                                 above this
                                                             reader's
    ``duplicate entry``         ``container``                two entries of
                                                             one name
    ``escapes the archive``     ``container._check_path``    ``..``, an
                                                             absolute path, or
                                                             a drive letter
    ==========================  ===========================  ================
    """


@dataclass(frozen=True, slots=True)
class Diagnostic:
    """One thing the reader could not use as written (SPEC 6.3).

    ``path`` is a JSON Pointer (RFC 6901) into the document as it was written,
    not into the model after elements were dropped.  ``message`` is short
    English.  There is deliberately no severity: every diagnostic this format
    defines reports the same kind of fact.

    ``code`` is a short stable token naming WHICH diagnostic this is, and it is
    the only thing a conformance case compares (SPEC 6.3).  The English is never
    compared across implementations -- a corpus that matched substrings of prose
    would pin translations of a rule rather than the rule.  A reader MAY leave
    the code absent on any diagnostic no corpus case pins; codes are wire
    commitments and the format does not need one for every way a document can be
    malformed.
    """

    path: str
    message: str
    code: Union[str, None] = None

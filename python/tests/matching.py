"""Pairing reported diagnostics with expected ones (SPEC 6.3).

Emission order is not significant, so the comparison is a set comparison.  A set
comparison is exactly the shape that decays into "anything passes" with nothing
going red -- every case in ``corpus/diagnose/`` expects at most one diagnostic,
so a matcher that checked nothing would keep the whole tier green.  Two
properties therefore have to hold and are tested directly in
``test_matching.py``:

* every expected diagnostic pairs with a *distinct* reported one, so one report
  cannot satisfy two expectations;
* the counts are equal, so an extra report is a failure and not a bonus.
"""

from __future__ import annotations

from collections.abc import Sequence
from typing import Union

from vnx.errors import Diagnostic

__all__ = ["match_diagnostics"]


def _satisfies(expected: dict[str, str], reported: Diagnostic) -> bool:
    path, message = reported.path, reported.message
    if "path" in expected and expected["path"] != path:
        return False
    # SPEC 8.1: a message is matched as a substring.  The exact wording of a
    # diagnostic is an implementation's own business; naming the cause is not.
    if "message" in expected and expected["message"] not in message:
        return False
    return True


def match_diagnostics(
    expected: list[dict[str, str]], reported: Sequence[Diagnostic]
) -> Union[str, None]:
    """Return None when the two agree, else a description of how they do not."""
    if len(expected) != len(reported):
        return (
            f"expected {len(expected)} diagnostic(s), got {len(reported)}: "
            f"{[(d.path, d.message) for d in reported]}"
        )

    candidates = [
        [j for j, report in enumerate(reported) if _satisfies(want, report)]
        for want in expected
    ]
    for index, options in enumerate(candidates):
        if not options:
            return f"no reported diagnostic satisfies {expected[index]!r}"

    # Maximum bipartite matching by augmenting paths.  A perfect matching is
    # what makes each expectation cost one distinct report.
    assigned: dict[int, int] = {}

    def augment(want: int, seen: set[int]) -> bool:
        for report in candidates[want]:
            if report in seen:
                continue
            seen.add(report)
            if report not in assigned or augment(assigned[report], seen):
                assigned[report] = want
                return True
        return False

    for want in range(len(expected)):
        if not augment(want, set()):
            return (
                f"expectations cannot be paired one-to-one with reports; "
                f"{expected[want]!r} has no report left of its own"
            )
    return None

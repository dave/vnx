"""Pairing reported diagnostics with expected ones (SPEC 6.3).

Emission order is not significant, so the comparison is a set comparison.  A set
comparison is exactly the shape that decays into "anything passes" with nothing
going red -- most cases in ``corpus/diagnose/`` expect one diagnostic, whose
path alone identifies it, so a matcher that compared only paths, or let one
report stand for two expectations, would keep the whole tier green.  Two
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


# SPEC 6.3: `message_advisory` exists so a person reading a sidecar can see what
# the case is about, and it is NEVER compared -- a corpus that matched substrings
# of prose across implementations would pin translations of a rule rather than
# the rule.  The field is named advisory precisely so that nothing compares it.
UNCOMPARED_DIAGNOSTIC_FIELDS = frozenset({"message_advisory"})
COMPARED_DIAGNOSTIC_FIELDS = frozenset({"path", "code"})


def _satisfies(expected: dict[str, str], reported: Diagnostic) -> bool:
    if "path" in expected and expected["path"] != reported.path:
        return False
    if "code" in expected and expected["code"] != reported.code:
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

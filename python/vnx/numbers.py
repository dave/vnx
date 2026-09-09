"""ECMAScript ``Number::toString`` rendering (SPEC 4.5.3).

Python's own ``repr`` disagrees with the required rendering on both exponent
boundaries -- ``1e-06`` where VNX needs ``0.000001``, ``1e-07`` where VNX needs
``1e-7`` -- so a VNX writer in Python renders numbers itself.  SPEC 4.5.6 says
as much.

``repr`` is still the right starting point: it produces the shortest digit
string that round-trips to the same binary64, which is exactly the ``k``
digits ECMAScript asks for.  Only the *placement* of the decimal point differs,
and that is what this module reimplements.
"""

from __future__ import annotations

import math
import re

__all__ = ["render_number", "NonFiniteNumberError"]


class NonFiniteNumberError(ValueError):
    """A non-finite number reached the serialiser (SPEC 4.2)."""


_REPR = re.compile(r"\A(\d+)(?:\.(\d+))?(?:[eE]([+-]?\d+))?\Z")


def render_number(value: float) -> str:
    """Render *value* as ECMAScript ``Number::toString`` would.

    Negative zero renders as ``0`` (SPEC 4.5.3): ``-0`` and ``0`` are the same
    value and two writers disagreeing about the sign is a byte incompatibility
    carrying no information.
    """
    x = float(value)
    if math.isnan(x) or math.isinf(x):
        raise NonFiniteNumberError(f"non-finite number: {x!r}")
    if x == 0.0:
        return "0"
    if x < 0.0:
        return "-" + render_number(-x)

    digits, exponent = _shortest_digits(x)
    k = len(digits)
    n = k + exponent

    if k <= n <= 21:
        return digits + "0" * (n - k)
    if 0 < n <= 21:
        return digits[:n] + "." + digits[n:]
    if -6 < n <= 0:
        return "0." + "0" * -n + digits
    e = n - 1
    sign = "+" if e >= 0 else "-"
    mantissa = digits if k == 1 else digits[0] + "." + digits[1:]
    return f"{mantissa}e{sign}{abs(e)}"


def _shortest_digits(x: float) -> tuple[str, int]:
    """Return ``(digits, exponent)`` with ``int(digits) * 10**exponent == x``.

    ``digits`` carries no leading or trailing zero, so its length is the ``k``
    of the ECMAScript algorithm and ``len(digits) + exponent`` is its ``n``.
    """
    match = _REPR.match(repr(x))
    if match is None:  # pragma: no cover - repr of a positive finite float
        raise ValueError(f"cannot read the repr of {x!r}")
    whole, frac, exp = match.group(1), match.group(2) or "", match.group(3)
    digits = whole + frac
    exponent = (int(exp) if exp is not None else 0) - len(frac)

    stripped = digits.lstrip("0")
    digits = stripped if stripped else "0"
    trimmed = digits.rstrip("0")
    exponent += len(digits) - len(trimmed)
    return trimmed, exponent

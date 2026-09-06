# Formatter cases

Each `X.in.vnx` is a deliberately messy document; `X.canonical.vnx` is exactly
what a conforming formatter must emit for it. The runner canonicalises the input
and byte-compares.

**These exist because the `accept/` files cannot catch a formatter bug.** Those
are stored canonical, so canonicalising one is a no-op over values that are
already correctly spelled — a renderer that gets `-0` wrong, or writes `1e-06`
for `0.000001`, is never asked the question, because no accept file contains the
input form that would trigger it. Found 2026-09-01, when a negative-zero bug in
the reference implementation turned out to be invisible to its own corpus.

So the inputs here are written in the WRONG spellings on purpose: keys out of
order, positions spread over several lines, `340.0`, `1e-06`, `1E21`, `-0.0`,
and a double spelled with the longer of its two equivalent decimal forms. If a
formatter passes this file, its layout agrees with every other implementation's.

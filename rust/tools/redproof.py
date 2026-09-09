#!/usr/bin/env python3
"""Red-proof the conformance runners (corpus, numbers, zip64).

Break the reader, one defect at a time, and require the runner to go RED. A
tier that stays green under a mutation is a tier that was not testing what its
name says — and every defect below is one a plausible implementation would
actually have.

    cd rust && python3 tools/redproof.py

It edits files in place and restores them from a snapshot after each mutation,
so it must be run on a clean tree. It needs no dependencies beyond cargo and
Python 3.

Measured 2026-09-09, all sixteen caught:

    transpose lon and lat                accept, format
    normalise [lon, lat, null]           accept
    discard every data record            accept, format
    discard rulesets                     accept
    discard every style object           accept, format
    discard every notes field            accept, format
    answer 0 for an unknown moving time  accept
    skip the chain continuity check      diagnose
    skip the unrecognised-key rule       diagnose
    refuse on a newer PATCH too          accept
    resolve a tie away from zero         format
    escape < > and & as Go does          format
    render a position over several lines accept, format
    sort keys plain lexicographically    accept, format
    round coordinates while decoding     accept, format
    keep only a nested polygon's ring    diagnose
    resolve a rendering tie away         numbers (render, round6, round1)
    scale before rounding                numbers (round1, round6)
    round half away from zero            numbers (round1, round6)
    compare a diagnostic's path only     the runner's own self-test
    ignore a stale `message` sidecar     the runner's own self-test
    skip the ZIP64 extra-field parser    zip64 (no corpus fixture reaches it)
    ignore the ZIP64 end record          zip64
"""
import io, os, shutil, subprocess, sys, tempfile

ROOT = "/Users/dave/src/vnx/rust"
FILES = ["src/model.rs", "src/decode.rs", "src/json/mod.rs", "src/json/number.rs",
         "src/render.rs", "src/canonical.rs", "src/zip/mod.rs", "tests/corpus.rs"]

MUTATIONS = [
    ("transpose lon and lat everywhere", "src/model.rs",
     "        Position { channels }",
     "        let mut channels = channels; channels.swap(0, 1); Position { channels }"),

    ("normalise [lon, lat, null] to [lon, lat]", "src/decode.rs",
     "        Some(Position::new(channels))",
     "        while channels.len() > 2 && channels[channels.len()-1].is_none() { channels.pop(); }\n        Some(Position::new(channels))"),

    ("discard every data record", "src/decode.rs",
     "        let items = self.opt_array(entries, \"data\", path)?;",
     "        let items = self.opt_array(entries, \"data\", path)?; if true { return None; }"),

    ("discard rulesets", "src/decode.rs",
     "            document.rulesets = Some(Json::Array(items.to_vec()));",
     "            let _ = items;"),

    ("discard every style object", "src/decode.rs",
     "        let members = self.opt_object(entries, \"style\", path)?;",
     "        let members = self.opt_object(entries, \"style\", path)?; if true { return None; }"),

    # Applied to EVERY host, not just the first: only some hosts carry `notes`
    # in the fixtures, and a mutation that misses them all proves nothing.
    ("discard every notes field", "src/decode.rs",
     "            notes: self.opt_string(entries, \"notes\", path),",
     "            notes: None,", "all"),

    ("answer 0 for an unknown moving time", "src/model.rs",
     "        any.then_some(total)",
     "        let _ = any; Some(total)"),

    ("skip the chain continuity check", "src/decode.rs",
     "        self.check_continuity(&slots, &links_path, network);",
     "        let _ = &links_path;"),

    ("skip the unrecognised-key rule", "src/decode.rs",
     "                self.report(&path.key(key), Cause::UnknownKey);",
     "                let _ = key;"),

    ("refuse on a newer PATCH too", "src/decode.rs",
     "    if (version.major, version.minor) > SUPPORTED {",
     "    if (version.major, version.minor, version.patch) > (SUPPORTED.0, SUPPORTED.1, 0) {"),

    ("resolve a tie away from zero, as Rust does", "src/json/number.rs",
     "    if last_is_odd {",
     "    if false && last_is_odd {"),

    ("escape < > and & as Go does by default", "src/json/mod.rs",
     "            c if (c as u32) < 0x20 => {",
     "            '<' | '>' | '&' => { out.push_str(\"\\\\u00\"); out.push_str(&format!(\"{:02x}\", ch as u32)); }\n            c if (c as u32) < 0x20 => {"),

    ("render a position over several lines", "src/canonical.rs",
     "    if inline_worthy(items) {",
     "    if false && inline_worthy(items) {"),

    ("sort keys plain lexicographically", "src/canonical.rs",
     "        a_container.cmp(&b_container).then_with(|| a.0.cmp(&b.0))",
     "        a.0.cmp(&b.0)"),

    ("round coordinates while decoding", "src/decode.rs",
     "                Json::Number(n) => channels.push(Some(*n)),",
     "                Json::Number(n) => channels.push(Some((*n * 1e6).round() / 1e6)),"),

    # The number-conformance table is the only instrument that reaches these
    # three: SPEC 4.5.3's rendering tie and SPEC 4.5.4's rounding tie decide
    # bytes, and no corpus tier ever asks a library to write.
    ("scale before rounding, as the obvious spelling does", "src/render.rs",
     "    format!(\"{value:.places$}\")\n        .parse()\n        .expect(\"a fixed-precision formatting of a finite double parses back\")",
     "    let s = 10f64.powi(places as i32); (value * s).round_ties_even() / s"),

    ("round half away from zero at the writer boundary", "src/render.rs",
     "    format!(\"{value:.places$}\")\n        .parse()\n        .expect(\"a fixed-precision formatting of a finite double parses back\")",
     "    let s = 10f64.powi(places as i32); (value * s).round() / s"),

    # This one is caught by the runner's own SELF-TEST, not by any corpus
    # fixture: every diagnose fixture carries at most one diagnostic and its
    # path identifies it, so no case can separate a matcher that compares the
    # code from one that ignores it.
    ("compare a diagnostic's path and ignore its code", "tests/corpus.rs",
     ".position(|&i| reported[i].path == want.path && reported[i].code() == want.code);",
     ".position(|&i| reported[i].path == want.path);"),

    # Same shape: the sidecar-shape refusals have no fixture behind them.
    ("ignore a sidecar's stale `message` field", "tests/corpus.rs",
     '        if want.get("message").is_some() {',
     '        if false && want.get("message").is_some() {'),

    # SPEC 7 requires a reader to accept ZIP64 and the corpus has no ZIP64
    # fixture, so `tests/zip64.rs` is the only thing that reaches these two.
    ("skip the ZIP64 extra-field parser", "src/zip/mod.rs",
     "        if id == 0x0001 {",
     "        if false && id == 0x0001 {"),

    ("ignore the ZIP64 end of central directory record", "src/zip/mod.rs",
     "        if count == 0xffff || cd_offset == 0xffff_ffff || cd_size == 0xffff_ffff {",
     "        if false {"),

    ("keep only the outer ring of a nested polygon", "src/decode.rs",
     "            self.report(path, Cause::PolygonNestedRings);\n            return None;",
     "            self.report(path, Cause::PolygonNestedRings);\n            return items.first()?.as_array().map(|r| r.iter().enumerate().filter_map(|(i, c)| self.position(c, &path.index(i))).collect());"),
]


def snapshot():
    tmp = tempfile.mkdtemp(prefix="vnx-redproof-")
    for rel in FILES:
        dst = os.path.join(tmp, rel.replace("/", "__"))
        shutil.copyfile(os.path.join(ROOT, rel), dst)
    return tmp


def restore(tmp):
    for rel in FILES:
        shutil.copyfile(os.path.join(tmp, rel.replace("/", "__")), os.path.join(ROOT, rel))


def run_corpus():
    # `--no-fail-fast`, or cargo stops after the first failing test BINARY and
    # a mutation that reddens both tiers is reported as reddening only one.
    out = subprocess.run(["cargo", "test", "--no-fail-fast", "--test", "corpus",
                          "--test", "numbers", "--test", "zip64"],
                         cwd=ROOT, capture_output=True, text=True)
    text = out.stdout + out.stderr
    if "test result: ok." in text and "FAILED" not in text:
        return "GREEN", text
    if "test result: FAILED" in text:
        # The names cargo lists under `failures:`, which are indented four
        # spaces and carry no other whitespace. Taking them by shape rather than
        # by a keyword, because a filter written from the failures you EXPECT
        # cannot show the one you did not.
        failed = sorted({l.strip() for l in text.splitlines()
                         if l.startswith("    ") and l.strip()
                         and " " not in l.strip() and "::" not in l.strip()
                         and l.strip().replace("_", "").isalnum()
                         and l.strip() not in ("null", "true", "false")})
        return "red", ", ".join(failed) or "?"
    return "DID NOT BUILD", text[-400:]


def main():
    backup = snapshot()
    baseline, detail = run_corpus()
    if baseline != "GREEN":
        print("baseline is not green; aborting\n", detail)
        return 1
    print("baseline: GREEN\n")
    survivors = []
    for name, rel, old, new, *rest in MUTATIONS:
        count = -1 if rest and rest[0] == "all" else 1
        path = os.path.join(ROOT, rel)
        s = io.open(path, encoding="utf-8").read()
        if old not in s:
            print(f"  SKIPPED (pattern gone): {name}")
            survivors.append(name + " [pattern not found]")
            continue
        io.open(path, "w", encoding="utf-8").write(
            s.replace(old, new) if count == -1 else s.replace(old, new, 1))
        verdict, detail = run_corpus()
        restore(backup)
        mark = "red  " if verdict == "red" else verdict
        print(f"  {mark:14} {name}" + (f"   ({detail})" if verdict == "red" else ""))
        if verdict != "red":
            survivors.append(f"{name} -> {verdict}")
            if verdict == "DID NOT BUILD":
                print("      " + detail.replace("\n", "\n      ")[:600])
    print()
    if survivors:
        print("SURVIVED (the runner did not notice):")
        for s in survivors:
            print("  -", s)
        return 1
    print("every mutation was caught.")
    return 0


sys.exit(main())

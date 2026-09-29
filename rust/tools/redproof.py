#!/usr/bin/env python3
"""Red-proof the conformance runners (corpus, zip64, hostile).

Break the reader, one defect at a time, and require the runner to go RED. A
tier that stays green under a mutation is a tier that was not testing what its
name says — and every defect below is one a plausible implementation would
actually have.

    cd rust && python3 tools/redproof.py

It edits files in place and restores them from a snapshot after each mutation,
so it must be run on a clean tree. It needs no dependencies beyond cargo and
Python 3.

Measured 2026-09-29, when format 0.2 replaced a highlight's `links` with a `walk`.
The 50 m tolerance, the terminal-pip check and the highlight continuity check went
with it, and so did their mutations; continuity is now valid-walk rule 4, and
the walk mutations below hold it. `take a stored entry's compressed size` went
too: `6d58c62` removed the check it broke, so its pattern no longer exists.

    transpose lon and lat                accept, diagnose, write, unit
    normalise [lon, lat, null]           GREEN, expected (SPEC 8.1 permits it)
    discard data / app keys / style / notes   accept
    answer 0 for an unknown moving time  accept, unit
    skip the unrecognised-key rule       diagnose, unit
    refuse on a newer PATCH too          accept, write, unit
    compare the version series with `>`  reject (version-too-old), unit
    round coordinates while decoding     accept, write, unit
    compare a diagnostic's path only     the runner's own self-test
    ignore a stale `message` sidecar     the runner's own self-test
    skip the ZIP64 parser / end record   zip64, accept
    reach inward to the TIMED pips       accept
    keep the good pips, drop one         diagnose, unit
    conflate the two wrong-type codes    diagnose, unit
    the hostile-archive mutations        hostile
    skip the CRC check                   hostile, diagnose
    make the tree comparison blind       write, the runner's own self-test
    write a false `hidden`               write, unit
    emit attachments in reverse order    GREEN, expected (SPEC 7.1 leaves it)
    the attachment mutations             accept, diagnose, write, unit
    keep only a nested polygon's ring    diagnose
    walk: pass not entered where the last left      diagnose, unit
    walk: `from` node not an end of its arc         unit only
    walk: a pass before the last leaves at a position   unit only
    walk: ignore a turn when finding the exit       accept (walk-forms), unit
    walk: `from` on a pass after the first          diagnose, unit
    walk: skip pinned-turn / the route rule         diagnose, unit
    walk: drop the bad pass, keep the highlight         diagnose, unit
    skip extra-channel (walk, node) / arc-pip-time  diagnose, unit
    report arc-pip-time on every arc                accept, diagnose, write
    keep a self-loop / an arc naming an absent node diagnose, unit
    keep a ring's repeat / a ring under 3 points    diagnose, unit
    count distinct ring positions by every channel  unit only
    write a false `pinned`               accept, unit
    write without the arc/walk/ring checks   unit only (no tier writes a model
                                         the reader did not produce)

A mutation may carry "expect-green", meaning it documents a hole in the CORPUS
rather than in this crate and fails the script if it ever goes red. None does
today: the one that did — reaching inward to a segment's first and last TIMED
pip — was promoted on 2026-09-10 when `8979621` fixed the fixture behind it.
"""

import atexit, datetime, io, os, shutil, subprocess, sys, tempfile

# Derived from this script's own location, NOT hardcoded. A hardcoded path
# means "run it in a detached scratch worktree" — the practice this script's
# own hazard notes recommend — silently mutates the shared tree instead, which
# is the opposite of what the reader was trying to do.
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FILES = [
    "src/model.rs",
    "src/decode.rs",
    "src/json/mod.rs",
    "src/encode.rs",
    "src/zip/mod.rs",
    "src/walk.rs",
    "src/diag.rs",
    "src/vnz.rs",
    "tests/corpus.rs",
    "tests/support/tree.rs",
]

MUTATIONS = [
    (
        "transpose lon and lat everywhere",
        "src/model.rs",
        "        Position { channels }",
        "        let mut channels = channels; channels.swap(0, 1); Position { channels }",
    ),
    # EXPECTED SURVIVOR since 2026-09-10, and it is the spec that changed rather
    # than a guard that weakened. SPEC 4.1 says `[lon, lat, null]` and
    # `[lon, lat]` state the same thing and a reader takes either, and SPEC 8.1
    # then makes "trailing `null` channels dropped" one of the two permitted
    # normalisations. A reader that shortens on the way IN is therefore
    # indistinguishable at every boundary the format defines. The accept tier
    # caught this while it compared bytes; nothing can now, and nothing should.
    (
        "normalise [lon, lat, null] to [lon, lat]",
        "src/decode.rs",
        "    Ok(Position::new(channels))",
        "    while channels.len() > 2 && channels[channels.len()-1].is_none() { channels.pop(); }\n    Ok(Position::new(channels))",
        "expect-green",
    ),
    (
        "discard every data record",
        "src/decode.rs",
        '        let items = self.opt_array(entries, "data", path)?;',
        '        let items = self.opt_array(entries, "data", path)?; if true { return None; }',
    ),
    (
        "discard every application key",
        "src/decode.rs",
        "        document.app_keys = app_keys;",
        "        let _ = app_keys;",
    ),
    (
        "discard every style object",
        "src/decode.rs",
        '        let members = self.opt_object(entries, "style", path)?;',
        '        let members = self.opt_object(entries, "style", path)?; if true { return None; }',
    ),
    # Applied to EVERY host, not just the first: only some hosts carry `notes`
    # in the fixtures, and a mutation that misses them all proves nothing.
    (
        "discard every notes field",
        "src/decode.rs",
        '            notes: self.opt_string(entries, "notes", path),',
        "            notes: None,",
        "all",
    ),
    (
        "answer 0 for an unknown moving time",
        "src/model.rs",
        "            total += last.time_ms()? - first.time_ms()?;",
        "            total += last.time_ms().unwrap_or(0.0) - first.time_ms().unwrap_or(0.0);",
    ),
    (
        "skip the unrecognised-key rule",
        "src/decode.rs",
        "                self.report(&path.key(key), Cause::UnknownKey);",
        "                let _ = key;",
    ),
    (
        "refuse on a newer PATCH too",
        "src/decode.rs",
        "    if (version.major, version.minor) != SUPPORTED {",
        "    if (version.major, version.minor) != SUPPORTED || version.patch > 0 {",
    ),
    # SPEC 2.2 through 0.x: each MINOR is its own series, so the test is "not
    # this reader's series", not "above it". `>` is the shape five of the six
    # implementations had, and it passes reject/version-too-new while READING
    # a 0.0 document — silently, under rules that were never written for it.
    # Only reject/version-too-old.vnx reaches this.
    (
        "compare the version series with `>` rather than `!=`",
        "src/decode.rs",
        "    if (version.major, version.minor) != SUPPORTED {",
        "    if (version.major, version.minor) > SUPPORTED {",
    ),
    (
        "round coordinates while decoding",
        "src/decode.rs",
        "            Json::Number(n) => channels.push(Some(*n)),",
        "            Json::Number(n) => channels.push(Some((*n * 1e6).round() / 1e6)),",
    ),
    # This one is caught by the runner's own SELF-TEST, not by any corpus
    # fixture: every diagnose fixture carries at most one diagnostic and its
    # path identifies it, so no case can separate a matcher that compares the
    # code from one that ignores it.
    (
        "compare a diagnostic's path and ignore its code",
        "tests/corpus.rs",
        ".position(|&i| reported[i].path == want.path && reported[i].code() == want.code);",
        ".position(|&i| reported[i].path == want.path);",
    ),
    # Same shape: the sidecar-shape refusals have no fixture behind them.
    (
        "ignore a sidecar's stale `message` field",
        "tests/corpus.rs",
        '        if want.get("message").is_some() {',
        '        if false && want.get("message").is_some() {',
    ),
    # SPEC 7 requires a reader to accept ZIP64 and the corpus has no ZIP64
    # fixture, so `tests/zip64.rs` is the only thing that reaches these two.
    (
        "skip the ZIP64 extra-field parser",
        "src/zip/mod.rs",
        "        if id == 0x0001 {",
        "        if false && id == 0x0001 {",
    ),
    (
        "ignore the ZIP64 end of central directory record",
        "src/zip/mod.rs",
        "        if count == 0xffff || cd_offset == 0xffff_ffff || cd_size == 0xffff_ffff {",
        "        if false {",
    ),
    # `accept/track-partly-timed.vnx` separates "read a segment's END pips" from
    # "read its first and last TIMED pip". This was an expected-SURVIVOR from
    # 2026-09-09 to 2026-09-10, because the fixture's untimed segment carried no
    # times at all and both readings returned unknown. `8979621` gave it an
    # untimed first pip, a TIMED middle and an untimed last, and this mutation
    # went red — which is how the promotion was noticed rather than remembered.
    #
    # Deliberately NARROW: it changes only WHICH pips are read and keeps the
    # rule that a segment with no timed pip makes the whole track unknown. A
    # first attempt also skipped wholly-untimed segments, which is a second
    # defect, and it went red for the skipping rather than for the reaching
    # inward. A mutation broader than the rule proves the corpus catches
    # something, not that it catches this.
    (
        "reach inward to a segment's first and last TIMED pip",
        "src/model.rs",
        "            total += last.time_ms()? - first.time_ms()?;",
        "            let timed: Vec<f64> = segment.pips.iter().filter_map(|p| p.time_ms()).collect();\n"
        "            if timed.is_empty() { return None; }\n"
        "            total += timed[timed.len() - 1] - timed[0];\n"
        "            let _ = (first, last);",
    ),
    # SPEC 6.2: "GEOMETRY IS CONTINUOUS AND STRUCTURE IS NOT." Keeping the good
    # pips leaves a shorter, plausible, wrong line with nothing to indicate it.
    # This crate shipped that reading until 2026-09-10.
    (
        "keep the good pips and drop only the unusable one",
        "src/decode.rs",
        "            out.push(self.position(item, &path.index(i), dropped)?);",
        "            if let Some(p) = self.position(item, &path.index(i), dropped) { out.push(p); }",
    ),
    (
        "conflate the optional and required wrong-type codes",
        "src/diag.rs",
        '            | Cause::RecordTypeMissing => "wrong-type-required",',
        '            | Cause::RecordTypeMissing => "wrong-type-optional",',
    ),
    # Hostile input, which no corpus tier reaches: every fixture is well-formed,
    # including the ones that must be refused. A panic is not a refusal.
    (
        "add without checking, so a ZIP64 offset overflows a usize",
        "src/zip/mod.rs",
        '    let end = at\n        .checked_add(len)\n        .ok_or_else(|| ZipError("an offset in the archive is out of range".into()))?;',
        "    let end = at + len;",
    ),
    (
        "trust a declared uncompressed size as a bound",
        "src/zip/mod.rs",
        "        .take(ceiling)",
        "        .take(u64::MAX)",
    ),
    # A CRC is a claim the archive makes about its own bytes. Without the check,
    # a corrupt ATTACHMENT is returned as content — the document keeps parsing,
    # so nothing else notices.
    (
        "skip the CRC check",
        "src/zip/mod.rs",
        "        if crc != entry.crc32 {",
        "        if false && crc != entry.crc32 {",
    ),
    # SPEC 8.1: the accept tier compares against the ORIGINAL FILE. A
    # comparison that sees nothing passes every reader, including one that threw
    # away every data record, ruleset, style and `notes`.
    (
        "make the tree comparison blind",
        "tests/support/tree.rs",
        "fn diff(got: &Json, want: &Json, path: &str) -> Option<String> {",
        "fn diff(got: &Json, want: &Json, path: &str) -> Option<String> {\n    if true { return None; }",
    ),
    # SPEC 3.8: a writer emits `hidden` only when it is true.
    (
        "write a false `hidden`",
        "src/encode.rs",
        "        if value == Some(true) {",
        "        if let Some(true | false) = value {",
    ),
    # ⚠ THE PAIR BELOW IS ONE INSTRUMENT AND MUST BE READ TOGETHER. A test that
    # fails on REORDERING but not on LOSS is asserting an order SPEC 7.1
    # explicitly leaves to the writer. Running only the loss direction cannot
    # see it, because such a test is already red there — for the right reason,
    # which is what makes it invisible. Kotlin found one this way on
    # 2026-09-10: reorder reddened one test, loss reddened nine.
    (
        "emit the attachments in descending name order",
        "src/vnz.rs",
        "    names[1..].sort_by(|a, b| a.path.cmp(&b.path));",
        "    names[1..].sort_by(|a, b| b.path.cmp(&a.path));",
        "expect-green",
    ),
    # The corpus pins attachment NAMES and CONTENTS separately, and names alone
    # pass a reader that returns every attachment empty. `attachment_contents`
    # is the half that proves an attachment was actually decompressed.
    (
        "return every attachment empty",
        "src/vnz.rs",
        "            Ok(bytes) => attachments.push(Attachment {\n"
        "                path: entry.name.clone(),\n"
        "                bytes,\n"
        "            }),",
        "            Ok(bytes) => attachments.push(Attachment {\n"
        "                path: entry.name.clone(),\n"
        "                bytes: { let _ = bytes; Vec::new() },\n"
        "            }),",
    ),
    # SPEC 7: a corrupt attachment is DROPPED with a diagnostic, never
    # refused. Refusing is the shape to guard against, because it reads as
    # strictness and silently costs the caller a whole readable document.
    (
        "refuse a corrupt attachment rather than dropping it",
        "src/vnz.rs",
        "            Err(e) => reading.diagnostics.push(Diagnostic::new(\n"
        "                attachment_pointer(&entry.name),\n"
        "                Cause::AttachmentUnreadable {\n"
        "                    fault: AttachmentFault::CorruptStream(e.to_string()),\n"
        "                },\n"
        "            )),",
        "            Err(e) => return Err(Refusal::ContainerMalformed { detail: e.to_string() }),",
    ),
    (
        "drop the last attachment",
        "src/vnz.rs",
        "    Ok(crate::zip::write_archive(&names))",
        "    if names.len() > 1 { names.pop(); }\n    Ok(crate::zip::write_archive(&names))",
    ),
    (
        "keep only the outer ring of a nested polygon",
        "src/decode.rs",
        "            self.report(path, Cause::PolygonNestedRings);\n            return None;",
        '            self.report(path, Cause::PolygonNestedRings);\n            return items.first()?.as_array().map(|r| r.iter().enumerate().filter_map(|(i, c)| self.position(c, &path.index(i), "the area")).collect());',
    ),
    # ---- Format 0.2: the walk (SPEC 3.3.3, 3.4, 6.2) ----
    #
    # Walk continuity is valid-walk rule 4 in the new shape: a pass after the
    # first enters its arc at the node where the previous pass left.
    (
        "skip the check that a pass enters where the last one left",
        "src/walk.rs",
        "                if !is_end(&self.at) {",
        "                if false && !is_end(&self.at) {",
    ),
    (
        "skip the check that a `from` node is an end of its arc",
        "src/walk.rs",
        '            Some(Endpoint::Node(node)) if !is_end(node) => {\n                return Err(not_connected(\n                    Some("from"),',
        '            Some(Endpoint::Node(node)) if false && !is_end(node) => {\n                return Err(not_connected(\n                    Some("from"),',
    ),
    (
        "let a pass other than the last leave at a position",
        "src/walk.rs",
        "            (Some(Endpoint::At(_)), _, _) if i != self.last => {",
        "            (Some(Endpoint::At(_)), _, _) if false && i != self.last => {",
    ),
    # A pass with a turn and no `to` leaves by the node it entered by. A
    # reader that ignores the turn walks on to the far end of the arc.
    (
        "ignore a turn when deriving where a pass leaves",
        "src/walk.rs",
        "            (None, Some(_), entry) => entry.clone(),",
        "            (None, Some(_), Endpoint::Node(node)) if *node == arc.from => Endpoint::Node(arc.to.clone()),\n            (None, Some(_), _) => Endpoint::Node(arc.from.clone()),",
    ),
    (
        "let a pass after the first carry `from`",
        "src/walk.rs",
        "        if i > 0 && p.from.is_some() {",
        "        if false && i > 0 && p.from.is_some() {",
    ),
    (
        "skip the pinned-turn check",
        "src/walk.rs",
        "        if p.turn.is_some() && p.pinned {",
        "        if false && p.turn.is_some() && p.pinned {",
    ),
    (
        "skip the route rule",
        "src/walk.rs",
        "        if self.route {",
        "        if false && self.route {",
    ),
    # SPEC 6.2: a walk is used whole or not at all. Keeping the passes that
    # read and dropping the one that did not hands the caller a different walk.
    (
        "drop the bad pass and keep the highlight",
        "src/decode.rs",
        "                    self.report(&fault.path(&walk_path), fault.cause);\n                    return None;",
        "                    self.report(&fault.path(&walk_path), fault.cause);\n                    continue;",
    ),
    (
        "skip the elevation-or-time check on a walk position",
        "src/decode.rs",
        "                    if position.ele().is_some() || position.time_ms().is_some() {",
        "                    if false {",
    ),
    (
        "skip the time check on a node",
        "src/decode.rs",
        "            if position.time_ms().is_some() {\n                self.report(\n                    &at,",
        "            if false {\n                self.report(\n                    &at,",
    ),
    (
        "skip the time check on an arc pip",
        "src/decode.rs",
        "        if positions.iter().any(|p| p.time_ms().is_some()) {",
        "        if false {",
    ),
    # The other direction: `arc-pip-time` on every arc, timed or not.
    (
        "report a pip time on every arc",
        "src/decode.rs",
        "        if positions.iter().any(|p| p.time_ms().is_some()) {",
        "        if true {",
    ),
    (
        "keep a self-loop arc",
        "src/decode.rs",
        "        if from == to {",
        "        if false && from == to {",
    ),
    # Report the absent node and keep the arc, which was the 0.1 remedy.
    (
        "keep an arc that names an absent node",
        "src/decode.rs",
        "        if absent {\n            return None;",
        "        if false && absent {\n            return None;",
    ),
    (
        "keep a ring's closing repeat",
        "src/decode.rs",
        "            self.report(&path.index(ring.len() - 1), Cause::RingClosed);\n            ring.pop();",
        "            self.report(&path.index(ring.len() - 1), Cause::RingClosed);",
    ),
    (
        "keep a ring of fewer than three distinct positions",
        "src/decode.rs",
        "        if distinct < 3 {",
        "        if false && distinct < 3 {",
    ),
    # SPEC 3.7: distinct is longitude and latitude only. Comparing every
    # channel counts two positions that differ only in elevation as two.
    (
        "count a ring's distinct positions by every channel",
        "src/decode.rs",
        "        if !seen.iter().any(|q| q.same_point(p)) {",
        "        if !seen.iter().any(|q| *q == p) {",
    ),
    # SPEC 3.3.3: a writer emits `pinned` only when it is true.
    (
        "write a false `pinned`",
        "src/encode.rs",
        '                if p.pinned {\n                    o.raw("pinned", Json::Bool(true));\n                }',
        '                o.raw("pinned", Json::Bool(p.pinned));',
    ),
    # The writer's checks. No corpus tier writes a model the reader did not
    # produce, so the unit tests in `encode.rs` are what hold these.
    (
        "write a walk without checking it",
        "src/encode.rs",
        '    check_walk(&c.walk, arcs, false, &path.key("walk"))?;',
        "    let _ = check_walk;",
    ),
    (
        "write arcs without checking them",
        "src/encode.rs",
        '    check_arcs(nodes, arcs, &path.key("arcs"))?;',
        "    let _ = check_arcs;",
    ),
    (
        "write a closed ring",
        "src/encode.rs",
        '    check_ring(&a.polygon, &path.key("polygon"))?;',
        "    let _ = check_ring;",
    ),
]


MARKER = os.path.join(ROOT, ".redproof-running")


def write_marker(name, rel):
    """Say in the TREE that a run is in progress, and which mutation is live.

    ⚠ THIS IS FOR EVERYONE ELSE, NOT FOR THE RUNNER. While this script runs,
    any other lane sharing the worktree sees a red suite and a violated source
    file — and nothing distinguishes that from a red proof somebody abandoned
    without reverting. The two look identical. On 2026-09-10 that cost two
    lanes a measurement each: one built a detached worktree to prove HEAD was
    clean, the other found the same files dirty and left them alone, and the
    change they were both reasoning about was this script's own
    `refuse on a newer PATCH too` mutation, held for about twenty seconds.

    Chat cannot fix that. The observer is a different agent who arrives
    mid-run, reads `git status`, and finds an untracked file — which is why
    this is deliberately NOT gitignored.
    """
    io.open(MARKER, "w", encoding="utf-8").write(f"""\
redproof.py is running in this tree.

The mutation below is DELIBERATE and TRANSIENT. Do not revert it, do not
commit it, and do not trust a test result from this tree until the run ends.

  pid       {os.getpid()}
  started   {STARTED}
  updated   {datetime.datetime.now().isoformat(timespec="seconds")}
  mutation  {name}
  file      {rel}

Is the run still alive?

  kill -0 {os.getpid()}      # exit 0 = alive, "No such process" = STALE

⚠ A STALE MARKER IS THE DANGEROUS CASE. If the process is gone, this script
was killed while holding the mutation, so the file above is still violated and
nothing is going to put it back. That is the one state that really is an
abandoned red proof:

  git -C {ROOT} checkout -- {rel}
  rm {MARKER}
""")


def remove_marker():
    try:
        os.remove(MARKER)
    except FileNotFoundError:
        pass


STARTED = datetime.datetime.now().isoformat(timespec="seconds")
atexit.register(remove_marker)


def snapshot():
    """Copy every file this script may mutate, and return the copy's directory.

    ⚠ TAKEN IMMEDIATELY BEFORE EACH MUTATION, NEVER ONCE FOR THE WHOLE RUN.
    A run is about thirty mutations and ten minutes, and `restore` puts back
    whatever the snapshot holds — so a snapshot taken at the start silently
    reverts every edit made to a listed file while the run is in progress, and
    the run reports success. That is not hypothetical: it happened on
    2026-09-10, to comment edits made in this same lane while a run was in the
    background, and it surfaced as unrelated test failures rather than as
    anything naming a restore.

    Re-reading immediately before the write is the same remedy as
    `~/src/knowledge/a-backup-brackets-its-own-writers-edit.md`: a copy taken at
    the start of a turn brackets that turn's own edit and nothing else.
    """
    tmp = tempfile.mkdtemp(prefix="vnx-redproof-")
    for rel in FILES:
        dst = os.path.join(tmp, rel.replace("/", "__"))
        shutil.copyfile(os.path.join(ROOT, rel), dst)
    return tmp


def restore(tmp):
    for rel in FILES:
        shutil.copyfile(
            os.path.join(tmp, rel.replace("/", "__")), os.path.join(ROOT, rel)
        )
    shutil.rmtree(tmp, ignore_errors=True)


def run_corpus():
    # `--no-fail-fast`, or cargo stops after the first failing test BINARY and
    # a mutation that reddens both tiers is reported as reddening only one.
    # `--lib` too: the question is whether the SUITE notices, and several rules
    # have no corpus case behind them and are held by unit tests alone. The
    # detail line names which tests reddened, so corpus and unit coverage stay
    # distinguishable.
    out = subprocess.run(
        [
            "cargo",
            "test",
            "--no-fail-fast",
            "--lib",
            "--test",
            "corpus",
            "--test",
            "zip64",
            "--test",
            "hostile",
        ],
        cwd=ROOT,
        capture_output=True,
        text=True,
    )
    text = out.stdout + out.stderr
    if "test result: ok." in text and "FAILED" not in text:
        return "GREEN", text
    if "test result: FAILED" in text:
        # The names cargo lists under `failures:`, which are indented four
        # spaces and carry no other whitespace. Taking them by shape rather than
        # by a keyword, because a filter written from the failures you EXPECT
        # cannot show the one you did not.
        # The names cargo lists under `failures:`, which are indented four
        # spaces and carry no other whitespace. Taken by SHAPE rather than by a
        # keyword, because a filter written from the failures you EXPECT cannot
        # show the one you did not — and `::`-qualified unit-test names count,
        # or a mutation caught only by a unit test reports as "(?)". Reporting
        # "something reddened" without saying what is the same defect as
        # reporting only the first failing binary.
        def looks_like_a_test_name(name: str) -> bool:
            return (
                name
                and " " not in name
                and "_" in name
                and name[0].islower()
                and all(c.isalnum() or c in "_:" for c in name)
            )

        failed = sorted(
            {
                l.strip()
                for l in text.splitlines()
                if l.startswith("    ") and looks_like_a_test_name(l.strip())
            }
        )
        return "red", ", ".join(failed) or "?"
    return "DID NOT BUILD", text[-400:]


def main():
    baseline, detail = run_corpus()
    if baseline != "GREEN":
        print("baseline is not green; aborting\n", detail)
        return 1
    print("baseline: GREEN\n")
    print(
        f"  (holding a write lock on {len(FILES)} files for this run — "
        "do not edit them until it finishes)"
    )
    print(
        f"  (a marker at {MARKER} names the live mutation, "
        "so another lane can read the tree rather than guess)\n"
    )
    survivors = []
    for name, rel, old, new, *rest in MUTATIONS:
        count = -1 if "all" in rest else 1
        # A mutation may be expected to SURVIVE, documenting a hole in the
        # corpus rather than a hole in this crate. It fails the script when it
        # goes red, so the note cannot outlive the limitation it records.
        want_green = "expect-green" in rest
        # The snapshot is per-mutation. See `snapshot`.
        backup = snapshot()
        write_marker(name, rel)
        path = os.path.join(ROOT, rel)
        s = io.open(path, encoding="utf-8").read()
        if old not in s:
            print(f"  SKIPPED (pattern gone): {name}")
            survivors.append(name + " [pattern not found]")
            restore(backup)
            continue
        io.open(path, "w", encoding="utf-8").write(
            s.replace(old, new) if count == -1 else s.replace(old, new, 1)
        )
        verdict, detail = run_corpus()
        restore(backup)
        wanted = "GREEN" if want_green else "red"
        ok = verdict == wanted
        mark = ("red  " if verdict == "red" else verdict) + ("" if ok else "  !!")
        note = f"   ({detail})" if verdict == "red" else ""
        if want_green and ok:
            note = "   (survives: a corpus blind spot, reported)"
        print(f"  {mark:14} {name}{note}")
        if not ok:
            if want_green:
                survivors.append(
                    f"{name} -> went red, so the corpus can now see it; "
                    "turn this into an ordinary mutation"
                )
            else:
                survivors.append(f"{name} -> {verdict}")
            if verdict == "DID NOT BUILD":
                print("      " + detail.replace("\n", "\n      ")[:600])
    print()
    if survivors:
        print("UNEXPECTED VERDICTS:")
        for s in survivors:
            print("  -", s)
        return 1
    print("every mutation reached the verdict it should.")
    return 0


# Importing this file must not run it: main() edits rust/src in place.
if __name__ == "__main__":
    sys.exit(main())

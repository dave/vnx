import Foundation
import XCTest
@testable import VNX

final class CorpusTests: XCTestCase {

    // MARK: - accept/

    func testAcceptTier() throws {
        let cases = try Corpus.cases(in: "accept")
        XCTAssertGreaterThan(cases.count, 0, "the accept tier is empty; the runner is not finding the corpus")
        for url in cases {
            runCase(url.lastPathComponent) { try self.runAcceptCase(url) }
        }
    }

    private func runAcceptCase(_ url: URL) throws {
        let name = url.lastPathComponent
        let sidecar = try loadSidecar(for: url)
        XCTAssertEqual(sidecar.kind, "accept", "\(name): sidecar `kind` does not match its directory")

        let bytes = try Data(contentsOf: url)
        let document: Document
        let diagnostics: [Diagnostic]
        var attachments: [Attachment] = []
        var documentSource = bytes

        if url.pathExtension == "vnz" {
            let container = try VNX.readContainer(bytes)
            document = container.document
            diagnostics = container.diagnostics
            attachments = container.attachments
            // The tree comparison is against the DOCUMENT inside the archive,
            // not against the archive: SPEC 7.1 constrains five rules and leaves
            // every other field of a container to its writer.
            documentSource = try containerDocumentBytes(bytes)
        } else {
            let result = try VNX.read(bytes)
            document = result.document
            diagnostics = result.diagnostics
        }

        XCTAssertEqual(diagnostics.map(\.message), [],
                       "\(name): the accept tier requires zero diagnostics")

        try assertCounts(sidecar, document, name)
        try assertPositions(sidecar, document, name)
        try assertTracks(sidecar, document, name)
        assertAttachments(sidecar, attachments, name)

        // SPEC 8.1: decode, ENCODE, and compare the tree parsed from the
        // original file against the tree parsed from the output. Never the
        // decoded model against another decoded model — see `Tree`.
        let written = try VNX.write(document)
        if let d = try Tree.difference(got: written, want: documentSource,
                                       Tree.Options(emitBoundary: true)) {
            XCTFail("\(name): what this library wrote is not what it read — \(d)")
        }
    }

    private func containerDocumentBytes(_ archive: Data) throws -> Data {
        let bytes = [UInt8](archive)
        let entries = try Zip.entries(in: bytes).filter { !$0.name.hasSuffix("/") }
        let root = entries.first { $0.name.lowercased() == "doc.vnx" }
            ?? entries.first { $0.name.lowercased().hasSuffix(".vnx") }!
        return Data(try Zip.contents(of: root, in: bytes))
    }

    // MARK: - reject/

    func testRejectTier() throws {
        let cases = try Corpus.cases(in: "reject")
        XCTAssertGreaterThan(cases.count, 0)
        for url in cases {
            runCase(url.lastPathComponent) { try self.runRejectCase(url) }
        }
    }

    private func runRejectCase(_ url: URL) throws {
        let name = url.lastPathComponent
        let sidecar = try loadSidecar(for: url)
        XCTAssertEqual(sidecar.kind, "reject", "\(name): sidecar `kind` does not match its directory")
        guard let expected = sidecar.refusal else {
            XCTFail("\(name): a reject sidecar must name the expected refusal")
            return
        }
        let bytes = try Data(contentsOf: url)
        do {
            if url.pathExtension == "vnz" {
                _ = try VNX.readContainer(bytes)
            } else {
                _ = try VNX.read(bytes)
            }
            XCTFail("\(name): expected a refusal naming \"\(expected)\", but the document was read")
        } catch let refusal as Refusal {
            XCTAssertTrue(refusal.message.contains(expected),
                          "\(name): the refusal must name its cause. Expected the substring \"\(expected)\", got \"\(refusal.message)\"")
            Corpus.observed.record(refusal.cause, matching: expected)
        }
    }

    // MARK: - diagnose/

    func testDiagnoseTier() throws {
        let cases = try Corpus.cases(in: "diagnose")
        XCTAssertGreaterThan(cases.count, 0)
        for url in cases {
            runCase(url.lastPathComponent) { try self.runDiagnoseCase(url) }
        }
    }

    private func runDiagnoseCase(_ url: URL) throws {
        let name = url.lastPathComponent
        let sidecar = try loadSidecar(for: url)
        XCTAssertEqual(sidecar.kind, "diagnose", "\(name): sidecar `kind` does not match its directory")
        guard let expected = sidecar.diagnostics else {
            XCTFail("\(name): a diagnose sidecar must list its expected diagnostics, even when the list is empty")
            return
        }
        let result = try Corpus.read(url)
        try assertCounts(sidecar, result.document, name)
        try assertPositions(sidecar, result.document, name)
        try assertTracks(sidecar, result.document, name)
        assertAttachments(sidecar, result.attachments, name)
        assertDiagnosticsMatchAsSet(expected: expected, reported: result.diagnostics, name: name)
    }

    /// SPEC 6.3 makes emission order insignificant, so the comparison is a set
    /// comparison — but each expected diagnostic has to be paired with one
    /// DISTINCT reported diagnostic, or a runner that let one report satisfy
    /// two expectations would pass a reader that found half the defects.
    private func assertDiagnosticsMatchAsSet(expected: [Corpus.Sidecar.ExpectedDiagnostic],
                                             reported: [Diagnostic], name: String) {
        XCTAssertEqual(reported.count, expected.count,
                       "\(name): expected \(expected.count) diagnostic(s), got \(reported.count): \(reported.map { "\($0.path) — \($0.code) — \($0.message)" })")
        var used = [Bool](repeating: false, count: reported.count)
        for e in expected {
            var matched = false
            for (i, r) in reported.enumerated() where !used[i] {
                guard r.path == e.path, Corpus.satisfies(r, e) else { continue }
                used[i] = true
                matched = true
                break
            }
            XCTAssertTrue(matched,
                          "\(name): no unused diagnostic matched \(e.path) (code \(e.code ?? "-"), advisory \(e.advisory ?? "-")). Reported: \(reported.map { "\($0.path) — \($0.code) — \($0.message)" })")
        }
    }

    // MARK: - write/

    /// SPEC 4.1's emit boundary, which only this tier reaches: every other tier
    /// encodes what it decoded, so it applies the precision rule to values that
    /// already satisfy it and shortens positions that are already short.
    ///
    /// Two things about `emit-boundary`'s elevation ties, so that neither reads
    /// as an oversight to whoever tidies the fixture next:
    ///
    /// - BOTH `340.25` and `340.75` are load-bearing, and it takes the pair.
    ///   `340.25` separates half-to-even from half-away-from-zero and half-up;
    ///   `340.75` separates it from half-toward-zero and half-down. Neither
    ///   alone leaves one rule standing —
    ///   `RoundingTests.testOnlyHalfToEvenSurvivesThePair` asserts exactly that.
    /// - There is deliberately NO NEGATIVE TIE, and that is not a gap: all five
    ///   wrong rules are already separated by the positive pair, which
    ///   `RoundingTests.testOnlyHalfToEvenSurvivesThePair` asserts directly
    ///   rather than taking on trust. Note what that does NOT say: over
    ///   positives alone, half-away-from-zero and half-up are the same
    ///   behaviour, as are half-toward-zero and half-down. Separating a wrong
    ///   rule from half-to-even and telling two wrong rules apart are different
    ///   jobs, and the fixture only needs the first.
    ///
    /// The tier's README is worth quoting, because it corrects something this
    /// library's own comments asserted: "That was stated for months as a
    /// permanent property — 'rounding is unreachable because no tier rounds' —
    /// and it was not a fact about the format. It was a fact about which tiers
    /// had been built. An unreachability claim is a claim about the current
    /// state of someone's ingenuity, not about the format."
    func testWriteTier() throws {
        let directory = Corpus.root.appendingPathComponent("write")
        let names = try FileManager.default.contentsOfDirectory(atPath: directory.path).sorted()
        let inputs = names.filter { $0.hasSuffix(".in.vnx") }
        XCTAssertGreaterThan(inputs.count, 0, "the write tier is empty")

        for input in inputs {
            let stem = String(input.dropLast(".in.vnx".count))
            // A case carrying an attachment manifest writes a CONTAINER, so its
            // expected output is an archive rather than a document. See
            // `corpus/write/README.md`.
            if names.contains("\(stem).attachments.json") {
                runCase(input) { try self.runWriteContainerCase(directory, stem) }
                continue
            }
            runCase(input) { try self.runWriteCase(directory, input) }
        }
        XCTAssertTrue(names.contains { $0.hasSuffix(".attachments.json") },
                      "the write tier holds no container case")
    }

    /// A write case whose expected output is an archive.
    ///
    /// The attachments come from a manifest rather than from real files beside
    /// the sidecar: one name holds U+1F600, and a filename is subject to the
    /// checkout filesystem's Unicode normalisation and case folding, so a
    /// checked-out file could arrive under a name that is not the one the case
    /// means and the fixture would test the filesystem instead of the writer.
    ///
    /// The attachments are fed REVERSED. A writer's output must not depend on
    /// the order it was handed, and feeding them in manifest order cannot see
    /// one that does.
    ///
    /// ⚠ THE ARCHIVE'S BYTES ARE NOT COMPARED. SPEC 7.1 constrains five rules
    /// and leaves every other field to the writer, so two conforming writers are
    /// not expected to agree byte for byte — the expected archive here was
    /// written by another implementation. What is compared is what 7.1 does
    /// constrain: the root document's tree, the entry names, the attachment
    /// bodies, and the document being first.
    private func runWriteContainerCase(_ directory: URL, _ stem: String) throws {
        let inputURL = directory.appendingPathComponent("\(stem).in.vnx")
        let writtenURL = directory.appendingPathComponent("\(stem).written.vnz")
        let sidecarURL = directory.appendingPathComponent("\(stem).expect.json")
        guard FileManager.default.fileExists(atPath: writtenURL.path) else {
            XCTFail("\(stem): the manifest promises \(stem).written.vnz and there is none")
            return
        }
        guard FileManager.default.fileExists(atPath: sidecarURL.path) else {
            XCTFail("\(stem): a case with no sidecar is a failure, not a skip")
            return
        }
        let sidecar = try Corpus.Sidecar(sidecarURL)
        XCTAssertEqual(sidecar.kind, "write", "\(stem): sidecar `kind` does not match its directory")
        XCTAssertEqual(sidecar.rootEntry, "doc.vnx", "\(stem): this runner writes the document as doc.vnx")

        let manifest = try manifestAttachments(directory.appendingPathComponent("\(stem).attachments.json"))
        let expected = try archiveEntries(try Data(contentsOf: writtenURL))
        XCTAssertEqual(expected.first?.name, "doc.vnx",
                       "\(stem): SPEC 7.1 puts the root document first")

        let input = try VNX.read(try Data(contentsOf: inputURL))
        XCTAssertEqual(input.diagnostics.map(\.message), [], "\(stem): a writer case's input must read cleanly")
        let archive = try VNX.writeContainer(document: input.document, attachments: manifest.reversed())
        let produced = try archiveEntries(archive)

        // ⚠ THE ATTACHMENTS ARE AN UNORDERED COLLECTION. SPEC 7.1 constrains the
        // ROOT DOCUMENT's position and nothing else about entry order — "every
        // other field of the archive is the writer's own business" — so a
        // comparison that pinned the order of the attachments would enforce a
        // rule the specification explicitly disclaims. Ruled 2026-09-10, after
        // the Kotlin lane's tier went red for deleting a comparator it did not
        // need.
        XCTAssertEqual(produced.first?.name, "doc.vnx", "\(stem): SPEC 7.1 puts the root document first")
        XCTAssertEqual(produced.dropFirst().map(\.name).sorted(),
                       expected.dropFirst().map(\.name).sorted(),
                       "\(stem): the attachment entries, as a collection")
        if let d = try Tree.difference(got: produced[0].body, want: expected[0].body) {
            XCTFail("\(stem): the archive's root document is not the one the case expects — \(d)")
        }
        if let names = sidecar.attachments {
            XCTAssertEqual(produced.dropFirst().map(\.name).sorted(), names.sorted(),
                           "\(stem): the attachments the sidecar names")
        }
        // An attachment is bytes and is compared as bytes. Nothing in this
        // format interprets one.
        var bodies: [String: Data] = [:]
        for a in manifest { bodies[a.path] = a.bytes }
        for entry in produced.dropFirst() {
            guard let want = bodies[entry.name] else {
                XCTFail("\(stem): the archive carries \"\(entry.name)\", which the manifest does not name")
                continue
            }
            XCTAssertEqual(entry.body, want, "\(stem): the body of \(entry.name)")
        }
        XCTAssertEqual(produced.count - 1, manifest.count, "\(stem): every attachment, and no others")

        // And the archive this library wrote is one it reads. Sorted on both
        // sides for the comparison only — what is asserted is which attachments
        // came back, not the order they came back in.
        let back = try VNX.readContainer(archive)
        XCTAssertEqual(back.diagnostics.map(\.message), [], "\(stem): the archive must read back cleanly")
        XCTAssertEqual(back.attachments.sorted { $0.path < $1.path },
                       manifest.sorted { $0.path < $1.path },
                       "\(stem): the archive does not carry the manifest's attachments")
    }

    /// Every entry of an archive, in archive order, through this library's own
    /// ZIP reader.
    private func archiveEntries(_ data: Data) throws -> [(name: String, body: Data)] {
        let bytes = [UInt8](data)
        return try Zip.entries(in: bytes)
            .filter { !$0.name.hasSuffix("/") }
            .map { (name: $0.name, body: Data(try Zip.contents(of: $0, in: bytes))) }
    }

    /// The manifest's attachments, in the order it lists them.
    private func manifestAttachments(_ url: URL) throws -> [Attachment] {
        let parsed = try JSONParser.parse(try Data(contentsOf: url))
        guard let listed = parsed.objectValue?["attachments"]?.arrayValue else {
            throw Corpus.SidecarError("\(url.lastPathComponent) carries no `attachments` array")
        }
        return try listed.map { entry in
            guard let e = entry.objectValue, let name = e["name"]?.stringValue,
                  let body = e["body"]?.stringValue else {
                throw Corpus.SidecarError("\(url.lastPathComponent): an attachment has no `name` and `body`")
            }
            return Attachment(path: name, bytes: Data(body.utf8))
        }
    }

    private func runWriteCase(_ directory: URL, _ input: String) throws {
        let stem = String(input.dropLast(".in.vnx".count))
        let inputURL = directory.appendingPathComponent(input)
        let writtenURL = directory.appendingPathComponent("\(stem).written.vnx")
        let sidecarURL = directory.appendingPathComponent("\(stem).expect.json")

        XCTAssertTrue(FileManager.default.fileExists(atPath: writtenURL.path),
                      "\(stem): a write case is a pair; the written half is missing")
        guard FileManager.default.fileExists(atPath: sidecarURL.path) else {
            XCTFail("\(stem): a case with no sidecar is a failure, not a skip")
            return
        }
        let sidecar = try Corpus.Sidecar(sidecarURL)
        XCTAssertEqual(sidecar.kind, "write", "\(stem): sidecar `kind` does not match its directory")

        let inputBytes = try Data(contentsOf: inputURL)
        let expected = try Data(contentsOf: writtenURL)

        // ⚠ AND THE TIER NORMALISES ALMOST NOTHING. The accept tier applies the
        // precision rule to both sides and drops a trailing `null` from both,
        // because it compares a document against a re-encoding of itself. Doing
        // that here would normalise away the two rules under test. The one
        // normalisation left is the one that is not about the emit boundary at
        // all: SPEC 3.1's absent-versus-empty.
        //
        // Without the first assertion a writer that echoed its input would pass
        // every case in the tier.
        if try Tree.difference(got: inputBytes, want: expected) == nil {
            XCTFail("\(stem): the input is already the written form, so this case cannot fail for a writer that echoes")
            return
        }

        let produced = try VNX.write(try VNX.read(inputBytes).document)
        if let d = try Tree.difference(got: produced, want: expected) {
            XCTFail("\(stem): the written document is not the one the case expects — \(d)")
        }

        // Rounding and shortening are both idempotent, so the written form is a
        // fixed point. A case that fails this is a defect in the fixture rather
        // than in the writer.
        let again = try VNX.write(try VNX.read(expected).document)
        if let d = try Tree.difference(got: again, want: expected) {
            XCTFail("\(stem): the expected output is not a fixed point of the writer — \(d)")
        }
    }

    /// A written document is still a document, so it must read back cleanly.
    func testWriteTierOutputIsReadableWithNoDiagnostics() throws {
        let directory = Corpus.root.appendingPathComponent("write")
        for name in try FileManager.default.contentsOfDirectory(atPath: directory.path).sorted()
        where name.hasSuffix(".written.vnx") {
            let result = try VNX.read(try Data(contentsOf: directory.appendingPathComponent(name)))
            XCTAssertEqual(result.diagnostics.map(\.message), [], name)
        }
    }

    // MARK: - Sidecar presence

    /// SPEC 8.1 and the corpus README: "A file with no sidecar is a failure,
    /// not a skip: the file everyone assumes is covered is otherwise exactly
    /// the one that is not."
    func testEveryCaseHasASidecar() throws {
        for tier in ["accept", "reject", "diagnose", "write"] {
            for url in try Corpus.cases(in: tier) {
                let sidecar = url.deletingPathExtension().appendingPathExtension("expect.json")
                XCTAssertTrue(FileManager.default.fileExists(atPath: sidecar.path),
                              "\(tier)/\(url.lastPathComponent) has no sidecar")
            }
        }
    }

    // MARK: - Shared assertions

    private func loadSidecar(for url: URL) throws -> Corpus.Sidecar {
        let sidecarURL = url.deletingPathExtension().appendingPathExtension("expect.json")
        guard FileManager.default.fileExists(atPath: sidecarURL.path) else {
            throw Corpus.SidecarError("\(url.lastPathComponent) has no sidecar, which is a failure and not a skip")
        }
        return try Corpus.Sidecar(sidecarURL)
    }

    /// `counts` is exact, over the top-level collections AND the nested
    /// containers inside them. A bare name is a top-level collection; a name
    /// starting with `/` is a container addressed by the same JSON Pointer
    /// vocabulary `positions` uses.
    ///
    /// The completeness rule runs in BOTH directions, and neither half is
    /// optional. A container the document carries non-empty that the sidecar
    /// does not name is an error, because unnamed would otherwise mean
    /// unchecked and a decoder could invent items there; and a sidecar key the
    /// walk cannot produce is an error naming the key, because a runner that
    /// quietly skipped an expectation it did not understand is worse than one
    /// that never had it.
    private func assertCounts(_ sidecar: Corpus.Sidecar, _ document: Document, _ name: String) throws {
        guard let expected = sidecar.counts else { return }
        let actual = document.corpusCounts
        for (container, count) in actual {
            if count == 0 && expected[container] == nil {
                // A container the document does not carry need not be named.
                // A NON-EMPTY one must be, which the next line enforces.
                continue
            }
            guard let want = expected[container] else {
                XCTFail("\(name): the document carries \(count) at `\(container)` and the sidecar does not name it; "
                        + "an unnamed container is unchecked, not empty")
                continue
            }
            XCTAssertEqual(count, want, "\(name): `\(container)` count")
        }
        for container in expected.keys where actual[container] == nil {
            XCTFail("\(name): the sidecar counts `\(container)`, which this runner's walk cannot produce")
        }
    }

    /// The attachments a container case expects: their NAMES, and — where the
    /// sidecar states them — their BYTES.
    ///
    /// The two are separate expectations and the name list is the weaker of
    /// them: it cannot see a reader that returns an attachment whole but with
    /// the wrong bytes in it, which is what an off-by-one on a local header, a
    /// payload bounded by the wrong following entry, or a stored entry run
    /// through the inflater all produce. Only the content map sees those.
    ///
    /// Asserted for the DIAGNOSE tier as well as accept, and that is the half
    /// that carries the interesting case: `container-unusable-method.vnz` holds
    /// an entry a reader cannot decompress, which SPEC 7 drops with a
    /// diagnostic while returning the document. Its sidecar names only the
    /// readable entry, so a whole-map comparison asserts the drop was SCOPED —
    /// a reader that dropped both, or kept the unreadable one, fails here
    /// rather than only in the diagnostics list.
    private func assertAttachments(_ sidecar: Corpus.Sidecar, _ attachments: [Attachment], _ name: String) {
        if let expected = sidecar.attachments {
            // SPEC 7 does not order a reader's attachment list, and the corpus
            // README says so outright, so the comparison sorts both sides.
            XCTAssertEqual(attachments.map(\.path).sorted(), expected.sorted(), "\(name): attachments")
        }
        guard let expected = sidecar.attachmentContents else { return }
        var actual: [String: String] = [:]
        for attachment in attachments {
            guard let text = String(data: attachment.bytes, encoding: .utf8) else {
                XCTFail("\(name): the attachment \"\(attachment.path)\" is not UTF-8, and the sidecar states its content as text")
                continue
            }
            actual[attachment.path] = text
        }
        XCTAssertEqual(actual, expected, "\(name): attachment contents")
    }

    private func assertPositions(_ sidecar: Corpus.Sidecar, _ document: Document, _ name: String) throws {
        for expected in sidecar.positions {
            guard let p = document.position(atPointer: expected.pointer) else {
                XCTFail("\(name): the pointer \(expected.pointer) does not resolve in the decoded model")
                continue
            }
            // BY BIT PATTERN. A sidecar's expected value is a JSON number read
            // by THIS parser, so this cannot see a parser that is an ulp out —
            // it would be wrong on both sides. What it does see is anything that
            // moves a value between the parse and the model, and by bits it sees
            // a move of one ulp, which by value it would not.
            XCTAssertEqual(p.lon.bitPattern, expected.lon.bitPattern,
                           "\(name): longitude at \(expected.pointer): \(p.lon) against \(expected.lon)")
            XCTAssertEqual(p.lat.bitPattern, expected.lat.bitPattern,
                           "\(name): latitude at \(expected.pointer): \(p.lat) against \(expected.lat)")
        }
    }

    private func assertTracks(_ sidecar: Corpus.Sidecar, _ document: Document, _ name: String) throws {
        for expected in sidecar.tracks {
            guard let track = (document.tracks ?? []).first(where: { $0.name == expected.name }) else {
                XCTFail("\(name): no track named \"\(expected.name)\" in the decoded model")
                continue
            }
            // `null` means unknown, which is not zero (SPEC 3.5).
            XCTAssertEqual(track.movingTimeMilliseconds, expected.movingTimeMilliseconds,
                           "\(name): moving time for \"\(expected.name)\"")
        }
    }
}

import Foundation
import XCTest
@testable import VNX

final class CorpusTests: XCTestCase {

    // MARK: - accept/

    func testAcceptTier() throws {
        let cases = try Corpus.cases(in: "accept")
        XCTAssertGreaterThan(cases.count, 0, "the accept tier is empty; the runner is not finding the corpus")
        for url in cases {
            try runAcceptCase(url)
        }
    }

    private func runAcceptCase(_ url: URL) throws {
        let name = url.lastPathComponent
        let sidecar = try loadSidecar(for: url)
        XCTAssertEqual(sidecar.kind, "accept", "\(name): sidecar `kind` does not match its directory")

        let bytes = try Data(contentsOf: url)
        let document: Document
        let diagnostics: [Diagnostic]
        var attachments: [String] = []
        var canonicalSource = bytes

        if url.pathExtension == "vnz" {
            let container = try VNX.readContainer(bytes)
            document = container.document
            diagnostics = container.diagnostics
            attachments = container.attachments.map(\.path)
            // The document inside an accept container is stored canonical too,
            // so the re-render check applies to it rather than to the archive.
            canonicalSource = try containerDocumentBytes(bytes)
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
        if let expected = sidecar.attachments {
            XCTAssertEqual(attachments.sorted(), expected.sorted(), "\(name): attachments")
        }

        // SPEC 8.1: decode, RE-RENDER, and require the original bytes back.
        let rendered = VNX.render(document)
        XCTAssertEqual(String(decoding: rendered, as: UTF8.self),
                       String(decoding: canonicalSource, as: UTF8.self),
                       "\(name): re-render did not reproduce the file")
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
            let name = url.lastPathComponent
            let sidecar = try loadSidecar(for: url)
            XCTAssertEqual(sidecar.kind, "reject", "\(name): sidecar `kind` does not match its directory")
            guard let expected = sidecar.refusal else {
                XCTFail("\(name): a reject sidecar must name the expected refusal")
                continue
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
    }

    // MARK: - diagnose/

    func testDiagnoseTier() throws {
        let cases = try Corpus.cases(in: "diagnose")
        XCTAssertGreaterThan(cases.count, 0)
        for url in cases {
            let name = url.lastPathComponent
            let sidecar = try loadSidecar(for: url)
            XCTAssertEqual(sidecar.kind, "diagnose", "\(name): sidecar `kind` does not match its directory")
            guard let expected = sidecar.diagnostics else {
                XCTFail("\(name): a diagnose sidecar must list its expected diagnostics, even when the list is empty")
                continue
            }
            let result = try VNX.read(try Data(contentsOf: url))
            try assertCounts(sidecar, result.document, name)
            try assertPositions(sidecar, result.document, name)
            try assertTracks(sidecar, result.document, name)
            assertDiagnosticsMatchAsSet(expected: expected, reported: result.diagnostics, name: name)
        }
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
                          "\(name): no unused diagnostic matched \(e.path) (code \(e.code ?? "-"), message \(e.message ?? "-")). Reported: \(reported.map { "\($0.path) — \($0.code) — \($0.message)" })")
        }
    }

    // MARK: - format/

    func testFormatTier() throws {
        let directory = Corpus.root.appendingPathComponent("format")
        let names = try FileManager.default.contentsOfDirectory(atPath: directory.path).sorted()
        let inputs = names.filter { $0.hasSuffix(".in.vnx") }
        XCTAssertGreaterThan(inputs.count, 0, "the format tier is empty")
        for input in inputs {
            let stem = String(input.dropLast(".in.vnx".count))
            let inputURL = directory.appendingPathComponent(input)
            let canonicalURL = directory.appendingPathComponent("\(stem).canonical.vnx")
            XCTAssertTrue(FileManager.default.fileExists(atPath: canonicalURL.path),
                          "\(stem): a format case is a pair; the canonical half is missing")

            let inputBytes = try Data(contentsOf: inputURL)
            let expected = try Data(contentsOf: canonicalURL)

            // SPEC 8.1: a runner MUST assert that each format input is NOT
            // already canonical. Without it a formatter that echoed its input
            // would pass every case in the tier.
            XCTAssertNotEqual(inputBytes, expected,
                              "\(stem): the input is already canonical, so this case cannot fail for a formatter that echoes its input")

            let produced = try VNX.canonicalise(inputBytes)
            XCTAssertEqual(String(decoding: produced, as: UTF8.self),
                           String(decoding: expected, as: UTF8.self),
                           "\(stem): canonical output")

            // Canonical form is idempotent: the expected bytes are already it.
            XCTAssertEqual(try VNX.canonicalise(expected), expected,
                           "\(stem): canonicalising canonical bytes changed them")
        }
    }

    /// The one direction of the sidecar's `schema` verdict that a reader can
    /// check without a JSON Schema validator: `not-json` means no validator
    /// ever sees the document, and neither can this reader, so it must refuse.
    ///
    /// This library does NOT validate against `corpus/schema/vnx-0.1.schema.json`.
    /// SPEC 8.2 publishes the schema so a document can be checked with no VNX
    /// implementation present, and records a verdict per case so that
    /// schema/reader disagreements are visible; it does not ask a library to
    /// carry a validator, and this one does not.
    func testNotJSONCasesAreRefused() throws {
        var checked = 0
        for tier in ["accept", "reject", "diagnose"] {
            for url in try Corpus.cases(in: tier) {
                let sidecar = try Corpus.Sidecar(url.deletingPathExtension().appendingPathExtension("expect.json"))
                guard sidecar.schema == "not-json" else { continue }
                checked += 1
                XCTAssertThrowsError(try VNX.read(try Data(contentsOf: url)), url.lastPathComponent) {
                    XCTAssertEqual(($0 as? Refusal)?.cause, .malformedJSON, url.lastPathComponent)
                }
            }
        }
        XCTAssertGreaterThan(checked, 0, "no case is marked `not-json` any more; this check has stopped checking anything")
    }

    // MARK: - write/

    /// SPEC 4.5.4's WRITE path, which until this tier landed was beyond the
    /// corpus entirely: every other tier RE-RENDERS, and a re-render applies
    /// neither rounding nor shortening.
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
            let inputURL = directory.appendingPathComponent(input)
            let writtenURL = directory.appendingPathComponent("\(stem).written.vnx")
            let sidecarURL = directory.appendingPathComponent("\(stem).expect.json")

            XCTAssertTrue(FileManager.default.fileExists(atPath: writtenURL.path),
                          "\(stem): a write case is a pair; the written half is missing")
            guard FileManager.default.fileExists(atPath: sidecarURL.path) else {
                XCTFail("\(stem): a case with no sidecar is a failure, not a skip")
                continue
            }
            let sidecar = try Corpus.Sidecar(sidecarURL)
            XCTAssertEqual(sidecar.kind, "write", "\(stem): sidecar `kind` does not match its directory")

            let inputBytes = try Data(contentsOf: inputURL)
            let expected = try Data(contentsOf: writtenURL)

            // Without this a writer that echoed its input would pass every case.
            XCTAssertNotEqual(inputBytes, expected,
                              "\(stem): the input is already the written form, so this case cannot fail for a writer that echoes")

            let produced = VNX.write(try VNX.read(inputBytes).document)
            XCTAssertEqual(String(decoding: produced, as: UTF8.self),
                           String(decoding: expected, as: UTF8.self),
                           "\(stem): written output")

            // Rounding and shortening are both idempotent, so the written form
            // is a fixed point. A case that fails this is a defect in the
            // fixture rather than in the writer.
            XCTAssertEqual(VNX.write(try VNX.read(expected).document), expected,
                           "\(stem): the written form is not a fixed point")
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

    /// The format tier is the documented exception: a case there is a pair of
    /// files rather than a file and a sidecar.
    func testFormatTierUsesPairsNotSidecars() throws {
        let directory = Corpus.root.appendingPathComponent("format")
        let names = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        XCTAssertTrue(names.filter { $0.hasSuffix(".expect.json") }.isEmpty,
                      "the format tier is a pair-based tier; a sidecar here would not be read")
    }

    // MARK: - Shared assertions

    private func loadSidecar(for url: URL) throws -> Corpus.Sidecar {
        let sidecarURL = url.deletingPathExtension().appendingPathExtension("expect.json")
        guard FileManager.default.fileExists(atPath: sidecarURL.path) else {
            throw Corpus.SidecarError("\(url.lastPathComponent) has no sidecar, which is a failure and not a skip")
        }
        return try Corpus.Sidecar(sidecarURL)
    }

    /// `counts` is exact per collection, and a collection the sidecar does not
    /// name must be empty. Without the second half a decoder that invented
    /// items in an unnamed collection would pass.
    private func assertCounts(_ sidecar: Corpus.Sidecar, _ document: Document, _ name: String) throws {
        guard let expected = sidecar.counts else { return }
        let actual = document.collectionCounts
        for (collection, count) in actual {
            XCTAssertEqual(count, expected[collection] ?? 0,
                           "\(name): `\(collection)` count")
        }
        for collection in expected.keys {
            XCTAssertNotNil(actual[collection], "\(name): sidecar names an unknown collection `\(collection)`")
        }
    }

    private func assertPositions(_ sidecar: Corpus.Sidecar, _ document: Document, _ name: String) throws {
        for expected in sidecar.positions {
            guard let p = document.position(atPointer: expected.pointer) else {
                XCTFail("\(name): the pointer \(expected.pointer) does not resolve in the decoded model")
                continue
            }
            XCTAssertEqual(p.lon, expected.lon, "\(name): longitude at \(expected.pointer)")
            XCTAssertEqual(p.lat, expected.lat, "\(name): latitude at \(expected.pointer)")
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

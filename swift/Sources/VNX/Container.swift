import Foundation

/// A file inside a VNZ container that is not the root document (SPEC 7).
public struct Attachment: Equatable, Sendable {
    public let path: String
    public let bytes: Data
}

/// The result of reading a `.vnz`.
public struct ContainerResult: Sendable {
    /// The archive entry the root document was read from.
    public let documentName: String
    public let document: Document
    public let diagnostics: [Diagnostic]
    public let attachments: [Attachment]
}

enum Container {
    static func read(_ data: Data) throws -> ContainerResult {
        let bytes = [UInt8](data)
        let entries: [Zip.Entry]
        do {
            entries = try Zip.entries(in: bytes)
        } catch {
            throw Refusal(.containerMalformed, "this is not a readable ZIP archive: \(error)")
        }

        // SPEC 7 and SPEC 9. The path rules are a security requirement: an
        // attachment path containing `..` or an absolute path can direct an
        // extractor to write outside the destination directory. Checked at READ
        // time rather than left to whoever extracts later and may forget.
        for entry in entries {
            if let reason = unsafePath(entry.name) {
                throw Refusal(.containerPathEscapes,
                              "the entry \"\(entry.name)\" \(reason) and escapes the archive")
            }
        }

        // SPEC 7: duplicate entry names MUST be refused, compared
        // case-insensitively. Language runtimes resolve a repeated name
        // differently, so one archive would otherwise yield different documents
        // in different languages — and that applies to `doc.vnx` beside
        // `DOC.VNX` exactly as it applies to an exact repeat.
        var seen: Set<String> = []
        for entry in entries {
            let key = entry.name.lowercased()
            if !seen.insert(key).inserted {
                throw Refusal(.containerDuplicateEntry,
                              "the archive holds a duplicate entry named \"\(entry.name)\"; which one wins differs between languages, so the archive is refused")
            }
        }

        let files = entries.filter { !$0.name.hasSuffix("/") }
        guard let root = rootDocument(among: files) else {
            throw Refusal(.containerNoDocument, "the archive holds no `.vnx` entry, so there is no document to return")
        }
        guard root.method == Zip.methodStored || root.method == Zip.methodDeflate else {
            throw Refusal(.containerRootCompressionUnsupported,
                          "the root document \"\(root.name)\" uses compression method \(root.method); only stored and deflate are available in every target language, so there is no document to return")
        }
        let documentBytes: [UInt8]
        do {
            documentBytes = try Zip.contents(of: root, in: bytes)
        } catch {
            throw Refusal(.containerMalformed, "the root document \"\(root.name)\" could not be read: \(error)")
        }

        let read = try VNX.read(Data(documentBytes))
        var diagnostics = read.diagnostics
        var attachments: [Attachment] = []
        for entry in files where entry.name != root.name {
            guard entry.method == Zip.methodStored || entry.method == Zip.methodDeflate else {
                // SPEC 7: refusing a whole library because one photograph used
                // an unusual method would be disproportionate.
                diagnostics.append(Diagnostic(
                    path: "/" + entry.name,
                    message: "the attachment \"\(entry.name)\" uses compression method \(entry.method), which is not stored or deflate, so it was dropped",
                    cause: .attachmentCompressionUnsupported))
                continue
            }
            do {
                attachments.append(Attachment(path: entry.name, bytes: Data(try Zip.contents(of: entry, in: bytes))))
            } catch {
                diagnostics.append(Diagnostic(
                    path: "/" + entry.name,
                    message: "the attachment \"\(entry.name)\" could not be read and was dropped: \(error)",
                    cause: .attachmentCompressionUnsupported))
            }
        }
        return ContainerResult(documentName: root.name, document: read.document,
                               diagnostics: diagnostics, attachments: attachments)
    }

    /// SPEC 7: prefer an entry named `doc.vnx` AT THE ARCHIVE ROOT; where there
    /// is none, take the first entry with a `.vnx` extension in archive order.
    /// Names are compared case-insensitively.
    ///
    /// The match is on the FULL entry name, which SPEC 7's closing note calls
    /// out: matching on the base name alone matches at any depth and lets the
    /// last match win rather than the first.
    private static func rootDocument(among files: [Zip.Entry]) -> Zip.Entry? {
        if let exact = files.first(where: { $0.name.lowercased() == "doc.vnx" }) { return exact }
        return files.first { $0.name.lowercased().hasSuffix(".vnx") }
    }

    /// SPEC 7: attachment paths MUST be relative and forward-slash separated.
    /// An absolute path, a path carrying a drive letter, or a path with a `..`
    /// SEGMENT is refused. `..` is a segment, not a substring: `a..b/photo.jpg`
    /// is a legal name and escapes nothing.
    static func unsafePath(_ name: String) -> String? {
        if name.hasPrefix("/") { return "is an absolute path" }
        if name.contains("\\") { return "is not forward-slash separated" }
        let scalars = Array(name.unicodeScalars)
        if scalars.count >= 2, scalars[1] == ":",
           (scalars[0].properties.isAlphabetic && scalars[0].value < 128) {
            return "carries a drive letter"
        }
        for segment in name.split(separator: "/", omittingEmptySubsequences: false) where segment == ".." {
            return "has a `..` segment"
        }
        return nil
    }

    static func write(document: Document, attachments: [Attachment]) -> Data {
        var entries: [(name: String, body: [UInt8])] = [
            ("doc.vnx", [UInt8](VNX.write(document)))
        ]
        for a in attachments { entries.append((a.path, [UInt8](a.bytes))) }
        return Zip.archive(entries: entries)
    }
}

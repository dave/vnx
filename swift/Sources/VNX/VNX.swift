import Foundation

/// The VNX codec: read and write `.vnx` and `.vnz`, produce canonical form,
/// report diagnostics. That is what SPEC 8 tests and what the README calls "a
/// VNX library".
///
/// Not here, deliberately: GPX, KML and GeoJSON. The repository README calls
/// those "conversion tools, not part of the format ... no library is expected
/// to implement them."
public enum VNX {
    /// The format version this reader supports. SPEC 2.2's MAJOR and MINOR
    /// comparison is reader-relative, and this is the reader's side of it.
    public static let version = "0.1.0"

    public struct ReadResult: Sendable {
        public let document: Document
        /// SPEC 6.3: a reader that returns a document alone has discarded the
        /// only evidence that anything was wrong.
        public let diagnostics: [Diagnostic]
    }

    // MARK: - Reading

    /// Reads a plain `.vnx` document.
    ///
    /// Throws a `Refusal` for the four causes of SPEC 6.2 and for nothing else.
    /// Everything a reader can tolerate arrives in `diagnostics` instead.
    public static func read(_ data: Data) throws -> ReadResult {
        let root: JSONValue
        do {
            root = try JSONParser.parse(data)
        } catch let e as JSONSyntaxError {
            throw Refusal(.malformedJSON, "malformed JSON at byte \(e.offset): \(e.message)")
        }
        let decoder = DocumentDecoder()
        let document = try decoder.decode(root)
        return ReadResult(document: document, diagnostics: decoder.diagnostics)
    }

    /// Reads a `.vnz` container.
    public static func readContainer(_ data: Data) throws -> ContainerResult {
        try Container.read(data)
    }

    /// Reads either, chosen by the file's extension.
    public static func read(contentsOf url: URL) throws -> ReadResult {
        let data = try Data(contentsOf: url)
        if url.pathExtension.lowercased() == "vnz" {
            let c = try readContainer(data)
            return ReadResult(document: c.document, diagnostics: c.diagnostics)
        }
        return try read(data)
    }

    // MARK: - Writing

    /// RE-RENDER: the document a reader was handed, in canonical form, with
    /// neither rounding nor position shortening applied, so arity and precision
    /// survive (SPEC 8.1).
    public static func render(_ document: Document) -> Data {
        CanonicalWriter.render(DocumentEncoder.encode(document, mode: .reRender))
    }

    /// WRITE: a model turned into a document, applying the emit-boundary rules
    /// — coordinates rounded to 6 decimal places and elevations to 1 (SPEC
    /// 4.5.4), and the shortest spelling of every position (SPEC 4.1).
    public static func write(_ document: Document) -> Data {
        CanonicalWriter.render(DocumentEncoder.encode(document, mode: .write))
    }

    /// Canonicalises a document's bytes: decode, then RE-RENDER.
    ///
    /// Never bytes to bytes. SPEC 8.1: "Reformatting bytes into bytes never
    /// builds a model, so it cannot observe anything about what a model kept —
    /// a reader that discarded every data record, every ruleset, every style
    /// and every `notes` passes such a check."
    public static func canonicalise(_ data: Data) throws -> Data {
        render(try read(data).document)
    }

    /// Writes a `.vnz` container holding `document` as `doc.vnx` plus its
    /// attachments, every entry stored (SPEC 7 permits stored or deflate).
    public static func writeContainer(document: Document, attachments: [Attachment] = []) -> Data {
        Container.write(document: document, attachments: attachments)
    }
}

import Foundation

/// The VNX codec: read and write `.vnx` and `.vnz`, and report diagnostics.
/// That is what SPEC 8 tests and what the README calls "a VNX library".
///
/// Not here, deliberately: GPX, KML and GeoJSON. The repository README calls
/// those "conversion tools, not part of the format ... no library is expected
/// to implement them."
public enum VNX {
  /// The format version this reader supports. SPEC 2.2's MAJOR and MINOR
  /// comparison is reader-relative, and this is the reader's side of it.
  public static let version = "0.10.6"

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

  /// Writes a document.
  ///
  /// The emit-boundary rules of SPEC 4.1 are applied here and nowhere else:
  /// longitude and latitude rounded to 6 decimal places, elevation to 1, and
  /// the shortest spelling of every position.
  ///
  /// ⚠ THE OUTPUT IS COMPACT, AND THAT IS A DECISION RATHER THAN AN
  /// OMISSION. Nothing about a document's whitespace is constrained (SPEC
  /// 4.4), and indenting costs about three times the file. Pretty-printing
  /// belongs to a tool, not to the format.
  ///
  /// Throws where the model holds a number JSON cannot spell (SPEC 4.2), and
  /// `ApplicationKeyError` for an application key without a full stop or
  /// held twice (SPEC 3.9).
  public static func write(_ document: Document) throws -> Data {
    // Members sorted by code point, because an object has no order of its
    // own and an output that changed between runs could not be compared
    // with itself. Key order is not significant to a reader (SPEC 4.4).
    return try JSONWriter.render(try DocumentEncoder.encode(document))
  }

  /// Writes a `.vnz` container holding `document` as `doc.vnx` plus its
  /// attachments, every entry deflated (SPEC 7.1 permits stored or deflate).
  public static func writeContainer(document: Document, attachments: [Attachment] = []) throws
    -> Data
  {
    try Container.write(document: document, attachments: attachments)
  }
}

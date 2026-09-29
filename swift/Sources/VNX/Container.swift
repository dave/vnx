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
        throw Refusal(
          .containerPathEscapes,
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
        throw Refusal(
          .containerDuplicateEntry,
          "the archive holds a duplicate entry named \"\(entry.name)\"; which one wins differs between languages, so the archive is refused"
        )
      }
    }

    let files = entries.filter { !$0.name.hasSuffix("/") }
    guard let root = rootDocument(among: files) else {
      throw Refusal(
        .containerNoDocument, "the archive holds no `.vnx` entry, so there is no document to return"
      )
    }
    guard root.method == Zip.methodStored || root.method == Zip.methodDeflate else {
      throw Refusal(
        .containerRootCompressionUnsupported,
        "the root document \"\(root.name)\" uses compression method \(root.method); only stored and deflate are available in every target language, so there is no document to return"
      )
    }
    let documentBytes: [UInt8]
    do {
      documentBytes = try Zip.contents(of: root, in: bytes)
    } catch {
      throw Refusal(
        .containerMalformed, "the root document \"\(root.name)\" could not be read: \(error)")
    }

    let read = try VNX.read(Data(documentBytes))
    var diagnostics = read.diagnostics
    var attachments: [Attachment] = []
    for entry in files where entry.name != root.name {
      guard entry.method == Zip.methodStored || entry.method == Zip.methodDeflate else {
        // SPEC 7: refusing a whole library because one photograph used
        // an unusual method would be disproportionate.
        diagnostics.append(
          Diagnostic(
            path: entryPointer(entry.name),
            message:
              "the attachment \"\(entry.name)\" uses compression method \(entry.method), which is not stored or deflate, so it was dropped",
            cause: .attachmentUnreadable))
        continue
      }
      do {
        attachments.append(
          Attachment(path: entry.name, bytes: Data(try Zip.contents(of: entry, in: bytes))))
      } catch {
        diagnostics.append(
          Diagnostic(
            path: entryPointer(entry.name),
            message: "the attachment \"\(entry.name)\" could not be read and was dropped: \(error)",
            cause: .attachmentUnreadable))
      }
    }
    return ContainerResult(
      documentName: root.name, document: read.document,
      diagnostics: diagnostics, attachments: attachments)
  }

  /// SPEC 6.3's `#/entries/` space, which is where a diagnostic about an
  /// archive ENTRY lives: an entry is not part of the document, so no pointer
  /// into the document can locate one.
  ///
  /// THE MEMBER NAME IS ONE RFC 6901 REFERENCE TOKEN, and the escaping ORDER
  /// decides the answer: `~` becomes `~0` BEFORE `/` becomes `~1`. Reversed,
  /// the second pass escapes the `~1` the first pass just wrote, so
  /// `photos/a~b.jpg` comes out `photos~01a~0b.jpg`. An attachment name is the
  /// one place in this format where a member name routinely holds a slash.
  static func entryPointer(_ name: String) -> String {
    "#/entries/"
      + name.replacingOccurrences(of: "~", with: "~0")
      .replacingOccurrences(of: "/", with: "~1")
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
    // The backslash refusal comes FIRST, unconditionally, and that ordering
    // is the point rather than a tidiness. SPEC 7 says a `..` SEGMENT is
    // refused and that `..` is a segment rather than a substring, but never
    // says whether `\` counts as a separator while hunting for one — so a
    // reader splitting on both refuses `..\x` and a reader splitting on `/`
    // alone accepts it, and NEITHER fails any corpus tier. On a
    // Windows-authored archive that is a live escape. Refusing the
    // backslash before the hunt means the subtle question never arises.
    // Deliberately stricter than the ZIP appnote; ruled 2026-09-09.
    if name.contains("\\") { return "is not forward-slash separated" }
    if name.hasPrefix("/") { return "is an absolute path" }
    let scalars = Array(name.unicodeScalars)
    if scalars.count >= 2, scalars[1] == ":",
      scalars[0].properties.isAlphabetic && scalars[0].value < 128
    {
      return "carries a drive letter"
    }
    for segment in name.split(separator: "/", omittingEmptySubsequences: false)
    where segment == ".." {
      return "has a `..` segment"
    }
    return nil
  }

  /// SPEC 7.1: the root document first, at the archive root, named `doc.vnx`.
  ///
  /// The attachments follow, SORTED BY ENTRY NAME. 7.1 does not constrain
  /// their order; sorting them means that caller order — one exporter
  /// iterating a dictionary, another a list — is not a property of the
  /// archive. The comparator is by code point, because Swift's `<` normalises
  /// and so places `"\u{212B}"` below `"\u{00C6}"` where code point puts it
  /// far above.
  static func write(document: Document, attachments: [Attachment]) throws -> Data {
    var entries: [(name: String, body: [UInt8])] = [
      ("doc.vnx", [UInt8](try VNX.write(document)))
    ]
    for a in attachments.sorted(by: { codePointLess($0.path, $1.path) }) {
      entries.append((a.path, [UInt8](a.bytes)))
    }
    return try Zip.archive(entries: entries)
  }
}

import Foundation

/// ZIP archives the conformance corpus does not carry, built once with
/// Python's `zipfile` and embedded as base64 so the test suite stays
/// dependency-free.
///
/// For format 0.2 the version inside each document entry was changed from
/// `0.1.0` to `0.2.0` in place, for 0.8 from `0.2.0` to `0.8.0`, and for
/// 0.9 from `0.8.0` to `0.9.0`, the same way: the entry recompressed to its old
/// length and its CRC-32 replaced wherever the archive states it, so every
/// other byte — offsets, extra fields, padding, entry order — is the archive as
/// built. For 0.10 the version is one byte longer, so `"vnx": "0.9.0"` became
/// `"vnx":"0.10.0"` (the space after the colon pays for the digit), and where
/// the entry is deflated, indentation spaces moved to the end of the document
/// until it deflated to its old length. `largeDeflate` is the exception: its
/// test compares the entry with `largeDeflateBody`, so it is rebuilt with
/// Python's `zipfile` from that body (the same recipe gives the 0.9 archive
/// byte for byte). When the top-level `waypoints` became `points`, the key was
/// renamed in place the same way: the three freed bytes became whitespace (at
/// the end of a stored entry, wherever a deflated entry kept its old length),
/// and `largeDeflate` was rebuilt again. For 0.11, `"0.10.0"` became
/// `"0.11.0"` in every decoded entry, `largeDeflate` included: the same length,
/// so a stored entry changed one byte, a deflated entry was recompressed with
/// the zlib level and memory level that gave its old compressed length, and the
/// CRC-32 was replaced where the archive states it. A decoy entry no reader decodes
/// (`nested/doc.vnx`, `second.vnx`, `DOC.VNX`) keeps its old version.
/// `bzip2Root` is the exception and still carries `0.1.0`: its root document
/// is refused for its compression method, so no reader decodes it.
///
/// SPEC 7 requires a reader to handle ZIP64, deflate, a data descriptor and an
/// unsupported compression method, and to apply the root-document rules to a
/// FULL entry name. `corpus/tools/makevnz` writes four small stored-and-deflate archives
/// and none of those cases, so without these the code paths are unasserted.
enum ZipFixtures {
  /// A conforming archive in every respect EXCEPT entry order: attachments
  /// out of sequence, every header field right. SPEC 7.1 constrains a WRITER;
  /// SPEC 7 has a reader accept whatever it is given, so this must read
  /// cleanly. Byte-for-byte the same length as its sorted twin, which is worth
  /// having: a check that compared sizes could not tell them apart either.
  static let unsortedOnTheWire =
    "UEsDBAoAAAgAAAAAIQBuKmbUdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMS4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLAwQKAAAIAAAAACEA+Q4LTgMAAAADAAAACAAAAMOGYWUudHh0YWUKUEsDBAoAAAgAAAAAIQCdatx0AgAAAAIAAAALAAAAenotbGFzdC50eHR6ClBLAwQKAAAIAAAAACEAB6Hq3QIAAAACAAAADwAAAGFhYS9lYXJsaWVyLnR4dGEKUEsBAgoACgAACAAAAAAhAG4qZtR1AAAAdQAAAAcAAAAAAAAAAAAAAAAAAAAAAGRvYy52bnhQSwECCgAKAAAIAAAAACEA+Q4LTgMAAAADAAAACAAAAAAAAAAAAAAAAACaAAAAw4ZhZS50eHRQSwECCgAKAAAIAAAAACEAnWrcdAIAAAACAAAACwAAAAAAAAAAAAAAAADDAAAAenotbGFzdC50eHRQSwECCgAKAAAIAAAAACEAB6Hq3QIAAAACAAAADwAAAAAAAAAAAAAAAADuAAAAYWFhL2VhcmxpZXIudHh0UEsFBgAAAAAEAAQA4QAAAB0BAAAAAA=="

  static let zip64PaddedCentralDirectory =
    "IyEvYmluL3NoCiMgYSBzZWxmLWV4dHJhY3Rpbmcgc3R1Yiwgb3IgYW55IHByZWZpeCBhdCBhbGwKZXhpdCAwCiMhL2Jpbi9zaAojIGEgc2VsZi1leHRyYWN0aW5nIHN0dWIsIG9yIGFueSBwcmVmaXggYXQgYWxsCmV4aXQgMAojIS9iaW4vc2gKIyBhIHNlbGYtZXh0cmFjdGluZyBzdHViLCBvciBhbnkgcHJlZml4IGF0IGFsbApleGl0IDAKUEsDBC0AAAgAAAAAAADwcRrEKAAAACgAAAAKAAAAZmlsbGVyLnR4dHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHhQSwMELQAACAAAAAAAAG4qZtR1AAAAdQAAAAcAAABkb2Mudm54ewogICJ2bngiOiIwLjExLjAiLAogICJwb2ludHMiOiBbCiAgICB7CiAgICAgICJuYW1lIjogIkluIGEgY29udGFpbmVyIiwKICAgICAgImF0IjogWy0zLjY3NSwgNTcuMTIwMV0KICAgIH0KICBdCn0KICAgUEsBAi0ALQAACAAAAAAAAPBxGsT//////////woAHAAAAAAAAAAAAAAA/////2ZpbGxlci50eHQBABgAKAAAAAAAAAAoAAAAAAAAAMAAAAAAAAAAUEsBAi0ALQAACAAAAAAAAG4qZtT//////////wcAHAAAAAAAAAAAAAAA/////2RvYy52bngBABgAdQAAAAAAAAB1AAAAAAAAABABAAAAAAAAUEsGBiwAAAAAAAAALQAtAAAAAAAAAAAAAgAAAAAAAAACAAAAAAAAAKUAAAAAAAAAqgEAAAAAAABQSwYHAAAAAE8CAAAAAAAAAQAAAFBLBQb/////////////////////AAA="

  static let dataDescriptorBit3 =
    "UEsDBBQACAgAAAAAAAAAAAAAAAAAAAAAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMS4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLBwhuKmbUdQAAAHUAAABQSwMEFAAICAAAAAAAAAAAAAAAAAAAAAAAABQAAABhdHRhY2htZW50cy9ub3RlLnR4dGhlbGxvClBLBwggMDo2BgAAAAYAAABQSwECFAAUAAgIAAAAAAAAbipm1HUAAAB1AAAABwAAAAAAAAAAAAAAAAAAAAAAZG9jLnZueFBLAQIUABQACAgAAAAAAAAgMDo2BgAAAAYAAAAUAAAAAAAAAAAAAAAAAKoAAABhdHRhY2htZW50cy9ub3RlLnR4dFBLBQYAAAAAAgACAHcAAADyAAAAAAA="

  static let zip64CentralDirectory =
    "UEsDBC0AAAgAAAAAAABuKmbUdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMS4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLAQItAC0AAAgAAAAAAABuKmbU//////////8HABwAAAAAAAAAAAAAAP////9kb2Mudm54AQAYAHUAAAAAAAAAdQAAAAAAAAAAAAAAAAAAAFBLBgYsAAAAAAAAAC0ALQAAAAAAAAAAAAEAAAAAAAAAAQAAAAAAAABRAAAAAAAAAJoAAAAAAAAAUEsGBwAAAADrAAAAAAAAAAEAAABQSwUG/////////////////////wAA"
  static let zip64Deflate =
    "UEsDBC0AAAAIAAAAIQArbtxM//////////8HABQAZG9jLnZueAEAEAB1AAAAAAAAAF8AAAAAAAAAU1BQqOZSKsurULJSMtAzNNQzUNLhUirIz8wrKVayUojmAsrmJeamAtlKnnkKiQrJ+XkliZl5qUVAdQpgoJRYAlKpa6xnZm6qo2BqrmdoZGAYC5atBZKxXLUQhTCSCwBQSwMELQAAAAgAAAAhAEIqpf3//////////xUAFABhdHRhY2htZW50cy9waG90by50eHQBABAAEwAAAAAAAAAVAAAAAAAAAMvLL1EoSk3MyalUSFQoyMgvyecCAFBLAQItAy0AAAAIAAAAIQArbtxMXwAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAi0DLQAAAAgAAAAhAEIqpf0VAAAAEwAAABUAAAAAAAAAAAAAAIABmAAAAGF0dGFjaG1lbnRzL3Bob3RvLnR4dFBLBQYAAAAAAgACAHgAAAD0AAAAAAA="
  static let largeDeflate =
    "UEsDBBQAAAAIAAAAIQD/8AL8ygAAADRFAAAHAAAAZG9jLnZueO3LSw6CQBCE4T2n6LBGAhok8QaewbAYcVR8zCAvUcLdZSbxFv+mk6r6egpEwsGM4U7CJE7TOAkjV9W2Ml27tIcliUz+Lr1RT+3s3oiS0ppOVUY3/sfvqnM/q028zbNIsjxO10la+HVebhHMQdBdtbz6qrzLsbFvI2c7yq1/1q3YQTfi5of6fuRkLz5gsVgsFovFYrFYLBaLxWKxWCwWi8VisVgsFovFYrFYLBaLxWKxWCwWi8VisVgsFovFYrFYLBaLxWKxWOzf/gBQSwECFAMUAAAACAAAACEA//AC/MoAAAA0RQAABwAAAAAAAAAAAAAAgAEAAAAAZG9jLnZueFBLBQYAAAAAAQABADUAAADvAAAAAAA="
  static let bzip2Attachment =
    "UEsDBBQAAAAIAAAAIQArbtxMXwAAAHUAAAAHAAAAZG9jLnZueFNQUKjmUirLq1CyUjLQMzTUM1DS4VIqyM/MKylWslKI5gLK5iXmpgLZSp55CokKyfl5JYmZealFQHUKYKCUWAJSqWusZ2ZuqqNgaq5naGRgGAuWrQWSsVy1EIUwkgsAUEsDBC4AAAAMAAAAIQCma8t+JwAAAMgAAAANAAAAbm90ZXMuYnoyLnR4dEJaaDkxQVkmU1lBGPknAAAAAIQAQAQAIAAhAIKDF3JFOFCQQRj5J1BLAQIUAxQAAAAIAAAAIQArbtxMXwAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAi4DLgAAAAwAAAAhAKZry34nAAAAyAAAAA0AAAAAAAAAAAAAAIABhAAAAG5vdGVzLmJ6Mi50eHRQSwUGAAAAAAIAAgBwAAAA1gAAAAAA"
  static let bzip2Root =
    "UEsDBC4AAAAMAAAAIQB9ju+mgAAAAHUAAAAHAAAAZG9jLnZueEJaaDkxQVkmU1nF2LU2AAA5X4BQEFAHe5AAIAAKKiPd6iAAdCKek9J6j1AAGQPU9INRoQDQ2oaaeoBoYBA6ZhfwY4BVSlZAg4gSR5xbNO/GwCwYkcbE2kcHA+iBo9I1TAQGIW5kxelqo+GpCzNKNyVggC6SiP4u5IpwoSGLsWpsUEsBAi4DLgAAAAwAAAAhAH2O76aAAAAAdQAAAAcAAAAAAAAAAAAAAIABAAAAAGRvYy52bnhQSwUGAAAAAAEAAQA1AAAApQAAAAAA"
  static let nestedDocDecoy =
    "UEsDBBQAAAAIAAAAIQArbtxMXwAAAHUAAAAMAAAAYS1leHBvcnQudm54U1BQqOZSKsurULJSMtAzNNQzUNLhUirIz8wrKVayUojmAsrmJeamAtlKnnkKiQrJ+XkliZl5qUVAdQpgoJRYAlKpa6xnZm6qo2BqrmdoZGAYC5atBZKxXLUQhTCSCwBQSwMEFAAAAAgAAAAhAGz9ciMRAAAADwAAAA4AAABuZXN0ZWQvZG9jLnZueKtWKsurULJSMtCz1DNQqgUAUEsBAhQDFAAAAAgAAAAhACtu3ExfAAAAdQAAAAwAAAAAAAAAAAAAAIABAAAAAGEtZXhwb3J0LnZueFBLAQIUAxQAAAAIAAAAIQBs/XIjEQAAAA8AAAAOAAAAAAAAAAAAAACAAYkAAABuZXN0ZWQvZG9jLnZueFBLBQYAAAAAAgACAHYAAADGAAAAAAA="
  static let twoCandidates =
    "UEsDBBQAAAAAAAAAIQBuKmbUdQAAAHUAAAAJAAAAZmlyc3Qudm54ewogICJ2bngiOiIwLjExLjAiLAogICJwb2ludHMiOiBbCiAgICB7CiAgICAgICJuYW1lIjogIkluIGEgY29udGFpbmVyIiwKICAgICAgImF0IjogWy0zLjY3NSwgNTcuMTIwMV0KICAgIH0KICBdCn0KICAgUEsDBBQAAAAAAAAAIQBs/XIjDwAAAA8AAAAKAAAAc2Vjb25kLnZueHsidm54IjoiMC45LjAifVBLAQIUAxQAAAAAAAAAIQBuKmbUdQAAAHUAAAAJAAAAAAAAAAAAAACAAQAAAABmaXJzdC52bnhQSwECFAMUAAAAAAAAACEAbP1yIw8AAAAPAAAACgAAAAAAAAAAAAAAgAGcAAAAc2Vjb25kLnZueFBLBQYAAAAAAgACAG8AAADTAAAAAAA="
  static let caseDuplicate =
    "UEsDBBQAAAAAAAAAIQBuKmbUdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMS4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLAwQUAAAAAAAAACEA3NQSHg8AAAAPAAAABwAAAERPQy5WTlh7InZueCI6IjAuOC4wIn1QSwECFAMUAAAAAAAAACEAbipm1HUAAAB1AAAABwAAAAAAAAAAAAAAgAEAAAAAZG9jLnZueFBLAQIUAxQAAAAAAAAAIQDc1BIeDwAAAA8AAAAHAAAAAAAAAAAAAACAAZoAAABET0MuVk5YUEsFBgAAAAACAAIAagAAAM4AAAAAAA=="
  static let dotsInName =
    "UEsDBBQAAAAAAAAAIQBuKmbUdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMS4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLAwQUAAAAAAAAACEAMsty6gQAAAAEAAAADgAAAGEuLmIvcGhvdG8uanBnanBlZ1BLAQIUAxQAAAAAAAAAIQBuKmbUdQAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAhQDFAAAAAAAAAAhADLLcuoEAAAABAAAAA4AAAAAAAAAAAAAAIABmgAAAGEuLmIvcGhvdG8uanBnUEsFBgAAAAACAAIAcQAAAMoAAAAAAA=="
  static let noDocument =
    "UEsDBBQAAAAAAAAAIQCGphA2BQAAAAUAAAAKAAAAcmVhZG1lLnR4dGhlbGxvUEsBAhQDFAAAAAAAAAAhAIamEDYFAAAABQAAAAoAAAAAAAAAAAAAAIABAAAAAHJlYWRtZS50eHRQSwUGAAAAAAEAAQA4AAAALQAAAAAA"

  /// The canonical document `makevnz` puts inside every corpus archive.
  static let document = """
    {
      "vnx": "0.11.0",
      "points": [
        {
          "name": "In a container",
          "at": [-3.675, 57.1201]
        }
      ]
    }

    """

  /// The body of `largeDeflate`'s single entry: long enough and repetitive
  /// enough that the compressor reaches for dynamic Huffman codes and
  /// back-references, which the corpus's 138-byte entries never do.
  static let largeDeflateBody: String =
    document + "\n" + String(repeating: "the quick brown fox jumps over the lazy dog ", count: 400)

  static func bytes(_ base64: String) -> Data { Data(base64Encoded: base64)! }
}

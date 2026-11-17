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
/// and `largeDeflate` was rebuilt again. A decoy entry no reader decodes
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
    "UEsDBAoAAAgAAAAAIQCl/73cdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLAwQKAAAIAAAAACEA+Q4LTgMAAAADAAAACAAAAMOGYWUudHh0YWUKUEsDBAoAAAgAAAAAIQCdatx0AgAAAAIAAAALAAAAenotbGFzdC50eHR6ClBLAwQKAAAIAAAAACEAB6Hq3QIAAAACAAAADwAAAGFhYS9lYXJsaWVyLnR4dGEKUEsBAgoACgAACAAAAAAhAKX/vdx1AAAAdQAAAAcAAAAAAAAAAAAAAAAAAAAAAGRvYy52bnhQSwECCgAKAAAIAAAAACEA+Q4LTgMAAAADAAAACAAAAAAAAAAAAAAAAACaAAAAw4ZhZS50eHRQSwECCgAKAAAIAAAAACEAnWrcdAIAAAACAAAACwAAAAAAAAAAAAAAAADDAAAAenotbGFzdC50eHRQSwECCgAKAAAIAAAAACEAB6Hq3QIAAAACAAAADwAAAAAAAAAAAAAAAADuAAAAYWFhL2VhcmxpZXIudHh0UEsFBgAAAAAEAAQA4QAAAB0BAAAAAA=="

  static let zip64PaddedCentralDirectory =
    "IyEvYmluL3NoCiMgYSBzZWxmLWV4dHJhY3Rpbmcgc3R1Yiwgb3IgYW55IHByZWZpeCBhdCBhbGwKZXhpdCAwCiMhL2Jpbi9zaAojIGEgc2VsZi1leHRyYWN0aW5nIHN0dWIsIG9yIGFueSBwcmVmaXggYXQgYWxsCmV4aXQgMAojIS9iaW4vc2gKIyBhIHNlbGYtZXh0cmFjdGluZyBzdHViLCBvciBhbnkgcHJlZml4IGF0IGFsbApleGl0IDAKUEsDBC0AAAgAAAAAAADwcRrEKAAAACgAAAAKAAAAZmlsbGVyLnR4dHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHhQSwMELQAACAAAAAAAAKX/vdx1AAAAdQAAAAcAAABkb2Mudm54ewogICJ2bngiOiIwLjEwLjAiLAogICJwb2ludHMiOiBbCiAgICB7CiAgICAgICJuYW1lIjogIkluIGEgY29udGFpbmVyIiwKICAgICAgImF0IjogWy0zLjY3NSwgNTcuMTIwMV0KICAgIH0KICBdCn0KICAgUEsBAi0ALQAACAAAAAAAAPBxGsT//////////woAHAAAAAAAAAAAAAAA/////2ZpbGxlci50eHQBABgAKAAAAAAAAAAoAAAAAAAAAMAAAAAAAAAAUEsBAi0ALQAACAAAAAAAAKX/vdz//////////wcAHAAAAAAAAAAAAAAA/////2RvYy52bngBABgAdQAAAAAAAAB1AAAAAAAAABABAAAAAAAAUEsGBiwAAAAAAAAALQAtAAAAAAAAAAAAAgAAAAAAAAACAAAAAAAAAKUAAAAAAAAAqgEAAAAAAABQSwYHAAAAAE8CAAAAAAAAAQAAAFBLBQb/////////////////////AAA="

  static let dataDescriptorBit3 =
    "UEsDBBQACAgAAAAAAAAAAAAAAAAAAAAAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLBwil/73cdQAAAHUAAABQSwMEFAAICAAAAAAAAAAAAAAAAAAAAAAAABQAAABhdHRhY2htZW50cy9ub3RlLnR4dGhlbGxvClBLBwggMDo2BgAAAAYAAABQSwECFAAUAAgIAAAAAAAApf+93HUAAAB1AAAABwAAAAAAAAAAAAAAAAAAAAAAZG9jLnZueFBLAQIUABQACAgAAAAAAAAgMDo2BgAAAAYAAAAUAAAAAAAAAAAAAAAAAKoAAABhdHRhY2htZW50cy9ub3RlLnR4dFBLBQYAAAAAAgACAHcAAADyAAAAAAA="

  static let zip64CentralDirectory =
    "UEsDBC0AAAgAAAAAAACl/73cdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLAQItAC0AAAgAAAAAAACl/73c//////////8HABwAAAAAAAAAAAAAAP////9kb2Mudm54AQAYAHUAAAAAAAAAdQAAAAAAAAAAAAAAAAAAAFBLBgYsAAAAAAAAAC0ALQAAAAAAAAAAAAEAAAAAAAAAAQAAAAAAAABRAAAAAAAAAJoAAAAAAAAAUEsGBwAAAADrAAAAAAAAAAEAAABQSwUG/////////////////////wAA"
  static let zip64Deflate =
    "UEsDBC0AAAAIAAAAIQBmiDT6//////////8HABQAZG9jLnZueAEAEAB1AAAAAAAAAF8AAAAAAAAAU1BQqOZSKsurULJSMtAzNNAzUNLhUirIz8wrKVayUojmAsrmJeamAtlKnnkKiQrJ+XkliZl5qUVAdQpgoJRYAlKpa6xnZm6qo2BqrmdoZGAYC5atBZKxXLUQhTCSCwBQSwMELQAAAAgAAAAhAEIqpf3//////////xUAFABhdHRhY2htZW50cy9waG90by50eHQBABAAEwAAAAAAAAAVAAAAAAAAAMvLL1EoSk3MyalUSFQoyMgvyecCAFBLAQItAy0AAAAIAAAAIQBmiDT6XwAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAi0DLQAAAAgAAAAhAEIqpf0VAAAAEwAAABUAAAAAAAAAAAAAAIABmAAAAGF0dGFjaG1lbnRzL3Bob3RvLnR4dFBLBQYAAAAAAgACAHgAAAD0AAAAAAA="
  static let largeDeflate =
    "UEsDBBQAAAAIAAAAIQC7w1CpygAAADRFAAAHAAAAZG9jLnZueO3LQU6DUBQF0DmreGFcCWhqE3fgGpoOsKKi9lMprdWmexcYuYgzucm797xLFpGf0jl/iLwsqrIo88VU7bs2DYexXY9XxGXOsU/1rpnsY4o6tl0a6jY1/fwz7/Uw/dzcFfer5SKWq6K6LavNvF7H3GTXLBvemvg6ttuPeOq77xQv3Tnej7v9IbpT08c0f9a/P/HcvQbLsizLsizLsizLsizLsizLsizLsizLsizLsizLsizLsizLsizLsizLsizLsizLsizLsux/+wdQSwECFAMUAAAACAAAACEAu8NQqcoAAAA0RQAABwAAAAAAAAAAAAAAgAEAAAAAZG9jLnZueFBLBQYAAAAAAQABADUAAADvAAAAAAA="
  static let bzip2Attachment =
    "UEsDBBQAAAAIAAAAIQBmiDT6XwAAAHUAAAAHAAAAZG9jLnZueFNQUKjmUirLq1CyUjLQMzTQM1DS4VIqyM/MKylWslKI5gLK5iXmpgLZSp55CokKyfl5JYmZealFQHUKYKCUWAJSqWusZ2ZuqqNgaq5naGRgGAuWrQWSsVy1EIUwkgsAUEsDBC4AAAAMAAAAIQCma8t+JwAAAMgAAAANAAAAbm90ZXMuYnoyLnR4dEJaaDkxQVkmU1lBGPknAAAAAIQAQAQAIAAhAIKDF3JFOFCQQRj5J1BLAQIUAxQAAAAIAAAAIQBmiDT6XwAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAi4DLgAAAAwAAAAhAKZry34nAAAAyAAAAA0AAAAAAAAAAAAAAIABhAAAAG5vdGVzLmJ6Mi50eHRQSwUGAAAAAAIAAgBwAAAA1gAAAAAA"
  static let bzip2Root =
    "UEsDBC4AAAAMAAAAIQB9ju+mgAAAAHUAAAAHAAAAZG9jLnZueEJaaDkxQVkmU1nF2LU2AAA5X4BQEFAHe5AAIAAKKiPd6iAAdCKek9J6j1AAGQPU9INRoQDQ2oaaeoBoYBA6ZhfwY4BVSlZAg4gSR5xbNO/GwCwYkcbE2kcHA+iBo9I1TAQGIW5kxelqo+GpCzNKNyVggC6SiP4u5IpwoSGLsWpsUEsBAi4DLgAAAAwAAAAhAH2O76aAAAAAdQAAAAcAAAAAAAAAAAAAAIABAAAAAGRvYy52bnhQSwUGAAAAAAEAAQA1AAAApQAAAAAA"
  static let nestedDocDecoy =
    "UEsDBBQAAAAIAAAAIQBmiDT6XwAAAHUAAAAMAAAAYS1leHBvcnQudm54U1BQqOZSKsurULJSMtAzNNAzUNLhUirIz8wrKVayUojmAsrmJeamAtlKnnkKiQrJ+XkliZl5qUVAdQpgoJRYAlKpa6xnZm6qo2BqrmdoZGAYC5atBZKxXLUQhTCSCwBQSwMEFAAAAAgAAAAhAGz9ciMRAAAADwAAAA4AAABuZXN0ZWQvZG9jLnZueKtWKsurULJSMtCz1DNQqgUAUEsBAhQDFAAAAAgAAAAhAGaINPpfAAAAdQAAAAwAAAAAAAAAAAAAAIABAAAAAGEtZXhwb3J0LnZueFBLAQIUAxQAAAAIAAAAIQBs/XIjEQAAAA8AAAAOAAAAAAAAAAAAAACAAYkAAABuZXN0ZWQvZG9jLnZueFBLBQYAAAAAAgACAHYAAADGAAAAAAA="
  static let twoCandidates =
    "UEsDBBQAAAAAAAAAIQCl/73cdQAAAHUAAAAJAAAAZmlyc3Qudm54ewogICJ2bngiOiIwLjEwLjAiLAogICJwb2ludHMiOiBbCiAgICB7CiAgICAgICJuYW1lIjogIkluIGEgY29udGFpbmVyIiwKICAgICAgImF0IjogWy0zLjY3NSwgNTcuMTIwMV0KICAgIH0KICBdCn0KICAgUEsDBBQAAAAAAAAAIQBs/XIjDwAAAA8AAAAKAAAAc2Vjb25kLnZueHsidm54IjoiMC45LjAifVBLAQIUAxQAAAAAAAAAIQCl/73cdQAAAHUAAAAJAAAAAAAAAAAAAACAAQAAAABmaXJzdC52bnhQSwECFAMUAAAAAAAAACEAbP1yIw8AAAAPAAAACgAAAAAAAAAAAAAAgAGcAAAAc2Vjb25kLnZueFBLBQYAAAAAAgACAG8AAADTAAAAAAA="
  static let caseDuplicate =
    "UEsDBBQAAAAAAAAAIQCl/73cdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLAwQUAAAAAAAAACEA3NQSHg8AAAAPAAAABwAAAERPQy5WTlh7InZueCI6IjAuOC4wIn1QSwECFAMUAAAAAAAAACEApf+93HUAAAB1AAAABwAAAAAAAAAAAAAAgAEAAAAAZG9jLnZueFBLAQIUAxQAAAAAAAAAIQDc1BIeDwAAAA8AAAAHAAAAAAAAAAAAAACAAZoAAABET0MuVk5YUEsFBgAAAAACAAIAagAAAM4AAAAAAA=="
  static let dotsInName =
    "UEsDBBQAAAAAAAAAIQCl/73cdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAicG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9CiAgIFBLAwQUAAAAAAAAACEAMsty6gQAAAAEAAAADgAAAGEuLmIvcGhvdG8uanBnanBlZ1BLAQIUAxQAAAAAAAAAIQCl/73cdQAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAhQDFAAAAAAAAAAhADLLcuoEAAAABAAAAA4AAAAAAAAAAAAAAIABmgAAAGEuLmIvcGhvdG8uanBnUEsFBgAAAAACAAIAcQAAAMoAAAAAAA=="
  static let noDocument =
    "UEsDBBQAAAAAAAAAIQCGphA2BQAAAAUAAAAKAAAAcmVhZG1lLnR4dGhlbGxvUEsBAhQDFAAAAAAAAAAhAIamEDYFAAAABQAAAAoAAAAAAAAAAAAAAIABAAAAAHJlYWRtZS50eHRQSwUGAAAAAAEAAQA4AAAALQAAAAAA"

  /// The canonical document `makevnz` puts inside every corpus archive.
  static let document = """
    {
      "vnx": "0.10.0",
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

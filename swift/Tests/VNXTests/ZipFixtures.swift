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
/// byte for byte). A decoy entry no reader decodes
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
    "UEsDBAoAAAgAAAAAIQA5S/lDdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAid2F5cG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9ClBLAwQKAAAIAAAAACEA+Q4LTgMAAAADAAAACAAAAMOGYWUudHh0YWUKUEsDBAoAAAgAAAAAIQCdatx0AgAAAAIAAAALAAAAenotbGFzdC50eHR6ClBLAwQKAAAIAAAAACEAB6Hq3QIAAAACAAAADwAAAGFhYS9lYXJsaWVyLnR4dGEKUEsBAgoACgAACAAAAAAhADlL+UN1AAAAdQAAAAcAAAAAAAAAAAAAAAAAAAAAAGRvYy52bnhQSwECCgAKAAAIAAAAACEA+Q4LTgMAAAADAAAACAAAAAAAAAAAAAAAAACaAAAAw4ZhZS50eHRQSwECCgAKAAAIAAAAACEAnWrcdAIAAAACAAAACwAAAAAAAAAAAAAAAADDAAAAenotbGFzdC50eHRQSwECCgAKAAAIAAAAACEAB6Hq3QIAAAACAAAADwAAAAAAAAAAAAAAAADuAAAAYWFhL2VhcmxpZXIudHh0UEsFBgAAAAAEAAQA4QAAAB0BAAAAAA=="

  static let zip64PaddedCentralDirectory =
    "IyEvYmluL3NoCiMgYSBzZWxmLWV4dHJhY3Rpbmcgc3R1Yiwgb3IgYW55IHByZWZpeCBhdCBhbGwKZXhpdCAwCiMhL2Jpbi9zaAojIGEgc2VsZi1leHRyYWN0aW5nIHN0dWIsIG9yIGFueSBwcmVmaXggYXQgYWxsCmV4aXQgMAojIS9iaW4vc2gKIyBhIHNlbGYtZXh0cmFjdGluZyBzdHViLCBvciBhbnkgcHJlZml4IGF0IGFsbApleGl0IDAKUEsDBC0AAAgAAAAAAADwcRrEKAAAACgAAAAKAAAAZmlsbGVyLnR4dHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHhQSwMELQAACAAAAAAAADlL+UN1AAAAdQAAAAcAAABkb2Mudm54ewogICJ2bngiOiIwLjEwLjAiLAogICJ3YXlwb2ludHMiOiBbCiAgICB7CiAgICAgICJuYW1lIjogIkluIGEgY29udGFpbmVyIiwKICAgICAgImF0IjogWy0zLjY3NSwgNTcuMTIwMV0KICAgIH0KICBdCn0KUEsBAi0ALQAACAAAAAAAAPBxGsT//////////woAHAAAAAAAAAAAAAAA/////2ZpbGxlci50eHQBABgAKAAAAAAAAAAoAAAAAAAAAMAAAAAAAAAAUEsBAi0ALQAACAAAAAAAADlL+UP//////////wcAHAAAAAAAAAAAAAAA/////2RvYy52bngBABgAdQAAAAAAAAB1AAAAAAAAABABAAAAAAAAUEsGBiwAAAAAAAAALQAtAAAAAAAAAAAAAgAAAAAAAAACAAAAAAAAAKUAAAAAAAAAqgEAAAAAAABQSwYHAAAAAE8CAAAAAAAAAQAAAFBLBQb/////////////////////AAA="

  static let dataDescriptorBit3 =
    "UEsDBBQACAgAAAAAAAAAAAAAAAAAAAAAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAid2F5cG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9ClBLBwg5S/lDdQAAAHUAAABQSwMEFAAICAAAAAAAAAAAAAAAAAAAAAAAABQAAABhdHRhY2htZW50cy9ub3RlLnR4dGhlbGxvClBLBwggMDo2BgAAAAYAAABQSwECFAAUAAgIAAAAAAAAOUv5Q3UAAAB1AAAABwAAAAAAAAAAAAAAAAAAAAAAZG9jLnZueFBLAQIUABQACAgAAAAAAAAgMDo2BgAAAAYAAAAUAAAAAAAAAAAAAAAAAKoAAABhdHRhY2htZW50cy9ub3RlLnR4dFBLBQYAAAAAAgACAHcAAADyAAAAAAA="

  static let zip64CentralDirectory =
    "UEsDBC0AAAgAAAAAAAA5S/lDdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAid2F5cG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9ClBLAQItAC0AAAgAAAAAAAA5S/lD//////////8HABwAAAAAAAAAAAAAAP////9kb2Mudm54AQAYAHUAAAAAAAAAdQAAAAAAAAAAAAAAAAAAAFBLBgYsAAAAAAAAAC0ALQAAAAAAAAAAAAEAAAAAAAAAAQAAAAAAAABRAAAAAAAAAJoAAAAAAAAAUEsGBwAAAADrAAAAAAAAAAEAAABQSwUG/////////////////////wAA"
  static let zip64Deflate =
    "UEsDBC0AAAAIAAAAIQCfbQLK//////////8HABQAZG9jLnZueAEAEAB1AAAAAAAAAF8AAAAAAAAAq+ZSKsurULJSMtAzNNAzUNLhUipPrCzIz8wrKVayUojmquZSykvMTQWylTzzFBIVkvPzShIz81KLgEoVwEApsQSkUtdYz8zcVEfB1FzP0MjAMBYsWwskY7lqFVAAFwBQSwMELQAAAAgAAAAhAEIqpf3//////////xUAFABhdHRhY2htZW50cy9waG90by50eHQBABAAEwAAAAAAAAAVAAAAAAAAAMvLL1EoSk3MyalUSFQoyMgvyecCAFBLAQItAy0AAAAIAAAAIQCfbQLKXwAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAi0DLQAAAAgAAAAhAEIqpf0VAAAAEwAAABUAAAAAAAAAAAAAAIABmAAAAGF0dGFjaG1lbnRzL3Bob3RvLnR4dFBLBQYAAAAAAgACAHgAAAD0AAAAAAA="
  static let largeDeflate =
    "UEsDBBQAAAAIAAAAIQC3OkctygAAADdFAAAHAAAAZG9jLnZueO3LOw6CQBRG4Z5V3FAjAQ2SuAPXYChGRMXHDPJGwt4dpnIRp7nFf747eyJ+r0f/IH4UxlEY+cE6DWqqTKnbxoaTHURmd23S6l2s/KhFSW50q0pd1O7NddWuP5tduE+TQJI0jLdRnLm62Jt5i+e190I+XZk/5VybQcvVjPLo3lUjpi9qWfNLfSe5mJtgsVgsFovFYrFYLBaLxWKxWCwWi8VisVgsFovFYrFYLBaLxWKxWCwWi8VisVgsFovFYrFYLBaLxWKxWOy//QFQSwECFAMUAAAACAAAACEAtzpHLcoAAAA3RQAABwAAAAAAAAAAAAAAgAEAAAAAZG9jLnZueFBLBQYAAAAAAQABADUAAADvAAAAAAA="
  static let bzip2Attachment =
    "UEsDBBQAAAAIAAAAIQCfbQLKXwAAAHUAAAAHAAAAZG9jLnZueKvmUirLq1CyUjLQMzTQM1DS4VIqT6wsyM/MKylWslKI5qrmUspLzE0FspU88xQSFZLz80oSM/NSi4BKFcBAKbEEpFLXWM/M3FRHwdRcz9DIwDAWLFsLJGO5ahVQABcAUEsDBC4AAAAMAAAAIQCma8t+JwAAAMgAAAANAAAAbm90ZXMuYnoyLnR4dEJaaDkxQVkmU1lBGPknAAAAAIQAQAQAIAAhAIKDF3JFOFCQQRj5J1BLAQIUAxQAAAAIAAAAIQCfbQLKXwAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAi4DLgAAAAwAAAAhAKZry34nAAAAyAAAAA0AAAAAAAAAAAAAAIABhAAAAG5vdGVzLmJ6Mi50eHRQSwUGAAAAAAIAAgBwAAAA1gAAAAAA"
  static let bzip2Root =
    "UEsDBC4AAAAMAAAAIQB9ju+mgAAAAHUAAAAHAAAAZG9jLnZueEJaaDkxQVkmU1nF2LU2AAA5X4BQEFAHe5AAIAAKKiPd6iAAdCKek9J6j1AAGQPU9INRoQDQ2oaaeoBoYBA6ZhfwY4BVSlZAg4gSR5xbNO/GwCwYkcbE2kcHA+iBo9I1TAQGIW5kxelqo+GpCzNKNyVggC6SiP4u5IpwoSGLsWpsUEsBAi4DLgAAAAwAAAAhAH2O76aAAAAAdQAAAAcAAAAAAAAAAAAAAIABAAAAAGRvYy52bnhQSwUGAAAAAAEAAQA1AAAApQAAAAAA"
  static let nestedDocDecoy =
    "UEsDBBQAAAAIAAAAIQCfbQLKXwAAAHUAAAAMAAAAYS1leHBvcnQudm54q+ZSKsurULJSMtAzNNAzUNLhUipPrCzIz8wrKVayUojmquZSykvMTQWylTzzFBIVkvPzShIz81KLgEoVwEApsQSkUtdYz8zcVEfB1FzP0MjAMBYsWwskY7lqFVAAFwBQSwMEFAAAAAgAAAAhAGz9ciMRAAAADwAAAA4AAABuZXN0ZWQvZG9jLnZueKtWKsurULJSMtCz1DNQqgUAUEsBAhQDFAAAAAgAAAAhAJ9tAspfAAAAdQAAAAwAAAAAAAAAAAAAAIABAAAAAGEtZXhwb3J0LnZueFBLAQIUAxQAAAAIAAAAIQBs/XIjEQAAAA8AAAAOAAAAAAAAAAAAAACAAYkAAABuZXN0ZWQvZG9jLnZueFBLBQYAAAAAAgACAHYAAADGAAAAAAA="
  static let twoCandidates =
    "UEsDBBQAAAAAAAAAIQA5S/lDdQAAAHUAAAAJAAAAZmlyc3Qudm54ewogICJ2bngiOiIwLjEwLjAiLAogICJ3YXlwb2ludHMiOiBbCiAgICB7CiAgICAgICJuYW1lIjogIkluIGEgY29udGFpbmVyIiwKICAgICAgImF0IjogWy0zLjY3NSwgNTcuMTIwMV0KICAgIH0KICBdCn0KUEsDBBQAAAAAAAAAIQBs/XIjDwAAAA8AAAAKAAAAc2Vjb25kLnZueHsidm54IjoiMC45LjAifVBLAQIUAxQAAAAAAAAAIQA5S/lDdQAAAHUAAAAJAAAAAAAAAAAAAACAAQAAAABmaXJzdC52bnhQSwECFAMUAAAAAAAAACEAbP1yIw8AAAAPAAAACgAAAAAAAAAAAAAAgAGcAAAAc2Vjb25kLnZueFBLBQYAAAAAAgACAG8AAADTAAAAAAA="
  static let caseDuplicate =
    "UEsDBBQAAAAAAAAAIQA5S/lDdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAid2F5cG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9ClBLAwQUAAAAAAAAACEA3NQSHg8AAAAPAAAABwAAAERPQy5WTlh7InZueCI6IjAuOC4wIn1QSwECFAMUAAAAAAAAACEAOUv5Q3UAAAB1AAAABwAAAAAAAAAAAAAAgAEAAAAAZG9jLnZueFBLAQIUAxQAAAAAAAAAIQDc1BIeDwAAAA8AAAAHAAAAAAAAAAAAAACAAZoAAABET0MuVk5YUEsFBgAAAAACAAIAagAAAM4AAAAAAA=="
  static let dotsInName =
    "UEsDBBQAAAAAAAAAIQA5S/lDdQAAAHUAAAAHAAAAZG9jLnZueHsKICAidm54IjoiMC4xMC4wIiwKICAid2F5cG9pbnRzIjogWwogICAgewogICAgICAibmFtZSI6ICJJbiBhIGNvbnRhaW5lciIsCiAgICAgICJhdCI6IFstMy42NzUsIDU3LjEyMDFdCiAgICB9CiAgXQp9ClBLAwQUAAAAAAAAACEAMsty6gQAAAAEAAAADgAAAGEuLmIvcGhvdG8uanBnanBlZ1BLAQIUAxQAAAAAAAAAIQA5S/lDdQAAAHUAAAAHAAAAAAAAAAAAAACAAQAAAABkb2Mudm54UEsBAhQDFAAAAAAAAAAhADLLcuoEAAAABAAAAA4AAAAAAAAAAAAAAIABmgAAAGEuLmIvcGhvdG8uanBnUEsFBgAAAAACAAIAcQAAAMoAAAAAAA=="
  static let noDocument =
    "UEsDBBQAAAAAAAAAIQCGphA2BQAAAAUAAAAKAAAAcmVhZG1lLnR4dGhlbGxvUEsBAhQDFAAAAAAAAAAhAIamEDYFAAAABQAAAAoAAAAAAAAAAAAAAIABAAAAAHJlYWRtZS50eHRQSwUGAAAAAAEAAQA4AAAALQAAAAAA"

  /// The canonical document `makevnz` puts inside every corpus archive.
  static let document = """
    {
      "vnx": "0.10.0",
      "waypoints": [
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

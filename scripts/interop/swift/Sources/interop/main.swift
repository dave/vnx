// Read a VNX document with the Swift implementation and write it back out.
//
// One of six drivers, one per language; scripts/interop.py runs each over each
// other's output. No fallback: a refusal, a diagnostic, or an unwritable path
// exits non-zero with the cause on stderr.

import Foundation
import VNX

let args = CommandLine.arguments
guard args.count == 3 else {
    FileHandle.standardError.write(Data("usage: interop <in.vnx> <out.vnx>\n".utf8))
    exit(2)
}

func die(_ message: String) -> Never {
    FileHandle.standardError.write(Data("interop: \(message)\n".utf8))
    exit(1)
}

guard let input = FileManager.default.contents(atPath: args[1]) else {
    die("read: cannot read \(args[1])")
}

let result: VNX.ReadResult
do {
    result = try VNX.read(input)
} catch let refusal as Refusal {
    die("refused: \(refusal.message)")
} catch {
    die("refused: \(error)")
}

for d in result.diagnostics {
    FileHandle.standardError.write(
        Data("interop: diagnostic \(d.code) at \(d.path): \(d.message)\n".utf8))
}
if !result.diagnostics.isEmpty { exit(1) }

let output: Data
do {
    output = try VNX.write(result.document)
} catch {
    die("encode: \(error)")
}
do {
    try output.write(to: URL(fileURLWithPath: args[2]))
} catch {
    die("write: \(error)")
}

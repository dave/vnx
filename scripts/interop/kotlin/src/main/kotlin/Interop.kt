// Read a VNX document with the Kotlin implementation and write it back out.
//
// One of six drivers, one per language; scripts/interop.py runs each over each
// other's output. No fallback: a refusal, a diagnostic, or an unwritable path
// exits non-zero with the cause on stderr.

import app.viatrail.vnx.Vnx
import app.viatrail.vnx.VnxRefusal
import java.io.File
import kotlin.system.exitProcess

fun main(args: Array<String>) {
    if (args.size != 2) {
        System.err.println("usage: interop <in.vnx> <out.vnx>")
        exitProcess(2)
    }
    val input = File(args[0])
    if (!input.isFile) {
        System.err.println("interop: read: no such file ${args[0]}")
        exitProcess(1)
    }
    val result =
        try {
            Vnx.readDocument(input.readBytes())
        } catch (refusal: VnxRefusal) {
            System.err.println("interop: refused: ${refusal.message}")
            exitProcess(1)
        }
    for (d in result.diagnostics) {
        System.err.println("interop: diagnostic ${d.cause.corpusCode} at ${d.path}: ${d.message}")
    }
    if (result.diagnostics.isNotEmpty()) exitProcess(1)
    File(args[1]).writeText(Vnx.write(result.document))
}

package app.viatrail.vnx

import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlin.test.fail

/**
 * The migration table from English substrings to reason codes.
 *
 * The corpus matches refusals and diagnostics as English substrings today, which
 * makes the wording load-bearing for everyone who did not write it. Every
 * message this library emits therefore carries a stable [DiagnosticCause] or
 * [RefusalCause] beside its text, and each cause records the substring the
 * corpus expects. When the corpus grows a reason-code field, that table IS the
 * migration.
 *
 * These tests keep the table honest in both directions: every entry really is a
 * substring of what the library says, and every substring the corpus expects has
 * an entry.
 */
class WordingTest {

    private val corpus = File(System.getProperty("vnx.corpus")!!)

    private fun read(json: String): ReadResult = Vnx.readDocument(json)

    private fun causeOf(json: String, cause: DiagnosticCause): Diagnostic {
        val diagnostics = read(json).diagnostics
        return diagnostics.firstOrNull { it.cause == cause }
            ?: fail("no $cause diagnostic; got: $diagnostics")
    }

    private fun refusalOf(json: String): VnxRefusal =
        try {
            read(json)
            fail("expected a refusal from: $json")
        } catch (e: VnxRefusal) {
            e
        }

    @Test
    fun `each diagnostic cause emits the code and the advisory wording its table claims`() {
        val provoked = mapOf(
            DiagnosticCause.WRONG_TYPE_OPTIONAL to causeOf(
                """{"vnx":"0.1.0","waypoints":[{"at":[-3,57],"hidden":"yes"}]}""",
                DiagnosticCause.WRONG_TYPE_OPTIONAL,
            ),
            DiagnosticCause.WRONG_TYPE_REQUIRED to causeOf(
                """{"vnx":"0.1.0","waypoints":[{"at":"beside the burn"}]}""",
                DiagnosticCause.WRONG_TYPE_REQUIRED,
            ),
            DiagnosticCause.POSITION_UNUSABLE to causeOf(
                """{"vnx":"0.1.0","tracks":[{"segments":[{"pips":[[-3,57],[null,57]]}]}]}""",
                DiagnosticCause.POSITION_UNUSABLE,
            ),
            DiagnosticCause.UNKNOWN_KEY to causeOf(
                """{"vnx":"0.1.0","waypoints":[{"nmae":"typo","at":[-3,57]}]}""",
                DiagnosticCause.UNKNOWN_KEY,
            ),
            DiagnosticCause.TIMESTAMP_NOT_UTC to causeOf(
                """{"vnx":"0.1.0","tracks":[{"started":"2026-05-12T08:03:11+01:00","segments":[]}]}""",
                DiagnosticCause.TIMESTAMP_NOT_UTC,
            ),
            DiagnosticCause.POLYGON_NESTED_RINGS to causeOf(
                """{"vnx":"0.1.0","areas":[{"polygon":[[[-3,57],[-2,57],[-2,58],[-3,57]],[[-3,57],[-2,57],[-2,58],[-3,57]]]}]}""",
                DiagnosticCause.POLYGON_NESTED_RINGS,
            ),
            DiagnosticCause.LINK_ARC_ABSENT to causeOf(
                """{"vnx":"0.1.0","networks":[{"arcs":{},"nodes":{},"chains":[{"links":[{"arc":"ghost","from":"a","to":"b"}]}]}]}""",
                DiagnosticCause.LINK_ARC_ABSENT,
            ),
            DiagnosticCause.INLINE_ENDPOINT_BEYOND_TOLERANCE to causeOf(
                """{"vnx":"0.1.0","networks":[{"nodes":{"a":[-3,55],"b":[-3,68]},
                   "arcs":{"up":{"from":"a","to":"b","pips":[[-3,55],[-3,68]]}},
                   "chains":[{"links":[{"arc":"up","from":"a","to":[-2.99,67.5]}]}]}]}""",
                DiagnosticCause.INLINE_ENDPOINT_BEYOND_TOLERANCE,
            ),
            DiagnosticCause.CHAIN_NOT_CONNECTED to causeOf(
                """{"vnx":"0.1.0","networks":[{"nodes":{"a":[-3.68,57.12],"b":[-3.67,57.11],"c":[-3.6,57.05],"d":[-3.59,57.04]},
                   "arcs":{"ab":{"from":"a","to":"b","pips":[[-3.68,57.12],[-3.67,57.11]]},
                           "cd":{"from":"c","to":"d","pips":[[-3.6,57.05],[-3.59,57.04]]}},
                   "chains":[{"links":[{"arc":"ab","from":"a","to":"b"},{"arc":"cd","from":"c","to":"d"}]}]}]}""",
                DiagnosticCause.CHAIN_NOT_CONNECTED,
            ),
            // The node sits 100 m north of the pip the arc's `from` end names,
            // and the arc's other end coincides exactly — so this provokes the
            // cause once rather than twice.
            DiagnosticCause.TERMINAL_PIP_OFF_NODE to causeOf(
                """{"vnx":"0.1.0","networks":[{"nodes":{"a":[-3.68,57.1209],"b":[-3.67,57.11]},
                   "arcs":{"ab":{"from":"a","to":"b","pips":[[-3.68,57.12],[-3.67,57.11]]}}}]}""",
                DiagnosticCause.TERMINAL_PIP_OFF_NODE,
            ),
            // The one cause that needs a CONTAINER rather than a document: an
            // archive entry compressed by a method neither stored nor deflate.
            // The corpus case is the provocation because no ZIP writer here can
            // produce a method it has no compressor for.
            DiagnosticCause.ATTACHMENT_UNREADABLE to
                Vnx.readContainer(File(corpus, "diagnose/container-unusable-method.vnz").readBytes())
                    .diagnostics
                    .firstOrNull { it.cause == DiagnosticCause.ATTACHMENT_UNREADABLE }
                    .let { it ?: fail("the corpus case provoked no ATTACHMENT_UNREADABLE diagnostic") },
        )
        for (cause in DiagnosticCause.entries) {
            val code = cause.corpusCode ?: continue
            val diagnostic = provoked[cause] ?: fail("$cause claims the code \"$code\" but nothing provokes it here")
            // The commitment.
            assertEquals(code, diagnostic.code, "$cause reported the wrong reason code")
            // The quality bar. Nothing compares this; a message that does not name
            // its cause is still a bad message.
            val substring = cause.corpusSubstring
            if (substring != null) {
                assertTrue(
                    diagnostic.message.contains(substring),
                    "$cause claims the advisory wording \"$substring\" but says: ${diagnostic.message}",
                )
            }
        }
    }

    @Test
    fun `each refusal cause emits the substrings its table entry claims`() {
        // A LIST of provocations rather than a map, because a cause is not a
        // sentence. VERSION_SERIES_UNSUPPORTED is reached from two directions and
        // section 2.2 wants the refusal to say which, so it appears twice — and a
        // reader that answered both directions with the same sentence would leave
        // one of its two claimed substrings unprovoked and fail below.
        val provoked = listOf(
            RefusalCause.MALFORMED_JSON to refusalOf("""{"vnx":"0.1.0","waypoints":[{"at":[NaN,57]}]}"""),
            RefusalCause.NOT_A_VNX_DOCUMENT to refusalOf("""{"profile":"full"}"""),
            RefusalCause.VERSION_MALFORMED to refusalOf("""{"vnx":"0.1"}"""),
            RefusalCause.VERSION_SERIES_UNSUPPORTED to refusalOf("""{"vnx":"9.9.9"}"""),
            RefusalCause.VERSION_SERIES_UNSUPPORTED to refusalOf("""{"vnx":"0.0.9"}"""),
            RefusalCause.CONTAINER_DUPLICATE_ENTRY to containerRefusal("reject/container-duplicate-entries.vnz"),
            RefusalCause.CONTAINER_PATH_ESCAPES to containerRefusal("reject/container-escapes.vnz"),
        )
        for ((cause, refusal) in provoked) {
            assertEquals(cause, refusal.refusal, "the wrong cause was raised: ${refusal.message}")
        }
        for (cause in RefusalCause.entries) {
            val messages = provoked.filter { it.first == cause }.map { it.second.message }
            for (substring in cause.corpusSubstrings) {
                if (messages.isEmpty()) fail("$cause claims \"$substring\" and nothing provokes it here")
                assertTrue(
                    messages.any { it.contains(substring) },
                    "$cause claims \"$substring\" and no provocation says it. Got: $messages",
                )
            }
            // The other direction: a provocation whose message claims none of the
            // cause's substrings is wording that has drifted from the table.
            for (message in messages) {
                assertTrue(
                    cause.corpusSubstrings.any { message.contains(it) },
                    "$cause says \"$message\", which claims none of ${cause.corpusSubstrings.toList()}",
                )
            }
        }
    }

    private fun containerRefusal(path: String): VnxRefusal = try {
        Vnx.readContainer(File(corpus, path).readBytes())
        fail("$path must be refused")
    } catch (e: VnxRefusal) {
        e
    }

    @Test
    fun `every code the corpus pins has a table entry, and every entry is reachable`() {
        // Both directions. The first catches a code the corpus added that this
        // library cannot emit; the second catches a code this library claims that
        // the corpus has retired, which would otherwise sit here looking checked.
        val claimed = RefusalCause.entries.flatMap { it.corpusSubstrings.toList() }.toSet()
        val corpusCodes = HashSet<String>()
        val unclaimed = ArrayList<String>()
        for (tier in listOf("accept", "reject", "diagnose")) {
            for (file in File(corpus, tier).listFiles()!!.filter { it.name.endsWith(".expect.json") }) {
                val sidecar = Sidecar.read(file)
                sidecar.refusal?.let { if (it !in claimed) unclaimed.add("${file.name}: refusal \"$it\"") }
                sidecar.diagnostics()?.forEach { expected ->
                    corpusCodes.add(expected.code)
                    if (DiagnosticCause.entries.none { it.corpusCode == expected.code }) {
                        unclaimed.add("${file.name}: reason code \"${expected.code}\" maps to no cause")
                    }
                }
            }
        }
        assertTrue(
            unclaimed.isEmpty(),
            "the corpus pins expectations no cause claims:\n" + unclaimed.joinToString("\n"),
        )
        // And the refusal table's own direction: a substring claimed here that no
        // sidecar pins any more sits looking checked and is not.
        val pinned = HashSet<String>()
        for (tier in listOf("accept", "reject", "diagnose")) {
            for (file in File(corpus, tier).listFiles()!!.filter { it.name.endsWith(".expect.json") }) {
                Sidecar.read(file).refusal?.let { pinned.add(it) }
            }
        }
        val stale = claimed.filter { it !in pinned }
        assertTrue(stale.isEmpty(), "these refusal substrings are claimed and no corpus case pins them: $stale")

        val orphaned = DiagnosticCause.entries.mapNotNull { it.corpusCode }.filter { it !in corpusCodes }
        assertTrue(
            orphaned.isEmpty(),
            "these causes claim a reason code the corpus no longer pins, so they look checked and are not: $orphaned",
        )
        assertEquals(
            11, corpusCodes.size,
            "the corpus pinned eleven codes on 2026-09-10; it now pins ${corpusCodes.size}. " +
                "A new one is a deliberate edit here, not a silent widening.",
        )
    }
}

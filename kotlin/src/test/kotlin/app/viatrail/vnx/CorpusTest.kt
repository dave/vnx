package app.viatrail.vnx

import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlin.test.fail

/**
 * The conformance corpus. SPEC.md section 1.3: an implementation conforms when
 * it passes this, and where the prose and the corpus disagree the disagreement
 * is a defect in one of them and is reported rather than resolved by
 * interpretation.
 *
 * Every case is run. A file with no sidecar is a failure, not a skip: the file
 * everyone assumes is covered is otherwise exactly the one that is not.
 */
class CorpusTest {

    private val corpus = File(
        System.getProperty("vnx.corpus")
            ?: error("the corpus location must be passed as -Dvnx.corpus; see build.gradle.kts"),
    )

    private fun cases(tier: String): List<File> {
        val dir = File(corpus, tier)
        assertTrue(dir.isDirectory, "the corpus tier $tier is missing at ${dir.absolutePath}")
        return dir.listFiles()!!
            .filter { it.name.endsWith(".vnx") || it.name.endsWith(".vnz") }
            .sortedBy { it.name }
    }

    private fun sidecarFor(case: File): Sidecar {
        val sidecar = File(case.parentFile, case.name.substringBeforeLast('.') + ".expect.json")
        assertTrue(sidecar.isFile, "${case.name} has no ${sidecar.name}: a file with no sidecar is a failure, not a skip")
        return Sidecar.read(sidecar)
    }

    // ------------------------------------------------------------- invariants

    @Test
    fun `every case carries a well-formed sidecar whose kind matches its directory`() {
        for (tier in listOf("accept", "reject", "diagnose")) {
            eachCase(tier, cases(tier), { it.name }) { case ->
                val sidecar = sidecarFor(case)
                assertEquals(tier, sidecar.kind, "${case.name}: `kind` must match the directory")
                assertTrue(sidecar.note.isNotBlank(), "${case.name}: `note` says why the case exists and is required")
                assertTrue(
                    sidecar.unknownFields.isEmpty(),
                    "${case.name}: the sidecar carries fields this runner does not read: ${sidecar.unknownFields}. " +
                        "A field nothing looks at is a field that quietly stops being tested.",
                )
            }
        }
    }

    @Test
    fun `the corpus has at least the cases section 8 requires`() {
        // A guard against a tier being emptied or a directory being renamed: the
        // runner would otherwise pass with nothing to run.
        assertTrue(cases("accept").size >= 14, "the accept tier has shrunk: ${cases("accept").size} cases")
        assertTrue(cases("reject").size >= 7, "the reject tier has shrunk: ${cases("reject").size} cases")
        assertTrue(cases("diagnose").size >= 6, "the diagnose tier has shrunk: ${cases("diagnose").size} cases")
        assertTrue(writePairs().isNotEmpty(), "the write tier is empty")
        assertTrue(writeContainerCases().isNotEmpty(), "the write tier holds no container case")
    }

    // ---------------------------------------------------------------- accept

    @Test
    fun accept() {
        eachCase("accept", cases("accept"), { it.name }) { case ->
            val sidecar = sidecarFor(case)
            val result = try {
                Vnx.read(case)
            } catch (e: VnxRefusal) {
                fail("${case.name} must be read, and was refused: $e")
            }
            assertEquals(
                emptyList(), result.diagnostics,
                "${case.name} must be read with no diagnostics",
            )
            checkCounts(case, sidecar, result)
            checkPositions(case, sidecar, result)
            checkTracks(case, sidecar, result)
            checkAttachments(case, sidecar, result)
            if (case.name.endsWith(".vnx")) checkTree(case, result)
        }
    }

    /**
     * SPEC.md section 8.1: the runner decodes the file, ENCODES the decoded
     * document, and compares the tree parsed from the original file against the
     * tree parsed from the output.
     *
     * TREES, not bytes: key order, number spelling and whitespace are the
     * writer's own business. The shortest spelling of a position is applied to
     * BOTH sides, because this tier compares a document against a re-encoding of
     * itself and would otherwise require the file to have been written by this
     * library. VALUES ARE NOT NORMALISED: section 4.1 stopped requiring a writer
     * to round, so a value that moved here is a defect and must show as one.
     *
     * And against the FILE, never against another model. AcceptTierTest holds
     * that distinction, and it is the whole worth of this check.
     */
    private fun checkTree(case: File, result: ReadResult) {
        val written = Vnx.write(result.document)
        Tree.compare(written, case.readText(), Tree.Options(emitBoundary = true))?.let {
            fail("${case.name}: what this library wrote is not what it read: $it")
        }
    }

    private fun checkCounts(case: File, sidecar: Sidecar, result: ReadResult) {
        val expected = sidecar.counts() ?: fail("${case.name}: an accept or diagnose case needs `counts`")
        val document = result.document
        val actual = LinkedHashMap<String, Int>()
        actual["folders"] = document.folders?.size ?: 0
        actual["networks"] = document.networks?.size ?: 0
        actual["routes"] = document.routes?.size ?: 0
        actual["tracks"] = document.tracks?.size ?: 0
        actual["waypoints"] = document.waypoints?.size ?: 0
        actual["areas"] = document.areas?.size ?: 0
        // The nested containers, addressed by the same JSON Pointer vocabulary
        // `positions` uses. A bare name is a top-level collection; a name
        // starting with `/` is a container inside one.
        actual.putAll(containerCountsByPointer(document))

        for ((key, want) in expected) {
            val got = actual[key]
                ?: fail("${case.name}: `counts` names `$key`, which is not a collection the document carries")
            assertEquals(want, got, "${case.name}: `$key` count")
        }
        // A collection the sidecar does not name is expected to be empty. That is
        // what makes `counts` complete rather than a spot check: an omitted
        // collection would otherwise mean "unchecked", which lets a decoder
        // invent or drop items in it with nothing to say so.
        for ((key, got) in actual) {
            if (got == 0) continue
            assertTrue(
                key in expected,
                "${case.name}: the document carries $got `$key` and the sidecar does not name that collection; " +
                    "an unnamed collection is unchecked, not empty",
            )
        }
    }

    /**
     * The number of children of every NESTED container in the decoded document,
     * keyed by a JSON Pointer in the wire's own key names — the same vocabulary
     * `positions` uses.
     *
     * IT EXISTS BECAUSE `counts` COUNTED ONLY THE TOP-LEVEL COLLECTIONS. A
     * reader that dropped every route's arcs, or truncated an arc's pips, or
     * lost every chain in a network, changed nothing a sidecar could see: the
     * document still carried one route, one network, one track. The accept tier
     * catches that through its decode-encode-compare, but section 8.1 exempts
     * the diagnose tier from the round trip — which is exactly the tier where a
     * reader is supposed to drop something in a SCOPED way, and where dropping
     * more than the scope went unseen.
     *
     * It is a producer rather than a pointer resolver, for the reason
     * ModelPointer's companion in Go is: one traversal answers every pointer, so
     * a case can pin a container deep inside a network without this needing a
     * pointer parser.
     *
     * ARC AND NODE CONTAINERS ARE KEYED BY THEIR OWN KEY, NOT BY AN INDEX.
     * `/networks/0/arcs/arc-1/pips` — section 3.3 makes `arcs` and `nodes`
     * objects, and an object has no order, so an index there would export this
     * implementation's map iteration order as a conformance requirement.
     */
    private fun containerCountsByPointer(document: VnxDocument): Map<String, Int> {
        val out = LinkedHashMap<String, Int>()
        document.networks?.forEachIndexed { i, network ->
            out["/networks/$i/nodes"] = network.nodes?.size ?: 0
            out["/networks/$i/arcs"] = network.arcs?.size ?: 0
            network.arcs?.forEach { (key, arc) -> out["/networks/$i/arcs/$key/pips"] = arc.pips.size }
            out["/networks/$i/chains"] = network.chains?.size ?: 0
            network.chains?.forEachIndexed { j, chain -> out["/networks/$i/chains/$j/walk"] = chain.walk.size }
            // UNCONDITIONALLY, like every other container here. A guard on these
            // two alone would make `/networks/0/points: 0` a legal sidecar entry
            // for a runner that emits it and a hard "not a collection the
            // document carries" error for one that does not — a divergence
            // between conforming implementations with nothing in the spec to
            // separate them. A zero never triggers the completeness rule either
            // way, so the guard bought nothing for the cost.
            out["/networks/$i/points"] = network.points?.size ?: 0
            out["/networks/$i/areas"] = network.areas?.size ?: 0
            network.areas?.forEachIndexed { j, area -> out["/networks/$i/areas/$j/polygon"] = area.polygon.size }
        }
        document.routes?.forEachIndexed { i, route ->
            out["/routes/$i/nodes"] = route.nodes?.size ?: 0
            out["/routes/$i/arcs"] = route.arcs?.size ?: 0
            route.arcs?.forEach { (key, arc) -> out["/routes/$i/arcs/$key/pips"] = arc.pips.size }
            out["/routes/$i/walk"] = route.walk.size
        }
        document.tracks?.forEachIndexed { i, track ->
            out["/tracks/$i/segments"] = track.segments.size
            track.segments.forEachIndexed { j, segment -> out["/tracks/$i/segments/$j/pips"] = segment.pips.size }
        }
        document.areas?.forEachIndexed { i, area -> out["/areas/$i/polygon"] = area.polygon.size }
        return out
    }

    private fun checkPositions(case: File, sidecar: Sidecar, result: ReadResult) {
        for (expected in sidecar.positions()) {
            val position = ModelPointer.resolve(result.document, expected.at)
            checkChannel(case, expected.at, "longitude", expected.lon, position.lon)
            checkChannel(case, expected.at, "latitude", expected.lat, position.lat)
        }
    }

    /**
     * One channel of an expected position, compared BY BIT PATTERN.
     *
     * A value comparison cannot see a one-ulp move between the parse and the
     * model, and on failure it prints two numbers that look identical.
     *
     * It is also one of only two instruments in the whole corpus that CAN see
     * negative zero. Every tree comparison is by value and `-0` is written as
     * `0`, so the other is the number table — a table of literals.
     *
     * That second reach is LATENT today, and the comment says so rather than
     * claiming a guard nobody has: measured 2026-09-10, no `positions` entry in
     * any sidecar carries a zero longitude or latitude, so flipping the sign of
     * every zero channel in the decoder changes nothing here. The check is ready
     * for a fixture that carries one. It is not evidence that one exists.
     *
     * WHAT IT CANNOT SEE, so that nobody reads more into it: a parser that is one
     * ulp out. The sidecar's expected value is read by the same parser, so it is
     * wrong on both sides and they agree. `a coordinate literal parses to the
     * correctly rounded double` in ReaderRulesTest is the guard for that. This
     * one is about anything that moves a value between the parse and the model.
     */
    private fun checkChannel(case: File, at: String, channel: String, want: Double, got: Double) {
        if (got.toRawBits() == want.toRawBits()) return
        fail(
            "${case.name}: $channel at $at: got $got (%016x), want $want (%016x)"
                .format(got.toRawBits(), want.toRawBits()),
        )
    }

    private fun checkTracks(case: File, sidecar: Sidecar, result: ReadResult) {
        for (expected in sidecar.tracks()) {
            val track = result.document.tracks?.firstOrNull { it.name == expected.name }
                ?: fail("${case.name}: no track named \"${expected.name}\"")
            assertEquals(
                expected.movingTimeMs, track.movingTimeMs(),
                "${case.name}: moving time of \"${expected.name}\" — null means unknown, which is not zero",
            )
        }
    }

    /**
     * The sidecar's `attachments`, as a COLLECTION.
     *
     * Section 7 states one rule about archive order, and it is about selecting
     * the ROOT DOCUMENT: where there is no `doc.vnx`, the reader takes the first
     * `.vnx` entry in archive order. Nothing states the order a reader reports
     * the attachments in, and this sidecar is read by six implementations, so
     * comparing the list in order would make every one of them agree on
     * something the format does not ask them to agree on.
     */
    private fun checkAttachments(case: File, sidecar: Sidecar, result: ReadResult) {
        val got = result.attachments ?: emptyList()
        sidecar.attachments()?.let { expected ->
            assertEquals(
                expected.sorted(), got.map { it.path }.sorted(),
                "${case.name}: attachments",
            )
        }
        // And their BYTES. A reader that returns names without content passes
        // the list check and carries nothing; that is what this is here to see.
        // The sidecar spells a body as UTF-8 text, and need not name every
        // attachment — a dropped one is not returned and is not listed.
        val contents = sidecar.attachmentContents() ?: return
        val byPath = got.associateBy { it.path }
        for ((path, body) in contents) {
            val attachment = byPath[path]
                ?: fail("${case.name}: `attachment_contents` names \"$path\" and no such attachment came back")
            assertEquals(
                body, String(attachment.bytes, Charsets.UTF_8),
                "${case.name}: the content of \"$path\"",
            )
        }
    }

    // ---------------------------------------------------------------- reject

    @Test
    fun reject() {
        eachCase("reject", cases("reject"), { it.name }) { case ->
            val sidecar = sidecarFor(case)
            val expected = sidecar.refusal ?: fail("${case.name}: a reject case needs `refusal`")
            try {
                val result = Vnx.read(case)
                fail(
                    "${case.name} must be refused, and was read with ${result.diagnostics.size} diagnostic(s). " +
                        "The refusal was to name: \"$expected\"",
                )
            } catch (e: VnxRefusal) {
                assertTrue(
                    e.message.contains(expected),
                    "${case.name}: the refusal must name its cause. Expected the message to contain " +
                        "\"$expected\" and it was: ${e.message}",
                )
            }
        }
    }

    // -------------------------------------------------------------- diagnose

    @Test
    fun diagnose() {
        eachCase("diagnose", cases("diagnose"), { it.name }) { case ->
            val sidecar = sidecarFor(case)
            val expected = sidecar.diagnostics()
                ?: fail("${case.name}: a diagnose case needs `diagnostics`, and an empty list is an expectation")
            val result = try {
                Vnx.read(case)
            } catch (e: VnxRefusal) {
                fail("${case.name} must be READ while reporting, and was refused: $e")
            }
            DiagnosticMatcher.mismatch(expected, result.diagnostics)?.let { fail("${case.name}: $it") }
            checkCounts(case, sidecar, result)
            checkPositions(case, sidecar, result)
            checkTracks(case, sidecar, result)
            // A container case can sit in this tier: an attachment the reader
            // cannot decompress is DROPPED with a diagnostic while the document
            // is returned. Checking the attachments here is what proves the drop
            // was scoped to the one entry rather than total.
            checkAttachments(case, sidecar, result)
        }
    }

    // ----------------------------------------------------------------- write

    private fun writePairs(): List<Pair<File, File>> {
        val dir = File(corpus, "write")
        assertTrue(dir.isDirectory, "the corpus tier write is missing at ${dir.absolutePath}")
        return dir.listFiles()!!
            .filter { it.name.endsWith(".in.vnx") }
            .filter { !File(dir, it.name.removeSuffix(".in.vnx") + ".attachments.json").isFile }
            .sortedBy { it.name }
            .map { input ->
                val written = File(dir, input.name.removeSuffix(".in.vnx") + ".written.vnx")
                assertTrue(written.isFile, "${input.name} has no ${written.name}")
                val sidecar = File(dir, input.name.removeSuffix(".in.vnx") + ".expect.json")
                assertTrue(sidecar.isFile, "${input.name} has no sidecar: that is a failure, not a skip")
                assertEquals("write", Sidecar.read(sidecar).kind, "${sidecar.name}: `kind` must match the directory")
                input to written
            }
    }

    /**
     * The write cases whose output is a CONTAINER rather than a document.
     *
     * A case carrying an attachment manifest expects an ARCHIVE, and the
     * manifest is its third input file — see `corpus/write/README.md`. The
     * attachments live in JSON rather than in real files beside the sidecar
     * because one name holds U+1F600: a filename is subject to the checkout
     * filesystem's Unicode normalisation and case folding, so a checked-out file
     * could arrive under a name that is not the one the case means and the
     * fixture would test the filesystem instead of the writer.
     */
    private fun writeContainerCases(): List<Triple<File, File, File>> {
        val dir = File(corpus, "write")
        return dir.listFiles()!!
            .filter { it.name.endsWith(".attachments.json") }
            .sortedBy { it.name }
            .map { manifest ->
                val stem = manifest.name.removeSuffix(".attachments.json")
                val input = File(dir, "$stem.in.vnx")
                val written = File(dir, "$stem.written.vnz")
                assertTrue(input.isFile, "${manifest.name} has no ${input.name}")
                assertTrue(written.isFile, "${manifest.name} promises ${written.name} and there is none")
                val sidecar = File(dir, "$stem.expect.json")
                assertTrue(sidecar.isFile, "${input.name} has no sidecar: that is a failure, not a skip")
                val read = Sidecar.read(sidecar)
                assertEquals("write", read.kind, "${sidecar.name}: `kind` must match the directory")
                assertEquals("doc.vnx", read.rootEntry, "${sidecar.name}: this runner writes the document as doc.vnx")
                Triple(input, manifest, written)
            }
    }

    /** The manifest's attachments, in the order it lists them. */
    private fun manifestAttachments(manifest: File): Map<String, ByteArray> {
        val parsed = JsonParser.parse(manifest.readText()) as JsonObject
        val listed = parsed["attachments"] as JsonArray
        val out = LinkedHashMap<String, ByteArray>()
        for (item in listed.elements) {
            val entry = item as JsonObject
            val name = (entry["name"] as JsonString).value
            val body = (entry["body"] as JsonString).value
            out[name] = body.toByteArray(Charsets.UTF_8)
        }
        return out
    }

    /**
     * The emit boundary (SPEC.md section 4.1).
     *
     * This tier is the only one that reaches it. Every other tier compares a
     * document against a re-encoding of itself, which shortens positions that are
     * already short, so a defect in the rule is invisible outside this directory.
     *
     * AND SO IT NORMALISES ALMOST NOTHING. The accept tier drops a trailing
     * `null` from both sides. Doing that here would normalise away the rule under
     * test. The one normalisation left is the one that is not about the emit
     * boundary at all: section 3.1's absent-versus-empty.
     */
    @Test
    fun write() {
        val opt = Tree.Options(emitBoundary = false)
        eachCase("write", writePairs(), { it.first.name }) { (input, written) ->
            val expected = written.readText()
            // Without this a writer that echoed its input would pass every case.
            assertTrue(
                Tree.compare(input.readText(), expected, opt) != null,
                "${input.name} is already the written form, so this case would pass vacuously",
            )
            val actual = writeThrough(input.readText(), input.name)
            Tree.compare(actual, expected, opt)?.let {
                fail("${input.name}: the written document is not the one the case expects: $it")
            }
            // Shortening is idempotent, so the written form is a fixed point. A
            // case that fails this is a defect in the fixture.
            Tree.compare(writeThrough(expected, written.name), expected, opt)?.let {
                fail("${written.name} is not a fixed point of the writer: $it")
            }
        }
    }

    /**
     * Decodes a document and writes it back — the operation the tier tests. A
     * diagnostic is a failure here: a writer case's input is a document the
     * reader reads cleanly.
     */
    private fun writeThrough(text: String, name: String): String {
        val read = try {
            Vnx.readDocument(text)
        } catch (e: VnxRefusal) {
            fail("$name must be read before it can be written, and was refused: $e")
        }
        assertEquals(emptyList(), read.diagnostics, "$name: a writer case's input must read cleanly")
        return Vnx.write(read.document)
    }

    /**
     * A write case whose expected output is an archive.
     *
     * THE BYTES ARE NOT COMPARED. Section 7.1 states five rules and then says
     * every other field of the archive is the writer's own business, so two
     * conforming writers are not expected to agree byte for byte — deflate alone
     * makes that impossible, since no two languages emit the same deflate stream.
     *
     * AND NEITHER IS THE ORDER OF THE ATTACHMENTS. Only the root document's
     * place is a rule: 7.1 requires it first. Beyond that, comparing entry order
     * would enforce a rule the specification explicitly disclaims — so the
     * attachments are compared as a collection, by name and by body, and where
     * they sit in the archive is not checked.
     *
     * The attachments are fed REVERSED. Now that no sort rule is under test, that
     * proves what it claims and nothing else: a writer's output must not depend
     * on the order it was handed.
     */
    @Test
    fun `write container`() {
        val cases = writeContainerCases()
        assertTrue(cases.isNotEmpty(), "the write tier holds no container case")
        eachCase("write", cases, { it.first.name }) { (input, manifest, written) ->
            val attachments = manifestAttachments(manifest)
            val want = Zip.read(written.readBytes())
            assertTrue(want.isNotEmpty(), "${written.name} holds no entries")
            assertEquals("doc.vnx", want[0].name, "${written.name}: section 7.1 requires the root document first")

            val document = try {
                Vnx.readDocument(input.readText())
            } catch (e: VnxRefusal) {
                fail("${input.name} must be read before it can be written, and was refused: $e")
            }
            assertEquals(
                emptyList(), document.diagnostics,
                "${input.name}: a writer case's input must read cleanly",
            )
            val reversed = LinkedHashMap<String, ByteArray>()
            for (name in attachments.keys.reversed()) reversed[name] = attachments.getValue(name)
            val bytes = Vnx.writeContainer(document.document, reversed)

            val got = Zip.read(bytes)
            assertEquals("doc.vnx", got[0].name, "${input.name}: section 7.1 requires the root document first")
            assertEquals(
                want.map { it.name }.sorted(), got.map { it.name }.sorted(),
                "${input.name}: the archive's entry names, as a collection",
            )
            Tree.compare(String(got[0].bytes(), Charsets.UTF_8), String(want[0].bytes(), Charsets.UTF_8), Tree.Options(emitBoundary = false))?.let {
                fail("${input.name}: the archive's root document is not the one the case expects: $it")
            }
            // An attachment is bytes and is compared as bytes. Nothing in this
            // format interprets one.
            for (entry in got.drop(1)) {
                val body = attachments[entry.name] ?: fail("${input.name}: ${entry.name} is not in the manifest")
                assertTrue(body.contentEquals(entry.bytes()), "${input.name}: the body of ${entry.name}")
            }

            // And the archive reads back through this library's own reader,
            // carrying every attachment the manifest names and no other.
            val back = Vnx.readContainer(bytes)
            assertEquals(emptyList(), back.diagnostics, "${input.name}: the archive this library wrote must read cleanly")
            assertEquals(
                attachments.keys.sorted(), (back.attachments ?: emptyList()).map { it.path }.sorted(),
                "${input.name} does not carry the manifest's attachments",
            )
            for (attachment in back.attachments ?: emptyList()) {
                val body = attachments.getValue(attachment.path)
                assertTrue(
                    body.contentEquals(attachment.bytes),
                    "${input.name}: the body of ${attachment.path} did not survive the round trip",
                )
            }
        }
    }

    // ----------------------------------------------------------------- utility

    /**
     * Runs one tier, case by case, so that a single defect is reported as a
     * single defect.
     *
     * Two failures this prevents, and neither is hypothetical. A REFUSAL raised
     * where a tier does not expect one propagates out of the loop and takes every
     * remaining case with it — one defect reported as a total collapse, with the
     * cases that would have named it simply absent from the output. And an
     * ordinary failed assertion aborts the loop just as thoroughly, so the second
     * broken case is invisible until the first is fixed.
     *
     * A throwing language has to arrange this deliberately; a language returning
     * errors gets it structurally. Kotlin is the first kind.
     */
    private fun <T> eachCase(tier: String, cases: List<T>, name: (T) -> String, body: (T) -> Unit) {
        val failures = ArrayList<String>()
        for (case in cases) {
            try {
                body(case)
            } catch (e: Throwable) {
                failures.add("  ${name(case)}\n    ${(e.message ?: e.toString()).replace("\n", "\n    ")}")
            }
        }
        if (failures.isNotEmpty()) {
            fail("$tier: ${failures.size} of ${cases.size} case(s) failed\n\n" + failures.joinToString("\n\n"))
        }
    }
}

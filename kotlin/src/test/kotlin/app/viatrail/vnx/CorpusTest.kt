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
                    sidecar.schemaVerdict in listOf(null, "valid", "invalid", "not-json"),
                    "${case.name}: `schema` is \"${sidecar.schemaVerdict}\", which is not a verdict this runner knows",
                )
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
     * writer's own business. Rounding and the shortest spelling of a position are
     * applied to BOTH sides, because this tier compares a document against a
     * re-encoding of itself and would otherwise require the file to have been
     * written by this library.
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
        val unknown = expected.keys - Sidecar.COLLECTIONS.toSet()
        assertTrue(unknown.isEmpty(), "${case.name}: `counts` names collections that do not exist: $unknown")
        val document = result.document
        // A collection the sidecar does not name is expected to be empty. That is
        // what makes `counts` complete rather than a spot check.
        val actual = mapOf(
            "folders" to (document.folders?.size ?: 0),
            "networks" to (document.networks?.size ?: 0),
            "routes" to (document.routes?.size ?: 0),
            "tracks" to (document.tracks?.size ?: 0),
            "waypoints" to (document.waypoints?.size ?: 0),
            "areas" to (document.areas?.size ?: 0),
            "rulesets" to ((document.rulesets as? JsonArray)?.elements?.size ?: 0),
        )
        for (collection in Sidecar.COLLECTIONS) {
            assertEquals(
                expected[collection] ?: 0, actual[collection],
                "${case.name}: `$collection` count",
            )
        }
    }

    private fun checkPositions(case: File, sidecar: Sidecar, result: ReadResult) {
        for (expected in sidecar.positions()) {
            val position = ModelPointer.resolve(result.document, expected.at)
            assertEquals(expected.lon, position.lon, "${case.name}: longitude at ${expected.at}")
            assertEquals(expected.lat, position.lat, "${case.name}: latitude at ${expected.at}")
        }
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

    private fun checkAttachments(case: File, sidecar: Sidecar, result: ReadResult) {
        val expected = sidecar.attachments() ?: return
        assertEquals(expected, result.attachments ?: emptyList(), "${case.name}: attachments")
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
     * document against a re-encoding of itself, which applies rounding to values
     * that already satisfy it and shortening to positions already short, so a
     * defect in either rule is invisible outside this directory and
     * `corpus/numbers/`.
     *
     * AND SO IT NORMALISES ALMOST NOTHING. The accept tier rounds both sides and
     * drops a trailing `null` from both. Doing that here would normalise away the
     * two rules under test. The one normalisation left is the one that is not
     * about the emit boundary at all: section 3.1's absent-versus-empty.
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
            // Rounding and shortening are both idempotent, so the written form is
            // a fixed point. A case that fails this is a defect in the fixture.
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
     * THE BYTES ARE NOT COMPARED. Section 7.1 constrains the UTF-8 filename
     * flag, the root document being first, and duplicate names, and leaves every
     * other field to the writer, so two conforming writers are not expected to
     * agree byte for byte — deflate alone makes that impossible, since no two
     * languages emit the same deflate stream. What is compared is what 7.1 does
     * constrain, plus the root document's tree and the attachment bodies.
     *
     * The attachments are fed REVERSED. A writer's output must not depend on the
     * order it was handed, and feeding them in manifest order cannot see one that
     * does.
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
            assertEquals(want.map { it.name }, got.map { it.name }, "${input.name}: the archive's entry names, in order")
            Tree.compare(String(got[0].bytes(), Charsets.UTF_8), String(want[0].bytes(), Charsets.UTF_8), Tree.Options(emitBoundary = false))?.let {
                fail("${input.name}: the archive's root document is not the one the case expects: $it")
            }
            // An attachment is bytes and is compared as bytes. Nothing in this
            // format interprets one.
            for (entry in got.drop(1)) {
                val body = attachments[entry.name] ?: fail("${input.name}: ${entry.name} is not in the manifest")
                assertTrue(body.contentEquals(entry.bytes()), "${input.name}: the body of ${entry.name}")
            }

            // And the archive reads back through this library's own reader.
            val back = Vnx.readContainer(bytes)
            assertEquals(emptyList(), back.diagnostics, "${input.name}: the archive this library wrote must read cleanly")
            // The comparator is required and is not decoration: Kotlin's default
            // String order is by UTF-16 code unit, which puts U+1F600 before
            // U+FF3A because its high surrogate begins at U+D800. This case is
            // chosen to separate the two.
            assertEquals(
                attachments.keys.sortedWith(entryNameOrder), back.attachments,
                "${input.name} does not carry the manifest's attachments",
            )
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

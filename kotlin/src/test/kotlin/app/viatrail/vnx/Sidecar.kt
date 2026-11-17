package app.viatrail.vnx

import java.io.File

/**
 * A corpus case's `.expect.json` sidecar.
 *
 * The sidecar is plain JSON beside the case rather than a test in any one
 * language, so an implementation in any language can run the corpus. It is
 * parsed here with this library's own JSON parser, which is the only JSON parser
 * this project has.
 */
class Sidecar(
    val file: File,
    private val root: JsonObject,
) {
    companion object {
        /** Every field the sidecar schema defines. An unknown one is a runner that is out of date. */
        val KNOWN_FIELDS =
            setOf(
                "kind",
                "note",
                "counts",
                "positions",
                "tracks",
                "attachments",
                "refusal",
                "diagnostics",
                "cannot_detect",
                // Reason codes, 2026-09-09. `message_advisory` is read by nothing on
                // purpose: a field named `message` that nothing compares reads as a
                // claim and is not one, and naming it advisory is what stops a runner
                // quietly going on matching English.
                "code",
                "message_advisory",
                // The write tier's container shape, 2026-09-10: `root` names the
                // entry the document is written as, and the attachment list beside
                // it repeats what the manifest carries.
                "root",
                // 2026-09-12: `attachments` names the entries and
                // `attachment_contents` pins their BYTES, so a reader that returns
                // names without content cannot pass the container tier.
                "attachment_contents",
                // 2026-09-13: `schema` is the verdict the JSON Schema reaches on
                // this case. It is about corpus/schema/vnx-0.11.schema.json rather
                // than about any reader, so no implementation runner compares it —
                // go/internal/vnxschema is what asserts it, and it fails if a case
                // records no verdict. Named here so this runner's unknown-field
                // guard keeps its meaning for every field that IS a reader's.
                "schema",
            )

        /**
         * The six top-level collections `counts` may name (SPEC.md section 3.1).
         * An application key is not a collection and has no entry.
         */
        val COLLECTIONS = listOf("folders", "networks", "plans", "tracks", "points", "areas")

        fun read(file: File): Sidecar {
            val value = JsonParser.parse(file.readText())
            check(value is JsonObject) { "${file.name}: a sidecar must be a JSON object" }
            return Sidecar(file, value)
        }
    }

    val kind: String get() = string("kind") ?: error("${file.name}: a sidecar requires `kind`")
    val note: String get() = string("note") ?: error("${file.name}: a sidecar requires `note`")
    val refusal: String? get() = string("refusal")

    /** The entry name a container write case expects its document under. */
    val rootEntry: String? get() = string("root")

    val cannotDetect: String? get() = string("cannot_detect")

    val unknownFields: Set<String> get() = root.keys - KNOWN_FIELDS

    private fun string(key: String) = (root[key] as? JsonString)?.value

    /** `counts` gives the exact number of items per collection, and stops a case passing vacuously. */
    fun counts(): Map<String, Int>? {
        val v = root["counts"] as? JsonObject ?: return null
        return v.entries().associate { (k, n) ->
            k to ((n as? JsonNumber)?.value?.toInt() ?: error("${file.name}: counts.$k is not a number"))
        }
    }

    class ExpectedPosition(
        val at: String,
        val lon: Double,
        val lat: Double,
    )

    fun positions(): List<ExpectedPosition> {
        val v = root["positions"] as? JsonArray ?: return emptyList()
        return v.elements.map { element ->
            val o = element as? JsonObject ?: error("${file.name}: a `positions` entry must be an object")
            ExpectedPosition(
                at = (o["at"] as JsonString).value,
                lon = (o["lon"] as JsonNumber).value,
                lat = (o["lat"] as JsonNumber).value,
            )
        }
    }

    /** `moving_time_ms: null` means unknown, which is not zero (SPEC.md section 3.5). */
    class ExpectedTrack(
        val name: String,
        val movingTimeMs: Long?,
    )

    fun tracks(): List<ExpectedTrack> {
        val v = root["tracks"] as? JsonArray ?: return emptyList()
        return v.elements.map { element ->
            val o = element as? JsonObject ?: error("${file.name}: a `tracks` entry must be an object")
            val moving = o["moving_time_ms"]
            check(moving != null) { "${file.name}: a `tracks` entry must state moving_time_ms, null meaning unknown" }
            ExpectedTrack(
                name = (o["name"] as JsonString).value,
                movingTimeMs = if (moving is JsonNull) null else (moving as JsonNumber).value.toLong(),
            )
        }
    }

    fun attachments(): List<String>? {
        val v = root["attachments"] as? JsonArray ?: return null
        return v.elements.map { (it as JsonString).value }
    }

    /**
     * `attachment_contents`: each entry name against its bytes, spelled as
     * UTF-8 text.
     *
     * TEXT rather than base64 because every body in the corpus is text and a
     * fixture a maintainer cannot read is a fixture nobody checks. An
     * attachment is arbitrary bytes in the format; if one ever needs a body
     * that is not text, this field grows a sibling rather than changing
     * meaning.
     *
     * It need not name every attachment. `diagnose/container-unusable-method.vnz`
     * carries an entry the reader drops, and the sidecar lists only what a
     * conforming reader returns.
     */
    fun attachmentContents(): Map<String, String>? {
        val v = root["attachment_contents"] as? JsonObject ?: return null
        return v.entries().associate { (name, body) ->
            name to (
                (body as? JsonString)?.value
                    ?: error("${file.name}: attachment_contents.$name is not a string")
            )
        }
    }

    /**
     * An expected diagnostic: a `path` and a reason [code], and nothing else.
     *
     * The English beside it in the sidecar is `message_advisory` and is not read
     * here. SPEC.md section 6.3 makes a code a wire commitment and a message a
     * reader's own business, so comparing the message would re-impose on every
     * implementation the wording of whichever one wrote the fixture.
     */
    class ExpectedDiagnostic(
        val path: String,
        val code: String,
    ) {
        override fun toString() = "$path: [$code]"
    }

    /** Null where the sidecar states no expectation at all, which is different from expecting none. */
    fun diagnostics(): List<ExpectedDiagnostic>? {
        val v = root["diagnostics"] as? JsonArray ?: return null
        return v.elements.map { element ->
            val o = element as? JsonObject ?: error("${file.name}: a `diagnostics` entry must be an object")
            val path = (o["path"] as JsonString).value
            val code = (o["code"] as? JsonString)?.value
            // Section 6.3: a diagnostic a corpus case pins MUST carry a code. A
            // sidecar without one is a case nothing can check, and that is a
            // failure rather than a skip.
            check(code != null) { "${file.name}: the diagnostic at $path carries no `code`" }
            check(o["message"] == null) {
                "${file.name}: the diagnostic at $path still carries `message`; the English field is " +
                    "`message_advisory` and is not compared"
            }
            ExpectedDiagnostic(path, code)
        }
    }
}

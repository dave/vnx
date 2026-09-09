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
class Sidecar(val file: File, private val root: JsonObject) {

    companion object {
        /** Every field the sidecar schema defines. An unknown one is a runner that is out of date. */
        val KNOWN_FIELDS = setOf(
            "kind", "note", "schema", "counts", "positions", "tracks",
            "attachments", "refusal", "diagnostics", "cannot_detect",
        )

        /** The seven top-level collections `counts` may name (SPEC.md section 3.1). */
        val COLLECTIONS = listOf("folders", "networks", "routes", "tracks", "waypoints", "areas", "rulesets")

        fun read(file: File): Sidecar {
            val value = JsonParser.parse(file.readText())
            check(value is JsonObject) { "${file.name}: a sidecar must be a JSON object" }
            return Sidecar(file, value)
        }
    }

    val kind: String get() = string("kind") ?: error("${file.name}: a sidecar requires `kind`")
    val note: String get() = string("note") ?: error("${file.name}: a sidecar requires `note`")
    val refusal: String? get() = string("refusal")

    /**
     * The case's JSON Schema verdict, or null where it states none.
     *
     * This runner reads the field and checks its shape but does not act on it.
     * Validating a document against a draft 2020-12 schema needs a schema
     * validator, which is a third-party dependency, and this library imports the
     * standard library and nothing else — the same reason the reference
     * implementation keeps its schema test in a separate package. The gap is
     * reported rather than papered over; the four container cases carry no
     * verdict at all, which is its own question for the corpus.
     */
    val schemaVerdict: String? get() = string("schema")
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

    class ExpectedPosition(val at: String, val lon: Double, val lat: Double)

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
    class ExpectedTrack(val name: String, val movingTimeMs: Long?)

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

    class ExpectedDiagnostic(val path: String, val message: String) {
        override fun toString() = "$path: ~\"$message\""
    }

    /** Null where the sidecar states no expectation at all, which is different from expecting none. */
    fun diagnostics(): List<ExpectedDiagnostic>? {
        val v = root["diagnostics"] as? JsonArray ?: return null
        return v.elements.map { element ->
            val o = element as? JsonObject ?: error("${file.name}: a `diagnostics` entry must be an object")
            ExpectedDiagnostic((o["path"] as JsonString).value, (o["message"] as JsonString).value)
        }
    }
}

package app.viatrail.vnx

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.JsonArray as KtJsonArray
import kotlinx.serialization.json.JsonNull as KtJsonNull
import kotlinx.serialization.json.JsonObject as KtJsonObject

/**
 * Renders a JSON value tree as the bytes of a document, through
 * kotlinx-serialization-json.
 *
 * NOTHING ABOUT THOSE BYTES IS A RULE. SPEC.md section 4.2 defines round-trip at
 * value level, and section 8.1 compares documents as JSON trees, so key order,
 * number spelling and whitespace are this writer's own business. That is why a
 * stock JSON library can do the work and why the hand-written serialiser this
 * replaced could go.
 *
 * COMPACT, with no whitespace but the newline that ends the file. Indentation
 * cost about three times the file where the reference implementation measured
 * it. Pretty-printing is a tool somebody may want; it is not a property of the
 * format.
 */
internal object Serialise {

    private val json = Json

    /** Renders a value as a complete document, newline-terminated. */
    fun render(value: JsonValue): String =
        json.encodeToString(JsonElement.serializer(), element(value)) + "\n"

    private fun element(value: JsonValue): JsonElement = when (value) {
        is JsonNull -> KtJsonNull
        is JsonBool -> JsonPrimitive(value.value)
        is JsonString -> JsonPrimitive(value.value)
        is JsonNumber -> number(value.value)
        is JsonArray -> KtJsonArray(value.elements.map(::element))
        is JsonObject -> {
            val out = LinkedHashMap<String, JsonElement>(value.size)
            for ((key, child) in value.entries()) out[key] = element(child)
            KtJsonObject(out)
        }
    }

    /**
     * One number.
     *
     * A whole value below 2^53 is written as an integer, because that is what it
     * is: a `time_ms` is a count of milliseconds and `1778914991000` is a better
     * spelling of it than `1.778914991E12`. Both parse to the same double, so
     * this is legibility and not conformance.
     *
     * THE BOUND IS NOT DECORATION. `Double.toLong()` SATURATES, so without it
     * 9.223372036854776E18 would be written as 9223372036854775807 — a different
     * value, silently. Section 4.2 guarantees `time_ms` below 2^53, and every
     * other whole number a document carries is a coordinate or an elevation.
     */
    private fun number(x: Double): JsonPrimitive {
        require(x.isFinite()) {
            "a non-finite number must never reach the serialiser, which SPEC 4.2 forbids"
        }
        val whole = x.toLong()
        if (whole.toDouble() == x && Math.abs(x) < 9007199254740992.0) return JsonPrimitive(whole)
        return JsonPrimitive(x)
    }
}

package app.viatrail.vnx

/**
 * The JSON value model this library parses into and renders from.
 *
 * It exists because VNX carries opaque regions — data record bodies, style
 * objects and application keys (SPEC.md sections 5.1, 3.9, 3.10) — that a reader must
 * preserve at value level through a read-modify-write without understanding
 * them. Those regions need a value tree, and the rest of the format is decoded
 * into the typed model in Model.kt.
 *
 * A number is held as a `Double` and nothing else. SPEC.md section 4.2 defines
 * round-trip at value level rather than at byte level: a reader is not required
 * to preserve the text of a number, only its value.
 */
sealed interface JsonValue

data class JsonString(
    val value: String,
) : JsonValue

data class JsonNumber(
    val value: Double,
) : JsonValue

data class JsonBool(
    val value: Boolean,
) : JsonValue

data object JsonNull : JsonValue

class JsonArray(
    val elements: List<JsonValue>,
) : JsonValue {
    override fun equals(other: Any?) = other is JsonArray && other.elements == elements

    override fun hashCode() = elements.hashCode()

    override fun toString() = "JsonArray($elements)"
}

/**
 * A JSON object. Entry order is the document's order. SPEC.md section 4.4 says
 * key order is not significant anywhere, so nothing in this library reads it,
 * and the writer emits the order it built rather than an order it chose.
 *
 * A repeated key resolves last-wins, which is what every mainstream parser does
 * and what SPEC.md section 6.4 documents as a limitation of the format.
 */
class JsonObject(
    entries: List<Pair<String, JsonValue>> = emptyList(),
) : JsonValue {
    private val map = LinkedHashMap<String, JsonValue>()

    init {
        for ((k, v) in entries) map[k] = v
    }

    val keys: Set<String> get() = map.keys
    val size: Int get() = map.size

    operator fun get(key: String): JsonValue? = map[key]

    operator fun contains(key: String): Boolean = map.containsKey(key)

    fun entries(): List<Pair<String, JsonValue>> = map.entries.map { it.key to it.value }

    override fun equals(other: Any?) = other is JsonObject && other.map == map

    override fun hashCode() = map.hashCode()

    override fun toString() = "JsonObject($map)"

    class Builder {
        private val entries = ArrayList<Pair<String, JsonValue>>()

        fun put(
            key: String,
            value: JsonValue?,
        ): Builder {
            if (value != null) entries.add(key to value)
            return this
        }

        fun putString(
            key: String,
            value: String?,
        ) = put(key, value?.let(::JsonString))

        fun putBool(
            key: String,
            value: Boolean?,
        ) = put(key, value?.let(::JsonBool))

        fun build() = JsonObject(entries)
    }
}

fun jsonArrayOf(vararg values: JsonValue) = JsonArray(values.toList())

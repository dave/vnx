package app.viatrail.vnx

/**
 * Resolves a sidecar `positions` pointer against the DECODED MODEL.
 *
 * The corpus README is explicit about why this walks the model and not the raw
 * JSON: resolving the pointer against the file compares the file to itself and
 * cannot see a decoder that swapped two channels, which is the entire point of
 * the check. Longitude and latitude transposition is the one defect nothing
 * structural can catch — both orderings parse, both are in range, and the point
 * lands 8,000 km away.
 */
object ModelPointer {
    fun resolve(
        document: VnxDocument,
        pointer: String,
    ): Position {
        require(pointer.startsWith("/")) { "a JSON Pointer starts with '/': $pointer" }
        val tokens = pointer.substring(1).split("/").map { it.replace("~1", "/").replace("~0", "~") }
        return when (val value = walk(document, tokens, pointer)) {
            is Position -> value
            // A node, an arc pip and a walk position are positions with fewer
            // channels, and are compared as positions.
            is Vertex -> value.position()
            is LonLat -> value.position()
            else -> fail(pointer, "resolves to ${value?.let { it::class.simpleName }}, not a position")
        }
    }

    private fun fail(
        pointer: String,
        why: String,
    ): Nothing = error("the pointer $pointer $why")

    private fun walk(
        document: VnxDocument,
        tokens: List<String>,
        pointer: String,
    ): Any? {
        var current: Any? = document
        for (token in tokens) {
            current = step(current, token) ?: fail(pointer, "does not resolve: no `$token` under ${describe(current)}")
        }
        return current
    }

    private fun describe(value: Any?) = value?.let { it::class.simpleName } ?: "nothing"

    private fun step(
        value: Any?,
        token: String,
    ): Any? {
        val index = token.toIntOrNull()
        return when (value) {
            is VnxDocument ->
                when (token) {
                    "folders" -> value.folders
                    "networks" -> value.networks
                    "plans" -> value.plans
                    "tracks" -> value.tracks
                    "points" -> value.points
                    "areas" -> value.areas
                    else -> null
                }
            is Network ->
                when (token) {
                    "nodes" -> value.nodes
                    "arcs" -> value.arcs
                    "highlights" -> value.highlights
                    "points" -> value.points
                    "areas" -> value.areas
                    else -> null
                }
            is Arc -> if (token == "pips") value.pips else null
            is Highlight -> if (token == "walk") value.walk else null
            // Only a POSITION in a walk is a position. A node key is a
            // reference, and is addressed at the node.
            is Pass ->
                when (token) {
                    "from" -> (value.from as? Endpoint.At)?.position
                    "to" -> (value.to as? Endpoint.At)?.position
                    "turn" -> value.turn
                    "pin" -> value.pin
                    else -> null
                }
            is Plan ->
                when (token) {
                    "nodes" -> value.nodes
                    "arcs" -> value.arcs
                    "walk" -> value.walk
                    else -> null
                }
            is Track -> if (token == "segments") value.segments else null
            is Segment -> if (token == "pips") value.pips else null
            is Point -> if (token == "at") value.at else null
            is Area -> if (token == "polygon") value.polygon else null
            is List<*> -> index?.let { value.getOrNull(it) }
            is Map<*, *> -> value[token]
            else -> null
        }
    }
}

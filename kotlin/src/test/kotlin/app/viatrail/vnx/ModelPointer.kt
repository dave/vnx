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

    fun resolve(document: VnxDocument, pointer: String): Position {
        require(pointer.startsWith("/")) { "a JSON Pointer starts with '/': $pointer" }
        val tokens = pointer.substring(1).split("/").map { it.replace("~1", "/").replace("~0", "~") }
        val value = walk(document, tokens, pointer)
        return value as? Position ?: fail(pointer, "resolves to ${value?.let { it::class.simpleName }}, not a position")
    }

    private fun fail(pointer: String, why: String): Nothing = error("the pointer $pointer $why")

    private fun walk(document: VnxDocument, tokens: List<String>, pointer: String): Any? {
        var current: Any? = document
        for (token in tokens) {
            current = step(current, token) ?: fail(pointer, "does not resolve: no `$token` under ${describe(current)}")
        }
        return current
    }

    private fun describe(value: Any?) = value?.let { it::class.simpleName } ?: "nothing"

    private fun step(value: Any?, token: String): Any? {
        val index = token.toIntOrNull()
        return when (value) {
            is VnxDocument -> when (token) {
                "folders" -> value.folders
                "networks" -> value.networks
                "routes" -> value.routes
                "tracks" -> value.tracks
                "waypoints" -> value.waypoints
                "areas" -> value.areas
                else -> null
            }
            is Network -> when (token) {
                "nodes" -> value.nodes
                "arcs" -> value.arcs
                "chains" -> value.chains
                "points" -> value.points
                "areas" -> value.areas
                else -> null
            }
            is NetworkArc -> if (token == "pips") value.pips else null
            is Chain -> if (token == "links") value.links else null
            is Link -> when (token) {
                "from" -> (value.from as? Endpoint.Inline)?.position
                "to" -> (value.to as? Endpoint.Inline)?.position
                else -> null
            }
            is Route -> if (token == "arcs") value.arcs else null
            is RouteArc -> if (token == "pips") value.pips else null
            is Track -> if (token == "segments") value.segments else null
            is Segment -> if (token == "pips") value.pips else null
            is Waypoint -> if (token == "at") value.at else null
            is Area -> if (token == "polygon") value.polygon else null
            is List<*> -> index?.let { value.getOrNull(it) }
            is Map<*, *> -> value[token]
            else -> null
        }
    }
}

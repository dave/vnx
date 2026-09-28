package app.viatrail.vnx

/**
 * The decoded VNX document model.
 *
 * One property of this model is load-bearing and easy to lose.
 *
 * **Absent is not empty.** Every optional collection is nullable, because
 * section 3.10 says an empty array is distinct from an absent one, and a
 * document that carried an empty one is not the same document as one that
 * carried none.
 */
data class VnxDocument(
    val vnx: String,
    val generator: String? = null,
    val created: String? = null,
    val folders: List<Folder>? = null,
    val networks: List<Network>? = null,
    val routes: List<Route>? = null,
    val tracks: List<Track>? = null,
    val waypoints: List<Waypoint>? = null,
    val areas: List<Area>? = null,
    /**
     * The application keys (SPEC.md section 3.9): every top-level key that
     * contains a full stop, each value opaque and preserved verbatim.
     *
     * A LIST OF PAIRS, NOT A MAP, sorted by key on decode. Key order is not
     * significant (section 4.4); sorting makes one document decode to one list,
     * whatever order its keys were written in. A JSON `null` is [JsonNull]: the key is present, and it is
     * written back as `null`. There is no absent-versus-empty distinction here,
     * so the list is never null.
     */
    val appKeys: List<AppKey> = emptyList(),
)

/**
 * One application key (SPEC.md section 3.9). [value] is never inspected.
 */
data class AppKey(val key: String, val value: JsonValue) {
    companion object {
        /** Whether a top-level key is an application key: it contains a full stop. */
        fun isAppKey(key: String): Boolean = '.' in key
    }
}

/**
 * A position: `[lon, lat, ele, time_ms]`, longitude first (SPEC.md section 4.1).
 *
 * [ele] and [timeMs] are null when the channel is absent, which section 4.1
 * makes a first-class state rather than an error. HOW MANY CHANNELS THE
 * DOCUMENT SPELLED IS NOT KEPT: `[lon, lat, null]` and `[lon, lat]` state the
 * same thing, a writer emits the shortest spelling of either, and a model that
 * remembered the difference would be remembering the text rather than the
 * value.
 *
 * A channel beyond those this version names is read and dropped, which section
 * 4.1 requires of a reader and reports no diagnostic for.
 */
data class Position(
    val lon: Double,
    val lat: Double,
    val ele: Double? = null,
    val timeMs: Double? = null,
) {

    /**
     * Equality for the continuity rule of SPEC.md section 6.2 and the shared
     * reversal endpoint of section 3.3.3.
     *
     * Longitude and latitude only. Section 3.3.3 says an inline endpoint never
     * carries an elevation and takes it from the polyline, so a node at
     * `[lon, lat, ele]` and the inline endpoint that names the same point can
     * never agree on a third channel.
     */
    fun meets(other: Position): Boolean = lon == other.lon && lat == other.lat

    companion object {
        fun of(lon: Double, lat: Double) = Position(lon, lat)
    }
}

/**
 * A data record (SPEC.md section 5). The body is an opaque JSON object apart
 * from `@type`, and must survive a read-modify-write at value level whether or
 * not the reader recognises the type.
 */
data class DataRecord(val type: String, val body: JsonObject)

data class Folder(
    val id: String,
    val parent: String? = null,
    val name: String? = null,
    val notes: String? = null,
    val hidden: Boolean? = null,
    val data: List<DataRecord>? = null,
)

data class Network(
    val name: String? = null,
    val notes: String? = null,
    val folder: String? = null,
    val hidden: Boolean? = null,
    /** Keyed by node identifier, scoped to this network (SPEC.md section 4.4). */
    val nodes: Map<String, Position>? = null,
    val arcs: Map<String, NetworkArc>? = null,
    val chains: List<Chain>? = null,
    val points: List<Waypoint>? = null,
    val areas: List<Area>? = null,
    val data: List<DataRecord>? = null,
)

/**
 * An arc in a network. `pips` is the arc's COMPLETE point list including the
 * positions of both endpoint nodes (SPEC.md section 3.3.2); a reader must not
 * prepend or append node positions to it.
 */
data class NetworkArc(
    val from: String,
    val to: String,
    val pips: List<Position>,
    val data: List<DataRecord>? = null,
)

data class Chain(
    val links: List<Link>,
    val name: String? = null,
    val notes: String? = null,
    val hidden: Boolean? = null,
    val style: JsonObject? = null,
    val data: List<DataRecord>? = null,
)

/**
 * A link endpoint: either a node key or an inline position on the named arc
 * (SPEC.md section 3.3.3).
 */
sealed interface Endpoint {
    data class Node(val key: String) : Endpoint
    data class Inline(val position: Position) : Endpoint
}

data class Link(val arc: String, val from: Endpoint, val to: Endpoint)

data class Route(
    val arcs: List<RouteArc>,
    val name: String? = null,
    val notes: String? = null,
    val folder: String? = null,
    val hidden: Boolean? = null,
    val style: JsonObject? = null,
    val data: List<DataRecord>? = null,
)

data class RouteArc(val pips: List<Position>, val data: List<DataRecord>? = null)

data class Track(
    val segments: List<Segment>,
    val name: String? = null,
    val notes: String? = null,
    val folder: String? = null,
    val hidden: Boolean? = null,
    val style: JsonObject? = null,
    val started: String? = null,
    val ended: String? = null,
    val data: List<DataRecord>? = null,
) {
    /**
     * Moving time in milliseconds, or null where it is not derivable
     * (SPEC.md section 3.5).
     *
     * The sum over segments of the difference between the last and first pip
     * times IN THAT SEGMENT. Deliberately not the wall-clock span between
     * `started` and `ended`, which pauses lengthen.
     *
     * Three rules that each look like a detail and are not:
     *
     * It is derivable only when EVERY segment carries a time on its first and
     * its last pip. A partial sum is worse than zero rather than better: it is
     * indistinguishable from a complete one, so no consumer can tell which it was
     * given, where zero is at least recognisable as suspicious.
     *
     * The times are read from the ENDS of the segment, not from the first and
     * last pip that happen to carry one. A segment whose interior is timed and
     * whose ends are not has no measurable span.
     *
     * An EMPTY segment contributes zero and does not make the track unknown: it
     * has no span to measure, so it is not an unmeasured span. A track with no
     * segments AT ALL is unknown, for the same reason a partial sum is.
     */
    fun movingTimeMs(): Long? {
        if (segments.isEmpty()) return null
        var total = 0L
        for (segment in segments) {
            if (segment.pips.isEmpty()) continue
            val first = segment.pips.first().timeMs ?: return null
            val last = segment.pips.last().timeMs ?: return null
            total += (last - first).toLong()
        }
        return total
    }
}

data class Segment(val pips: List<Position>)

/**
 * A top-level waypoint or a network's point. SPEC.md section 3.6 makes them one
 * shape; section 3.10 gives a `folder` to the waypoint only, so `folder` is always
 * null on a network's point.
 */
data class Waypoint(
    val at: Position,
    val name: String? = null,
    val notes: String? = null,
    val folder: String? = null,
    val hidden: Boolean? = null,
    val style: JsonObject? = null,
    val data: List<DataRecord>? = null,
)

/**
 * A closed ring (SPEC.md section 3.7). Holes and multiple rings are not
 * expressible in this format.
 */
data class Area(
    val polygon: List<Position>,
    val name: String? = null,
    val notes: String? = null,
    val folder: String? = null,
    val hidden: Boolean? = null,
    val style: JsonObject? = null,
    val data: List<DataRecord>? = null,
)

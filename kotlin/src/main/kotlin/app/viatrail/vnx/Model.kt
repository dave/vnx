package app.viatrail.vnx

/**
 * The decoded VNX document model.
 *
 * Two properties of this model are load-bearing and easy to lose.
 *
 * **A position keeps its arity.** SPEC.md section 4.1 makes `[lon, lat, null]`
 * and `[lon, lat]` the same statement but not the same spelling, and says
 * shortening happens in the writer rather than in the model. A model with a
 * nullable elevation field — the shape most implementations reach for first —
 * cannot re-render `accept/absent-elevation-and-time.vnx`.
 *
 * **Absent is not empty.** Every optional collection is nullable, because
 * section 3.10 says an empty array is distinct from an absent one and a
 * re-render has to reproduce whichever the document carried.
 */
data class VnxDocument(
    val vnx: String,
    val profile: String? = null,
    val generator: String? = null,
    val created: String? = null,
    val folders: List<Folder>? = null,
    val networks: List<Network>? = null,
    val routes: List<Route>? = null,
    val tracks: List<Track>? = null,
    val waypoints: List<Waypoint>? = null,
    val areas: List<Area>? = null,
    /**
     * Opaque. SPEC.md section 3.9: rulesets are not defined in 0.1, and a 0.1.x
     * reader treats the contents as opaque and preserves them through a
     * read-modify-write.
     */
    val rulesets: JsonValue? = null,
)

/**
 * A position: `[lon, lat, ele, time_ms]`, longitude first (SPEC.md section 4.1).
 *
 * Channels from index 2 upward may be `null` or omitted; the list holds exactly
 * what the document held, so arity survives a read-modify-write.
 */
data class Position(val channels: List<Double?>) {
    init {
        require(channels.size >= 2) { "a position carries at least lon and lat" }
        require(channels[0] != null && channels[1] != null) { "lon and lat are never null" }
    }

    val lon: Double get() = channels[0]!!
    val lat: Double get() = channels[1]!!
    val ele: Double? get() = channels.getOrNull(2)
    val timeMs: Double? get() = channels.getOrNull(3)
    val arity: Int get() = channels.size

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
        fun of(lon: Double, lat: Double) = Position(listOf(lon, lat))
    }
}

/**
 * A data record (SPEC.md section 5). The body is an opaque JSON object apart
 * from `@type`, and must survive a read-modify-write at value level whether or
 * not the reader recognises the type.
 */
data class DataRecord(val type: String, val body: JsonObject) {
    companion object {
        /** SPEC.md section 5.3: the namespace is reserved without regard to case. */
        const val VIATRAIL_PREFIX = "app.viatrail."
    }

    /**
     * Whether this record is inside the reserved `app.viatrail.` namespace, which
     * section 5.3 matches case-insensitively so that a near-miss produced by a
     * capitalising keyboard is not classified as a third party's extension.
     *
     * ASCII folding rather than `lowercase()`: `lowercase()` is locale-sensitive
     * in the JVM's default locale for the letter `i`, and there is an `i` in
     * `viatrail`.
     */
    val isViatrailNamespace: Boolean
        get() = type.length >= VIATRAIL_PREFIX.length &&
            type.regionMatches(0, VIATRAIL_PREFIX, 0, VIATRAIL_PREFIX.length, ignoreCase = true)
}

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
     * Moving time in milliseconds, or null when the track carries no pip times
     * and the question cannot be answered (SPEC.md section 3.5).
     *
     * The sum over segments of the difference between the last and first pip
     * times in that segment. Deliberately not the wall-clock span between
     * `started` and `ended`, which pauses lengthen — and never 0 for a track
     * with no times, which is indistinguishable from a track on which nobody
     * moved.
     */
    fun movingTimeMs(): Long? {
        var total = 0L
        var answered = false
        for (segment in segments) {
            val first = segment.pips.firstOrNull { it.timeMs != null } ?: continue
            val last = segment.pips.lastOrNull { it.timeMs != null } ?: continue
            total += (last.timeMs!! - first.timeMs!!).toLong()
            answered = true
        }
        return if (answered) total else null
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

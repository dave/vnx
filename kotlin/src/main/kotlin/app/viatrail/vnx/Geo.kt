package app.viatrail.vnx

/**
 * The projection tolerance model of SPEC.md section 3.3.3.
 *
 * The spec states the model because the choice decides which documents are
 * legal, not because the arithmetic is delicate: four plausible models read one
 * endpoint of the worked example at 19.81 m, 20.34 m, 25.52 m and 36.47 m, and
 * Web Mercator — the largest — is the one an implementation carrying a tile
 * pipeline reaches for first.
 */
internal object Geo {

    const val EARTH_RADIUS_M = 6371008.8
    const val TOLERANCE_M = 50.0

    /**
     * The least distance in metres from [point] to the polyline [pips].
     *
     * Every coordinate is projected by a local equirectangular map about the
     * latitude OF THE QUERY POINT, held constant for every vertex. Holding one
     * latitude keeps the map affine, so a straight polyline segment stays
     * straight and the plane geometry is exact. Taking the cosine of each
     * segment's start latitude instead reads a north-south arc measurably
     * further away, and is one of the four models section 3.3.3 rules out.
     *
     * Returns null when the polyline has no vertices: section 3.3.3 says such a
     * polyline has no distance and the reader must record a diagnostic and drop
     * the link rather than invent a number.
     */
    fun distanceToPolyline(point: Position, pips: List<Position>): Double? {
        if (pips.isEmpty()) return null
        val cosLat = Math.cos(Math.toRadians(point.lat))
        fun x(p: Position) = Math.toRadians(p.lon) * EARTH_RADIUS_M * cosLat
        fun y(p: Position) = Math.toRadians(p.lat) * EARTH_RADIUS_M

        val px = x(point)
        val py = y(point)
        // A polyline of one vertex is that point.
        if (pips.size == 1) return Math.hypot(px - x(pips[0]), py - y(pips[0]))

        var best = Double.MAX_VALUE
        for (i in 0 until pips.size - 1) {
            val ax = x(pips[i])
            val ay = y(pips[i])
            val bx = x(pips[i + 1])
            val by = y(pips[i + 1])
            best = minOf(best, distanceToSegment(px, py, ax, ay, bx, by))
        }
        return best
    }

    /** The distance from (px, py) to the nearest point on the segment a-b. */
    private fun distanceToSegment(
        px: Double, py: Double,
        ax: Double, ay: Double,
        bx: Double, by: Double,
    ): Double {
        val dx = bx - ax
        val dy = by - ay
        val lengthSquared = dx * dx + dy * dy
        if (lengthSquared == 0.0) return Math.hypot(px - ax, py - ay)
        var t = ((px - ax) * dx + (py - ay) * dy) / lengthSquared
        if (t < 0.0) t = 0.0
        if (t > 1.0) t = 1.0
        return Math.hypot(px - (ax + t * dx), py - (ay + t * dy))
    }
}

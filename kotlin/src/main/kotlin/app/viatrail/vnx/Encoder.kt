package app.viatrail.vnx

import java.math.BigDecimal
import java.math.RoundingMode

/**
 * Turns a decoded model back into a JSON value tree.
 *
 * There is one operation, and it is WRITE: SPEC.md section 4.1's emit-boundary
 * rules always apply — coordinates rounded, and the shortest spelling of a
 * position. Writing what was read is legitimately not what was read, byte for
 * byte, because a writer rounds; section 8.1 makes the comparison a comparison
 * of JSON trees for exactly that reason.
 */
internal class Encoder {

    fun encode(doc: VnxDocument): JsonValue = JsonObject.Builder()
        .putString("vnx", doc.vnx)
        .putString("profile", doc.profile)
        .putString("generator", doc.generator)
        .putString("created", doc.created)
        .put("folders", doc.folders?.let { array(it, ::folder) })
        .put("networks", doc.networks?.let { array(it, ::network) })
        .put("routes", doc.routes?.let { array(it, ::route) })
        .put("tracks", doc.tracks?.let { array(it, ::track) })
        .put("waypoints", doc.waypoints?.let { array(it) { w -> waypoint(w, inNetwork = false) } })
        .put("areas", doc.areas?.let { array(it) { a -> area(a, inNetwork = false) } })
        .put("rulesets", doc.rulesets)
        .build()

    private fun <T> array(items: List<T>, f: (T) -> JsonValue) = JsonArray(items.map(f))

    // ------------------------------------------------------------ positions

    /**
     * Section 4.1: a writer rounds at the serialisation boundary, longitude and
     * latitude to 6 decimal places and elevation to 1. Time is an integer count
     * of milliseconds and is not rounded. Rounding MUST NOT happen in the model,
     * because endpoint comparison and de-duplication compare positions by full
     * value.
     *
     * Section 4.1 again: a writer emits the SHORTEST spelling of a position,
     * which means dropping trailing nulls. An interior absence keeps its `null` —
     * dropping it would move the time into the elevation channel.
     */
    fun position(p: Position): JsonArray {
        var channels: List<Double?> = listOf(
            Rounding.toPlaces(p.lon, 6),
            Rounding.toPlaces(p.lat, 6),
            p.ele?.let { Rounding.toPlaces(it, 1) },
            p.timeMs,
        )
        while (channels.size > 2 && channels.last() == null) {
            channels = channels.dropLast(1)
        }
        return JsonArray(channels.map { if (it == null) JsonNull else JsonNumber(it) })
    }

    private fun positions(list: List<Position>) = JsonArray(list.map(::position))

    // -------------------------------------------------------------- records

    private fun data(records: List<DataRecord>?): JsonValue? = records?.let {
        JsonArray(
            it.map { record ->
                val entries = ArrayList<Pair<String, JsonValue>>()
                entries.add("@type" to JsonString(record.type))
                entries.addAll(record.body.entries())
                JsonObject(entries)
            },
        )
    }

    // --------------------------------------------------------------- items

    private fun folder(f: Folder) = JsonObject.Builder()
        .putString("id", f.id)
        .putString("parent", f.parent)
        .putString("name", f.name)
        .putString("notes", f.notes)
        .putBool("hidden", hidden(f.hidden))
        .put("data", data(f.data))
        .build()

    private fun network(n: Network) = JsonObject.Builder()
        .putString("name", n.name)
        .putString("notes", n.notes)
        .putString("folder", n.folder)
        .putBool("hidden", hidden(n.hidden))
        .put("nodes", n.nodes?.let { JsonObject(it.map { (k, v) -> k to position(v) }) })
        .put("arcs", n.arcs?.let { JsonObject(it.map { (k, v) -> k to networkArc(v) }) })
        .put("chains", n.chains?.let { array(it, ::chain) })
        .put("points", n.points?.let { array(it) { w -> waypoint(w, inNetwork = true) } })
        .put("areas", n.areas?.let { array(it) { a -> area(a, inNetwork = true) } })
        .put("data", data(n.data))
        .build()

    private fun networkArc(a: NetworkArc) = JsonObject.Builder()
        .putString("from", a.from)
        .putString("to", a.to)
        .put("pips", positions(a.pips))
        .put("data", data(a.data))
        .build()

    private fun chain(c: Chain) = JsonObject.Builder()
        .putString("name", c.name)
        .putString("notes", c.notes)
        .putBool("hidden", hidden(c.hidden))
        .put("style", c.style)
        .put("links", JsonArray(c.links.map(::link)))
        .put("data", data(c.data))
        .build()

    private fun link(l: Link) = JsonObject.Builder()
        .putString("arc", l.arc)
        .put("from", endpoint(l.from))
        .put("to", endpoint(l.to))
        .build()

    private fun endpoint(e: Endpoint): JsonValue = when (e) {
        is Endpoint.Node -> JsonString(e.key)
        is Endpoint.Inline -> position(e.position)
    }

    private fun route(r: Route) = JsonObject.Builder()
        .putString("name", r.name)
        .putString("notes", r.notes)
        .putString("folder", r.folder)
        .putBool("hidden", hidden(r.hidden))
        .put("style", r.style)
        .put("arcs", JsonArray(r.arcs.map(::routeArc)))
        .put("data", data(r.data))
        .build()

    private fun routeArc(a: RouteArc) = JsonObject.Builder()
        .put("pips", positions(a.pips))
        .put("data", data(a.data))
        .build()

    private fun track(t: Track) = JsonObject.Builder()
        .putString("name", t.name)
        .putString("notes", t.notes)
        .putString("folder", t.folder)
        .putBool("hidden", hidden(t.hidden))
        .putString("started", t.started)
        .putString("ended", t.ended)
        .put("style", t.style)
        .put("segments", JsonArray(t.segments.map { JsonObject(listOf("pips" to positions(it.pips))) }))
        .put("data", data(t.data))
        .build()

    private fun waypoint(w: Waypoint, inNetwork: Boolean) = JsonObject.Builder()
        .putString("name", w.name)
        .putString("notes", w.notes)
        .putString("folder", if (inNetwork) null else w.folder)
        .putBool("hidden", hidden(w.hidden))
        .put("at", position(w.at))
        .put("style", w.style)
        .put("data", data(w.data))
        .build()

    private fun area(a: Area, inNetwork: Boolean) = JsonObject.Builder()
        .putString("name", a.name)
        .putString("notes", a.notes)
        .putString("folder", if (inNetwork) null else a.folder)
        .putBool("hidden", hidden(a.hidden))
        .put("polygon", positions(a.polygon))
        .put("style", a.style)
        .put("data", data(a.data))
        .build()

    /**
     * Section 3.8: a writer emits `hidden` only when its value is true, and a
     * document SHOULD NOT contain `"hidden": false`. So a document that carried
     * one loses it here, which is the writer obeying the rule rather than
     * copying its input.
     */
    private fun hidden(value: Boolean?): Boolean? = if (value == true) true else null
}

/**
 * The emit-boundary rounding of SPEC.md section 4.1: longitude and latitude to 6
 * decimal places, elevation to 1, over the exact binary value of the double.
 * Time is an integer count of milliseconds and is not rounded.
 *
 * ROUNDING IS BY FORMATTING, NEVER BY SCALING, which is what section 4.1
 * requires in as many words. `BigDecimal(value)` is the EXACT binary value, so
 * `setScale` is the correctly rounded decimal of the number the writer was
 * handed — the same operation as formatting to that many places and parsing the
 * text back. Scaling by a power of ten first rounds a product that is itself a
 * binary64, which manufactures ties the true decimal never had and destroys ties
 * it did: measured in Go, the scaling route disagreed with the shared reference
 * table on 175 of 5,329 rows.
 *
 * Ties round to EVEN. Measured 2026-09-09: Go's `strconv.FormatFloat` and
 * Python's `%.1f` both write 340.25 to one place as `340.2`, where JavaScript's
 * `toFixed` gives `340.3`. `RoundingMode.HALF_UP` agrees with `toFixed` and is
 * wrong here. The tie is not a corner case in these channels — at 1 place every
 * odd quarter is one, so every quarter-metre elevation, and at 6 places every
 * odd multiple of 1/128.
 *
 * `Math.round` is the other trap and the worse one: in Java and JavaScript alike
 * `Math.round(x * 10) / 10` rounds a tie towards POSITIVE INFINITY, which is not
 * this rule. Where it differs is decided by the PARITY OF THE LOWER CANDIDATE,
 * not by the sign:
 *
 * ```
 *   340.25 -> 340.2 here, 340.3 there    differs
 *   340.75 -> 340.8 here, 340.8 there    agrees
 *  -340.25 -> -340.2 here, -340.2 there  agrees
 *  -340.75 -> -340.8 here, -340.7 there  differs
 * ```
 *
 * It differs exactly where this rule rounds towards negative infinity. So a
 * fixture carrying only `340.75` passes a `Math.round` implementation, and one
 * carrying only positives does not: `340.25` catches it on its own.
 */
internal object Rounding {
    fun toPlaces(value: Double, places: Int): Double =
        BigDecimal(value).setScale(places, RoundingMode.HALF_EVEN).toDouble()
}

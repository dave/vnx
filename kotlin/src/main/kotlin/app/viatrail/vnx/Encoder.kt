package app.viatrail.vnx

import java.math.BigDecimal
import java.math.RoundingMode

/**
 * Turns a decoded model back into a JSON value tree.
 *
 * SPEC.md section 8.1 distinguishes two operations that both produce a document,
 * and this class is both of them, selected by [rounding]:
 *
 * | Operation | What it does |
 * |---|---|
 * | WRITE | applies the emit-boundary rules: rounds coordinates (4.5.4) and emits the shortest spelling of a position (4.1) |
 * | RE-RENDER | applies neither, so arity and precision survive |
 *
 * The accept tier of the corpus requires the second. Writing what was read is
 * legitimately not what was read, because a writer rounds.
 */
internal class Encoder(private val rounding: Boolean) {

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
     * Section 4.5.4: a writer rounds at the serialisation boundary, longitude and
     * latitude to 6 decimal places and elevation to 1. Time is an integer count
     * of milliseconds and is not rounded. Rounding MUST NOT happen in the model,
     * because endpoint comparison and de-duplication compare positions by full
     * value.
     *
     * Section 4.1: a writer emits the SHORTEST spelling of a position, which
     * means dropping trailing nulls. An interior absence keeps its `null` —
     * dropping it would move the time into the elevation channel.
     */
    fun position(p: Position): JsonArray {
        var channels: List<Double?> = p.channels
        if (rounding) {
            channels = channels.mapIndexed { index, value ->
                when {
                    value == null -> null
                    index < 2 -> round(value, 6)
                    index == 2 -> round(value, 1)
                    else -> value
                }
            }
            while (channels.size > 2 && channels.last() == null) {
                channels = channels.dropLast(1)
            }
        }
        return JsonArray(channels.map { if (it == null) JsonNull else JsonNumber(it) })
    }

    private fun round(value: Double, places: Int): Double = Rounding.toPlaces(value, places)

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
     * document SHOULD NOT contain `"hidden": false`. A re-render reproduces what
     * the document carried, false included, because it reproduces the document
     * rather than deciding what a writer would have said.
     */
    private fun hidden(value: Boolean?): Boolean? = when {
        value == null -> null
        rounding && !value -> null
        else -> value
    }
}

/**
 * The emit-boundary rounding of SPEC.md section 4.5.4: longitude and latitude to
 * 6 decimal places, elevation to 1, over the exact binary value of the double.
 * Time is an integer count of milliseconds and is not rounded.
 *
 * A separate object because it is a separate code path from number RENDERING and
 * needs its own guard. Both need the same tie rule and only rendering is reached
 * by the conformance corpus: every corpus tier re-renders an existing document,
 * and rounding happens only when a writer emits a model, so no fixture can
 * exercise this at all. `NumberConformanceTest` is what covers it.
 *
 * Ties round to EVEN. Measured 2026-09-09: Go's `strconv.FormatFloat` and
 * Python's `%.1f` both write 340.25 to one place as `340.2`, where JavaScript's
 * `toFixed` gives `340.3`. `RoundingMode.HALF_UP` agrees with `toFixed` and is
 * wrong here. The tie is not a corner case in these channels — at 1 place every
 * odd quarter is one, so every quarter-metre elevation, and at 6 places every
 * odd multiple of 1/128.
 */
internal object Rounding {
    fun toPlaces(value: Double, places: Int): Double =
        BigDecimal(value).setScale(places, RoundingMode.HALF_EVEN).toDouble()
}

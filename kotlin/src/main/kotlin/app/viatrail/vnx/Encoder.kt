package app.viatrail.vnx

/**
 * Turns a decoded model back into a JSON value tree.
 *
 * There is one operation, and it is WRITE: SPEC.md section 4.1's one
 * emit-boundary rule always applies — the shortest spelling of a position.
 * Writing what was read is legitimately not what was read, byte for byte,
 * because a writer may spell a number differently and omits `hidden: false`;
 * section 8.1 makes the comparison a comparison of JSON trees for that reason.
 */
internal class Encoder {

    fun encode(doc: VnxDocument): JsonValue {
        checkAppKeys(doc.appKeys)
        return JsonObject.Builder()
            .putString("vnx", doc.vnx)
            .putString("generator", doc.generator)
            .putString("created", doc.created)
            .put("folders", doc.folders?.let { array(it, ::folder) })
            .put("networks", doc.networks?.let { array(it, ::network) })
            .put("routes", doc.routes?.let { array(it, ::route) })
            .put("tracks", doc.tracks?.let { array(it, ::track) })
            .put("waypoints", doc.waypoints?.let { array(it) { w -> waypoint(w, inNetwork = false) } })
            .put("areas", doc.areas?.let { array(it) { a -> area(a, inNetwork = false) } })
            .apply { for (k in doc.appKeys) put(k.key, k.value) }
            .build()
    }

    /**
     * Section 3.9: each application key is written verbatim, a `null` as `null`.
     *
     * A KEY WITH NO FULL STOP IS REFUSED, NOT WRITTEN: a reader would take it
     * for an unrecognised key and drop it, or for one of the format's own keys.
     * A key given twice is refused too: which of two equal members a reader
     * keeps is its JSON parser's choice (section 6.4).
     */
    private fun checkAppKeys(keys: List<AppKey>) {
        val seen = HashSet<String>()
        for (k in keys) {
            require(AppKey.isAppKey(k.key)) {
                "application key \"${k.key}\" contains no full stop (SPEC.md section 3.9)"
            }
            require(seen.add(k.key)) { "application key \"${k.key}\" appears twice" }
        }
    }

    private fun <T> array(items: List<T>, f: (T) -> JsonValue) = JsonArray(items.map(f))

    // ------------------------------------------------------------ positions

    /**
     * Section 4.1: a writer emits the SHORTEST spelling of a position, which
     * means dropping trailing nulls. An interior absence keeps its `null` —
     * dropping it would move the time into the elevation channel. That is ARITY,
     * not precision.
     *
     * A VALUE PASSES THROUGH UNCHANGED. Section 4.1 does not constrain how
     * precisely a writer states a coordinate: rounding is advice there, mirroring
     * [RFC 7946] Section 11.2, and it carries no requirement. A caller that wants
     * smaller files rounds its own model before handing it here.
     */
    fun position(p: Position): JsonArray {
        var channels: List<Double?> = listOf(p.lon, p.lat, p.ele, p.timeMs)
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

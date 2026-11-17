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
        return JsonObject
            .Builder()
            .putString("vnx", doc.vnx)
            .putString("generator", doc.generator)
            .putString("created", doc.created)
            .put("folders", doc.folders?.let { array(it, ::folder) })
            .put("networks", doc.networks?.let { array(it, ::network) })
            .put("plans", doc.plans?.let { array(it, ::plan) })
            .put("tracks", doc.tracks?.let { array(it, ::track) })
            .put("points", doc.points?.let { array(it) { w -> point(w, inNetwork = false) } })
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

    private fun <T> array(
        items: List<T>,
        f: (T) -> JsonValue,
    ) = JsonArray(items.map(f))

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

    private fun data(records: List<DataRecord>?): JsonValue? =
        records?.let {
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

    private fun folder(f: Folder) =
        JsonObject
            .Builder()
            .putString("id", f.id)
            .putString("parent", f.parent)
            .putString("name", f.name)
            .putString("notes", f.notes)
            .putBool("hidden", hidden(f.hidden))
            .put("data", data(f.data))
            .build()

    /**
     * A network, checked first: every arc runs between two different nodes it
     * holds, and every highlight's walk follows section 3.3.3. A writer never emits
     * what its own reader would drop.
     */
    private fun network(n: Network): JsonObject {
        val nodes = n.nodes ?: emptyMap()
        val arcs = n.arcs ?: emptyMap()
        val owner = "network ${jsonQuoted(n.name.orEmpty())}"
        checkArcs(owner, nodes, arcs)
        n.highlights?.forEachIndexed { i, h -> checkWalkOf("$owner highlight $i", h.walk, arcs, plan = false) }
        return JsonObject
            .Builder()
            .putString("name", n.name)
            .putString("notes", n.notes)
            .putString("folder", n.folder)
            .putBool("hidden", hidden(n.hidden))
            .putString("pinned", n.pinned)
            .put("nodes", n.nodes?.let(::nodes))
            .put("arcs", n.arcs?.let(::arcs))
            .put("highlights", n.highlights?.let { array(it, ::highlight) })
            .put("points", n.points?.let { array(it) { w -> point(w, inNetwork = true) } })
            .put("areas", n.areas?.let { array(it) { a -> area(a, inNetwork = true) } })
            .put("data", data(n.data))
            .build()
    }

    /** A plan: a network's nodes and arcs, and one walk that runs node to node (section 3.4). */
    private fun plan(r: Plan): JsonObject {
        val arcs = r.arcs ?: emptyMap()
        val owner = "plan ${jsonQuoted(r.name.orEmpty())}"
        checkArcs(owner, r.nodes ?: emptyMap(), arcs)
        checkWalkOf(owner, r.walk, arcs, plan = true)
        return JsonObject
            .Builder()
            .putString("name", r.name)
            .putString("notes", r.notes)
            .putString("folder", r.folder)
            .putBool("hidden", hidden(r.hidden))
            .putString("pinned", r.pinned)
            .put("style", r.style)
            .put("nodes", r.nodes?.let(::nodes))
            .put("arcs", r.arcs?.let(::arcs))
            .put("walk", walk(r.walk))
            .put("data", data(r.data))
            .build()
    }

    /**
     * Section 3.3.2: an arc's `from` and `to` name two DIFFERENT nodes of its
     * owner. A self-loop cannot be walked, and an arc naming an absent node has
     * no line; a reader drops either, so this writer refuses to emit one.
     */
    private fun checkArcs(
        owner: String,
        nodes: Map<String, Vertex>,
        arcs: Map<String, Arc>,
    ) {
        for ((key, arc) in arcs) {
            require(arc.from != arc.to) {
                "$owner: arc \"$key\" runs from node \"${arc.from}\" to itself, and a self-loop cannot be written"
            }
            for (node in listOf(arc.from, arc.to)) {
                require(node in nodes) { "$owner: arc \"$key\" names node \"$node\", which is not in its owner's nodes" }
            }
        }
    }

    /** [s] as a JSON string literal, so a name holding a quote or backslash reads unambiguously (as every library writes it). */
    private fun jsonQuoted(s: String): String =
        kotlinx.serialization.json
            .JsonPrimitive(s)
            .toString()

    private fun checkWalkOf(
        owner: String,
        walk: List<Pass>,
        arcs: Map<String, Arc>,
        plan: Boolean,
    ) {
        val fault = checkWalk(walk, arcs, plan) ?: return
        val at = "pass ${fault.pass}" + (fault.key?.let { " `$it`" } ?: "")
        throw IllegalArgumentException("$owner: $at: ${fault.message} (${fault.cause.corpusCode})")
    }

    private fun nodes(nodes: Map<String, Vertex>) = JsonObject(nodes.map { (k, v) -> k to position(v.position()) })

    private fun arcs(arcs: Map<String, Arc>) = JsonObject(arcs.map { (k, v) -> k to arc(v) })

    /** `pips` is always written, empty for a straight arc: section 3.10 makes the key required. */
    private fun arc(a: Arc) =
        JsonObject
            .Builder()
            .putString("from", a.from)
            .putString("to", a.to)
            .put("pips", JsonArray(a.pips.map { position(it.position()) }))
            .put("data", data(a.data))
            .build()

    private fun highlight(c: Highlight) =
        JsonObject
            .Builder()
            .putString("name", c.name)
            .putBool("hidden", hidden(c.hidden))
            .putString("pinned", c.pinned)
            .put("walk", walk(c.walk))
            .put("data", data(c.data))
            .build()

    /**
     * A walk. `from` and `to` are written where the pass carries them, which is
     * where section 3.3.3's rule puts them; `turn` and `pin` where the pass has one.
     */
    private fun walk(walk: List<Pass>) =
        JsonArray(
            walk.map { p ->
                JsonObject
                    .Builder()
                    .putString("arc", p.arc)
                    .put("from", p.from?.let(::endpoint))
                    .put("to", p.to?.let(::endpoint))
                    .put("turn", p.turn?.let { position(it.position()) })
                    .put("pin", p.pin?.let { position(it.position()) })
                    .build()
            },
        )

    private fun endpoint(e: Endpoint): JsonValue =
        when (e) {
            is Endpoint.Node -> JsonString(e.key)
            is Endpoint.At -> position(e.position.position())
        }

    private fun track(t: Track) =
        JsonObject
            .Builder()
            .putString("name", t.name)
            .putString("notes", t.notes)
            .putString("folder", t.folder)
            .putBool("hidden", hidden(t.hidden))
            .putString("pinned", t.pinned)
            .putString("started", t.started)
            .putString("ended", t.ended)
            .put("style", t.style)
            .put("segments", JsonArray(t.segments.map { JsonObject(listOf("pips" to positions(it.pips))) }))
            .put("data", data(t.data))
            .build()

    private fun point(
        w: Point,
        inNetwork: Boolean,
    ) = JsonObject
        .Builder()
        .putString("name", w.name)
        .putString("notes", w.notes)
        .putString("folder", if (inNetwork) null else w.folder)
        .putBool("hidden", hidden(w.hidden))
        .putString("pinned", w.pinned)
        .put("at", position(w.at))
        .put("style", w.style)
        .put("data", data(w.data))
        .build()

    /**
     * An area, whose ring is checked first. A writer emits a ring OPEN, with at
     * least three distinct positions (section 3.7).
     *
     * A CLOSED RING IS REFUSED, NOT OPENED. Opening it would be this writer
     * guessing that its caller meant the repeat, and that is the caller's to fix.
     */
    private fun area(
        a: Area,
        inNetwork: Boolean,
    ): JsonObject {
        val ring = a.polygon
        val n = ring.size
        require(!(n >= 2 && ring[0].lon == ring[n - 1].lon && ring[0].lat == ring[n - 1].lat)) {
            "area \"${a.name}\": a ring is written open, and this one's last position repeats its first"
        }
        val distinct = distinctLonLat(ring)
        require(distinct >= 3) {
            "area \"${a.name}\": a ring needs at least 3 distinct positions, and this one has $distinct"
        }
        return JsonObject
            .Builder()
            .putString("name", a.name)
            .putString("notes", a.notes)
            .putString("folder", if (inNetwork) null else a.folder)
            .putBool("hidden", hidden(a.hidden))
            .putString("pinned", a.pinned)
            .put("polygon", positions(a.polygon))
            .put("style", a.style)
            .put("data", data(a.data))
            .build()
    }

    /**
     * Section 3.8: a writer emits `hidden` only when its value is true, and a
     * document SHOULD NOT contain `"hidden": false`. So a document that carried
     * one loses it here, which is the writer obeying the rule rather than
     * copying its input.
     */
    private fun hidden(value: Boolean?): Boolean? = if (value == true) true else null
}

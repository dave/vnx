package app.viatrail.vnx

/**
 * JSON-tree comparison, which is what the corpus compares now that the bytes of
 * a document are not constrained (SPEC.md section 8.1).
 *
 * IT IS NEVER MODEL-VERSUS-MODEL, and that is the finding the accept tier rests
 * on. A reader that discarded every data record, every application key, every
 * style and every `notes` produces a model that compares equal to itself, and passes an
 * accept tier built that way green — that is the whole extensibility mechanism,
 * all styling and all user prose. Comparing the tree parsed from the ORIGINAL
 * FILE against the tree parsed from `write(read(file))` is what proves a reader
 * kept what it read. AcceptTierTest freezes the proof.
 */
internal object Tree {
    /**
     * Which normalisations a comparison applies.
     *
     * THE TWO TIERS NORMALISE DIFFERENTLY, AND MUST. The accept tier compares a
     * document against a re-encoding of itself, so the shortest spelling of a
     * position is applied to both sides — otherwise the tier would require the
     * file to have been written by this library. The write tier compares one
     * writer's output against another's, where shortening is the very rule under
     * test: normalising it there would make the tier prove nothing.
     */
    class Options(
        val emitBoundary: Boolean,
    )

    /** Section 8.1's two permitted normalisations, and no others. */
    fun normalise(
        value: JsonValue,
        opt: Options,
    ): JsonValue {
        val doc = value as? JsonObject ?: return value
        val out = mutable(doc)
        items(out, "folders", opt, ::folder)
        items(out, "waypoints", opt, ::waypoint)
        items(out, "areas", opt, ::area)
        items(out, "tracks", opt, ::track)
        items(out, "routes", opt, ::route)
        items(out, "networks", opt, ::network)
        // Application keys are not touched: section 3.9 exempts them from the
        // absent-equals-empty normalisation, so an empty or null value is
        // compared as written and a key dropped for holding one is a difference.
        return JsonObject(out.entries.map { it.key to it.value })
    }

    private fun folder(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        dropEmptyContainer(o, "data")
    }

    private fun waypoint(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        o["at"]?.let { o["at"] = position(it, opt) }
        dropEmptyContainer(o, "style")
        dropEmptyContainer(o, "data")
    }

    private fun area(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        positionList(o, "polygon", opt)
        dropEmptyContainer(o, "style")
        dropEmptyContainer(o, "data")
    }

    private fun track(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        items(o, "segments", opt) { segment, _ -> positionList(segment, "pips", opt) }
        dropEmptyContainer(o, "style")
        dropEmptyContainer(o, "data")
    }

    private fun route(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        graph(o, opt)
        items(o, "walk", opt, ::pass)
        dropEmptyContainer(o, "style")
        dropEmptyContainer(o, "data")
    }

    /** A network's or a route's `nodes` and `arcs`, which have one shape (section 3.4). */
    private fun graph(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        (o["nodes"] as? JsonObject)?.let { nodes ->
            o["nodes"] = JsonObject(nodes.entries().map { (k, v) -> k to position(v, opt) })
        }
        (o["arcs"] as? JsonObject)?.let { arcs ->
            o["arcs"] =
                JsonObject(
                    arcs.entries().map { (k, v) ->
                        if (v !is JsonObject) {
                            k to v
                        } else {
                            val inner = mutable(v)
                            arc(inner, opt)
                            k to JsonObject(inner.entries.map { it.key to it.value })
                        }
                    },
                )
        }
        dropEmptyContainer(o, "nodes")
        dropEmptyContainer(o, "arcs")
    }

    /**
     * A pass. `from` and `to` are a node key or a position (section 3.3.3), and
     * `turn` is a position; [position] leaves a string alone.
     */
    private fun pass(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        for (key in listOf("from", "to", "turn")) o[key]?.let { o[key] = position(it, opt) }
    }

    private fun arc(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        positionList(o, "pips", opt)
        dropEmptyContainer(o, "data")
    }

    private fun network(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        graph(o, opt)
        items(o, "chains", opt, ::chain)
        items(o, "points", opt, ::waypoint)
        items(o, "areas", opt, ::area)
        dropEmptyContainer(o, "data")
    }

    private fun chain(
        o: MutableMap<String, JsonValue>,
        opt: Options,
    ) {
        items(o, "walk", opt, ::pass)
        dropEmptyContainer(o, "style")
        dropEmptyContainer(o, "data")
    }

    /** Normalises an array of objects under [key], and drops the key when empty. */
    private fun items(
        o: MutableMap<String, JsonValue>,
        key: String,
        opt: Options,
        each: (MutableMap<String, JsonValue>, Options) -> Unit,
    ) {
        val arr = o[key] as? JsonArray ?: return
        if (arr.elements.isEmpty()) {
            o.remove(key)
            return
        }
        o[key] =
            JsonArray(
                arr.elements.map { item ->
                    if (item !is JsonObject) {
                        item
                    } else {
                        val inner = mutable(item)
                        each(inner, opt)
                        JsonObject(inner.entries.map { it.key to it.value })
                    }
                },
            )
    }

    private fun positionList(
        o: MutableMap<String, JsonValue>,
        key: String,
        opt: Options,
    ) {
        val arr = o[key] as? JsonArray ?: return
        if (arr.elements.isEmpty()) {
            o.remove(key)
            return
        }
        o[key] = JsonArray(arr.elements.map { position(it, opt) })
    }

    /**
     * What section 8.1 lets a comparison ignore about a position: a trailing
     * `null`, and nothing else. It is an emit-boundary rule and is applied only
     * where [Options.emitBoundary] says so.
     *
     * A VALUE IS NEVER NORMALISED. Section 4.1 stopped requiring a writer to
     * round, so a comparison that rounded both sides would hide a writer that
     * damaged a coordinate — and section 8.1 names the two permitted
     * normalisations, which no longer include rounding.
     *
     * A CHANNEL THIS VERSION DOES NOT NAME IS NOT NORMALISED AWAY, and no corpus
     * file carries one. Section 4.1's preservation rule is gone, so a fixture
     * asking a writer to reproduce a fifth channel would be asking for a rule the
     * format does not state — the fixture is what changes, not the comparison.
     */
    private fun position(
        value: JsonValue,
        opt: Options,
    ): JsonValue {
        val arr = value as? JsonArray ?: return value
        if (!opt.emitBoundary) return arr
        var out: List<JsonValue> = arr.elements
        while (out.size > 2 && out.last() is JsonNull) out = out.dropLast(1)
        return JsonArray(out)
    }

    /**
     * Section 3.1: an absent collection and an empty one carry the same content,
     * and a reader MUST treat them alike. So the comparison does too, and a
     * writer that omits its empty containers stays conforming.
     */
    private fun dropEmptyContainer(
        o: MutableMap<String, JsonValue>,
        key: String,
    ) {
        when (val v = o[key]) {
            is JsonArray -> if (v.elements.isEmpty()) o.remove(key)
            is JsonObject -> if (v.size == 0) o.remove(key)
            else -> {}
        }
    }

    private fun mutable(o: JsonObject): MutableMap<String, JsonValue> {
        val out = LinkedHashMap<String, JsonValue>(o.size)
        for ((k, v) in o.entries()) out[k] = v
        return out
    }

    /**
     * The first place two trees differ, in JSON Pointer form, or null when they
     * are equal.
     *
     * A PATH rather than a boolean, because a failing corpus case has to say
     * which rule broke to be actionable.
     */
    fun diff(
        got: JsonValue,
        want: JsonValue,
        path: String = "",
    ): String? {
        when (want) {
            is JsonObject -> {
                if (got !is JsonObject) return "${pathOrRoot(path)}: got ${describe(got)}, want an object"
                for (key in want.keys.sorted()) {
                    val mine = got[key] ?: return "${pathOrRoot(path)}: key \"$key\" is missing"
                    diff(mine, want[key]!!, "$path/${escape(key)}")?.let { return it }
                }
                val extra = (got.keys - want.keys).sorted()
                if (extra.isNotEmpty()) return "${pathOrRoot(path)}: unexpected key(s) $extra"
            }
            is JsonArray -> {
                if (got !is JsonArray) return "${pathOrRoot(path)}: got ${describe(got)}, want an array"
                if (got.elements.size != want.elements.size) {
                    return "${pathOrRoot(path)}: got ${got.elements.size} element(s), " +
                        "want ${want.elements.size}"
                }
                for (i in want.elements.indices) {
                    diff(got.elements[i], want.elements[i], "$path/$i")?.let { return it }
                }
            }
            is JsonNumber -> {
                // A number compares by VALUE, so -0 and 0 are the same number.
                // `JsonNumber` is a data class over a Double, whose own equals
                // says otherwise, which is why this reads the doubles.
                if (got !is JsonNumber || got.value != want.value) {
                    return "${pathOrRoot(path)}: got ${describe(got)}, want ${describe(want)}"
                }
            }
            else -> if (got != want) return "${pathOrRoot(path)}: got ${describe(got)}, want ${describe(want)}"
        }
        return null
    }

    /** Parses two documents and returns the first difference between their trees. */
    fun compare(
        got: String,
        want: String,
        opt: Options,
    ): String? = diff(normalise(JsonParser.parse(got), opt), normalise(JsonParser.parse(want), opt))

    private fun pathOrRoot(p: String) = if (p.isEmpty()) "the document" else p

    private fun escape(key: String) = key.replace("~", "~0").replace("/", "~1")

    private fun describe(v: JsonValue?): String =
        when (v) {
            null -> "nothing"
            is JsonNull -> "null"
            is JsonString -> "\"${v.value}\""
            is JsonBool -> v.value.toString()
            is JsonNumber -> v.value.toString()
            is JsonObject -> "an object"
            is JsonArray -> "an array"
        }
}

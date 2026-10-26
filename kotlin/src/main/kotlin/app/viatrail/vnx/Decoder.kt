package app.viatrail.vnx

/**
 * The reader of SPEC.md section 6: tolerant of damage and silent about nothing.
 *
 * A refusal leaves through [VnxRefusal] and is one of the four causes section
 * 6.2 permits. Everything else that could not be used as written is dropped or
 * skipped, and reported as a [Diagnostic].
 */
internal class Decoder(
    private val readerVersion: Version = Version.SUPPORTED,
) {
    private val diagnostics = ArrayList<Diagnostic>()
    private var folderIds: Set<String> = emptySet()

    // ---------------------------------------------------------------- keys

    /**
     * The recognised keys of SPEC.md section 3.10, which is the set the
     * unrecognised-key rule of section 6.2 cannot be implemented without.
     */
    private object Keys {
        val DOCUMENT_REQUIRED = setOf("vnx")
        val DOCUMENT_OPTIONAL =
            setOf(
                "generator",
                "created",
                "folders",
                "networks",
                "plans",
                "tracks",
                "waypoints",
                "areas",
            )
        val FOLDER_REQUIRED = setOf("id")
        val FOLDER_OPTIONAL = setOf("parent", "name", "notes", "hidden", "data")
        val NETWORK_REQUIRED = emptySet<String>()
        val NETWORK_OPTIONAL =
            setOf(
                "name",
                "notes",
                "folder",
                "hidden",
                "pinned",
                "nodes",
                "arcs",
                "highlights",
                "points",
                "areas",
                "data",
            )

        // An arc has one shape in a network and in a plan (section 3.4).
        val ARC_REQUIRED = setOf("from", "to", "pips")
        val ARC_OPTIONAL = setOf("data")
        val HIGHLIGHT_REQUIRED = setOf("walk")
        val HIGHLIGHT_OPTIONAL = setOf("name", "notes", "hidden", "pinned", "style", "data")
        val PASS_REQUIRED = setOf("arc")
        val PASS_OPTIONAL = setOf("from", "to", "turn", "pin")
        val PLAN_REQUIRED = setOf("walk")
        val PLAN_OPTIONAL = setOf("name", "notes", "folder", "hidden", "pinned", "style", "nodes", "arcs", "data")
        val TRACK_REQUIRED = setOf("segments")
        val TRACK_OPTIONAL = setOf("name", "notes", "folder", "hidden", "pinned", "style", "started", "ended", "data")
        val SEGMENT_REQUIRED = setOf("pips")
        val SEGMENT_OPTIONAL = emptySet<String>()
        val WAYPOINT_REQUIRED = setOf("at")
        val WAYPOINT_OPTIONAL = setOf("name", "notes", "folder", "hidden", "pinned", "style", "data")

        // A network's point is a waypoint without `folder`: its network already
        // contains it (section 3.10).
        val POINT_OPTIONAL = setOf("name", "notes", "hidden", "pinned", "style", "data")
        val AREA_REQUIRED = setOf("polygon")
        val AREA_OPTIONAL = setOf("name", "notes", "folder", "hidden", "pinned", "style", "data")
        val NETWORK_AREA_OPTIONAL = setOf("name", "notes", "hidden", "pinned", "style", "data")
    }

    // ------------------------------------------------------------ entry point

    fun decode(root: JsonValue): ReadResult {
        val doc =
            root as? JsonObject
                ?: refuse(RefusalCause.NOT_A_VNX_DOCUMENT, "not a VNX document: the top level is not a JSON object")

        val versionValue =
            doc["vnx"]
                ?: refuse(RefusalCause.NOT_A_VNX_DOCUMENT, "not a VNX document: it carries no `vnx` key")
        val versionText =
            (versionValue as? JsonString)?.value
                ?: refuse(
                    RefusalCause.VERSION_MALFORMED,
                    "`vnx` must be a version string of the shape MAJOR.MINOR.PATCH",
                )
        val version =
            Version.parseOrNull(versionText)
                ?: refuse(
                    RefusalCause.VERSION_MALFORMED,
                    "`vnx` is \"$versionText\", which is not of the shape MAJOR.MINOR.PATCH " +
                        "— three runs of decimal digits separated by full stops",
                )
        if (!version.isSupportedBy(readerVersion)) {
            // Section 2.2 requires the refusal to NAME the version, and the two
            // directions are not one sentence. A document from a series this
            // reader is too old for is answered with the reader a user needs; a
            // document from a series this reader has no rules for is answered
            // with the series, because no newer reader would help.
            refuse(
                RefusalCause.VERSION_SERIES_UNSUPPORTED,
                "this document is version $version and this reader implements $readerVersion, " +
                    if (version.isAbove(readerVersion)) {
                        "so a newer reader is needed to read it"
                    } else {
                        "so the ${version.major}.${version.minor} series is not one it supports"
                    },
            )
        }

        val at = Pointer.ROOT
        // Section 3.9: at the TOP LEVEL ONLY, a key containing a full stop is an
        // application key and is recognised. Every other object keeps its fixed
        // key set, so a dotted key on an item is still reported.
        recogniseKeys(doc, at, Keys.DOCUMENT_REQUIRED, Keys.DOCUMENT_OPTIONAL, AppKey::isAppKey)

        // Folders first: every other item's `folder` is checked against these ids,
        // and a folder that was itself dropped must not resolve.
        val folders = optionalItems(doc, "folders", at) { o, p -> folder(o, p) }
        folderIds = folders?.map { it.id }?.toSet() ?: emptySet()

        val document =
            VnxDocument(
                vnx = versionText,
                generator = optionalString(doc, "generator", at),
                created = optionalTimestamp(doc, "created", at),
                folders = folders,
                networks = optionalItems(doc, "networks", at) { o, p -> network(o, p) },
                plans = optionalItems(doc, "plans", at) { o, p -> plan(o, p) },
                tracks = optionalItems(doc, "tracks", at) { o, p -> track(o, p) },
                waypoints = optionalItems(doc, "waypoints", at) { o, p -> waypoint(o, p, inNetwork = false) },
                areas = optionalItems(doc, "areas", at) { o, p -> area(o, p, inNetwork = false) },
                // Section 3.9: opaque, preserved verbatim, interpreted not at all.
                // A `null` value is kept as JsonNull; it is a present key.
                appKeys =
                    doc
                        .entries()
                        .filter { (k, _) -> AppKey.isAppKey(k) }
                        .map { (k, v) -> AppKey(k, v) }
                        .sortedBy { it.key },
            )
        return ReadResult(document, diagnostics.toList())
    }

    // ------------------------------------------------------------- reporting

    private fun report(
        at: Pointer,
        message: String,
        cause: DiagnosticCause,
    ) {
        diagnostics.add(Diagnostic(at.toString(), message, cause))
    }

    /**
     * Section 6.2: an unrecognised key on an object this format defines is
     * recorded, skipped, and the item kept. An unrecognised key is not an
     * unusable value.
     */
    private fun recogniseKeys(
        obj: JsonObject,
        at: Pointer,
        required: Set<String>,
        optional: Set<String>,
        alsoRecognised: (String) -> Boolean = { false },
    ) {
        for (key in obj.keys) {
            if (key !in required && key !in optional && !alsoRecognised(key)) {
                report(at.child(key), "unknown key `$key`, skipped", DiagnosticCause.UNKNOWN_KEY)
            }
        }
    }

    private fun wrongType(
        at: Pointer,
        key: String,
        expected: String,
        dropped: String,
    ) {
        report(
            at.child(key),
            "`$key` must be $expected; $dropped",
            DiagnosticCause.WRONG_TYPE_OPTIONAL,
        )
    }

    // -------------------------------------------------------------- scalars

    private fun optionalString(
        obj: JsonObject,
        key: String,
        at: Pointer,
    ): String? {
        val v = obj[key] ?: return null
        if (v !is JsonString) {
            wrongType(at, key, "a string", "the field is dropped and the item kept")
            return null
        }
        return v.value
    }

    private fun optionalBool(
        obj: JsonObject,
        key: String,
        at: Pointer,
    ): Boolean? {
        val v = obj[key] ?: return null
        if (v !is JsonBool) {
            wrongType(at, key, "a boolean", "the field is dropped and the item kept")
            return null
        }
        return v.value
    }

    private fun optionalStyle(
        obj: JsonObject,
        at: Pointer,
    ): JsonObject? {
        val v = obj["style"] ?: return null
        if (v !is JsonObject) {
            wrongType(at, "style", "an object", "the field is dropped and the item kept")
            return null
        }
        // Section 3.10: a style object is opaque. A reader carries any key in it
        // through a read-modify-write without reporting it, because style
        // vocabulary grows faster than structural vocabulary.
        //
        // `importance` is the one exception, and the two rules are in tension.
        // Section 3.6 states its range of 1 to 5 and says in as many words that
        // the range is normative and that a value outside it is handled under
        // section 6.2; section 6.2 in turn names `importance` as its worked
        // example of a range this document states normatively. Section 3.10's
        // opacity is about the UNRECOGNISED-key rule, which is a different rule,
        // so the narrow reading — check the one key the format gives a normative
        // range, carry every other key untouched — is the one that satisfies
        // both. Reported to Dave; no corpus case decides it.
        val importance = v["importance"]
        if (importance != null && !isImportanceInRange(importance)) {
            report(
                at.child("style").child("importance"),
                "`importance` is an integer from 1 to 5; the field is dropped and the item kept",
                if (importance is JsonNumber) DiagnosticCause.OUT_OF_RANGE else DiagnosticCause.WRONG_TYPE_OPTIONAL,
            )
            return JsonObject(v.entries().filter { it.first != "importance" })
        }
        return v
    }

    private fun isImportanceInRange(value: JsonValue): Boolean {
        val n = (value as? JsonNumber)?.value ?: return false
        return n >= 1.0 && n <= 5.0 && n == Math.floor(n)
    }

    /**
     * Section 4.3: a record-level timestamp is ISO 8601 in UTC with a literal `Z`
     * and OPTIONAL fractional seconds. An offset other than `Z` is an unusable
     * value in an optional key, so the field goes and the item stays (6.2).
     */
    private fun optionalTimestamp(
        obj: JsonObject,
        key: String,
        at: Pointer,
    ): String? {
        val text = optionalString(obj, key, at) ?: return null
        if (!TIMESTAMP.matches(text)) {
            report(
                at.child(key),
                "`$key` is \"$text\": a record-level timestamp must be UTC with a Z suffix, " +
                    "so the field is dropped and the item kept",
                DiagnosticCause.TIMESTAMP_NOT_UTC,
            )
            return null
        }
        return text
    }

    /**
     * The `folder` key. Section 6.2: a `folder` naming a folder that is not
     * present is NOT an unusable value — the key is treated as absent, so the
     * item sits at the top level, and a diagnostic is recorded. Dropping the item
     * would lose content over a lost grouping.
     */
    private fun optionalFolderRef(
        obj: JsonObject,
        at: Pointer,
    ): String? {
        val id = optionalString(obj, "folder", at) ?: return null
        if (id !in folderIds) {
            report(
                at.child("folder"),
                "`folder` names \"$id\", which is not a folder in this document; " +
                    "the item sits at the top level",
                DiagnosticCause.FOLDER_ABSENT,
            )
            return null
        }
        return id
    }

    // -------------------------------------------------------- collections

    /**
     * Decodes an optional collection. Returns null when the key is absent, which
     * section 3.10 distinguishes from an empty array — a distinction the accept
     * tier's round trip has to reproduce.
     */
    private fun <T> optionalItems(
        obj: JsonObject,
        key: String,
        at: Pointer,
        decode: (JsonObject, Pointer) -> T?,
    ): List<T>? {
        val v = obj[key] ?: return null
        if (v !is JsonArray) {
            wrongType(at, key, "an array", "the collection is dropped")
            return null
        }
        val here = at.child(key)
        val out = ArrayList<T>()
        for ((index, element) in v.elements.withIndex()) {
            val itemAt = here.child(index)
            if (element !is JsonObject) {
                report(
                    itemAt,
                    "an entry of `$key` must be an object; the item is dropped",
                    DiagnosticCause.WRONG_TYPE_REQUIRED,
                )
                continue
            }
            decode(element, itemAt)?.let(out::add)
        }
        return out
    }

    /**
     * Section 5: a `data` array holds records, each an object carrying `@type`.
     * A record with no `@type`, or one that is not a string, is dropped — the
     * record goes, not the key (6.2). The body is opaque and is not swept for
     * unrecognised keys.
     */
    private fun optionalData(
        obj: JsonObject,
        at: Pointer,
    ): List<DataRecord>? {
        val v = obj["data"] ?: return null
        if (v !is JsonArray) {
            wrongType(at, "data", "an array", "the field is dropped and the item kept")
            return null
        }
        val here = at.child("data")
        val out = ArrayList<DataRecord>()
        for ((index, element) in v.elements.withIndex()) {
            val recordAt = here.child(index)
            if (element !is JsonObject) {
                report(recordAt, "a data record must be an object; the record is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
                continue
            }
            val type = element["@type"]
            if (type !is JsonString) {
                report(
                    recordAt,
                    "a data record must carry an `@type` string; the record is dropped",
                    DiagnosticCause.DATA_RECORD_TYPE_MISSING,
                )
                continue
            }
            val body = JsonObject(element.entries().filter { it.first != "@type" })
            out.add(DataRecord(type.value, body))
        }
        return out
    }

    // --------------------------------------------------------- positions

    /**
     * Section 4.1. A position carries at least lon and lat; `null` marks an
     * absent value from index 2 upward; trailing channels may be omitted; and a
     * channel beyond those this version names is read, checked and dropped, with
     * no diagnostic, so that a future channel is additive.
     *
     * Returns null after calling [unusable] with the reason when the position
     * will not parse. It reports nothing itself: what the reader drops, and so
     * what the diagnostic says, belongs to the caller, which knows what the
     * position was part of.
     */
    private fun parsePosition(
        value: JsonArray,
        unusable: (String) -> Unit,
    ): Position? {
        if (value.elements.size < 2) {
            unusable("a position must carry at least lon and lat, and this one has ${value.elements.size} channels")
            return null
        }
        val channels = ArrayList<Double?>(value.elements.size)
        for ((index, channel) in value.elements.withIndex()) {
            when {
                channel is JsonNumber -> channels.add(channel.value)
                channel is JsonNull && index >= 2 -> channels.add(null)
                index < 2 -> {
                    unusable("lon and lat must both be numbers, and channel $index is ${channel.typeName()}")
                    return null
                }
                else -> {
                    unusable("channel $index of a position must be a number or null, and is ${channel.typeName()}")
                    return null
                }
            }
        }
        // Every channel is CHECKED, and then the ones this version names are
        // kept. Checking first and keeping second is what makes a fifth channel
        // of the wrong type a diagnostic rather than something nobody looked at.
        val problem = offTheGlobe(channels[0]!!, channels[1]!!)
        if (problem != null) {
            unusable(problem)
            return null
        }
        return Position(channels[0]!!, channels[1]!!, channels.getOrNull(2), channels.getOrNull(3))
    }

    /**
     * A position that IS its owner's required value: a waypoint's `at`.
     *
     * A value that is not an array at all is a value of the wrong JSON TYPE, not
     * a malformed position: the two are different facts and section 6.2 gives
     * them different codes. Both drop the owner, because a waypoint whose `at` is
     * a string is a waypoint that is nowhere.
     */
    private fun requiredPosition(
        value: JsonValue,
        at: Pointer,
    ): Position? {
        if (value !is JsonArray) {
            report(
                at,
                "a position must be an array of numbers, and this is ${value.typeName()}; the item is dropped",
                DiagnosticCause.WRONG_TYPE_REQUIRED,
            )
            return null
        }
        return parsePosition(value) { why ->
            report(at, "$why; the item is dropped", DiagnosticCause.POSITION_UNUSABLE)
        }
    }

    /**
     * A position inside something that holds positions: a node in `nodes`, a
     * pip in `pips`, a vertex in a ring. Section 6.2's table gives every one of
     * them `position-unusable`, whether the element is not an array or is an
     * array that will not parse, and has the caller drop the OWNER — [dropped]
     * says which owner that is.
     */
    private fun memberPosition(
        value: JsonValue,
        at: Pointer,
        dropped: String,
    ): Position? {
        if (value !is JsonArray) {
            report(
                at,
                "a position must be an array of numbers, and this is ${value.typeName()}; $dropped",
                DiagnosticCause.POSITION_UNUSABLE,
            )
            return null
        }
        return parsePosition(value) { why -> report(at, "$why; $dropped", DiagnosticCause.POSITION_UNUSABLE) }
    }

    /**
     * A required `pips` array. Returns null when the owner must be dropped.
     *
     * A position inside `pips` that will not parse drops the OWNER — the arc or
     * the segment — and not the pip (section 6.2). `pips` is an array and is
     * perfectly usable; one element in it is not, and dropping just that element
     * leaves a shorter, plausible, wrong line that nothing downstream can notice.
     */
    private fun requiredPips(
        obj: JsonObject,
        at: Pointer,
        owner: String,
    ): List<Position>? {
        val here = at.child("pips")
        val v = obj["pips"]
        if (v == null) {
            report(here, "$owner requires `pips`; the $owner is dropped", DiagnosticCause.MISSING_REQUIRED_KEY)
            return null
        }
        if (v !is JsonArray) {
            report(here, "`pips` must be an array; the $owner is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
            return null
        }
        val out = ArrayList<Position>()
        for ((index, element) in v.elements.withIndex()) {
            out.add(memberPosition(element, here.child(index), "the $owner is dropped") ?: return null)
        }
        return out
    }

    // ----------------------------------------------------------- folders

    private fun folder(
        obj: JsonObject,
        at: Pointer,
    ): Folder? {
        recogniseKeys(obj, at, Keys.FOLDER_REQUIRED, Keys.FOLDER_OPTIONAL)
        val id = obj["id"]
        if (id !is JsonString) {
            // Section 6.2: a folder with no `id` is dropped, and the items that
            // named it are then covered by the missing-folder rule and sit at the
            // top level, so a folder's contents survive the loss of the folder.
            report(
                at,
                "a folder requires an `id` string; the folder is dropped and its items sit at the top level",
                if (obj["id"] == null) DiagnosticCause.MISSING_REQUIRED_KEY else DiagnosticCause.WRONG_TYPE_REQUIRED,
            )
            return null
        }
        return Folder(
            id = id.value,
            parent = optionalString(obj, "parent", at),
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            hidden = optionalBool(obj, "hidden", at),
            data = optionalData(obj, at),
        )
    }

    // ---------------------------------------------------- networks and plans

    private fun network(
        obj: JsonObject,
        at: Pointer,
    ): Network {
        recogniseKeys(obj, at, Keys.NETWORK_REQUIRED, Keys.NETWORK_OPTIONAL)
        // Nodes, then arcs, then walks: a walk can only be checked against the
        // arcs that survived, and an arc only against the nodes that did.
        val nodes = nodes(obj, at)
        val arcs = arcs(obj, at, nodes)
        return Network(
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            folder = optionalFolderRef(obj, at),
            hidden = optionalBool(obj, "hidden", at),
            pinned = optionalTimestamp(obj, "pinned", at),
            nodes = nodes,
            arcs = arcs,
            highlights = optionalItems(obj, "highlights", at) { o, p -> highlight(o, p, arcs ?: emptyMap()) },
            points = optionalItems(obj, "points", at) { o, p -> waypoint(o, p, inNetwork = true) },
            areas = optionalItems(obj, "areas", at) { o, p -> area(o, p, inNetwork = true) },
            data = optionalData(obj, at),
        )
    }

    /**
     * A plan is its own highlight (section 3.4): it owns nodes and arcs of a
     * network's shape and one walk over them, and wherever a highlight would be
     * dropped the whole plan is.
     */
    private fun plan(
        obj: JsonObject,
        at: Pointer,
    ): Plan? {
        recogniseKeys(obj, at, Keys.PLAN_REQUIRED, Keys.PLAN_OPTIONAL)
        val nodes = nodes(obj, at)
        val arcs = arcs(obj, at, nodes)
        // Every field is read before the walk, so a plan the walk drops still
        // reports what else was wrong with it.
        val name = optionalString(obj, "name", at)
        val notes = optionalString(obj, "notes", at)
        val folder = optionalFolderRef(obj, at)
        val hidden = optionalBool(obj, "hidden", at)
        val pinned = optionalTimestamp(obj, "pinned", at)
        val style = optionalStyle(obj, at)
        val data = optionalData(obj, at)
        val walk = walk(obj, at, arcs ?: emptyMap(), plan = true) ?: return null
        return Plan(walk, nodes, arcs, name, notes, folder, hidden, pinned, style, data)
    }

    /**
     * A network's or a plan's `nodes` (section 3.3.1). A node that will not
     * parse is dropped; an arc naming it is then dropped under
     * `arc-node-absent`, so the loss surfaces on everything that depended on it.
     */
    private fun nodes(
        obj: JsonObject,
        at: Pointer,
    ): Map<String, Vertex>? {
        val v = obj["nodes"] ?: return null
        if (v !is JsonObject) {
            wrongType(at, "nodes", "an object keyed by node identifier", "the field is dropped")
            return null
        }
        val here = at.child("nodes")
        val out = LinkedHashMap<String, Vertex>()
        for ((key, value) in v.entries()) {
            val nodeAt = here.child(key)
            val p = memberPosition(value, nodeAt, "the node is dropped") ?: continue
            // A node is a place in a graph rather than a moment in a recording.
            if (p.timeMs != null) {
                report(
                    nodeAt,
                    "a node carries no time; the time channel is dropped and the node kept",
                    DiagnosticCause.EXTRA_CHANNEL,
                )
            }
            out[key] = Vertex(p.lon, p.lat, p.ele)
        }
        return out
    }

    /**
     * A network's or a plan's `arcs` (section 3.3.2).
     *
     * AN ARC THAT IS DROPPED TAKES EVERY HIGHLIGHT WALKING IT WITH IT, but not here:
     * the highlight is dropped when its walk is read, under `pass-arc-absent`, with a
     * diagnostic of its own (section 6.2).
     */
    private fun arcs(
        obj: JsonObject,
        at: Pointer,
        nodes: Map<String, Vertex>?,
    ): Map<String, Arc>? {
        val v = obj["arcs"] ?: return null
        if (v !is JsonObject) {
            wrongType(at, "arcs", "an object keyed by arc identifier", "the field is dropped")
            return null
        }
        val here = at.child("arcs")
        val out = LinkedHashMap<String, Arc>()
        for ((key, value) in v.entries()) {
            val arcAt = here.child(key)
            if (value !is JsonObject) {
                report(arcAt, "an arc must be an object; the arc is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
                continue
            }
            recogniseKeys(value, arcAt, Keys.ARC_REQUIRED, Keys.ARC_OPTIONAL)
            arc(value, arcAt, nodes ?: emptyMap())?.let { out[key] = it }
        }
        return out
    }

    private fun arc(
        obj: JsonObject,
        at: Pointer,
        nodes: Map<String, Vertex>,
    ): Arc? {
        val ends = ArrayList<String>(2)
        for (end in listOf("from", "to")) {
            val v = obj[end]
            if (v !is JsonString) {
                report(
                    at.child(end),
                    "an arc requires `$end` naming a node; the arc is dropped",
                    if (v == null) DiagnosticCause.MISSING_REQUIRED_KEY else DiagnosticCause.WRONG_TYPE_REQUIRED,
                )
                return null
            }
            ends.add(v.value)
        }
        val (from, to) = ends
        val pips = requiredPips(obj, at, "arc") ?: return null
        // An arc is geometry every highlight walking it shares, and a time belongs to
        // one recording. ONE diagnostic for the arc, at its `pips`, however many
        // pips carried a time (section 6.3 rule 3).
        if (pips.any { it.timeMs != null }) {
            report(
                at.child("pips"),
                "an arc pip carries no time; every time channel on this arc is dropped and its pips kept",
                DiagnosticCause.ARC_PIP_TIME,
            )
        }
        val data = optionalData(obj, at)
        if (from == to) {
            report(
                at,
                "the arc runs from node \"$from\" to itself, and a self-loop cannot be walked; the arc is dropped",
                DiagnosticCause.SELF_LOOP_ARC,
            )
            return null
        }
        // Both ends are checked, and each absent one is its own diagnostic at
        // its own key: the other end, and the pips, are usable.
        var absent = false
        for ((end, node) in listOf("from" to from, "to" to to)) {
            if (node !in nodes) {
                absent = true
                report(
                    at.child(end),
                    "`$end` names node \"$node\", which is not in this network or plan; the arc is dropped",
                    DiagnosticCause.ARC_NODE_ABSENT,
                )
            }
        }
        if (absent) return null
        return Arc(from, to, pips.map { Vertex(it.lon, it.lat, it.ele) }, data)
    }

    // ------------------------------------------------------------ highlights

    private fun highlight(
        obj: JsonObject,
        at: Pointer,
        arcs: Map<String, Arc>,
    ): Highlight? {
        recogniseKeys(obj, at, Keys.HIGHLIGHT_REQUIRED, Keys.HIGHLIGHT_OPTIONAL)
        // Every field is read before the walk, so a highlight the walk drops still
        // reports what else was wrong with it.
        val name = optionalString(obj, "name", at)
        val notes = optionalString(obj, "notes", at)
        val hidden = optionalBool(obj, "hidden", at)
        val pinned = optionalTimestamp(obj, "pinned", at)
        val style = optionalStyle(obj, at)
        val data = optionalData(obj, at)
        val walk = walk(obj, at, arcs, plan = false) ?: return null
        return Highlight(walk, name, notes, hidden, pinned, style, data)
    }

    /**
     * A highlight's or a plan's REQUIRED `walk`, checked in the order section 6.2
     * fixes. Returns null when the owner is to be dropped.
     *
     * A WALK IS USED WHOLE OR NOT AT ALL. Dropping a pass would hand the caller
     * a different walk that reads as valid — two arcs joined that do not meet, a
     * direction reversed by a lost turn, a start moved to a node — so the first
     * fault drops the owner, it is reported alone, and nothing after it in the
     * walk is examined. Diagnostics that do not drop the owner are recorded as
     * they are met.
     */
    private fun walk(
        obj: JsonObject,
        owner: Pointer,
        arcs: Map<String, Arc>,
        plan: Boolean,
    ): List<Pass>? {
        val here = owner.child("walk")
        val raw = obj["walk"]
        if (raw !is JsonArray) {
            report(
                here,
                "`walk` must be an array of passes; the ${if (plan) "plan" else "highlight"} is dropped",
                if (raw == null) DiagnosticCause.MISSING_REQUIRED_KEY else DiagnosticCause.WRONG_TYPE_REQUIRED,
            )
            return null
        }
        val dropped = if (plan) "the plan is dropped" else "the highlight is dropped"

        fun drop(fault: WalkFault): List<Pass>? {
            val at = here.child(fault.pass).let { if (fault.key == null) it else it.child(fault.key) }
            report(at, "${fault.message}; $dropped", fault.cause)
            return null
        }
        val cursor = WalkCursor(raw.elements.size, plan)
        val out = ArrayList<Pass>(raw.elements.size)
        for ((i, element) in raw.elements.withIndex()) {
            val passAt = here.child(i)
            // 1. The pass is an object naming an arc of its owner.
            if (element !is JsonObject) {
                return drop(
                    WalkFault(i, null, DiagnosticCause.PASS_ARC_ABSENT, "a pass must be an object, and this is ${element.typeName()}"),
                )
            }
            val arcKey = (element["arc"] as? JsonString)?.value
            val arc =
                arcKey?.let { arcs[it] }
                    ?: return drop(
                        WalkFault(
                            i,
                            null,
                            DiagnosticCause.PASS_ARC_ABSENT,
                            if (arcKey != null) {
                                "the pass names arc \"$arcKey\", which is not in this network or plan"
                            } else {
                                "the pass names no arc"
                            },
                        ),
                    )
            // 2. Unrecognised keys.
            recogniseKeys(element, passAt, Keys.PASS_REQUIRED, Keys.PASS_OPTIONAL)
            // 3. `from`, `to`, `turn` and `pin`, in that order, each parse.
            val ends = HashMap<String, Endpoint>()
            for (key in listOf("from", "to", "turn", "pin")) {
                val v = element[key] ?: continue
                val positionOnly = key == "turn" || key == "pin"
                if (v is JsonString && !positionOnly) {
                    ends[key] = Endpoint.Node(v.value)
                    continue
                }
                if (v !is JsonArray) {
                    val want = if (positionOnly) "a position" else "a node key or a position"
                    return drop(WalkFault(i, key, DiagnosticCause.POSITION_UNUSABLE, "`$key` is ${v.typeName()}, expected $want"))
                }
                var why: String? = null
                val p =
                    parsePosition(v) { why = it }
                        ?: return drop(WalkFault(i, key, DiagnosticCause.POSITION_UNUSABLE, why!!))
                if (p.ele != null || p.timeMs != null) {
                    report(
                        passAt.child(key),
                        "a walk position carries longitude and latitude only; its other channels are dropped",
                        DiagnosticCause.EXTRA_CHANNEL,
                    )
                }
                ends[key] = Endpoint.At(LonLat(p.lon, p.lat))
            }
            val pass =
                Pass(
                    arc = arcKey,
                    from = ends["from"],
                    to = ends["to"],
                    turn = (ends["turn"] as Endpoint.At?)?.position,
                    pin = (ends["pin"] as Endpoint.At?)?.position,
                )
            // 4 to 7: a plan's rule, pin and turn, spelling, connection.
            cursor.step(i, pass, arc)?.let { return drop(it) }
            out.add(pass)
        }
        return out
    }

    // ------------------------------------------------------------ tracks

    private fun track(
        obj: JsonObject,
        at: Pointer,
    ): Track? {
        recogniseKeys(obj, at, Keys.TRACK_REQUIRED, Keys.TRACK_OPTIONAL)
        val rawSegments = obj["segments"]
        if (rawSegments !is JsonArray) {
            report(
                at.child("segments"),
                "a track requires a `segments` array; the track is dropped",
                if (rawSegments == null) DiagnosticCause.MISSING_REQUIRED_KEY else DiagnosticCause.WRONG_TYPE_REQUIRED,
            )
            return null
        }
        val segmentsAt = at.child("segments")
        val segments = ArrayList<Segment>()
        for ((index, element) in rawSegments.elements.withIndex()) {
            val segmentAt = segmentsAt.child(index)
            if (element !is JsonObject) {
                report(segmentAt, "a segment must be an object; the segment is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
                continue
            }
            recogniseKeys(element, segmentAt, Keys.SEGMENT_REQUIRED, Keys.SEGMENT_OPTIONAL)
            // The owner of a track's pips is the SEGMENT rather than the track:
            // section 3.5 already makes the interval between two segments a
            // pause, so a missing segment leaves a gap a consumer can see.
            val pips = requiredPips(element, segmentAt, "segment") ?: continue
            segments.add(Segment(pips))
        }
        return Track(
            segments = segments,
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            folder = optionalFolderRef(obj, at),
            hidden = optionalBool(obj, "hidden", at),
            pinned = optionalTimestamp(obj, "pinned", at),
            style = optionalStyle(obj, at),
            started = optionalTimestamp(obj, "started", at),
            ended = optionalTimestamp(obj, "ended", at),
            data = optionalData(obj, at),
        )
    }

    // ------------------------------------------------- waypoints and points

    private fun waypoint(
        obj: JsonObject,
        at: Pointer,
        inNetwork: Boolean,
    ): Waypoint? {
        recogniseKeys(
            obj,
            at,
            Keys.WAYPOINT_REQUIRED,
            if (inNetwork) Keys.POINT_OPTIONAL else Keys.WAYPOINT_OPTIONAL,
        )
        val rawAt = obj["at"]
        if (rawAt == null) {
            report(at.child("at"), "a waypoint requires `at`; the item is dropped", DiagnosticCause.MISSING_REQUIRED_KEY)
            return null
        }
        val position = requiredPosition(rawAt, at.child("at")) ?: return null
        return Waypoint(
            at = position,
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            folder = if (inNetwork) null else optionalFolderRef(obj, at),
            hidden = optionalBool(obj, "hidden", at),
            pinned = optionalTimestamp(obj, "pinned", at),
            style = optionalStyle(obj, at),
            data = optionalData(obj, at),
        )
    }

    // ------------------------------------------------------------- areas

    private fun area(
        obj: JsonObject,
        at: Pointer,
        inNetwork: Boolean,
    ): Area? {
        recogniseKeys(
            obj,
            at,
            Keys.AREA_REQUIRED,
            if (inNetwork) Keys.NETWORK_AREA_OPTIONAL else Keys.AREA_OPTIONAL,
        )
        val polygon = polygon(obj, at) ?: return null
        return Area(
            polygon = polygon,
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            folder = if (inNetwork) null else optionalFolderRef(obj, at),
            hidden = optionalBool(obj, "hidden", at),
            pinned = optionalTimestamp(obj, "pinned", at),
            style = optionalStyle(obj, at),
            data = optionalData(obj, at),
        )
    }

    /**
     * Section 3.7: `polygon` is ONE OPEN RING of at least three distinct
     * positions.
     *
     * Holes and multiple rings are not expressible, and a reader that meets a
     * nested or additional ring reports and DROPS the item: keeping the outer
     * ring silently would draw an area whose hole had vanished, with nothing
     * anywhere to say so.
     *
     * A CLOSED RING IS REPAIRED, NOT DROPPED. Its last position repeating its
     * first is one fact written twice, so dropping the repeat loses nothing; the
     * diagnostic is kept so that a writer that closes rings can be seen. The
     * distinct positions are counted after that, so `[A, B, A]` is two points
     * and its area goes, with both diagnostics.
     */
    private fun polygon(
        obj: JsonObject,
        at: Pointer,
    ): List<Position>? {
        val here = at.child("polygon")
        val v = obj["polygon"]
        if (v == null) {
            report(here, "an area requires `polygon`; the item is dropped", DiagnosticCause.MISSING_REQUIRED_KEY)
            return null
        }
        if (v !is JsonArray) {
            report(here, "`polygon` must be an array; the item is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
            return null
        }
        if (v.elements.any { it is JsonArray && it.elements.any { inner -> inner is JsonArray } }) {
            report(
                here,
                "`polygon` carries nested rings, which this format cannot express; the item is dropped",
                DiagnosticCause.POLYGON_NESTED_RINGS,
            )
            return null
        }
        val ring = ArrayList<Position>()
        for ((index, element) in v.elements.withIndex()) {
            ring.add(memberPosition(element, here.child(index), "the item is dropped") ?: return null)
        }
        val n = ring.size
        if (n >= 2 && ring[0].lon == ring[n - 1].lon && ring[0].lat == ring[n - 1].lat) {
            report(
                here.child(n - 1),
                "a ring is stored open, and this last position repeats the first; the repeat is dropped",
                DiagnosticCause.RING_CLOSED,
            )
            ring.removeAt(n - 1)
        }
        val distinct = distinctLonLat(ring)
        if (distinct < 3) {
            report(
                here,
                "the ring holds $distinct distinct position(s) and needs at least 3; the item is dropped",
                DiagnosticCause.RING_TOO_FEW_POINTS,
            )
            return null
        }
        return ring
    }

    private fun JsonValue.typeName() =
        when (this) {
            is JsonString -> "a string"
            is JsonNumber -> "a number"
            is JsonBool -> "a boolean"
            is JsonNull -> "null"
            is JsonArray -> "an array"
            is JsonObject -> "an object"
        }

    private companion object {
        /** Section 4.3: ISO 8601 in UTC, literal `Z`, optional fractional seconds. */
        val TIMESTAMP = Regex("^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(\\.\\d+)?Z$")
    }
}

/**
 * The number of positions in [ring] that differ in longitude or in latitude; no
 * other channel takes part (SPEC.md section 3.7).
 *
 * Compared as numbers, so `-0.0` and `0.0` are one longitude. Adding `0.0` makes
 * the two one key: a boxed [Double] tells them apart, and `==` does not.
 */
internal fun distinctLonLat(ring: List<Position>): Int = ring.map { (it.lon + 0.0) to (it.lat + 0.0) }.toSet().size

/** Section 4.1's normative ranges, both bounds included. Null when in range. */
internal fun offTheGlobe(
    lon: Double,
    lat: Double,
): String? =
    when {
        lon !in -180.0..180.0 -> "longitude $lon is off the globe"
        lat !in -90.0..90.0 -> "latitude $lat is off the globe"
        else -> null
    }

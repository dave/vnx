package app.viatrail.vnx

/**
 * The reader of SPEC.md section 6: tolerant of damage and silent about nothing.
 *
 * A refusal leaves through [VnxRefusal] and is one of the four causes section
 * 6.2 permits. Everything else that could not be used as written is dropped or
 * skipped, and reported as a [Diagnostic].
 */
internal class Decoder(private val readerVersion: Version = Version.SUPPORTED) {

    private val diagnostics = ArrayList<Diagnostic>()
    private var folderIds: Set<String> = emptySet()

    // ---------------------------------------------------------------- keys

    /**
     * The recognised keys of SPEC.md section 3.10, which is the set the
     * unrecognised-key rule of section 6.2 cannot be implemented without.
     */
    private object Keys {
        val DOCUMENT_REQUIRED = setOf("vnx")
        val DOCUMENT_OPTIONAL = setOf(
            "generator", "created",
            "folders", "networks", "routes", "tracks", "waypoints", "areas",
        )
        val FOLDER_REQUIRED = setOf("id")
        val FOLDER_OPTIONAL = setOf("parent", "name", "notes", "hidden", "data")
        val NETWORK_REQUIRED = emptySet<String>()
        val NETWORK_OPTIONAL = setOf(
            "name", "notes", "folder", "hidden", "nodes", "arcs", "chains", "points", "areas", "data",
        )
        val NETWORK_ARC_REQUIRED = setOf("from", "to", "pips")
        val NETWORK_ARC_OPTIONAL = setOf("data")
        val CHAIN_REQUIRED = setOf("links")
        val CHAIN_OPTIONAL = setOf("name", "notes", "hidden", "style", "data")
        val LINK_REQUIRED = setOf("arc", "from", "to")
        val LINK_OPTIONAL = emptySet<String>()
        val ROUTE_REQUIRED = setOf("arcs")
        val ROUTE_OPTIONAL = setOf("name", "notes", "folder", "hidden", "style", "data")
        val ROUTE_ARC_REQUIRED = setOf("pips")
        val ROUTE_ARC_OPTIONAL = setOf("data")
        val TRACK_REQUIRED = setOf("segments")
        val TRACK_OPTIONAL = setOf("name", "notes", "folder", "hidden", "style", "started", "ended", "data")
        val SEGMENT_REQUIRED = setOf("pips")
        val SEGMENT_OPTIONAL = emptySet<String>()
        val WAYPOINT_REQUIRED = setOf("at")
        val WAYPOINT_OPTIONAL = setOf("name", "notes", "folder", "hidden", "style", "data")
        // A network's point is a waypoint without `folder`: its network already
        // contains it (section 3.10).
        val POINT_OPTIONAL = setOf("name", "notes", "hidden", "style", "data")
        val AREA_REQUIRED = setOf("polygon")
        val AREA_OPTIONAL = setOf("name", "notes", "folder", "hidden", "style", "data")
        val NETWORK_AREA_OPTIONAL = setOf("name", "notes", "hidden", "style", "data")
    }

    // ------------------------------------------------------------ entry point

    fun decode(root: JsonValue): ReadResult {
        val doc = root as? JsonObject
            ?: refuse(RefusalCause.NOT_A_VNX_DOCUMENT, "not a VNX document: the top level is not a JSON object")

        val versionValue = doc["vnx"]
            ?: refuse(RefusalCause.NOT_A_VNX_DOCUMENT, "not a VNX document: it carries no `vnx` key")
        val versionText = (versionValue as? JsonString)?.value
            ?: refuse(
                RefusalCause.VERSION_MALFORMED,
                "`vnx` must be a version string of the shape MAJOR.MINOR.PATCH",
            )
        val version = Version.parseOrNull(versionText)
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
                    if (version.isAbove(readerVersion)) "so a newer reader is needed to read it"
                    else "so the ${version.major}.${version.minor} series is not one it supports",
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

        val document = VnxDocument(
            vnx = versionText,
            generator = optionalString(doc, "generator", at),
            created = optionalTimestamp(doc, "created", at),
            folders = folders,
            networks = optionalItems(doc, "networks", at) { o, p -> network(o, p) },
            routes = optionalItems(doc, "routes", at) { o, p -> route(o, p) },
            tracks = optionalItems(doc, "tracks", at) { o, p -> track(o, p) },
            waypoints = optionalItems(doc, "waypoints", at) { o, p -> waypoint(o, p, inNetwork = false) },
            areas = optionalItems(doc, "areas", at) { o, p -> area(o, p, inNetwork = false) },
            // Section 3.9: opaque, preserved verbatim, interpreted not at all.
            // A `null` value is kept as JsonNull; it is a present key.
            appKeys = doc.entries()
                .filter { (k, _) -> AppKey.isAppKey(k) }
                .map { (k, v) -> AppKey(k, v) }
                .sortedBy { it.key },
        )
        return ReadResult(document, diagnostics.toList())
    }

    // ------------------------------------------------------------- reporting

    private fun report(at: Pointer, message: String, cause: DiagnosticCause) {
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

    private fun wrongType(at: Pointer, key: String, expected: String, dropped: String) {
        report(
            at.child(key),
            "`$key` must be $expected; $dropped",
            DiagnosticCause.WRONG_TYPE_OPTIONAL,
        )
    }

    // -------------------------------------------------------------- scalars

    private fun optionalString(obj: JsonObject, key: String, at: Pointer): String? {
        val v = obj[key] ?: return null
        if (v !is JsonString) {
            wrongType(at, key, "a string", "the field is dropped and the item kept")
            return null
        }
        return v.value
    }

    private fun optionalBool(obj: JsonObject, key: String, at: Pointer): Boolean? {
        val v = obj[key] ?: return null
        if (v !is JsonBool) {
            wrongType(at, key, "a boolean", "the field is dropped and the item kept")
            return null
        }
        return v.value
    }

    private fun optionalStyle(obj: JsonObject, at: Pointer): JsonObject? {
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
    private fun optionalTimestamp(obj: JsonObject, key: String, at: Pointer): String? {
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
    private fun optionalFolderRef(obj: JsonObject, at: Pointer): String? {
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
    private fun optionalData(obj: JsonObject, at: Pointer): List<DataRecord>? {
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
     */
    private fun position(value: JsonValue, at: Pointer): Position? {
        if (value !is JsonArray) {
            // A value of the wrong JSON TYPE, not a malformed position: the two
            // are different facts and the corpus gives them different codes. Every
            // caller drops the thing that held it, so this is the required-key
            // remedy — a waypoint whose `at` is a string is a waypoint that is
            // nowhere.
            report(
                at,
                "a position must be an array of numbers, and this is ${value.typeName()}; " +
                    "the item is dropped",
                DiagnosticCause.WRONG_TYPE_REQUIRED,
            )
            return null
        }
        if (value.elements.size < 2) {
            report(
                at,
                "a position must carry at least lon and lat, and this one has ${value.elements.size} channels",
                DiagnosticCause.POSITION_UNUSABLE,
            )
            return null
        }
        val channels = ArrayList<Double?>(value.elements.size)
        for ((index, channel) in value.elements.withIndex()) {
            when {
                channel is JsonNumber -> channels.add(channel.value)
                channel is JsonNull && index >= 2 -> channels.add(null)
                index < 2 -> {
                    report(at, "lon and lat must both be numbers", DiagnosticCause.POSITION_UNUSABLE)
                    return null
                }
                else -> {
                    report(
                        at,
                        "channel $index of a position must be a number or null",
                        DiagnosticCause.POSITION_UNUSABLE,
                    )
                    return null
                }
            }
        }
        // Every channel is CHECKED, and then the ones this version names are
        // kept. Checking first and keeping second is what makes a fifth channel
        // of the wrong type a diagnostic rather than something nobody looked at.
        return Position(channels[0]!!, channels[1]!!, channels.getOrNull(2), channels.getOrNull(3))
    }

    /** A required `pips` array. Returns null when the host item must be dropped. */
    private fun requiredPips(obj: JsonObject, at: Pointer, owner: String): List<Position>? {
        val v = obj["pips"]
        if (v == null) {
            report(at, "$owner requires `pips`; the item is dropped", DiagnosticCause.MISSING_REQUIRED_KEY)
            return null
        }
        if (v !is JsonArray) {
            report(at.child("pips"), "`pips` must be an array; the item is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
            return null
        }
        val here = at.child("pips")
        val out = ArrayList<Position>()
        for ((index, element) in v.elements.withIndex()) {
            // Section 6.2: a position inside `pips` that will not parse drops the
            // ARC — or the segment — and not the pip. This is not the required-key
            // rule: `pips` is an array and is perfectly usable; one element in it
            // is not, and dropping just that element leaves a shorter, plausible,
            // wrong line that nothing downstream can notice.
            val p = position(element, here.child(index)) ?: return null
            out.add(p)
        }
        return out
    }

    // ----------------------------------------------------------- folders

    private fun folder(obj: JsonObject, at: Pointer): Folder? {
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

    // ---------------------------------------------------------- networks

    private fun network(obj: JsonObject, at: Pointer): Network {
        recogniseKeys(obj, at, Keys.NETWORK_REQUIRED, Keys.NETWORK_OPTIONAL)
        val nodes = nodes(obj, at)
        val arcs = networkArcs(obj, at, nodes)
        return Network(
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            folder = optionalFolderRef(obj, at),
            hidden = optionalBool(obj, "hidden", at),
            nodes = nodes,
            arcs = arcs,
            chains = optionalItems(obj, "chains", at) { o, p -> chain(o, p, nodes ?: emptyMap(), arcs ?: emptyMap()) },
            points = optionalItems(obj, "points", at) { o, p -> waypoint(o, p, inNetwork = true) },
            areas = optionalItems(obj, "areas", at) { o, p -> area(o, p, inNetwork = true) },
            data = optionalData(obj, at),
        )
    }

    private fun nodes(obj: JsonObject, at: Pointer): Map<String, Position>? {
        val v = obj["nodes"] ?: return null
        if (v !is JsonObject) {
            wrongType(at, "nodes", "an object keyed by node identifier", "the field is dropped")
            return null
        }
        val here = at.child("nodes")
        val out = LinkedHashMap<String, Position>()
        for ((key, value) in v.entries()) {
            position(value, here.child(key))?.let { out[key] = it }
        }
        return out
    }

    private fun networkArcs(obj: JsonObject, at: Pointer, nodes: Map<String, Position>?): Map<String, NetworkArc>? {
        val v = obj["arcs"] ?: return null
        if (v !is JsonObject) {
            wrongType(at, "arcs", "an object keyed by arc identifier", "the field is dropped")
            return null
        }
        val here = at.child("arcs")
        val out = LinkedHashMap<String, NetworkArc>()
        for ((key, value) in v.entries()) {
            val arcAt = here.child(key)
            if (value !is JsonObject) {
                report(arcAt, "an arc must be an object; the arc is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
                continue
            }
            recogniseKeys(value, arcAt, Keys.NETWORK_ARC_REQUIRED, Keys.NETWORK_ARC_OPTIONAL)
            val from = value["from"]
            val to = value["to"]
            if (from !is JsonString || to !is JsonString) {
                report(
                    arcAt,
                    "an arc requires `from` and `to` node keys; the arc is dropped",
                    DiagnosticCause.MISSING_REQUIRED_KEY,
                )
                continue
            }
            val pips = requiredPips(value, arcAt, "an arc") ?: continue
            // Section 6.2: an arc naming a node that is not in its network keeps
            // the arc — its geometry is real — and reports the fact.
            if (nodes != null) {
                // An arc with no pips has no terminal to compare, so the
                // section 3.3.2 check below simply has nothing to say about it.
                // Section 3.3.3 already reports the empty polyline where it
                // matters, at the link that could not be projected onto it.
                val terminals = listOf(
                    Triple(from.value, "from", pips.firstOrNull()),
                    Triple(to.value, "to", pips.lastOrNull()),
                )
                for ((endpointKey, endpointName, terminal) in terminals) {
                    val node = nodes[endpointKey]
                    if (node == null) {
                        report(
                            arcAt.child(endpointName),
                            "`$endpointName` names node \"$endpointKey\", which is not in this network; " +
                                "the arc is kept because its geometry is real",
                            DiagnosticCause.ARC_NODE_ABSENT,
                        )
                        continue
                    }
                    // Section 3.3.2: an arc's pips are its COMPLETE point list,
                    // so its first pip is the position of the node `from` names
                    // and its last is `to`'s. The two are one fact written
                    // twice; the pips are authoritative and both are kept.
                    //
                    // The path names the END rather than the arc, which is what
                    // the corpus case pins: the arc's other end and its interior
                    // pips are all usable, and a reader naming the arc emits two
                    // diagnostics at one identical path that a runner cannot
                    // tell apart, since section 6.3 forbids comparing English.
                    if (terminal == null) continue
                    val metres = Geo.distanceToPolyline(node, listOf(terminal)) ?: continue
                    if (metres > Geo.TOLERANCE_M) {
                        report(
                            arcAt.child(endpointName),
                            "node \"$endpointKey\" is ${Math.round(metres)} m from the $endpointName pip, " +
                                "beyond the 50 m tolerance; both are kept and the pips are authoritative",
                            DiagnosticCause.TERMINAL_PIP_OFF_NODE,
                        )
                    }
                }
            }
            out[key] = NetworkArc(
                from = from.value,
                to = to.value,
                pips = pips,
                data = optionalData(value, arcAt),
            )
        }
        return out
    }

    // ------------------------------------------------------------ chains

    private fun chain(
        obj: JsonObject,
        at: Pointer,
        nodes: Map<String, Position>,
        arcs: Map<String, NetworkArc>,
    ): Chain? {
        recogniseKeys(obj, at, Keys.CHAIN_REQUIRED, Keys.CHAIN_OPTIONAL)
        val rawLinks = obj["links"]
        if (rawLinks !is JsonArray) {
            report(
                at,
                "a chain requires a `links` array; the chain is dropped",
                if (rawLinks == null) DiagnosticCause.MISSING_REQUIRED_KEY else DiagnosticCause.WRONG_TYPE_REQUIRED,
            )
            return null
        }
        val linksAt = at.child("links")

        // Decoded links positionally, so the continuity check below can skip a
        // pair whose member was dropped. Section 6.2's link-names-absent-arc case
        // requires exactly one diagnostic: dropping a link must not then also
        // report the chain as broken, or one defect becomes two.
        val slots = arrayOfNulls<Link>(rawLinks.elements.size)
        for ((index, element) in rawLinks.elements.withIndex()) {
            slots[index] = link(element, linksAt.child(index), nodes, arcs)
        }
        checkContinuity(slots, linksAt, nodes)
        return Chain(
            links = slots.filterNotNull(),
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            hidden = optionalBool(obj, "hidden", at),
            style = optionalStyle(obj, at),
            data = optionalData(obj, at),
        )
    }

    private fun link(
        value: JsonValue,
        at: Pointer,
        nodes: Map<String, Position>,
        arcs: Map<String, NetworkArc>,
    ): Link? {
        if (value !is JsonObject) {
            report(at, "a link must be an object; the link is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
            return null
        }
        recogniseKeys(value, at, Keys.LINK_REQUIRED, Keys.LINK_OPTIONAL)
        val arcKey = value["arc"]
        if (arcKey !is JsonString) {
            report(
                at,
                "a link requires an `arc` key naming the arc it traverses; the link is dropped",
                if (value["arc"] == null) DiagnosticCause.MISSING_REQUIRED_KEY else DiagnosticCause.WRONG_TYPE_REQUIRED,
            )
            return null
        }
        val arc = arcs[arcKey.value]
        if (arc == null) {
            // Section 6.2: the link's whole meaning is the arc it traverses, so
            // there is no geometry to keep and the link is dropped — but the
            // chain, and the links that are fine, survive.
            report(
                at,
                "`arc` names \"${arcKey.value}\", which is not in this network; the link is dropped",
                DiagnosticCause.LINK_ARC_ABSENT,
            )
            return null
        }
        val from = endpoint(value, "from", at) ?: return null
        val to = endpoint(value, "to", at) ?: return null
        for ((endpoint, name) in listOf(from to "from", to to "to")) {
            if (!endpointIsUsable(endpoint, name, at, arc, nodes)) return null
        }
        return Link(arcKey.value, from, to)
    }

    private fun endpoint(obj: JsonObject, key: String, at: Pointer): Endpoint? {
        return when (val v = obj[key]) {
            null -> {
                report(
                    at,
                    "a link requires `$key`; the link is dropped",
                    DiagnosticCause.MISSING_REQUIRED_KEY,
                )
                null
            }
            // Section 3.3.3: a node key given as a string, or an inline position
            // given as an array.
            is JsonString -> Endpoint.Node(v.value)
            is JsonArray -> position(v, at.child(key))?.let(Endpoint::Inline)
            else -> {
                report(
                    at.child(key),
                    "a link endpoint is a node key or an inline position; the link is dropped",
                    DiagnosticCause.WRONG_TYPE_REQUIRED,
                )
                null
            }
        }
    }

    /**
     * The inline-position rule of section 3.3.3. Returns false when the link must
     * be dropped.
     *
     * A node key naming a node absent from the network keeps the link — the arc
     * it traverses is real geometry, exactly as for an arc naming an absent node
     * (6.2) — and the continuity check below then declines to compare that end.
     */
    private fun endpointIsUsable(
        endpoint: Endpoint,
        name: String,
        at: Pointer,
        arc: NetworkArc,
        nodes: Map<String, Position>,
    ): Boolean {
        when (endpoint) {
            is Endpoint.Node -> {
                if (endpoint.key !in nodes) {
                    report(
                        at.child(name),
                        "`$name` names node \"${endpoint.key}\", which is not in this network; " +
                            "the link is kept and this end is left unresolved",
                        DiagnosticCause.LINK_NODE_ABSENT,
                    )
                }
                return true
            }
            is Endpoint.Inline -> {
                val distance = Geo.distanceToPolyline(endpoint.position, arc.pips)
                if (distance == null) {
                    report(
                        at.child(name),
                        "the arc has no vertices, so an inline endpoint has no distance to it; the link is dropped",
                        DiagnosticCause.INLINE_ENDPOINT_ON_EMPTY_ARC,
                    )
                    return false
                }
                if (distance > Geo.TOLERANCE_M) {
                    report(
                        at.child(name),
                        "the inline endpoint does not project onto its arc: it is beyond the 50 m tolerance; " +
                            "the link is dropped",
                        DiagnosticCause.INLINE_ENDPOINT_BEYOND_TOLERANCE,
                    )
                    return false
                }
                return true
            }
        }
    }

    /**
     * Section 6.2: a chain's links MUST form one connected walk. The point at
     * which link k ends must be the point at which link k+1 begins, and the
     * diagnostic addresses the SECOND link of the pair.
     *
     * A node key and an inline position meet when the node's resolved position
     * and the inline position are equal by value; two node keys meet when the
     * keys are equal. The spec stops there, and two DISTINCT keys at one
     * position is a case it does not name — this reader compares their resolved
     * positions, on the reasoning section 6.2 gives for comparing a node key at
     * all: "or a walk that is continuous is reported as broken".
     *
     * A pair either of whose links was dropped is not compared. Dropping a link
     * must not then manufacture a break, or one defect becomes two and the reader
     * cannot tell how much is actually wrong.
     */
    private fun checkContinuity(slots: Array<Link?>, linksAt: Pointer, nodes: Map<String, Position>) {
        for (i in 0 until slots.size - 1) {
            val a = slots[i] ?: continue
            val b = slots[i + 1] ?: continue
            if (meets(a.to, b.from, nodes) == false) {
                report(
                    linksAt.child(i + 1),
                    "this link does not begin where the previous one ends, " +
                        "so the chain is not one connected walk",
                    DiagnosticCause.CHAIN_NOT_CONNECTED,
                )
            }
        }
    }

    /** Null where an endpoint could not be resolved and the question cannot be answered. */
    private fun meets(end: Endpoint, start: Endpoint, nodes: Map<String, Position>): Boolean? {
        if (end is Endpoint.Node && start is Endpoint.Node && end.key == start.key) return true
        val a = resolve(end, nodes) ?: return null
        val b = resolve(start, nodes) ?: return null
        return a.meets(b)
    }

    private fun resolve(endpoint: Endpoint, nodes: Map<String, Position>): Position? = when (endpoint) {
        is Endpoint.Inline -> endpoint.position
        is Endpoint.Node -> nodes[endpoint.key]
    }

    // ------------------------------------------------------------ routes

    private fun route(obj: JsonObject, at: Pointer): Route? {
        recogniseKeys(obj, at, Keys.ROUTE_REQUIRED, Keys.ROUTE_OPTIONAL)
        val rawArcs = obj["arcs"]
        if (rawArcs !is JsonArray) {
            report(
                at,
                "a route requires an `arcs` array; the route is dropped",
                if (rawArcs == null) DiagnosticCause.MISSING_REQUIRED_KEY else DiagnosticCause.WRONG_TYPE_REQUIRED,
            )
            return null
        }
        val arcsAt = at.child("arcs")
        val arcs = ArrayList<RouteArc>()
        for ((index, element) in rawArcs.elements.withIndex()) {
            val arcAt = arcsAt.child(index)
            if (element !is JsonObject) {
                report(arcAt, "a route arc must be an object; the arc is dropped", DiagnosticCause.WRONG_TYPE_REQUIRED)
                continue
            }
            recogniseKeys(element, arcAt, Keys.ROUTE_ARC_REQUIRED, Keys.ROUTE_ARC_OPTIONAL)
            val pips = requiredPips(element, arcAt, "a route arc") ?: continue
            arcs.add(RouteArc(pips, optionalData(element, arcAt)))
        }
        return Route(
            arcs = arcs,
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            folder = optionalFolderRef(obj, at),
            hidden = optionalBool(obj, "hidden", at),
            style = optionalStyle(obj, at),
            data = optionalData(obj, at),
        )
    }

    // ------------------------------------------------------------ tracks

    private fun track(obj: JsonObject, at: Pointer): Track? {
        recogniseKeys(obj, at, Keys.TRACK_REQUIRED, Keys.TRACK_OPTIONAL)
        val rawSegments = obj["segments"]
        if (rawSegments !is JsonArray) {
            report(
                at,
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
            val pips = requiredPips(element, segmentAt, "a segment") ?: continue
            segments.add(Segment(pips))
        }
        return Track(
            segments = segments,
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            folder = optionalFolderRef(obj, at),
            hidden = optionalBool(obj, "hidden", at),
            style = optionalStyle(obj, at),
            started = optionalTimestamp(obj, "started", at),
            ended = optionalTimestamp(obj, "ended", at),
            data = optionalData(obj, at),
        )
    }

    // ------------------------------------------------- waypoints and points

    private fun waypoint(obj: JsonObject, at: Pointer, inNetwork: Boolean): Waypoint? {
        recogniseKeys(
            obj,
            at,
            Keys.WAYPOINT_REQUIRED,
            if (inNetwork) Keys.POINT_OPTIONAL else Keys.WAYPOINT_OPTIONAL,
        )
        val rawAt = obj["at"]
        if (rawAt == null) {
            report(at, "a waypoint requires `at`; the item is dropped", DiagnosticCause.MISSING_REQUIRED_KEY)
            return null
        }
        val position = position(rawAt, at.child("at")) ?: return null
        return Waypoint(
            at = position,
            name = optionalString(obj, "name", at),
            notes = optionalString(obj, "notes", at),
            folder = if (inNetwork) null else optionalFolderRef(obj, at),
            hidden = optionalBool(obj, "hidden", at),
            style = optionalStyle(obj, at),
            data = optionalData(obj, at),
        )
    }

    // ------------------------------------------------------------- areas

    private fun area(obj: JsonObject, at: Pointer, inNetwork: Boolean): Area? {
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
            style = optionalStyle(obj, at),
            data = optionalData(obj, at),
        )
    }

    /**
     * Section 3.7: `polygon` is ONE closed ring — at least four positions whose
     * first and last are equal. Holes and multiple rings are not expressible, and
     * a reader that encounters a nested or additional ring must report and DROP
     * the item: keeping the outer ring silently would draw an area whose hole had
     * vanished, with nothing anywhere to say so.
     */
    private fun polygon(obj: JsonObject, at: Pointer): List<Position>? {
        val v = obj["polygon"]
        if (v == null) {
            report(at, "an area requires `polygon`; the item is dropped", DiagnosticCause.MISSING_REQUIRED_KEY)
            return null
        }
        val here = at.child("polygon")
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
            // The position diagnostic IS the report: the vertex is the unusable
            // value and the item is what goes. Adding a second "not a ring"
            // diagnostic would turn one defect into two.
            val p = position(element, here.child(index)) ?: return null
            ring.add(p)
        }
        if (ring.size < 4) {
            report(
                here,
                "`polygon` holds ${ring.size} positions and a closed ring needs at least four; the item is dropped",
                DiagnosticCause.POLYGON_NOT_A_CLOSED_RING,
            )
            return null
        }
        if (!ring.first().meets(ring.last())) {
            report(
                here,
                "`polygon` does not close: its first and last positions differ; the item is dropped",
                DiagnosticCause.POLYGON_NOT_A_CLOSED_RING,
            )
            return null
        }
        return ring
    }

    private fun JsonValue.typeName() = when (this) {
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

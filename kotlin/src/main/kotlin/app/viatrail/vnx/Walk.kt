package app.viatrail.vnx

/**
 * The first rule a walk breaks (SPEC.md sections 3.3.3 and 6.2).
 *
 * [key] is null when the diagnostic names the pass itself, and otherwise the
 * pass key it names.
 */
internal data class WalkFault(
    val pass: Int,
    val key: String?,
    val cause: DiagnosticCause,
    val message: String,
)

/**
 * Carries the node a walk is at from one pass to the next.
 *
 * A PASS CANNOT BE CHECKED FROM ITS OWN KEYS. Its entry is where the previous
 * pass left, and where the previous pass left depends on whether it had a `to`
 * and a `turn`. So the cursor is threaded through the walk in order, and the
 * same stepping serves the reader's check and the writer's check: two callers,
 * one answer.
 */
internal class WalkCursor(
    walkSize: Int,
    private val route: Boolean,
) {
    private val last = walkSize - 1

    /** The node the walk is at; null before the first pass. */
    private var at: String? = null

    /**
     * Checks pass [i] against the rules of section 3.3.3 that span more than the
     * pass's own values — valid-walk rules 2 to 6, and a route's rule — in the
     * order section 6.2 fixes, and moves the cursor to where the pass leaves.
     * Returns the first fault, or null.
     */
    fun step(
        i: Int,
        pass: Pass,
        arc: Arc,
    ): WalkFault? {
        fun fault(
            key: String?,
            cause: DiagnosticCause,
            message: String,
        ) = WalkFault(i, key, cause, message)

        // 5. A route's walk runs node to node (section 3.4).
        if (route) {
            when {
                pass.from is Endpoint.At -> return fault(
                    "from",
                    DiagnosticCause.ROUTE_PIN_OR_POSITION,
                    "a route's walk runs node to node, and this `from` is a position",
                )
                pass.to is Endpoint.At -> return fault(
                    "to",
                    DiagnosticCause.ROUTE_PIN_OR_POSITION,
                    "a route's walk runs node to node, and this `to` is a position",
                )
                pass.turn != null -> return fault(
                    "turn",
                    DiagnosticCause.ROUTE_PIN_OR_POSITION,
                    "a route's walk carries no turn",
                )
                pass.pinned -> return fault(
                    "pinned",
                    DiagnosticCause.ROUTE_PIN_OR_POSITION,
                    "a route's walk carries no pin",
                )
            }
        }
        // 6. A turn already fixes a pass, so a pin on it states nothing.
        if (pass.turn != null && pass.pinned) {
            return fault(null, DiagnosticCause.PINNED_TURN, "a pass with a turn is never pinned: the turn already fixes it")
        }
        // 7. The rule for `from` and `to`: first `from`, then `to`.
        if (i == 0 && pass.from == null) {
            return fault(null, DiagnosticCause.WALK_SPELLING, "the first pass must carry `from`")
        }
        if (i > 0 && pass.from != null) {
            return fault("from", DiagnosticCause.WALK_SPELLING, "only the first pass carries `from`")
        }
        val wantTo = i == last || pass.from is Endpoint.At
        if (wantTo && pass.to == null) {
            return fault(
                null,
                DiagnosticCause.WALK_SPELLING,
                if (i == last) "the last pass must carry `to`" else "a pass whose `from` is a position must carry `to`",
            )
        }
        if (!wantTo && pass.to != null) {
            return fault(
                "to",
                DiagnosticCause.WALK_SPELLING,
                "`to` appears only on the last pass, or on a pass whose `from` is a position",
            )
        }

        // 8. The walk is connected.
        fun isEnd(node: String?) = node == arc.from || node == arc.to
        val entry: Endpoint
        val from = pass.from
        if (from != null) {
            if (from is Endpoint.Node && !isEnd(from.key)) {
                return fault(
                    "from",
                    DiagnosticCause.WALK_NOT_CONNECTED,
                    "`from` node \"${from.key}\" is not an end of arc \"${pass.arc}\"",
                )
            }
            entry = from
        } else {
            val node = at
            if (node == null || !isEnd(node)) {
                return fault(
                    null,
                    DiagnosticCause.WALK_NOT_CONNECTED,
                    "the walk is at node \"$node\", which is not an end of arc \"${pass.arc}\"",
                )
            }
            entry = Endpoint.Node(node)
        }
        val to = pass.to
        val exit: Endpoint =
            when {
                to is Endpoint.Node && !isEnd(to.key) -> return fault(
                    "to",
                    DiagnosticCause.WALK_NOT_CONNECTED,
                    "`to` node \"${to.key}\" is not an end of arc \"${pass.arc}\"",
                )
                to is Endpoint.At && i != last -> return fault(
                    "to",
                    DiagnosticCause.WALK_NOT_CONNECTED,
                    "only the last pass leaves at a position",
                )
                to != null -> to
                // The entry is a node here: a position `from` requires a `to`, which
                // the spelling check above has already enforced.
                pass.turn != null -> entry
                (entry as Endpoint.Node).key == arc.from -> Endpoint.Node(arc.to)
                else -> Endpoint.Node(arc.from)
            }
        at = (exit as? Endpoint.Node)?.key
        return null
    }
}

/**
 * The writer's check on a walk: every pass names an arc of its owner, and the
 * walk follows section 3.3.3's rules. A writer never emits a walk its own reader
 * would drop.
 */
internal fun checkWalk(
    walk: List<Pass>,
    arcs: Map<String, Arc>,
    route: Boolean,
): WalkFault? {
    val cursor = WalkCursor(walk.size, route)
    for ((i, pass) in walk.withIndex()) {
        val arc =
            arcs[pass.arc]
                ?: return WalkFault(i, null, DiagnosticCause.PASS_ARC_ABSENT, "the pass names arc \"${pass.arc}\", which is not here")
        cursor.step(i, pass, arc)?.let { return it }
    }
    return null
}

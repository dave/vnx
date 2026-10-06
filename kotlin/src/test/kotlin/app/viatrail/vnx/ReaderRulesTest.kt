package app.viatrail.vnx

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The rules of SPEC.md section 6.2 that no corpus case decides.
 *
 * Everything here is a reading of the prose rather than a verdict the corpus
 * has given, and each test says which. They are frozen so that a later ruling
 * changes a test rather than silently changing behaviour.
 */
class ReaderRulesTest {
    private fun read(json: String) = Vnx.readDocument(json)

    // --------------------------------------------------------- keep vs drop

    @Test
    fun `an unrecognised key is skipped and the item kept`() {
        val r = read("""{"vnx":"0.9.0","waypoints":[{"nmae":"typo","name":"Car park","at":[-3.675,57.1201]}]}""")
        assertEquals(1, r.document.waypoints!!.size)
        assertEquals("Car park", r.document.waypoints!![0].name)
        assertEquals(1, r.diagnostics.size)
        assertEquals("/waypoints/0/nmae", r.diagnostics[0].path)
    }

    @Test
    fun `a wrong type in an OPTIONAL key drops the field and keeps the item`() {
        val r = read("""{"vnx":"0.9.0","waypoints":[{"name":7,"at":[-3.675,57.1201]}]}""")
        assertEquals(1, r.document.waypoints!!.size)
        assertNull(r.document.waypoints!![0].name)
        assertEquals(DiagnosticCause.WRONG_TYPE_OPTIONAL, r.diagnostics.single().cause)
    }

    @Test
    fun `the two wrong-type remedies are different facts and carry different codes`() {
        // Section 6.2 gives two remedies and the corpus gives them two codes: a
        // consumer told only that a type was wrong would have to re-derive
        // required-ness from section 3.10's table to learn whether the rest of the
        // item survived, which is the lookup a code exists to save. A reader that
        // drops the item every time, and one that drops the field every time, each
        // pass a document carrying only one of the two.
        val r =
            read(
                """{"vnx":"0.9.0","waypoints":[
               {"name":"Kept, minus one field","at":[-3.6712,57.118],"hidden":"yes"},
               {"name":"Dropped","at":"beside the burn"}]}""",
            )
        assertEquals(1, r.document.waypoints!!.size, "one waypoint read, not two and not two with one positionless")
        assertEquals("Kept, minus one field", r.document.waypoints!![0].name)
        assertNull(r.document.waypoints!![0].hidden)
        assertEquals(
            listOf("wrong-type-optional", "wrong-type-required"),
            r.diagnostics.map { it.code },
        )
    }

    @Test
    fun `a wrong type in a REQUIRED key drops the item`() {
        // The exception is not a special case: dropping a required field would
        // leave a waypoint that is nowhere.
        val r = read("""{"vnx":"0.9.0","waypoints":[{"name":"nowhere","at":"here"}]}""")
        assertEquals(0, r.document.waypoints!!.size)
        assertEquals(1, r.diagnostics.size)
    }

    @Test
    fun `a folder naming a folder that is not present keeps the item at the top level`() {
        // Section 6.2 in as many words: dropping the item would lose content over
        // a lost grouping.
        val r = read("""{"vnx":"0.9.0","waypoints":[{"folder":"ghost","at":[-3,57]}]}""")
        assertEquals(1, r.document.waypoints!!.size)
        assertNull(r.document.waypoints!![0].folder)
        assertEquals(DiagnosticCause.FOLDER_ABSENT, r.diagnostics.single().cause)
    }

    @Test
    fun `a folder with no id is dropped and its contents survive it`() {
        val r =
            read(
                """{"vnx":"0.9.0","folders":[{"name":"No id here"}],"waypoints":[{"folder":"anything","at":[-3,57]}]}""",
            )
        assertEquals(0, r.document.folders!!.size)
        assertEquals(1, r.document.waypoints!!.size)
        assertNull(r.document.waypoints!![0].folder)
        assertEquals(2, r.diagnostics.size, "the folder going and the reference failing are two facts")
    }

    @Test
    fun `a data record with no @type is dropped and the rest of the bag survives`() {
        // Section 5.1's rule for an unusable `@type` is section 6.2's rule for a
        // required key: the record goes, not the key.
        val r =
            read(
                """{"vnx":"0.9.0","waypoints":[{"at":[-3,57],"data":[{"value":1},{"@type":"com.example.x","value":2}]}]}""",
            )
        val data = r.document.waypoints!![0].data!!
        assertEquals(1, data.size)
        assertEquals("com.example.x", data[0].type)
        assertEquals(DiagnosticCause.DATA_RECORD_TYPE_MISSING, r.diagnostics.single().cause)
        // Section 6.2's table: the record goes, under the required-key code.
        assertEquals("wrong-type-required", r.diagnostics.single().code)
    }

    @Test
    fun `an arc naming a node that is not in its network is dropped, at the end that names it`() {
        // Section 3.3.2: an arc's line runs from its `from` node to its `to` node,
        // so without the node there is no line. The path names the END, the
        // smallest element that could not be used (section 6.3 rule 1).
        val r =
            read(
                """{"vnx":"0.9.0","networks":[{"nodes":{"a":[-3,57]},
               "arcs":{"x":{"from":"a","to":"ghost","pips":[[-2.5,57.5]]}}}]}""",
            )
        assertEquals(
            0,
            r.document.networks!![0]
                .arcs!!
                .size,
        )
        assertEquals(listOf("/networks/0/arcs/x/to" to "arc-node-absent"), r.diagnostics.map { it.path to it.code })
    }

    @Test
    fun `an arc naming two absent nodes reports each end`() {
        // Each end is its own fact at its own path, so two diagnostics can never
        // share a path and a code (section 6.3).
        val r = read("""{"vnx":"0.9.0","networks":[{"arcs":{"x":{"from":"p","to":"q","pips":[]}}}]}""")
        assertEquals(
            0,
            r.document.networks!![0]
                .arcs!!
                .size,
        )
        assertEquals(
            setOf("/networks/0/arcs/x/from" to "arc-node-absent", "/networks/0/arcs/x/to" to "arc-node-absent"),
            r.diagnostics.map { it.path to it.code }.toSet(),
        )
    }

    @Test
    fun `a self-loop arc is dropped, at the arc`() {
        // Section 3.3.2: a pass over a self-loop would have no other end to
        // leave by, so this format cannot carry one.
        val r =
            read(
                """{"vnx":"0.9.0","networks":[{"nodes":{"a":[-3,57]},"arcs":{"o":{"from":"a","to":"a","pips":[[-2.9,57.1]]}}}]}""",
            )
        assertEquals(
            0,
            r.document.networks!![0]
                .arcs!!
                .size,
        )
        assertEquals(listOf("/networks/0/arcs/o" to "self-loop-arc"), r.diagnostics.map { it.path to it.code })
    }

    @Test
    fun `a time on an arc pip is dropped from every pip, and reported once for the arc`() {
        val r =
            read(
                """{"vnx":"0.9.0","networks":[{"nodes":{"a":[-3,57],"b":[-2,57]},
               "arcs":{"ab":{"from":"a","to":"b","pips":[[-2.8,57,10,1000],[-2.6,57,20],[-2.4,57,null,3000]]}}}]}""",
            )
        val pips =
            r.document.networks!![0]
                .arcs!!
                .getValue("ab")
                .pips
        assertEquals(
            listOf(Vertex(-2.8, 57.0, 10.0), Vertex(-2.6, 57.0, 20.0), Vertex(-2.4, 57.0, null)),
            pips,
            "every pip is kept, with its elevation",
        )
        assertEquals(listOf("/networks/0/arcs/ab/pips" to "arc-pip-time"), r.diagnostics.map { it.path to it.code })
    }

    @Test
    fun `a time on a node is dropped and the node kept`() {
        val r = read("""{"vnx":"0.9.0","networks":[{"nodes":{"a":[-3,57,410,1000]}}]}""")
        assertEquals(
            Vertex(-3.0, 57.0, 410.0),
            r.document.networks!![0]
                .nodes!!
                .getValue("a"),
        )
        assertEquals(listOf("/networks/0/nodes/a" to "extra-channel"), r.diagnostics.map { it.path to it.code })
    }

    @Test
    fun `a node that will not parse is dropped, and the arc naming it goes after it`() {
        // Section 6.2's cascade: the node goes under position-unusable, the arc
        // under arc-node-absent, and the highlight under pass-arc-absent. Three
        // facts, three diagnostics.
        val r =
            read(
                """{"vnx":"0.9.0","networks":[{"nodes":{"a":[-3,57],"b":"somewhere"},
               "arcs":{"ab":{"from":"a","to":"b","pips":[]}},
               "highlights":[{"walk":[{"arc":"ab","from":"a","to":"b"}]}]}]}""",
            )
        assertEquals(
            setOf(
                "/networks/0/nodes/b" to "position-unusable",
                "/networks/0/arcs/ab/to" to "arc-node-absent",
                "/networks/0/highlights/0/walk/0" to "pass-arc-absent",
            ),
            r.diagnostics.map { it.path to it.code }.toSet(),
        )
        assertEquals(
            0,
            r.document.networks!![0]
                .highlights!!
                .size,
        )
    }

    @Test
    fun `a ring that repeats its first position loses the repeat and keeps the area`() {
        val r = read("""{"vnx":"0.9.0","areas":[{"polygon":[[-3,57],[-2,57],[-2,58],[-3,57]]}]}""")
        assertEquals(
            3,
            r.document.areas!![0]
                .polygon.size,
        )
        assertEquals(listOf("/areas/0/polygon/3" to "ring-closed"), r.diagnostics.map { it.path to it.code })
    }

    @Test
    fun `a closed ring of two distinct positions takes both diagnostics`() {
        // Section 6.2: the repeat is dropped first, and what remains is counted.
        val r = read("""{"vnx":"0.9.0","areas":[{"polygon":[[-3,57],[-2,57],[-3,57]]}]}""")
        assertEquals(0, r.document.areas!!.size)
        assertEquals(
            setOf("/areas/0/polygon/2" to "ring-closed", "/areas/0/polygon" to "ring-too-few-points"),
            r.diagnostics.map { it.path to it.code }.toSet(),
        )
    }

    @Test
    fun `distinct positions differ in longitude or latitude, and in no other channel`() {
        // Elevation takes no part (section 3.7), and -0 and 0 are one number.
        for (polygon in listOf(
            "[[-3,57,1],[-2,57],[-2,57,5]]",
            "[[0,57],[-0.0,57],[1,58]]",
        )) {
            val r = read("""{"vnx":"0.9.0","areas":[{"polygon":$polygon}]}""")
            assertEquals(0, r.document.areas!!.size, polygon)
            assertEquals(listOf("ring-too-few-points"), r.diagnostics.map { it.code }, polygon)
        }
    }

    @Test
    fun `an importance outside 1 to 5 drops the field and keeps the item and the rest of the style`() {
        // Section 3.6 states the range and calls it normative; section 6.2 names
        // it as the worked example of a range this document states. Section 3.10
        // makes a style object opaque, and the two are in tension — this is the
        // narrow reading that satisfies both. REPORTED; no corpus case decides it.
        val r = read("""{"vnx":"0.9.0","waypoints":[{"at":[-3,57],"style":{"importance":9,"color":"#fff"}}]}""")
        val style = r.document.waypoints!![0].style!!
        assertNull(style["importance"])
        assertEquals(JsonString("#fff"), style["color"])
        assertEquals(DiagnosticCause.OUT_OF_RANGE, r.diagnostics.single().cause)
    }

    @Test
    fun `a style key this format never defined is carried through in silence`() {
        // Section 3.10: style vocabulary grows faster than structural vocabulary,
        // so a fixed style key set would make every implementation diagnose a
        // valid document each time one of them added a key.
        val r = read("""{"vnx":"0.9.0","waypoints":[{"at":[-3,57],"style":{"halo-blur":2}}]}""")
        assertEquals(emptyList(), r.diagnostics)
        assertEquals(JsonNumber(2.0), r.document.waypoints!![0].style!!["halo-blur"])
    }

    @Test
    fun `a field inside a data record is never checked, whatever section 5-2 says its type is`() {
        // Section 6.2, amended: a record body is opaque apart from `@type`, and a
        // reader MUST NOT check the type, range or enumeration of a field in one.
        val r =
            read(
                """{"vnx":"0.9.0","networks":[{"points":[{"at":[-3,57],
               "data":[{"@type":"app.viatrail.camp","flat":"maybe","capacity":"lots"},
                       {"@type":"app.viatrail.settlement","size":"metropolis"}]}]}]}""",
            )
        assertEquals(emptyList(), r.diagnostics)
        assertEquals(
            2,
            r.document.networks!![0]
                .points!![0]
                .data!!
                .size,
        )
    }

    // -------------------------------------------------------------- walks

    /**
     * A network of four nodes in a row, a to d, and three arcs: `ab` runs a to b,
     * `cb` runs c to b, and `cd` runs c to d. [highlights] is the network's `highlights`.
     */
    private fun network(highlights: String) =
        """{"vnx":"0.9.0","networks":[{"nodes":{"a":[-3.6,57.1],"b":[-3.59,57.1],"c":[-3.58,57.1],"d":[-3.57,57.1]},
           "arcs":{"ab":{"from":"a","to":"b","pips":[[-3.595,57.101]]},"cb":{"from":"c","to":"b","pips":[]},
                   "cd":{"from":"c","to":"d","pips":[[-3.575,57.102]]}},
           "highlights":$highlights}]}"""

    private fun walkOf(passes: String) = read(network("""[{"walk":$passes}]"""))

    private fun ReadResult.pairs() = diagnostics.map { it.path to it.code }

    @Test
    fun `a pass naming an absent arc drops the highlight, and nothing after it is examined`() {
        // Section 6.2: a walk is used whole or not at all, and only its first
        // problem is reported. Pass 2 breaks the spelling rule as well and is not
        // reported.
        val r = walkOf("""[{"arc":"ab","from":"a"},{"arc":"ghost"},{"arc":"cd","from":"c","to":"d"}]""")
        assertEquals(
            0,
            r.document.networks!![0]
                .highlights!!
                .size,
        )
        assertEquals(listOf("/networks/0/highlights/0/walk/1" to "pass-arc-absent"), r.pairs())
    }

    @Test
    fun `a pass that is not an object, or names no arc, is an absent arc`() {
        for (pass in listOf("7", "{}", """{"arc":7}""")) {
            val r = walkOf("""[$pass]""")
            assertEquals(listOf("/networks/0/highlights/0/walk/0" to "pass-arc-absent"), r.pairs(), pass)
        }
    }

    @Test
    fun `a highlight walking a dropped arc is dropped with a diagnostic of its own`() {
        // The arc going and the highlight going are two facts (section 6.2).
        val r =
            read(
                """{"vnx":"0.9.0","networks":[{"nodes":{"a":[-3,57],"b":[-2,57]},
               "arcs":{"ab":{"from":"a","to":"b","pips":[]},"bb":{"from":"b","to":"b","pips":[]}},
               "highlights":[{"name":"Keep","walk":[{"arc":"ab","from":"a","to":"b"}]},
                         {"name":"Loses its arc","walk":[{"arc":"ab","from":"a"},{"arc":"bb","to":"b"}]}]}]}""",
            )
        assertEquals(
            listOf("Keep"),
            r.document.networks!![0]
                .highlights!!
                .map { it.name },
        )
        assertEquals(
            setOf("/networks/0/arcs/bb" to "self-loop-arc", "/networks/0/highlights/1/walk/1" to "pass-arc-absent"),
            r.pairs().toSet(),
        )
    }

    @Test
    fun `the rule for from and to is checked first from, then to`() {
        // Section 6.3 rule 2: a key the rule requires and the pass lacks names
        // the pass; a key the rule forbids names that key.
        val cases =
            listOf(
                """[{"arc":"ab","to":"b"}]""" to "/networks/0/highlights/0/walk/0",
                """[{"arc":"ab","from":"a"}]""" to "/networks/0/highlights/0/walk/0",
                """[{"arc":"ab","from":"a"},{"arc":"cb","from":"b","to":"c"}]""" to "/networks/0/highlights/0/walk/1/from",
                """[{"arc":"ab","from":"a","to":"b"},{"arc":"cb","to":"c"}]""" to "/networks/0/highlights/0/walk/0/to",
                """[{"arc":"ab","from":[-3.597,57.1005]},{"arc":"cb","to":"c"}]""" to "/networks/0/highlights/0/walk/0",
                // Both keys wrong: `from` is checked first, so only the pass is named.
                """[{"arc":"ab"},{"arc":"cb","from":"b","to":"c"}]""" to "/networks/0/highlights/0/walk/0",
            )
        for ((passes, path) in cases) {
            val r = walkOf(passes)
            assertEquals(listOf(path to "walk-spelling"), r.pairs(), passes)
            assertEquals(
                0,
                r.document.networks!![0]
                    .highlights!!
                    .size,
                passes,
            )
        }
    }

    @Test
    fun `a walk that is not connected names the part that breaks it`() {
        val cases =
            listOf(
                // A `from` node that is not an end of its arc.
                """[{"arc":"ab","from":"c","to":"b"}]""" to "/networks/0/highlights/0/walk/0/from",
                // A pass that does not touch the node the previous one left at.
                """[{"arc":"ab","from":"a"},{"arc":"cd","to":"d"}]""" to "/networks/0/highlights/0/walk/1",
                // A `to` node that is not an end of its arc.
                """[{"arc":"ab","from":"a","to":"d"}]""" to "/networks/0/highlights/0/walk/0/to",
                // A `to` position on a pass other than the last.
                """[{"arc":"ab","from":[-3.597,57.1005],"to":[-3.596,57.1006]},{"arc":"cb","to":"c"}]""" to
                    "/networks/0/highlights/0/walk/0/to",
            )
        for ((passes, path) in cases) {
            val r = walkOf(passes)
            assertEquals(listOf(path to "walk-not-connected"), r.pairs(), passes)
        }
    }

    @Test
    fun `a pass with a turn leaves by the node it entered`() {
        // Section 3.3.3. Entered at b, turned, so the walk is at b again: `cb`
        // continues, and `cd`, which does not touch b, does not.
        val turn = """{"arc":"ab","from":"b","turn":[-3.596,57.1005]}"""
        assertEquals(emptyList(), walkOf("""[$turn,{"arc":"cb","to":"c"}]""").diagnostics)
        assertEquals(
            listOf("/networks/0/highlights/0/walk/1" to "walk-not-connected"),
            walkOf("""[$turn,{"arc":"cd","to":"d"}]""").pairs(),
        )
    }

    @Test
    fun `a pass carrying both turn and pinned drops the highlight, at the pass`() {
        val r = walkOf("""[{"arc":"cd","from":"c","turn":[-3.574,57.102],"pinned":true,"to":"c"}]""")
        assertEquals(listOf("/networks/0/highlights/0/walk/0" to "pinned-turn"), r.pairs())
    }

    @Test
    fun `pinned false is the same as absent, and a pinned of the wrong type drops only the field`() {
        val clean = walkOf("""[{"arc":"cd","from":"c","turn":[-3.574,57.102],"pinned":false,"to":"c"}]""")
        assertEquals(emptyList(), clean.diagnostics)
        assertFalse(
            clean.document.networks!![0]
                .highlights!![0]
                .walk[0]
                .pinned,
        )

        val typed = walkOf("""[{"arc":"ab","from":"a","pinned":"yes"},{"arc":"cb","to":"c"}]""")
        assertEquals(listOf("/networks/0/highlights/0/walk/0/pinned" to "wrong-type-optional"), typed.pairs())
        assertEquals(
            2,
            typed.document.networks!![0]
                .highlights!![0]
                .walk.size,
            "the highlight is kept",
        )
    }

    @Test
    fun `a walk position that will not parse drops the highlight, at the key`() {
        for ((pass, key) in listOf(
            """{"arc":"cd","from":"c","turn":"d","to":"c"}""" to "turn",
            """{"arc":"cd","from":7,"to":"d"}""" to "from",
            """{"arc":"cd","from":"c","to":[-3.57]}""" to "to",
        )) {
            val r = walkOf("[$pass]")
            assertEquals(listOf("/networks/0/highlights/0/walk/0/$key" to "position-unusable"), r.pairs(), pass)
        }
    }

    @Test
    fun `an elevation or a time on a walk position is dropped and the highlight kept`() {
        val r = walkOf("""[{"arc":"ab","from":[-3.597,57.1005,400],"to":[-3.596,57.1006,null,1000]}]""")
        val pass =
            r.document.networks!![0]
                .highlights!![0]
                .walk[0]
        assertEquals(Endpoint.At(LonLat(-3.597, 57.1005)), pass.from)
        assertEquals(Endpoint.At(LonLat(-3.596, 57.1006)), pass.to)
        assertEquals(
            setOf("/networks/0/highlights/0/walk/0/from" to "extra-channel", "/networks/0/highlights/0/walk/0/to" to "extra-channel"),
            r.pairs().toSet(),
        )
    }

    @Test
    fun `what a pass reports before its fault is kept, and what comes after is not`() {
        // Section 6.2: diagnostics that do not drop the highlight are recorded as they
        // are met, and nothing after the first fault is examined.
        val r = walkOf("""[{"arc":"ab","from":"a","x":1},{"arc":"cd","y":2},{"arc":"cb","z":3,"to":"c"}]""")
        assertEquals(
            listOf(
                "/networks/0/highlights/0/walk/0/x" to "unknown-key",
                "/networks/0/highlights/0/walk/1/y" to "unknown-key",
                "/networks/0/highlights/0/walk/1" to "walk-not-connected",
            ),
            r.pairs(),
        )
    }

    @Test
    fun `an empty walk is valid, and a missing one drops the highlight`() {
        assertEquals(emptyList(), walkOf("[]").diagnostics)
        assertEquals(
            1,
            walkOf("[]")
                .document.networks!![0]
                .highlights!!
                .size,
        )
        val r = read(network("""[{"name":"no walk"}]"""))
        assertEquals(listOf("/networks/0/highlights/0/walk" to "wrong-type-required"), r.pairs())
        assertEquals(
            0,
            r.document.networks!![0]
                .highlights!!
                .size,
        )
    }

    @Test
    fun `a route walks node to node, and anything else drops the route`() {
        fun route(walk: String) =
            read(
                """{"vnx":"0.9.0","routes":[{"nodes":{"a":[-3,57],"b":[-2,57]},
               "arcs":{"ab":{"from":"a","to":"b","pips":[]}},"walk":$walk}]}""",
            )
        assertEquals(emptyList(), route("""[{"arc":"ab","from":"b","to":"a"}]""").diagnostics)
        for ((walk, key) in listOf(
            """[{"arc":"ab","from":[-2.5,57],"to":"b"}]""" to "from",
            """[{"arc":"ab","from":"a","to":[-2.5,57]}]""" to "to",
            // The route's rule comes before pinned-turn, so the pair is reported
            // at `turn` and not at the pass.
            """[{"arc":"ab","from":"a","turn":[-2.5,57],"pinned":true,"to":"a"}]""" to "turn",
            """[{"arc":"ab","from":"a","pinned":true,"to":"b"}]""" to "pinned",
        )) {
            val r = route(walk)
            assertEquals(0, r.document.routes!!.size, walk)
            assertEquals(listOf("/routes/0/walk/0/$key" to "route-pin-or-position"), r.pairs(), walk)
        }
    }

    @Test
    fun `a route is its own highlight, so a route walking a dropped arc is dropped`() {
        val r =
            read(
                """{"vnx":"0.9.0","routes":[{"nodes":{"a":[-3,57]},
               "arcs":{"ab":{"from":"a","to":"b","pips":[]}},"walk":[{"arc":"ab","from":"a","to":"b"}]}]}""",
            )
        assertEquals(0, r.document.routes!!.size)
        assertEquals(
            setOf("/routes/0/arcs/ab/to" to "arc-node-absent", "/routes/0/walk/0" to "pass-arc-absent"),
            r.pairs().toSet(),
        )
    }

    // ------------------------------------------------------------ versions

    @Test
    fun `a leading zero is a legal spelling and the comparison is numeric`() {
        // Section 2.2: `00.09.0000` is a legal spelling of 0.9.0.
        assertEquals(emptyList(), read("""{"vnx":"00.09.0000"}""").diagnostics)
        assertEquals("00.09.0000", read("""{"vnx":"00.09.0000"}""").document.vnx)
    }

    @Test
    fun `a reader writes the version the document declared, not its own`() {
        // Section 2.2: a 0.9.0 reader that reads a 0.9.3 document writes 0.9.3.
        val r = read("""{"vnx":"0.9.99","waypoints":[{"at":[-3,57]}]}""")
        assertTrue(Vnx.write(r.document).contains("\"vnx\":\"0.9.99\""))
    }

    @Test
    fun `through 0-x a minor that is not the reader's own is refused, in EITHER direction`() {
        // Section 2.2: each MINOR is its own series, so the rule is "not the same
        // series" and not "greater than". A lower minor is refused for the same
        // reason as a higher one — 0.1 is not an older spelling of 0.9.
        //
        // BOTH DIRECTIONS, because a reader written to the wrong rule passes
        // whichever one it was tested on. This library read 0.0.9 until
        // 2026-09-12, with the higher direction green throughout.
        for (above in listOf("0.10.0", "1.0.0", "9.9.9")) {
            val e = assertFailsWith<VnxRefusal>("\"$above\" must be refused") { read("""{"vnx":"$above"}""") }
            assertEquals(RefusalCause.VERSION_SERIES_UNSUPPORTED, e.refusal, "for \"$above\"")
            assertTrue(e.message.contains(above), "the refusal must name the version: ${e.message}")
        }
        val older = assertFailsWith<VnxRefusal> { read("""{"vnx":"0.1.9"}""") }
        assertEquals(RefusalCause.VERSION_SERIES_UNSUPPORTED, older.refusal)
        assertTrue(older.message.contains("0.1.9"), "the refusal must name the version: ${older.message}")
        // And it must name the SERIES rather than claim a newer reader would
        // help, because none would.
        assertTrue(older.message.contains("0.1 series"), "the refusal must name the series: ${older.message}")
        assertFalse(
            older.message.contains("a newer reader is needed"),
            "no newer reader reads an older series: ${older.message}",
        )
    }

    @Test
    fun `the fix did not swap one direction for the other`() {
        // The failure mode of a rule written the other way round: a reader that
        // now refuses 0.1.9 and has stopped reading its own series.
        assertEquals(emptyList(), read("""{"vnx":"0.9.5"}""").diagnostics)
        assertEquals("0.9.5", read("""{"vnx":"0.9.5"}""").document.vnx)
        assertEquals(emptyList(), read("""{"vnx":"0.9.0"}""").diagnostics)
    }

    @Test
    fun `every malformed version shape is refused and none is coerced`() {
        for (bad in listOf("0.9", "0.9.0.0", "0.9.x", "0.9.+1", "", "v0.9.0", " 0.9.0")) {
            val e = assertFailsWith<VnxRefusal>("\"$bad\" must be refused") { read("""{"vnx":"$bad"}""") }
            assertEquals(RefusalCause.VERSION_MALFORMED, e.refusal, "for \"$bad\"")
            assertTrue(e.message.contains("MAJOR.MINOR.PATCH"), "the refusal must name the expected shape")
        }
    }

    // ------------------------------------------------------- absent vs empty

    @Test
    fun `an empty collection is distinct from an absent one in the model`() {
        // Section 3.10: requiring a key does not require a non-empty value, and an
        // empty array is distinct from an absent one IN THE MODEL. Section 3.1
        // then requires a reader to treat the two alike, which is why section
        // 8.1's tree comparison normalises them together and why a writer may
        // emit either.
        val withEmpty = read("""{"vnx":"0.9.0","waypoints":[],"networks":[]}""")
        assertNotNull(withEmpty.document.waypoints)
        assertEquals(0, withEmpty.document.waypoints!!.size)
        assertNull(withEmpty.document.tracks)
        assertEquals(
            """{"vnx":"0.9.0","networks":[],"waypoints":[]}""" + "\n",
            Vnx.write(withEmpty.document),
        )
        assertEquals("""{"vnx":"0.9.0"}""" + "\n", Vnx.write(read("""{"vnx":"0.9.0"}""").document))
    }

    @Test
    fun `application keys are opaque and survive a read-modify-write untouched`() {
        // Section 3.9: a top-level key containing a full stop is an application
        // key. Its value is kept verbatim and never reported: a null stays null
        // and an empty value stays empty. Decode sorts the keys by key.
        val json =
            """{"vnx":"0.9.0","app.viatrail.rulesets":[{"anything":true,"nested":{"deep":[1,2,3]}}],""" +
                """"com.example.empty":[],"com.example.null":null,"org.example.s":"text"}"""
        val r = read(json)
        assertEquals(emptyList(), r.diagnostics)
        assertEquals(
            listOf("app.viatrail.rulesets", "com.example.empty", "com.example.null", "org.example.s"),
            r.document.appKeys.map { it.key },
        )
        assertEquals(JsonNull, r.document.appKeys[2].value)
        assertEquals(json + "\n", Vnx.write(r.document))
    }

    @Test
    fun `application keys are decoded sorted by key`() {
        val r = read("""{"vnx":"0.9.0","z.b":1,"a.b":2,"m.b":3}""")
        assertEquals(listOf("a.b", "m.b", "z.b"), r.document.appKeys.map { it.key })
    }

    @Test
    fun `a dotted key below the top level is an unrecognised key`() {
        // Section 3.9 applies at the top level only. An undotted unknown key at
        // the top level is still unknown too.
        val r = read("""{"vnx":"0.9.0","rulesets":[],"waypoints":[{"at":[1,2],"app.viatrail.x":1}]}""")
        assertEquals(
            setOf("/rulesets", "/waypoints/0/app.viatrail.x"),
            r.diagnostics.map { it.path }.toSet(),
        )
        assertEquals(setOf(DiagnosticCause.UNKNOWN_KEY), r.diagnostics.map { it.cause }.toSet())
    }

    @Test
    fun `the writer refuses an application key it cannot write`() {
        for (keys in listOf(
            listOf(AppKey("rulesets", JsonArray(emptyList()))),
            listOf(AppKey("folders", JsonArray(emptyList()))),
            listOf(AppKey("", JsonNull)),
            listOf(AppKey("a.b", JsonNumber(1.0)), AppKey("a.b", JsonNumber(2.0))),
        )) {
            assertFailsWith<IllegalArgumentException>("$keys") {
                Vnx.write(VnxDocument(vnx = "0.9.0", appKeys = keys))
            }
        }
    }

    // ------------------------------------------------------------ positions

    @Test
    fun `a coordinate literal parses to the correctly rounded double, by bit pattern`() {
        // THE CORPUS CANNOT SEE THIS. `accept/full-precision-coordinate.vnx`
        // looks like the guard and is not: the accept tier applies section 4.1's
        // precision to both sides before comparing, so this literal becomes
        // -3.671235 on each side and a one-bit parse error is normalised away
        // before anything looks at it. The fixture guarded full precision only
        // while the tier compared bytes.
        //
        // Found by the Rust lane, which would have shipped a reader one bit out
        // with a green suite: serde_json without `float_roundtrip` parses this
        // literal to c00d5eb03aa8431d where the standard library gives ...431e.
        //
        // BIT PATTERNS, NOT VALUES. `==` on two doubles one bit apart is false,
        // but a failure printing them shows two identical-looking numbers and the
        // next person reads the test as broken rather than the parser.
        val literal = "-3.6712345678901235"
        val reference = literal.toDouble()
        assertEquals(
            reference.toRawBits(),
            ((JsonParser.parse(literal) as JsonNumber).value).toRawBits(),
            "the parser and the platform disagree on $literal by at least one bit",
        )
        // And through the whole reader, which is the path a document takes.
        val at = read("""{"vnx":"0.9.0","waypoints":[{"at":[$literal,57.118]}]}""").document.waypoints!![0].at
        assertEquals(reference.toRawBits(), at.lon.toRawBits(), "the decoded longitude is not the literal")
    }

    @Test
    fun `positions of differing arity may appear in one pips array`() {
        // Section 4.1: a reader MUST read each position independently of its
        // neighbours. Forcing one shape would mean inventing elevations or
        // discarding them.
        val r = read("""{"vnx":"0.9.0","tracks":[{"segments":[{"pips":[[-3,57],[-3,57,340],[-3,57,null,1]]}]}]}""")
        assertEquals(emptyList(), r.diagnostics)
        val pips =
            r.document.tracks!![0]
                .segments[0]
                .pips
        assertEquals(listOf<Double?>(null, 340.0, null), pips.map { it.ele })
        assertEquals(listOf<Double?>(null, null, 1.0), pips.map { it.timeMs })
    }

    @Test
    fun `a channel beyond those this reader knows is read and dropped, with no diagnostic`() {
        // Section 4.1: a reader MUST ignore a channel beyond those it knows and
        // MUST NOT report a diagnostic for it, so that a future channel is
        // additive. Nothing asks a writer to reproduce it.
        val r = read("""{"vnx":"0.9.0","waypoints":[{"at":[-3,57,340,1,99]}]}""")
        assertEquals(emptyList(), r.diagnostics)
        assertEquals("""{"vnx":"0.9.0","waypoints":[{"at":[-3,57,340,1]}]}""" + "\n", Vnx.write(r.document))
    }

    @Test
    fun `a pip that will not parse drops the ARC, not the pip`() {
        // Section 6.2: a missing vertex leaves a shorter, plausible, wrong line
        // with nothing to indicate it. Dropping the pip is the reading that looks
        // obviously right.
        val r =
            read(
                """{"vnx":"0.9.0","networks":[{"nodes":{"n1":[-3.68,57.12],"n2":[-3.66,57.1]},
               "arcs":{"broken":{"from":"n1","to":"n2","pips":[[-3.67,57.11],[null,57.11]]},
                       "sound":{"from":"n1","to":"n2","pips":[[-3.67,57.11]]}},
               "highlights":[{"walk":[{"arc":"broken","from":"n1","to":"n2"}]}]}]}""",
            )
        val network = r.document.networks!![0]
        assertEquals(listOf("sound"), network.arcs!!.keys.toList(), "the whole arc goes, and only it")
        // The cascade is what proves the arc left rather than the pip: the pass
        // now names an arc that is not there, and the highlight goes with it.
        assertEquals(
            listOf("position-unusable", "pass-arc-absent"),
            r.diagnostics.map { it.code },
        )
    }

    @Test
    fun `a pip that will not parse drops the SEGMENT, and moving time is what shows it`() {
        // counts cannot see this — the track is 1 either way — and neither can the
        // diagnostics, because an element-dropping reader emits the same code at
        // the same pointer. Only the derived number separates the two readings.
        val r =
            read(
                """{"vnx":"0.9.0","tracks":[{"segments":[
               {"pips":[[-3.6712,57.118,341,1778569391000],[-3.6699,"57.1158",352.4,1778569442000],
                        [-3.6681,57.1139,366.9,1778569591000]]},
               {"pips":[[-3.6644,57.1085,452,1778571280000],[-3.6605,57.1042,513.1,1778571344000]]}]}]}""",
            )
        val track = r.document.tracks!![0]
        assertEquals(1, track.segments.size, "the segment goes whole")
        assertEquals(64000L, track.movingTimeMs(), "a reader that dropped the PIP would answer 264000")
        assertEquals(listOf("position-unusable"), r.diagnostics.map { it.code })
    }

    @Test
    fun `a vertex that will not parse drops the area, and reports once`() {
        val r =
            read(
                """{"vnx":"0.9.0","areas":[
               {"name":"Dropped","polygon":[[-3.74,57.09],[-3.72],[-3.72,57.075]]},
               {"name":"Kept","polygon":[[-3.7,57.09],[-3.68,57.09],[-3.68,57.075]]}]}""",
            )
        assertEquals(listOf("Kept"), r.document.areas!!.map { it.name }, "the drop is scoped to the item")
        // One defect, one diagnostic: a second "too few points" report would say
        // the ring was short, which it was not — a vertex was unusable.
        assertEquals(listOf("position-unusable"), r.diagnostics.map { it.code })
    }

    @Test
    fun `a position of one channel is unusable`() {
        val r = read("""{"vnx":"0.9.0","waypoints":[{"at":[-3]}]}""")
        assertEquals(0, r.document.waypoints!!.size)
        assertEquals(DiagnosticCause.POSITION_UNUSABLE, r.diagnostics.single().cause)
    }

    @Test
    fun `a null longitude or latitude is unusable`() {
        // Section 4.1: `null` marks an absent value from index 2 UPWARD.
        assertEquals(0, read("""{"vnx":"0.9.0","waypoints":[{"at":[null,57]}]}""").document.waypoints!!.size)
        assertEquals(0, read("""{"vnx":"0.9.0","waypoints":[{"at":[-3,null]}]}""").document.waypoints!!.size)
    }

    // ---------------------------------------------------------- moving time

    @Test
    fun `moving time is the sum over segments and never the wall-clock span`() {
        val r = Vnx.read(java.io.File(System.getProperty("vnx.corpus")!!, "accept/track-with-pause.vnx"))
        assertEquals(167000L, r.document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `a track with no pip times reports unknown, which is not zero`() {
        val r = read("""{"vnx":"0.9.0","tracks":[{"segments":[{"pips":[[-3,57],[-3,58]]}]}]}""")
        assertNull(r.document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `a track with no segments at all reports unknown`() {
        // Section 3.5, now stated: a track with no segments at all is unknown, for
        // the same reason a partial sum is.
        assertNull(read("""{"vnx":"0.9.0","tracks":[{"segments":[]}]}""").document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `a partly timed track is unknown, not the sum of the segments that are timed`() {
        // A partial sum is worse than zero rather than better: it is
        // indistinguishable from a complete one, so no consumer can tell which it
        // was given, where zero is at least recognisable as suspicious. I summed,
        // and so did three other implementations including the reference one, and
        // every tier stayed green until accept/track-partly-timed.vnx existed.
        val r =
            read(
                """{"vnx":"0.9.0","tracks":[{"segments":[
               {"pips":[[-3,57,340,1000],[-3,57.1,341,61000]]},
               {"pips":[[-3,57.2,342],[-3,57.3,343]]}]}]}""",
            )
        assertNull(r.document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `the times are read from the ENDS of a segment, not from its timed pips`() {
        // A segment whose interior is timed and whose ends are not has no
        // measurable span. Taking the first and last pip that HAPPEN to carry a
        // time invents one.
        val r =
            read(
                """{"vnx":"0.9.0","tracks":[{"segments":[
               {"pips":[[-3,57,340],[-3,57.1,341,1000],[-3,57.2,342,61000],[-3,57.3,343]]}]}]}""",
            )
        assertNull(r.document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `an empty segment contributes zero and does not make a track unknown`() {
        // It has no span to measure, so it is not an unmeasured span. Section 3.5
        // says so explicitly, because the rule reads either way without it and two
        // implementations differed with nothing to catch them.
        val r =
            read(
                """{"vnx":"0.9.0","tracks":[{"segments":[
               {"pips":[[-3,57,340,1000],[-3,57.1,341,61000]]},
               {"pips":[]}]}]}""",
            )
        assertEquals(60000L, r.document.tracks!![0].movingTimeMs())
    }

    // -------------------------------------------------------- type identity

    @Test
    fun `a type differing only by case is a different type`() {
        // Section 5: `@type` is compared exactly, including case.
        val r = read("""{"vnx":"0.9.0","waypoints":[{"at":[-3,57],"data":[{"@type":"App.viatrail.tag","value":"x"}]}]}""")
        assertEquals(emptyList(), r.diagnostics, "an unrecognised type is inert on any host")
        assertEquals(
            "App.viatrail.tag",
            r.document.waypoints!![0]
                .data!![0]
                .type,
            "case is never folded for identity",
        )
    }
}

package app.viatrail.vnx

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
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
        val r = read("""{"vnx":"0.1.0","waypoints":[{"nmae":"typo","name":"Car park","at":[-3.675,57.1201]}]}""")
        assertEquals(1, r.document.waypoints!!.size)
        assertEquals("Car park", r.document.waypoints!![0].name)
        assertEquals(1, r.diagnostics.size)
        assertEquals("/waypoints/0/nmae", r.diagnostics[0].path)
    }

    @Test
    fun `a wrong type in an OPTIONAL key drops the field and keeps the item`() {
        val r = read("""{"vnx":"0.1.0","waypoints":[{"name":7,"at":[-3.675,57.1201]}]}""")
        assertEquals(1, r.document.waypoints!!.size)
        assertNull(r.document.waypoints!![0].name)
        assertEquals(DiagnosticCause.WRONG_TYPE, r.diagnostics.single().cause)
    }

    @Test
    fun `a wrong type in a REQUIRED key drops the item`() {
        // The exception is not a special case: dropping a required field would
        // leave a waypoint that is nowhere.
        val r = read("""{"vnx":"0.1.0","waypoints":[{"name":"nowhere","at":"here"}]}""")
        assertEquals(0, r.document.waypoints!!.size)
        assertEquals(1, r.diagnostics.size)
    }

    @Test
    fun `a folder naming a folder that is not present keeps the item at the top level`() {
        // Section 6.2 in as many words: dropping the item would lose content over
        // a lost grouping.
        val r = read("""{"vnx":"0.1.0","waypoints":[{"folder":"ghost","at":[-3,57]}]}""")
        assertEquals(1, r.document.waypoints!!.size)
        assertNull(r.document.waypoints!![0].folder)
        assertEquals(DiagnosticCause.FOLDER_ABSENT, r.diagnostics.single().cause)
    }

    @Test
    fun `a folder with no id is dropped and its contents survive it`() {
        val r = read(
            """{"vnx":"0.1.0","folders":[{"name":"No id here"}],"waypoints":[{"folder":"anything","at":[-3,57]}]}""",
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
        val r = read(
            """{"vnx":"0.1.0","waypoints":[{"at":[-3,57],"data":[{"value":1},{"@type":"com.example.x","value":2}]}]}""",
        )
        val data = r.document.waypoints!![0].data!!
        assertEquals(1, data.size)
        assertEquals("com.example.x", data[0].type)
        assertEquals(DiagnosticCause.DATA_RECORD_TYPE_MISSING, r.diagnostics.single().cause)
    }

    @Test
    fun `an arc naming a node that is not in its network keeps the arc`() {
        // Section 6.2's remedy table: keep the arc, its geometry is real.
        val r = read(
            """{"vnx":"0.1.0","networks":[{"nodes":{"a":[-3,57]},
               "arcs":{"x":{"from":"a","to":"ghost","pips":[[-3,57],[-2,58]]}}}]}""",
        )
        assertEquals(1, r.document.networks!![0].arcs!!.size)
        assertEquals(DiagnosticCause.ARC_NODE_ABSENT, r.diagnostics.single().cause)
    }

    @Test
    fun `a polygon that does not close is dropped`() {
        val r = read("""{"vnx":"0.1.0","areas":[{"polygon":[[-3,57],[-2,57],[-2,58],[-3,58]]}]}""")
        assertEquals(0, r.document.areas!!.size)
        assertEquals(DiagnosticCause.POLYGON_NOT_A_CLOSED_RING, r.diagnostics.single().cause)
    }

    @Test
    fun `a polygon of fewer than four positions is dropped`() {
        val r = read("""{"vnx":"0.1.0","areas":[{"polygon":[[-3,57],[-2,57],[-3,57]]}]}""")
        assertEquals(0, r.document.areas!!.size)
        assertEquals(DiagnosticCause.POLYGON_NOT_A_CLOSED_RING, r.diagnostics.single().cause)
    }

    @Test
    fun `an importance outside 1 to 5 drops the field and keeps the item and the rest of the style`() {
        // Section 3.6 states the range and calls it normative; section 6.2 names
        // it as the worked example of a range this document states. Section 3.10
        // makes a style object opaque, and the two are in tension — this is the
        // narrow reading that satisfies both. REPORTED; no corpus case decides it.
        val r = read("""{"vnx":"0.1.0","waypoints":[{"at":[-3,57],"style":{"importance":9,"color":"#fff"}}]}""")
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
        val r = read("""{"vnx":"0.1.0","waypoints":[{"at":[-3,57],"style":{"halo-blur":2}}]}""")
        assertEquals(emptyList(), r.diagnostics)
        assertEquals(JsonNumber(2.0), r.document.waypoints!![0].style!!["halo-blur"])
    }

    @Test
    fun `a field inside a data record is never checked, whatever section 5-2 says its type is`() {
        // Section 6.2, amended: a record body is opaque apart from `@type`, and a
        // reader MUST NOT check the type, range or enumeration of a field in one.
        val r = read(
            """{"vnx":"0.1.0","networks":[{"points":[{"at":[-3,57],
               "data":[{"@type":"app.viatrail.camp","flat":"maybe","capacity":"lots"},
                       {"@type":"app.viatrail.settlement","size":"metropolis"}]}]}]}""",
        )
        assertEquals(emptyList(), r.diagnostics)
        assertEquals(2, r.document.networks!![0].points!![0].data!!.size)
    }

    // ------------------------------------------------------------- chains

    @Test
    fun `dropping a link does not then also report the chain as broken`() {
        // One defect must not become two, or the reader cannot tell how much is
        // actually wrong.
        val r = read(
            """{"vnx":"0.1.0","networks":[{"nodes":{"a":[-3.68,57.12],"b":[-3.66,57.1]},
               "arcs":{"a1":{"from":"a","to":"b","pips":[[-3.68,57.12],[-3.66,57.1]]}},
               "chains":[{"links":[{"arc":"a1","from":"a","to":"b"},{"arc":"ghost","from":"b","to":"a"}]}]}]}""",
        )
        assertEquals(1, r.diagnostics.size, "got: ${r.diagnostics}")
        assertEquals(DiagnosticCause.LINK_ARC_ABSENT, r.diagnostics[0].cause)
    }

    @Test
    fun `a link naming a node absent from the network is kept, and that end is not compared`() {
        // An invention, reported: section 6.2's remedy table covers an ARC naming
        // an absent node ("keep the arc; its geometry is real") and says nothing
        // about a LINK doing it. Keeping the link follows the same reasoning and
        // loses less, and the unresolvable end is then not compared, so one defect
        // does not become two.
        val r = read(
            """{"vnx":"0.1.0","networks":[{"nodes":{"a":[-3.68,57.12],"b":[-3.66,57.1]},
               "arcs":{"a1":{"from":"a","to":"b","pips":[[-3.68,57.12],[-3.66,57.1]]}},
               "chains":[{"links":[{"arc":"a1","from":"a","to":"ghost"},{"arc":"a1","from":"b","to":"a"}]}]}]}""",
        )
        assertEquals(2, r.document.networks!![0].chains!![0].links.size)
        assertEquals(1, r.diagnostics.size, "got: ${r.diagnostics}")
        assertEquals(DiagnosticCause.LINK_NODE_ABSENT, r.diagnostics[0].cause)
    }

    @Test
    fun `a node key and an inline position meet when the node's position equals it`() {
        // Section 6.2: a reader MUST compare a node key by its resolved position
        // rather than refusing to compare it, or a walk that is continuous is
        // reported as broken.
        val r = read(
            """{"vnx":"0.1.0","networks":[{"nodes":{"a":[-3.68,57.12],"b":[-3.66,57.1,940]},
               "arcs":{"a1":{"from":"a","to":"b","pips":[[-3.68,57.12],[-3.66,57.1]]}},
               "chains":[{"links":[{"arc":"a1","from":"a","to":[-3.66,57.1]},
                                   {"arc":"a1","from":"b","to":"a"}]}]}]}""",
        )
        assertEquals(emptyList(), r.diagnostics, "the node carries an elevation and the inline endpoint cannot")
    }

    @Test
    fun `an inline endpoint on an arc with no vertices drops the link`() {
        // Section 3.3.3: a polyline of no vertices has no distance, and the reader
        // must record a diagnostic and drop the link rather than invent a number.
        val r = read(
            """{"vnx":"0.1.0","networks":[{"nodes":{"a":[-3,57]},
               "arcs":{"a1":{"from":"a","to":"a","pips":[]}},
               "chains":[{"links":[{"arc":"a1","from":"a","to":[-3,57]}]}]}]}""",
        )
        assertEquals(0, r.document.networks!![0].chains!![0].links.size)
        assertTrue(r.diagnostics.any { it.cause == DiagnosticCause.INLINE_ENDPOINT_ON_EMPTY_ARC })
    }

    // ------------------------------------------------------------ versions

    @Test
    fun `a leading zero is a legal spelling and the comparison is numeric`() {
        // Section 2.2: `00.01.0000` is a legal spelling of 0.1.0.
        assertEquals(emptyList(), read("""{"vnx":"00.01.0000"}""").diagnostics)
        assertEquals("00.01.0000", read("""{"vnx":"00.01.0000"}""").document.vnx)
    }

    @Test
    fun `a reader writes the version the document declared, not its own`() {
        // Section 2.2: a 0.1.0 reader that reads a 0.1.3 document writes 0.1.3.
        val r = read("""{"vnx":"0.1.99","waypoints":[{"at":[-3,57]}]}""")
        assertTrue(Vnx.render(r.document).contains("\"vnx\": \"0.1.99\""))
    }

    @Test
    fun `a minor below the reader's is accepted and a minor above is refused`() {
        assertEquals(emptyList(), read("""{"vnx":"0.0.9"}""").diagnostics)
        val e = assertFailsWith<VnxRefusal> { read("""{"vnx":"0.2.0"}""") }
        assertEquals(RefusalCause.VERSION_TOO_NEW, e.refusal)
        assertEquals(RefusalCause.VERSION_TOO_NEW, assertFailsWith<VnxRefusal> { read("""{"vnx":"1.0.0"}""") }.refusal)
    }

    @Test
    fun `every malformed version shape is refused and none is coerced`() {
        for (bad in listOf("0.1", "0.1.0.0", "0.1.x", "0.1.+1", "", "v0.1.0", " 0.1.0")) {
            val e = assertFailsWith<VnxRefusal>("\"$bad\" must be refused") { read("""{"vnx":"$bad"}""") }
            assertEquals(RefusalCause.VERSION_MALFORMED, e.refusal, "for \"$bad\"")
            assertTrue(e.message.contains("MAJOR.MINOR.PATCH"), "the refusal must name the expected shape")
        }
    }

    // ----------------------------------------------------------- profiles

    @Test
    fun `an unrecognised profile is preserved and not acted on`() {
        // Section 5.3: a reader MUST preserve the value and MUST NOT act on it.
        // The records a profile would have removed are already absent.
        val r = read(
            """{"vnx":"0.1.0","profile":"something-else",
               "waypoints":[{"at":[-3,57],"data":[{"@type":"app.viatrail.navigation","value":"avoid"}]}]}""",
        )
        assertEquals(emptyList(), r.diagnostics)
        assertEquals("something-else", r.document.profile)
        assertEquals(1, r.document.waypoints!![0].data!!.size, "a reader must not filter by profile")
    }

    // ------------------------------------------------------- absent vs empty

    @Test
    fun `an empty collection is distinct from an absent one, and both survive a re-render`() {
        // Section 3.10: requiring a key does not require a non-empty value, and an
        // empty array is distinct from an absent one.
        val withEmpty = read("""{"vnx":"0.1.0","waypoints":[],"networks":[]}""")
        assertNotNull(withEmpty.document.waypoints)
        assertNull(withEmpty.document.tracks)
        assertEquals(
            "{\n  \"vnx\": \"0.1.0\",\n  \"networks\": [],\n  \"waypoints\": []\n}\n",
            Vnx.render(withEmpty.document),
        )
        assertEquals("{\n  \"vnx\": \"0.1.0\"\n}\n", Vnx.render(read("""{"vnx":"0.1.0"}""").document))
    }

    @Test
    fun `rulesets are opaque and survive a read-modify-write untouched`() {
        // Section 3.9: a 0.1.x reader treats the contents as opaque and preserves
        // them, under the same rules as an unknown data record.
        val json = "{\n  \"vnx\": \"0.1.0\",\n  \"rulesets\": [\n    {\n      \"anything\": true,\n" +
            "      \"nested\": {\n        \"deep\": [1, 2, 3]\n      }\n    }\n  ]\n}\n"
        val r = read(json)
        assertEquals(emptyList(), r.diagnostics)
        assertEquals(json, Vnx.render(r.document))
    }

    // ------------------------------------------------------------ positions

    @Test
    fun `positions of differing arity may appear in one pips array`() {
        // Section 4.1: a reader MUST read each position independently of its
        // neighbours. Forcing one arity would mean inventing elevations or
        // discarding them.
        val r = read("""{"vnx":"0.1.0","tracks":[{"segments":[{"pips":[[-3,57],[-3,57,340],[-3,57,null,1]]}]}]}""")
        assertEquals(emptyList(), r.diagnostics)
        assertEquals(listOf(2, 3, 4), r.document.tracks!![0].segments[0].pips.map { it.arity })
    }

    @Test
    fun `a channel beyond those this reader knows is kept, not dropped`() {
        // Section 4.1: a reader MUST ignore channels beyond those it knows, so
        // that a future channel is additive. Ignoring is not discarding — a
        // discarded channel could not survive a re-render.
        val json = "{\n  \"vnx\": \"0.1.0\",\n  \"waypoints\": [\n    {\n      \"at\": [-3, 57, 340, 1, 99]\n    }\n  ]\n}\n"
        val r = read(json)
        assertEquals(emptyList(), r.diagnostics)
        assertEquals(json, Vnx.render(r.document))
    }

    @Test
    fun `a position of one channel is unusable`() {
        val r = read("""{"vnx":"0.1.0","waypoints":[{"at":[-3]}]}""")
        assertEquals(0, r.document.waypoints!!.size)
        assertEquals(DiagnosticCause.POSITION_MALFORMED, r.diagnostics.single().cause)
    }

    @Test
    fun `a null longitude or latitude is unusable`() {
        // Section 4.1: `null` marks an absent value from index 2 UPWARD.
        assertEquals(0, read("""{"vnx":"0.1.0","waypoints":[{"at":[null,57]}]}""").document.waypoints!!.size)
        assertEquals(0, read("""{"vnx":"0.1.0","waypoints":[{"at":[-3,null]}]}""").document.waypoints!!.size)
    }

    // ---------------------------------------------------------- moving time

    @Test
    fun `moving time is the sum over segments and never the wall-clock span`() {
        val r = Vnx.read(java.io.File(System.getProperty("vnx.corpus")!!, "accept/track-with-pause.vnx"))
        assertEquals(167000L, r.document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `a track with no pip times reports unknown, which is not zero`() {
        val r = read("""{"vnx":"0.1.0","tracks":[{"segments":[{"pips":[[-3,57],[-3,58]]}]}]}""")
        assertNull(r.document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `a track with no segments at all reports unknown`() {
        // Section 3.5, now stated: a track with no segments at all is unknown, for
        // the same reason a partial sum is.
        assertNull(read("""{"vnx":"0.1.0","tracks":[{"segments":[]}]}""").document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `a partly timed track is unknown, not the sum of the segments that are timed`() {
        // A partial sum is worse than zero rather than better: it is
        // indistinguishable from a complete one, so no consumer can tell which it
        // was given, where zero is at least recognisable as suspicious. I summed,
        // and so did three other implementations including the reference one, and
        // every tier stayed green until accept/track-partly-timed.vnx existed.
        val r = read(
            """{"vnx":"0.1.0","tracks":[{"segments":[
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
        val r = read(
            """{"vnx":"0.1.0","tracks":[{"segments":[
               {"pips":[[-3,57,340],[-3,57.1,341,1000],[-3,57.2,342,61000],[-3,57.3,343]]}]}]}""",
        )
        assertNull(r.document.tracks!![0].movingTimeMs())
    }

    @Test
    fun `an empty segment contributes zero and does not make a track unknown`() {
        // It has no span to measure, so it is not an unmeasured span. Section 3.5
        // says so explicitly, because the rule reads either way without it and two
        // implementations differed with nothing to catch them.
        val r = read(
            """{"vnx":"0.1.0","tracks":[{"segments":[
               {"pips":[[-3,57,340,1000],[-3,57.1,341,61000]]},
               {"pips":[]}]}]}""",
        )
        assertEquals(60000L, r.document.tracks!![0].movingTimeMs())
    }

    // ------------------------------------------------------------ namespace

    @Test
    fun `the app-viatrail namespace is matched without regard to case, and identity is not`() {
        // Section 5.3: a type differing from the prefix only by case is INSIDE the
        // namespace, and is still a different type from the one it resembles.
        assertTrue(DataRecord("App.viatrail.tag", JsonObject()).isViatrailNamespace)
        assertTrue(DataRecord("APP.VIATRAIL.X", JsonObject()).isViatrailNamespace)
        assertTrue(!DataRecord("com.example.app.viatrail.x", JsonObject()).isViatrailNamespace)
        val r = read("""{"vnx":"0.1.0","waypoints":[{"at":[-3,57],"data":[{"@type":"App.viatrail.tag","value":"x"}]}]}""")
        assertEquals(emptyList(), r.diagnostics, "an unrecognised type is inert on any host")
        assertEquals("App.viatrail.tag", r.document.waypoints!![0].data!![0].type, "case is never folded for identity")
    }
}

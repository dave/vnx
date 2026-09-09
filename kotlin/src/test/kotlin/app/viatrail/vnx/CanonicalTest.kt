package app.viatrail.vnx

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/** SPEC.md section 4.5, the rules the corpus reaches only through whole files. */
class CanonicalTest {

    private fun render(json: String) = Canonical.render(JsonParser.parse(json))

    @Test
    fun `scalars sort before containers, each group lexicographically`() {
        assertEquals(
            """
            {
              "created": 1,
              "generator": 2,
              "profile": 3,
              "vnx": 4,
              "areas": [],
              "networks": {},
              "notes": []
            }

            """.trimIndent(),
            render("""{"networks":{},"vnx":4,"areas":[],"profile":3,"notes":[],"created":1,"generator":2}"""),
        )
    }

    @Test
    fun `classification is by the value in this object, so no key is named by the rule`() {
        // A chain link is the live case: `from` is a scalar when it is a node key
        // and a container when it is an inline position, and
        // accept/shared-turnaround-handle.vnx carries one of each in one chain.
        assertTrue(render("""{"arc":"a","from":"n","to":[1, 2]}""").startsWith("{\n  \"arc\""))
        assertEquals(
            "{\n  \"arc\": \"a\",\n  \"to\": \"n\",\n  \"from\": [1, 2]\n}\n",
            render("""{"arc":"a","from":[1,2],"to":"n"}"""),
        )
        assertEquals(
            "{\n  \"arc\": \"a\",\n  \"from\": \"n\",\n  \"to\": [1, 2]\n}\n",
            render("""{"to":[1,2],"arc":"a","from":"n"}"""),
        )
    }

    @Test
    fun `an array of numbers and nulls goes on one line, and null counts as a number`() {
        assertEquals("[-3.6712, 57.118, 340]\n", render("[-3.6712, 57.118, 340]"))
        // Without `null` counting, a position that lost its elevation would also
        // lose its inline rendering, making a document's shape depend on its data.
        assertEquals("[-3.6712, 57.118, null]\n", render("[-3.6712,57.118,null]"))
        assertEquals("[-3.6712, 57.118, null, 1778914991000]\n", render("[-3.6712,57.118,null,1778914991000]"))
    }

    @Test
    fun `an array holding anything else goes multi-line`() {
        assertEquals("[\n  \"a\"\n]\n", render("""["a"]"""))
        assertEquals("[\n  1,\n  2,\n  {\n    \"b\": null\n  }\n]\n", render("""[1,2,{"b":null}]"""))
    }

    @Test
    fun `an empty array and an empty object are one line each`() {
        assertEquals("[]\n", render("[]"))
        assertEquals("{}\n", render("{}"))
        assertEquals("{\n  \"a\": [],\n  \"b\": {}\n}\n", render("""{"b":{},"a":[]}"""))
    }

    @Test
    fun `arrays are never reordered`() {
        assertEquals("[3, 1, 2]\n", render("[3,1,2]"))
    }

    @Test
    fun `a string escapes only what RFC 8259 requires`() {
        // Section 4.5.5: every other character is written raw, including non-ASCII,
        // `<`, `>`, `&`, U+2028 and U+2029, and no character above U+007F may be
        // written as an escape.
        val raw = "\u00e9 \u2028 \u2029 < > & \u2603"
        val out = render(JsonString(raw).let { Canonical.render(it) }.trim())
        assertTrue(out.contains("\u00e9"), "a non-ASCII character must be written raw")
        assertTrue(out.contains("\u2028") && out.contains("\u2029"), "U+2028 and U+2029 are written raw")
        assertTrue(!out.contains("\\u00e9") && !out.contains("\\u2028"), "no escape above U+007F")
        assertEquals("\"a\\\"b\\\\c\"\n", Canonical.render(JsonString("a\"b\\c")))
        assertEquals("\"\\n\\r\\t\"\n", Canonical.render(JsonString("\n\r\t")))
        assertEquals("\"\\u0001\"\n", Canonical.render(JsonString("\u0001")))
    }

    @Test
    fun `keys sort by code point, not by UTF-16 code unit`() {
        // The two orders disagree above U+FFFF. Every astral character is encoded
        // as a surrogate pair beginning at U+D800, which sorts BELOW a BMP
        // character in the U+E000..U+FFFF range by UTF-16 code unit and ABOVE it
        // by code point. Kotlin's own `String.compareTo` is the UTF-16 order, and
        // a data record body may carry any key at all (section 5.1).
        val bmp = "\uFF3A"
        val astral = "\uD800\uDC00"
        assertTrue(astral < bmp, "by UTF-16 code unit the astral key sorts first")
        assertTrue(
            astral.codePointAt(0) > bmp.codePointAt(0),
            "by code point the astral key sorts second, which is the required order",
        )
        val out = Canonical.render(JsonObject(listOf(astral to JsonNumber(1.0), bmp to JsonNumber(2.0))))
        assertTrue(
            out.indexOf(bmp) < out.indexOf(astral),
            "the BMP key must come first, and it did not:\n$out",
        )
    }

    @Test
    fun `every document ends with exactly one newline`() {
        val out = render("""{"vnx":"0.1.0"}""")
        assertTrue(out.endsWith("}\n") && !out.endsWith("\n\n"))
    }
}

package app.viatrail.vnx

/**
 * A strict [RFC 8259] JSON parser.
 *
 * The RENDERER is kotlinx-serialization-json (Serialise.kt) and the PARSER is
 * this. The two are not symmetric, and the asymmetry is deliberate.
 *
 * What a reader does with a bad document is specified in detail — SPEC.md
 * section 6.2 names the four causes a refusal may have and section 6.3 requires
 * a diagnostic to carry a JSON Pointer to the thing it is about — and a stock
 * parser answers on its own terms instead. It reports the first syntax error and
 * stops, it decides for itself what a number that overflows binary64 becomes,
 * and its permissive modes accept exactly what section 4.2 forbids: `NaN` and
 * `Infinity` are not JSON, and a coordinate of `NaN` poisons every distance and
 * every bounding box computed downstream without ever failing. Nothing about the
 * bytes a WRITER emits is constrained at all, which is why the renderer could go
 * and this could not.
 *
 * Anything this parser refuses is refused as [RefusalCause.MALFORMED_JSON].
 */
internal class JsonParser(private val src: String) {
    private var i = 0

    companion object {
        fun parse(text: String): JsonValue {
            val p = JsonParser(text)
            p.skipWhitespace()
            val v = p.parseValue()
            p.skipWhitespace()
            if (p.i != p.src.length) p.fail("trailing content after the top-level value")
            return v
        }
    }

    private fun fail(what: String): Nothing {
        var line = 1
        var col = 1
        for (j in 0 until minOf(i, src.length)) {
            if (src[j] == '\n') {
                line++
                col = 1
            } else {
                col++
            }
        }
        refuse(RefusalCause.MALFORMED_JSON, "malformed JSON at line $line column $col: $what")
    }

    private fun skipWhitespace() {
        while (i < src.length) {
            when (src[i]) {
                ' ', '\t', '\n', '\r' -> i++
                else -> return
            }
        }
    }

    private fun peek(): Char {
        if (i >= src.length) fail("unexpected end of input")
        return src[i]
    }

    private fun expect(c: Char) {
        if (i >= src.length || src[i] != c) fail("expected '$c'")
        i++
    }

    private fun parseValue(): JsonValue = when (val c = peek()) {
        '{' -> parseObject()
        '[' -> parseArray()
        '"' -> JsonString(parseString())
        't' -> {
            literal("true"); JsonBool(true)
        }
        'f' -> {
            literal("false"); JsonBool(false)
        }
        'n' -> {
            literal("null"); JsonNull
        }
        else -> if (c == '-' || c in '0'..'9') parseNumber() else fail("unexpected character '$c'")
    }

    private fun literal(word: String) {
        if (!src.startsWith(word, i)) fail("unexpected token, expected $word")
        i += word.length
    }

    private fun parseObject(): JsonObject {
        expect('{')
        val entries = ArrayList<Pair<String, JsonValue>>()
        skipWhitespace()
        if (peek() == '}') {
            i++
            return JsonObject(entries)
        }
        while (true) {
            skipWhitespace()
            if (peek() != '"') fail("an object key must be a string")
            val key = parseString()
            skipWhitespace()
            expect(':')
            skipWhitespace()
            entries.add(key to parseValue())
            skipWhitespace()
            when (peek()) {
                ',' -> i++
                '}' -> {
                    i++
                    return JsonObject(entries)
                }
                else -> fail("expected ',' or '}' in an object")
            }
        }
    }

    private fun parseArray(): JsonArray {
        expect('[')
        val elements = ArrayList<JsonValue>()
        skipWhitespace()
        if (peek() == ']') {
            i++
            return JsonArray(elements)
        }
        while (true) {
            skipWhitespace()
            elements.add(parseValue())
            skipWhitespace()
            when (peek()) {
                ',' -> i++
                ']' -> {
                    i++
                    return JsonArray(elements)
                }
                else -> fail("expected ',' or ']' in an array")
            }
        }
    }

    private fun parseString(): String {
        expect('"')
        val sb = StringBuilder()
        while (true) {
            if (i >= src.length) fail("unterminated string")
            when (val c = src[i]) {
                '"' -> {
                    i++
                    return sb.toString()
                }
                '\\' -> {
                    i++
                    if (i >= src.length) fail("unterminated escape")
                    when (val e = src[i]) {
                        '"' -> { sb.append('"'); i++ }
                        '\\' -> { sb.append('\\'); i++ }
                        '/' -> { sb.append('/'); i++ }
                        'b' -> { sb.append('\u0008'); i++ }
                        'f' -> { sb.append('\u000C'); i++ }
                        'n' -> { sb.append('\n'); i++ }
                        'r' -> { sb.append('\r'); i++ }
                        't' -> { sb.append('\t'); i++ }
                        'u' -> {
                            i++
                            if (i + 4 > src.length) fail("truncated unicode escape")
                            val hex = src.substring(i, i + 4)
                            if (!hex.all { it in '0'..'9' || it in 'a'..'f' || it in 'A'..'F' }) {
                                fail("a unicode escape needs four hexadecimal digits")
                            }
                            sb.append(hex.toInt(16).toChar())
                            i += 4
                        }
                        else -> fail("unrecognised escape for '$e'")
                    }
                }
                else -> {
                    if (c < ' ') fail("a raw control character in a string")
                    sb.append(c)
                    i++
                }
            }
        }
    }

    private fun parseNumber(): JsonNumber {
        val start = i
        if (i < src.length && src[i] == '-') i++
        if (i >= src.length) fail("truncated number")
        if (src[i] == '0') {
            i++
        } else if (src[i] in '1'..'9') {
            while (i < src.length && src[i] in '0'..'9') i++
        } else {
            fail("a number must begin with a digit or a minus sign")
        }
        // RFC 8259 forbids a leading zero followed by further digits.
        if (i < src.length && src[i] in '0'..'9') fail("a number may not have a leading zero")
        if (i < src.length && src[i] == '.') {
            i++
            if (i >= src.length || src[i] !in '0'..'9') fail("a fraction needs at least one digit")
            while (i < src.length && src[i] in '0'..'9') i++
        }
        if (i < src.length && (src[i] == 'e' || src[i] == 'E')) {
            i++
            if (i < src.length && (src[i] == '+' || src[i] == '-')) i++
            if (i >= src.length || src[i] !in '0'..'9') fail("an exponent needs at least one digit")
            while (i < src.length && src[i] in '0'..'9') i++
        }
        val text = src.substring(start, i)
        val d = text.toDouble()
        if (d.isNaN() || d.isInfinite()) {
            // Syntactically JSON, but it overflows binary64. SPEC.md section 4.2
            // forbids a non-finite number anywhere in a document.
            refuse(
                RefusalCause.NON_FINITE_NUMBER,
                "the number $text is not finite in binary64, which SPEC 4.2 forbids",
            )
        }
        return JsonNumber(d)
    }
}

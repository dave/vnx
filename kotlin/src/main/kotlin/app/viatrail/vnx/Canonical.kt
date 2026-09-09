package app.viatrail.vnx

/**
 * The canonical-form serialiser of SPEC.md section 4.5.
 *
 * Canonical form is the byte layout every conforming writer in every language
 * emits for a given document, so that two implementations either produce
 * identical files or one of them is wrong. No stock JSON encoder in any target
 * language emits it (section 4.5.6), which is why this is here.
 */
internal object Canonical {

    /** Renders a value as a complete canonical document, newline-terminated. */
    fun render(value: JsonValue): String {
        val sb = StringBuilder()
        write(sb, value, 0)
        sb.append('\n')
        return sb.toString()
    }

    private val byCodePoint = CodePointOrder.comparator

    private fun isContainer(v: JsonValue) = v is JsonObject || v is JsonArray

    private fun write(sb: StringBuilder, value: JsonValue, depth: Int) {
        when (value) {
            is JsonNull -> sb.append("null")
            is JsonBool -> sb.append(if (value.value) "true" else "false")
            is JsonNumber -> sb.append(EcmaNumber.render(value.value))
            is JsonString -> writeString(sb, value.value)
            is JsonArray -> writeArray(sb, value, depth)
            is JsonObject -> writeObject(sb, value, depth)
        }
    }

    private fun writeObject(sb: StringBuilder, obj: JsonObject, depth: Int) {
        if (obj.size == 0) {
            sb.append("{}")
            return
        }
        // Section 4.5.1: scalars first in lexicographic order, then containers in
        // lexicographic order. Classification is by the value present in THIS
        // object, so no key is named by the rule — a link's `from` sorts as a
        // scalar when it is a node key and as a container when it is an inline
        // position, in the same document.
        val entries = obj.entries()
        val scalars = entries.filter { !isContainer(it.second) }.sortedWith(compareBy(byCodePoint) { it.first })
        val containers = entries.filter { isContainer(it.second) }.sortedWith(compareBy(byCodePoint) { it.first })
        val ordered = scalars + containers

        val inner = "  ".repeat(depth + 1)
        sb.append("{\n")
        for ((index, entry) in ordered.withIndex()) {
            sb.append(inner)
            writeString(sb, entry.first)
            sb.append(": ")
            write(sb, entry.second, depth + 1)
            if (index != ordered.size - 1) sb.append(',')
            sb.append('\n')
        }
        sb.append("  ".repeat(depth)).append('}')
    }

    private fun writeArray(sb: StringBuilder, arr: JsonArray, depth: Int) {
        if (arr.elements.isEmpty()) {
            sb.append("[]")
            return
        }
        // Section 4.5.2: an array holding only numbers and `null` goes on one
        // line. `null` counts as a number for this rule, so a position does not
        // change shape when its elevation goes missing.
        if (arr.elements.all { it is JsonNumber || it is JsonNull }) {
            sb.append('[')
            for ((index, element) in arr.elements.withIndex()) {
                if (index > 0) sb.append(", ")
                write(sb, element, depth)
            }
            sb.append(']')
            return
        }
        val inner = "  ".repeat(depth + 1)
        sb.append("[\n")
        for ((index, element) in arr.elements.withIndex()) {
            sb.append(inner)
            write(sb, element, depth + 1)
            if (index != arr.elements.size - 1) sb.append(',')
            sb.append('\n')
        }
        sb.append("  ".repeat(depth)).append(']')
    }

    /**
     * Section 4.5.5: a string escapes only what RFC 8259 requires — `"`, `\` and
     * the control characters below U+0020. Every other character is written raw,
     * including non-ASCII, `<`, `>`, `&`, U+2028 and U+2029, and no character
     * above U+007F may be written as an escape.
     */
    private fun writeString(sb: StringBuilder, s: String) {
        sb.append('"')
        for (c in s) {
            when {
                c == '"' -> sb.append("\\\"")
                c == '\\' -> sb.append("\\\\")
                c == '\n' -> sb.append("\\n")
                c == '\r' -> sb.append("\\r")
                c == '\t' -> sb.append("\\t")
                c == '\u0008' -> sb.append("\\b")
                c == '\u000C' -> sb.append("\\f")
                c < ' ' -> sb.append("\\u").append("%04x".format(c.code))
                else -> sb.append(c)
            }
        }
        sb.append('"')
    }
}

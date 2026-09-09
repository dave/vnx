package app.viatrail.vnx

/**
 * Lexicographic by Unicode code point.
 *
 * SPEC.md section 4.5.1 defines this for object keys, and section 7.1 sorts a
 * container's attachment names with it — in as many words, "the comparator is
 * the one Section 4.5.1 already defines, so no implementation needs a second
 * one". So it is one object here rather than two call sites each reaching for a
 * sort.
 *
 * Kotlin's own `String.compareTo` is NOT this. It orders by UTF-16 code unit,
 * and the two disagree above U+FFFF: an astral character is a surrogate pair
 * beginning at U+D800, which sorts BELOW a BMP character in U+E000..U+FFFF by
 * code unit and ABOVE it by code point. `"😀".compareTo("�")` is
 * -10176 on the JVM, which is 0xD83D minus 0xFFFD: the high surrogate, not the
 * code point.
 *
 * Comparing UTF-8 bytes gives code point order exactly, which is why that is
 * what this does rather than decoding to code points and comparing those.
 *
 * The reach is not theoretical in either place. A data record body may carry any
 * key at all (section 5.1), and an attachment name is whatever a user called a
 * photograph.
 */
internal object CodePointOrder {
    val comparator: Comparator<String> = Comparator { a, b ->
        val x = a.toByteArray(Charsets.UTF_8)
        val y = b.toByteArray(Charsets.UTF_8)
        var i = 0
        while (i < x.size && i < y.size) {
            val d = (x[i].toInt() and 0xff) - (y[i].toInt() and 0xff)
            if (d != 0) return@Comparator d
            i++
        }
        x.size - y.size
    }
}

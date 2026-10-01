package app.viatrail.vnx

import java.io.File
import java.util.Locale
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Properties of this library that are about the JVM it happens to be running on
 * rather than about VNX, and that a corpus run on one machine cannot see.
 */
class PortabilityTest {
    /**
     * The container reader compares entry names without regard to case, and the
     * writer formats numbers. In a Turkish or Azeri locale `"I".lowercase()` is a
     * dotless `ı`, and in many locales the decimal separator is a comma, so
     * neither may go through the JVM's default locale.
     */
    @Test
    fun `case-insensitive comparison is locale-independent`() {
        val original = Locale.getDefault()
        try {
            for (locale in listOf(Locale.forLanguageTag("tr-TR"), Locale.forLanguageTag("az"), Locale.ROOT)) {
                Locale.setDefault(locale)
                val corpus = File(System.getProperty("vnx.corpus")!!)
                val r = Vnx.readContainer(File(corpus, "accept/container-root-doc.vnz").readBytes())
                assertEquals("In a container", r.document.waypoints!![0].name, "container reading broke under $locale")
                // And the writer's numbers must not pick up a locale's decimal
                // separator: a comma there would not be JSON at all.
                val written =
                    Vnx.write(
                        Vnx.readDocument("""{"vnx":"0.8.0","waypoints":[{"at":[-3.6712,57.118]}]}""").document,
                    )
                assertEquals("""{"vnx":"0.8.0","waypoints":[{"at":[-3.6712,57.118]}]}""" + "\n", written, "under $locale")
            }
        } finally {
            Locale.setDefault(original)
        }
    }

    /**
     * The library imports the standard library and ONE named dependency.
     *
     * A codec with dependencies is a codec people cannot adopt, so the list is
     * short and is written down here rather than left to whatever the build
     * happens to resolve. kotlinx-serialization-json is on it because the JVM
     * standard library has no JSON API at all; anything else arriving
     * transitively and being used without a decision is what this catches.
     *
     * The dependency is also confined to ONE FILE, which is the property that
     * keeps it replaceable.
     */
    @Test
    fun `the library imports the standard library and one named dependency`() {
        val main = File("src/main/kotlin")
        assertTrue(main.isDirectory, "the source tree moved: ${main.absolutePath}")
        val offenders = ArrayList<String>()
        val serialisationImports = ArrayList<String>()
        main.walkTopDown().filter { it.extension == "kt" }.forEach { file ->
            for ((number, line) in file.readLines().withIndex()) {
                val trimmed = line.trim()
                if (!trimmed.startsWith("import ")) continue
                val target = trimmed.removePrefix("import ").substringBefore(' ')
                if (target.startsWith("kotlinx.serialization.json.")) {
                    serialisationImports.add(file.name)
                    continue
                }
                val allowed =
                    target.startsWith("java.") ||
                        target.startsWith("javax.") ||
                        target.startsWith("kotlin.") ||
                        target.startsWith("app.viatrail.vnx.")
                if (!allowed) offenders.add("${file.name}:${number + 1}: $target")
            }
        }
        assertTrue(offenders.isEmpty(), "the library has grown a dependency:\n${offenders.joinToString("\n")}")
        assertTrue(serialisationImports.isNotEmpty(), "nothing imports the JSON library, so this test is dead")
        assertEquals(
            setOf("Serialise.kt"),
            serialisationImports.toSet(),
            "the JSON library has spread beyond the one file that renders a document",
        )
    }

    @Test
    fun `a byte order mark on the way in does not become one on the way out`() {
        // This writer emits none. A reader still meets one.
        val bytes = "{\"vnx\":\"0.8.0\"}".toByteArray(Charsets.UTF_8)
        val r = Vnx.readDocument(bytes)
        assertEquals(emptyList(), r.diagnostics)
        assertEquals("{\"vnx\":\"0.8.0\"}\n", Vnx.write(r.document))
    }
}

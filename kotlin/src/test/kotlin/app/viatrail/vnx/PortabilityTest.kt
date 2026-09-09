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
     * Every case-insensitive comparison in this format is over ASCII vocabulary —
     * `app.viatrail.`, `doc.vnx`, `.vnx` — and every one of them contains the
     * letter `i`. In a Turkish locale `"I".lowercase()` is a dotless `ı`, so a
     * reader that folded case through the default locale would stop recognising
     * `DOC.VNX`, and would classify `APP.VIATRAIL.TAG` as another program's
     * extension in a portable export that was meant to strip it.
     */
    @Test
    fun `case-insensitive comparison is locale-independent`() {
        val original = Locale.getDefault()
        try {
            for (locale in listOf(Locale.forLanguageTag("tr-TR"), Locale.forLanguageTag("az"), Locale.ROOT)) {
                Locale.setDefault(locale)
                assertTrue(
                    DataRecord("APP.VIATRAIL.TAG", JsonObject()).isViatrailNamespace,
                    "the reserved namespace stopped matching under $locale",
                )
                assertTrue(
                    DataRecord("App.Viatrail.Camp", JsonObject()).isViatrailNamespace,
                    "the reserved namespace stopped matching under $locale",
                )
                val corpus = File(System.getProperty("vnx.corpus")!!)
                val r = Vnx.readContainer(File(corpus, "accept/container-root-doc.vnz").readBytes())
                assertEquals("In a container", r.document.waypoints!![0].name, "container reading broke under $locale")
                // And the renderer's numbers must not pick up a locale's decimal
                // separator: a comma there would be a byte incompatibility.
                assertEquals("-3.6712", EcmaNumber.render(-3.6712), "number rendering broke under $locale")
                assertEquals("1e+21", EcmaNumber.render(1e21), "number rendering broke under $locale")
            }
        } finally {
            Locale.setDefault(original)
        }
    }

    /**
     * The library imports the standard library and nothing else.
     *
     * A codec with dependencies is a codec people cannot adopt, and this is the
     * property everyone reimplementing the format relies on. The build declares
     * no dependencies, so this guards against one arriving transitively and being
     * used without anyone noticing.
     */
    @Test
    fun `the library imports nothing outside the standard library`() {
        val main = File("src/main/kotlin")
        assertTrue(main.isDirectory, "the source tree moved: ${main.absolutePath}")
        val offenders = ArrayList<String>()
        main.walkTopDown().filter { it.extension == "kt" }.forEach { file ->
            for ((number, line) in file.readLines().withIndex()) {
                val trimmed = line.trim()
                if (!trimmed.startsWith("import ")) continue
                val target = trimmed.removePrefix("import ").substringBefore(' ')
                val allowed = target.startsWith("java.") || target.startsWith("javax.") ||
                    target.startsWith("kotlin.") || target.startsWith("app.viatrail.vnx.")
                if (!allowed) offenders.add("${file.name}:${number + 1}: $target")
            }
        }
        assertTrue(offenders.isEmpty(), "the library has grown a dependency:\n${offenders.joinToString("\n")}")
    }

    @Test
    fun `a byte order mark on the way in does not become one on the way out`() {
        // Section 4.5.5 forbids a writer emitting one. A reader still meets one.
        val bytes = "﻿{\"vnx\":\"0.1.0\"}".toByteArray(Charsets.UTF_8)
        val r = Vnx.readDocument(bytes)
        assertEquals(emptyList(), r.diagnostics)
        assertEquals("{\n  \"vnx\": \"0.1.0\"\n}\n", Vnx.render(r.document))
    }
}

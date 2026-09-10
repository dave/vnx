plugins {
    kotlin("jvm") version "2.1.20"
}

repositories {
    mavenCentral()
}

// One dependency: kotlinx-serialization-json.
//
// The JVM standard library has no JSON API at all, so a JVM implementation
// either takes a JSON library or writes one. The bytes of a document are no
// longer a rule (SPEC.md section 4.2 and section 8.1), so writing one buys
// nothing that a library does not give.
//
// The READER is still this library's own. See JsonParser.kt for why.
dependencies {
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.8.0")
    testImplementation(kotlin("test"))
}

kotlin {
    jvmToolchain(17)
}

tasks.test {
    useJUnitPlatform()
    // The corpus decides whether this library conforms, and it lives beside this
    // build rather than inside it. Passing its location explicitly means the
    // runner never has to guess a working directory.
    systemProperty("vnx.corpus", layout.projectDirectory.dir("../corpus").asFile.absolutePath)
    // The number-conformance table of SPEC 4.1, a corpus tier of its own.
    systemProperty("vnx.numbers", layout.projectDirectory.file("../corpus/numbers/numbers.tsv").asFile.absolutePath)
    testLogging {
        events("passed", "failed", "skipped")
        exceptionFormat = org.gradle.api.tasks.testing.logging.TestExceptionFormat.FULL
        showStandardStreams = true
    }
}

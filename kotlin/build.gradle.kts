plugins {
    kotlin("jvm") version "2.1.20"
}

repositories {
    mavenCentral()
}

// No third-party dependencies, on purpose. The JSON parser, the serialiser and
// the ECMAScript number renderer in this library are written here because a
// codec with dependencies is a codec people cannot adopt, and because no stock
// JSON library emits the canonical form SPEC.md section 4.5 requires.
//
// The only test dependency is the Kotlin test library, which is part of the
// Kotlin distribution this build already uses.
dependencies {
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
    // The number-conformance table (SPEC 4.5.3 and 4.5.4), now a corpus tier.
    systemProperty("vnx.numbers", layout.projectDirectory.file("../corpus/numbers/numbers.tsv").asFile.absolutePath)
    testLogging {
        events("passed", "failed", "skipped")
        exceptionFormat = org.gradle.api.tasks.testing.logging.TestExceptionFormat.FULL
        showStandardStreams = true
    }
}

plugins {
    kotlin("jvm") version "2.1.20"
    application
    id("org.jlleitschuh.gradle.ktlint") version "12.1.2"
}

// The library's formatter, at the same pin; scripts/check-format.sh runs it.
ktlint {
    version.set("1.5.0")
}

repositories {
    mavenCentral()
}

// Resolved from the included build at ../../../kotlin, not from a repository.
dependencies {
    implementation("app.viatrail:vnx:0.10.6")
}

kotlin {
    jvmToolchain(17)
}

application {
    mainClass.set("InteropKt")
}

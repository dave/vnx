plugins {
    kotlin("jvm") version "2.1.20"
    application
}

repositories {
    mavenCentral()
}

// Resolved from the included build at ../../../kotlin, not from a repository.
dependencies {
    implementation("app.viatrail:vnx:0.1.0")
}

kotlin {
    jvmToolchain(17)
}

application {
    mainClass.set("InteropKt")
}

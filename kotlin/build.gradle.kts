plugins {
    kotlin("jvm") version "2.1.20"
    `maven-publish`
    id("org.jlleitschuh.gradle.ktlint") version "12.1.2"
}

// The one formatter, pinned, at its default style. `ktlintCheck` is what
// scripts/check-format.sh runs; `ktlintFormat` fixes what it reports.
ktlint {
    version.set("1.5.0")
}

// The coordinate a consumer writes: app.viatrail:vnx:0.9.0. `app.viatrail`
// matches the package namespace the source already uses, and the version is the
// VNX version this library implements, as in the Python, JavaScript and Rust
// builds beside it.
group = "app.viatrail"
version = "0.9.1"

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

java {
    // Both come from the java plugin, so neither needs a task of its own.
    withSourcesJar()
    withJavadocJar()
}

// A publication is what makes the coordinate real. The POM it carries is the
// reason: `files(".../vnx.jar")` has no POM, so kotlinx-serialization never
// reaches a consumer's runtime classpath and writing a document dies with
// `NoClassDefFoundError: kotlinx/serialization/json/Json`. `from(components["java"])`
// carries the dependencies declared above into the POM, so the consumer names
// this library and nothing else.
publishing {
    publications {
        create<MavenPublication>("maven") {
            from(components["java"])
            pom {
                name.set("VNX for Kotlin")
                description.set(
                    "Reads and writes VNX documents and VNZ containers on the JVM, " +
                        "and reports diagnostics.",
                )
                url.set("https://github.com/dave/vnx")
                licenses {
                    license {
                        name.set("MIT License")
                        url.set("https://github.com/dave/vnx/blob/main/LICENCE")
                    }
                }
                scm {
                    url.set("https://github.com/dave/vnx")
                    connection.set("scm:git:https://github.com/dave/vnx.git")
                }
            }
        }
    }
}

tasks.test {
    useJUnitPlatform()
    // The corpus decides whether this library conforms, and it lives beside this
    // build rather than inside it. Passing its location explicitly means the
    // runner never has to guess a working directory.
    systemProperty(
        "vnx.corpus",
        layout.projectDirectory
            .dir("../corpus")
            .asFile.absolutePath,
    )
    // AND AS AN INPUT. Gradle does not look inside a system property, so
    // without this a corpus edit left the task UP-TO-DATE and `./gradlew test`
    // reported the last run's green against cases it never read.
    inputs.dir(layout.projectDirectory.dir("../corpus")).withPropertyName("corpus")
    testLogging {
        events("passed", "failed", "skipped")
        exceptionFormat = org.gradle.api.tasks.testing.logging.TestExceptionFormat.FULL
        showStandardStreams = true
    }
}

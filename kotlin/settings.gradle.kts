// Standalone. This build is deliberately not part of any other build: a VNX
// library that can only be compiled from inside an application is not a library
// anyone can adopt. It publishes `app.viatrail:vnx:0.1.0` instead, so a consumer
// writes a coordinate and nothing else. See the `publishing` block in
// build.gradle.kts.
rootProject.name = "vnx"

// A build of its own, including the library's build rather than being part of
// it, so the matrix measures the working tree rather than a published artifact.
// The library's own build does not know this exists: a driver nobody consumes
// has no business in a published library.
rootProject.name = "interop"

includeBuild("../../../kotlin")

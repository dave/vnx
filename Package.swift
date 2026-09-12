// swift-tools-version: 5.9
import PackageDescription

// The Swift library lives in `swift/`, because five other languages are equal
// citizens of this tree and none of them owns the root. This manifest is at the
// root anyway, because SwiftPM resolves a git dependency by looking for
// `Package.swift` at the repository root and nowhere else — a manifest in
// `swift/` is reachable only by path, from a clone the consumer made. Targets
// carry `path:` into `swift/`, which is what lets the two facts coexist.
//
// Foundation, and zlib through the system library target below.
//
// Nothing is added to `dependencies`: a conformance library that needs a
// package graph cannot be dropped into an app that has none, and the corpus is
// meant to be runnable by anyone with a toolchain. A SYSTEM library is a
// different bargain — zlib ships with the macOS SDK and with every Linux
// distribution, so it costs no fetch and no lock file, and it is what SPEC 7
// needs to write a deflated entry and read one back.
let package = Package(
    name: "VNX",
    // `JSONEncoder.OutputFormatting.withoutEscapingSlashes` needs 10.15, which
    // is 2019. An attachment path and a URL in a data record both hold slashes,
    // and `\/` for `/` is legal JSON that nothing gains by.
    platforms: [.macOS(.v10_15), .iOS(.v13), .tvOS(.v13), .watchOS(.v6)],
    products: [
        .library(name: "VNX", targets: ["VNX"])
    ],
    targets: [
        // No `pkgConfig`: zlib.h sits on the default header search path of both
        // the macOS SDK and a Linux distribution, so the module map alone finds
        // it. The providers are what a failed build prints, not a fetch.
        .systemLibrary(name: "CZlib", path: "swift/Sources/CZlib",
                       providers: [.apt(["zlib1g-dev"]), .brew(["zlib"])]),
        .target(name: "VNX", dependencies: ["CZlib"], path: "swift/Sources/VNX"),
        .testTarget(name: "VNXTests", dependencies: ["VNX"], path: "swift/Tests/VNXTests")
    ]
)

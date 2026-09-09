// swift-tools-version: 5.9
import PackageDescription

// Foundation only. Nothing is added to `dependencies`, ever: a conformance
// library that needs a package graph cannot be dropped into an app that has
// none, and the corpus is meant to be runnable by anyone with a toolchain.
let package = Package(
    name: "VNX",
    products: [
        .library(name: "VNX", targets: ["VNX"])
    ],
    targets: [
        .target(name: "VNX"),
        .testTarget(name: "VNXTests", dependencies: ["VNX"])
    ]
)

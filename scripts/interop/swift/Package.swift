// swift-tools-version: 5.9
import PackageDescription

// A package of its own, depending on the library by path, so the matrix
// measures the working tree rather than a published version. The root
// Package.swift is the shipped manifest and does not know this exists: an
// executable nobody consumes has no business in a library's product list.
let package = Package(
    name: "vnx-interop",
    platforms: [.macOS(.v10_15)],
    dependencies: [.package(path: "../../..")],
    targets: [
        .executableTarget(name: "interop", dependencies: [.product(name: "VNX", package: "vnx")],
                          path: "Sources/interop")
    ]
)

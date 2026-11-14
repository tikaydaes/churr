// swift-tools-version:5.9
// The notification extension's logic as a Swift package, so it can be
// tested on a Mac with `swift test` (Tests/ChurrNotifyTests runs the
// sealed-message vectors of testdata/sealed). The Xcode target
// "NotificationService" compiles the same files in Sources/ChurrNotify
// together with NotificationService.swift.
import PackageDescription

let package = Package(
    name: "ChurrNotify",
    platforms: [.macOS(.v13), .iOS(.v15)],
    products: [.library(name: "ChurrNotify", targets: ["ChurrNotify"])],
    targets: [
        .target(name: "ChurrNotify", path: "Sources/ChurrNotify"),
        .testTarget(name: "ChurrNotifyTests", dependencies: ["ChurrNotify"], path: "Tests/ChurrNotifyTests"),
    ]
)

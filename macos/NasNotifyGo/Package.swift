// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "NasNotifyGo",
    platforms: [
        .macOS(.v11)
    ],
    products: [
        .executable(
            name: "NasNotifyGo",
            targets: ["NasNotifyGo"]
        )
    ],
    targets: [
        .executableTarget(
            name: "NasNotifyGo",
            path: "Sources/NasNotifyGo"
        )
    ]
)
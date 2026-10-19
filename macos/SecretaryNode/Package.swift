// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "SecretaryNode",
    platforms: [.macOS(.v13)],
    products: [.executable(name: "SecretaryNode", targets: ["SecretaryNode"])],
    targets: [
        .target(name: "NodeLifecycle"),
        .executableTarget(name: "SecretaryNode", dependencies: ["NodeLifecycle"]),
        .testTarget(name: "NodeLifecycleTests", dependencies: ["NodeLifecycle"])
    ]
)

// swift-tools-version: 6.0
// CorrespondentKit: the Correspondent API as the Apple apps use it — models,
// client, live events, the offline copy and the token in the Keychain.
import PackageDescription

let package = Package(
    name: "CorrespondentKit",
    platforms: [.iOS(.v18), .macOS(.v15)],
    products: [.library(name: "CorrespondentKit", targets: ["CorrespondentKit"])],
    targets: [
        .target(name: "CorrespondentKit"),
        .testTarget(name: "CorrespondentKitTests", dependencies: ["CorrespondentKit"]),
    ]
)

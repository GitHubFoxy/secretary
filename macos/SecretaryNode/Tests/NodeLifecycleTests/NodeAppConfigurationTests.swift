import Foundation
import XCTest
@testable import NodeLifecycle

final class NodeAppConfigurationTests: XCTestCase {
    private func fixture(paired: Bool = false, credential: String? = String(repeating: "a", count: 64)) throws -> (URL, URL, URL) {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let state = home.appendingPathComponent(".local/share/secretary")
        let node = state.appendingPathComponent("node")
        let data = node.appendingPathComponent("data")
        try FileManager.default.createDirectory(at: data, withIntermediateDirectories: true)
        let config = node.appendingPathComponent("config.json")
        let json = ["server_url": "http://127.0.0.1:8081", "node": "test", "data_dir": data.path] as [String: Any]
        try JSONSerialization.data(withJSONObject: json).write(to: config)
        if let credential {
            let environment = state.appendingPathComponent("environment")
            try "export SECRETARY_BOOTSTRAP_TOKEN='do-not-inherit'\nexport SECRETARY_NODE_ADMIN_TOKEN='do-not-inherit'\nexport SECRETARY_NODE_PAIRING_TOKEN='\(credential)'\n".write(to: environment, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: environment.path)
        }
        if paired {
            try "{}".write(to: data.appendingPathComponent("identity.json"), atomically: true, encoding: .utf8)
        }
        return (home, config, data)
    }

    func testFirstEnrollmentReadsOnlyPersistedPairingToken() throws {
        let (home, _, _) = try fixture()
        defer { try? FileManager.default.removeItem(at: home) }
        let resolved = try NodeAppConfiguration.resolve(home: home, inherited: ["SECRETARY_NODE_ADMIN_TOKEN": "admin", "SECRETARY_BOOTSTRAP_TOKEN": "bootstrap"])
        XCTAssertEqual(resolved.environment["SECRETARY_NODE_PAIRING_TOKEN"], String(repeating: "a", count: 64))
        XCTAssertNil(resolved.environment["SECRETARY_NODE_ADMIN_TOKEN"])
        XCTAssertNil(resolved.environment["SECRETARY_BOOTSTRAP_TOKEN"])
        XCTAssertEqual(resolved.serverHealthURL.absoluteString, "http://127.0.0.1:8081/v1/health")
    }

    func testRestartReusesIdentityEvenWhenEnvironmentCredentialUnavailable() throws {
        let (home, _, data) = try fixture(paired: true, credential: nil)
        defer { try? FileManager.default.removeItem(at: home) }
        let before = try Data(contentsOf: data.appendingPathComponent("identity.json"))
        let first = try NodeAppConfiguration.resolve(home: home, inherited: [:])
        let second = try NodeAppConfiguration.resolve(home: home, inherited: [:])
        XCTAssertNil(first.environment["SECRETARY_NODE_PAIRING_TOKEN"])
        XCTAssertNil(second.environment["SECRETARY_NODE_PAIRING_TOKEN"])
        XCTAssertEqual(first.configuration, second.configuration)
        XCTAssertEqual(try Data(contentsOf: data.appendingPathComponent("identity.json")), before)
    }

    func testIncompleteSetupFailsWithoutExposingSecrets() throws {
        let (home, _, _) = try fixture(credential: nil)
        defer { try? FileManager.default.removeItem(at: home) }
        XCTAssertThrowsError(try NodeAppConfiguration.resolve(home: home, inherited: [:])) {
            XCTAssertEqual($0 as? NodeAppConfigurationError, .missingPairing)
            XCTAssertTrue($0.localizedDescription.contains("secretary setup"))
        }
        let environment = home.appendingPathComponent(".local/share/secretary/environment")
        try "export SECRETARY_NODE_PAIRING_TOKEN='BAD-SECRET'\n".write(to: environment, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: environment.path)
        XCTAssertThrowsError(try NodeAppConfiguration.resolve(home: home, inherited: [:])) {
            XCTAssertFalse($0.localizedDescription.contains("BAD-SECRET"))
        }
    }

    func testStandaloneConfigRemainsUsableWithoutManagedPairingFile() throws {
        let (home, config, _) = try fixture(credential: nil)
        defer { try? FileManager.default.removeItem(at: home) }
        let resolved = try NodeAppConfiguration.resolve(home: home, inherited: [
            "SECRETARY_NODE_EXECUTABLE": home.appendingPathComponent(".local/share/secretary-node/current/bin/secretary-node").path,
            "SECRETARY_NODE_CONFIG": config.path
        ])
        XCTAssertEqual(resolved.configuration, config)
        XCTAssertNil(resolved.environment["SECRETARY_NODE_PAIRING_TOKEN"])
    }

    func testInsecureCredentialFileIsRejected() throws {
        let (home, _, _) = try fixture()
        defer { try? FileManager.default.removeItem(at: home) }
        try FileManager.default.setAttributes([.posixPermissions: 0o644], ofItemAtPath: home.appendingPathComponent(".local/share/secretary/environment").path)
        XCTAssertThrowsError(try NodeAppConfiguration.resolve(home: home, inherited: [:]))
    }
    func testLifecycleFirstEnrollmentThenRelaunchPreservesIdentity() throws {
        let (home, _, data) = try fixture()
        defer { try? FileManager.default.removeItem(at: home) }
        let identity = data.appendingPathComponent("identity.json")
        let appDirectory = home.appendingPathComponent("Library/Application Support/SecretaryNode")
        let script = """
        if [ ! -f "$SECRETARY_TEST_IDENTITY" ]; then
          test -n "$SECRETARY_NODE_PAIRING_TOKEN" || exit 7
          printf 'original-node-identity' > "$SECRETARY_TEST_IDENTITY"
        else
          test -z "${SECRETARY_NODE_PAIRING_TOKEN:-}" || exit 8
          test "$(cat "$SECRETARY_TEST_IDENTITY")" = original-node-identity || exit 9
        fi
        sleep 1
        """
        func awaitExit(_ lifecycle: NodeLifecycle) {
            let deadline = Date().addingTimeInterval(5)
            while lifecycle.state == .running && Date() < deadline {
                Thread.sleep(forTimeInterval: 0.05)
            }
            XCTAssertEqual(lifecycle.state, .stopped)
        }
        var first: NodeLifecycle? = try {
            let settings = try NodeAppConfiguration.resolve(home: home, inherited: [:])
            return try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sh"), configuration: nil, arguments: ["-c", script], environment: settings.environment.merging(["SECRETARY_TEST_IDENTITY": identity.path]) { _, new in new }, directory: appDirectory)
        }()
        try first?.start()
        awaitExit(first!)
        XCTAssertEqual(try String(contentsOf: identity, encoding: .utf8), "original-node-identity")
        first = nil
        let settings = try NodeAppConfiguration.resolve(home: home, inherited: [:])
        XCTAssertNil(settings.environment["SECRETARY_NODE_PAIRING_TOKEN"])
        let second = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sh"), arguments: ["-c", script], environment: settings.environment.merging(["SECRETARY_TEST_IDENTITY": identity.path]) { _, new in new }, directory: appDirectory)
        try second.start()
        awaitExit(second)
        XCTAssertEqual(try String(contentsOf: identity, encoding: .utf8), "original-node-identity")
    }

}

private final class HealthURLProtocol: URLProtocol {
    static var statusCode = 200
    static var body = Data(#"{"status":"ok"}"#.utf8)

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let response = HTTPURLResponse(url: request.url!, statusCode: Self.statusCode, httpVersion: nil, headerFields: nil)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Self.body)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

final class ServerHealthMonitorTests: XCTestCase {
    func testHealthyServerRemainsOnlineDespiteNodeFailure() {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [HealthURLProtocol.self]
        let session = URLSession(configuration: configuration)
        defer { session.invalidateAndCancel() }
        let monitor = ServerHealthMonitor(url: URL(string: "http://localhost:8081/v1/health")!, session: session)
        let menu = NodeMenuPresentation()
        menu.render(state: .failed("Node exited with status 1"))
        let done = expectation(description: "health")
        monitor.check { state in
            menu.renderServer(state)
            XCTAssertEqual(menu.status.title, "Node: Node exited with status 1")
            XCTAssertEqual(menu.server.title, "Server: Online")
            done.fulfill()
        }
        wait(for: [done], timeout: 5)
    }

    func testServerResponseMustContainHealthyStatus() {
        HealthURLProtocol.statusCode = 503
        defer { HealthURLProtocol.statusCode = 200 }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [HealthURLProtocol.self]
        let session = URLSession(configuration: configuration)
        defer { session.invalidateAndCancel() }
        let monitor = ServerHealthMonitor(url: URL(string: "http://localhost:8081/v1/health")!, session: session)
        let done = expectation(description: "unhealthy")
        monitor.check { state in
            XCTAssertEqual(state, .offline)
            done.fulfill()
        }
        wait(for: [done], timeout: 5)
    }
}

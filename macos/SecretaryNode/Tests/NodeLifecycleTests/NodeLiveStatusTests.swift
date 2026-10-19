import AppKit
import Foundation
import XCTest
@testable import NodeLifecycle

final class NodeLiveStatusTests: XCTestCase {
    private func statusFile() throws -> URL {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return directory.appendingPathComponent("status.json")
    }

    private func write(_ url: URL, server: String, harnesses: String = "[]", timestamp: String = "2026-10-19T08:00:00.123456789Z") throws {
        let payload = """
        {"server":"\(server)","updated_at":"\(timestamp)","harnesses":\(harnesses)}
        """
        try payload.write(to: url, atomically: true, encoding: .utf8)
    }

    func testTransitionsAndHarnessDiscoveryReflectStatusFile() throws {
        let url = try statusFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let now = ISO8601DateFormatter().date(from: "2026-10-19T08:00:01Z")!
        let menu = NodeMenuPresentation()
        try write(url, server: "reconnecting")
        menu.renderLive(NodeLiveSnapshot.load(from: url, now: now))
        XCTAssertEqual(menu.server.title, "Server: Reconnecting")
        try write(url, server: "connected", harnesses: "[{\"kind\":\"codex\",\"status\":\"ready\"},{\"kind\":\"claude-code\",\"status\":\"unavailable\"}]")
        let connected = NodeLiveSnapshot.load(from: url, now: now)
        XCTAssertEqual(connected?.harnesses.count, 2)
        menu.renderLive(connected)
        XCTAssertEqual(menu.server.title, "Server: Connected")
        XCTAssertTrue(menu.items(in: menu.menu).contains("codex: Ready"))
        XCTAssertTrue(menu.items(in: menu.menu).contains("claude-code: Unavailable"))
        XCTAssertEqual(menu.menu.item(at: 2)?.isSeparatorItem, true)
        try write(url, server: "reconnecting")
        menu.renderLive(NodeLiveSnapshot.load(from: url, now: now))
        XCTAssertFalse(menu.items(in: menu.menu).contains("codex: Ready"))
        try write(url, server: "offline")
        menu.renderLive(NodeLiveSnapshot.load(from: url, now: now))
        XCTAssertEqual(menu.server.title, "Server: Offline")
    }

    func testStaleMissingCorruptAndShutdownFailClosed() throws {
        let url = try statusFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let now = ISO8601DateFormatter().date(from: "2026-10-19T08:02:00Z")!
        let menu = NodeMenuPresentation()
        XCTAssertNil(NodeLiveSnapshot.load(from: url, now: now))
        try write(url, server: "connected")
        XCTAssertNil(NodeLiveSnapshot.load(from: url, now: now))
        menu.renderLive(nil)
        XCTAssertEqual(menu.server.title, "Server: Offline")
        try "garbled".write(to: url, atomically: true, encoding: .utf8)
        XCTAssertNil(NodeLiveSnapshot.load(from: url, now: now))
        menu.renderLive(nil)
        XCTAssertEqual(menu.server.title, "Server: Offline")
    }

    func testMenuUsesNativeCompactRowsAndSystemAppearance() {
        let menu = NodeMenuPresentation()
        XCTAssertEqual(menu.menu.item(at: 2)?.isSeparatorItem, true)
        XCTAssertEqual(menu.menu.items.count, 4)
        XCTAssertNil(menu.menu.appearance)
        XCTAssertFalse(menu.menu.autoenablesItems)
    }
}

private extension NodeMenuPresentation {
    func items(in menu: NSMenu) -> [String] { menu.items.map(\.title) }
}

import Foundation
import XCTest
import ServiceManagement
@testable import NodeLifecycle

final class NodeIntegrationsTests: XCTestCase {
    func testLogsUseCompanionManagedPath() {
        XCTAssertTrue(NodeIntegrations.logURL.path.hasSuffix("/Library/Logs/SecretaryNode/node.log"))
    }

    func testMenuStartsWithLoginSettingIndependentOfTransientState() {
        let menu = NodeMenuPresentation()
        XCTAssertEqual(menu.launchAtLogin.state, .off)
        XCTAssertEqual(menu.openLogs.title, "Open Logs")
        XCTAssertTrue(menu.menu.items[2].isSeparatorItem)
        XCTAssertEqual(menu.menu.items[3].title, "Launch at Login")
        XCTAssertEqual(menu.menu.items[4].title, "Open Logs")
    }

    func testLoginItemStatusGuidance() {
        XCTAssertNil(NodeIntegrations.loginDescription(.enabled))
        XCTAssertNil(NodeIntegrations.loginDescription(.notRegistered))
        XCTAssertTrue(NodeIntegrations.loginDescription(.requiresApproval)?.contains("System Settings") == true)
        XCTAssertTrue(NodeIntegrations.loginDescription(.notFound)?.contains("Applications") == true)
        let menu = NodeMenuPresentation()
        menu.updateLogin(.requiresApproval)
        XCTAssertEqual(menu.launchAtLogin.state, .off)
        XCTAssertEqual(menu.launchAtLogin.title, "Launch at Login (Approval Needed)")
        XCTAssertNotNil(menu.launchAtLogin.toolTip)
        menu.updateLogin(.enabled)
        XCTAssertEqual(menu.launchAtLogin.state, .on)
        XCTAssertEqual(menu.launchAtLogin.title, "Launch at Login")
    }

    func testManagedNodeCreatesReadableLogFile() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        defer { try? FileManager.default.removeItem(at: directory) }
        let node = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), arguments: ["30"], directory: directory)
        try node.start()
        XCTAssertTrue(FileManager.default.isReadableFile(atPath: NodeIntegrations.logURL.path))
        let stopped = expectation(description: "managed node stopped")
        node.stop { error in
            XCTAssertNil(error)
            stopped.fulfill()
        }
        wait(for: [stopped], timeout: 20)
        XCTAssertEqual(node.state, .stopped)
    }

    func testMenuKeepsCompactNativeAppearance() {
        let menu = NodeMenuPresentation()
        XCTAssertTrue(menu.menu.autoenablesItems == false)
        XCTAssertEqual(menu.menu.items.filter { $0.title == "Quit Secretary Node" }.count, 1)
    }
}

import Foundation
import XCTest
@testable import NodeLifecycle

final class NodeIntegrationsTests: XCTestCase {
    func testLogsUseCompanionManagedPath() {
        XCTAssertTrue(NodeIntegrations.logURL.path.hasSuffix("/Library/Logs/SecretaryNode/node.log"))
    }

    func testMenuStartsWithLoginSettingIndependentOfTransientState() {
        let menu = NodeMenuPresentation()
        XCTAssertEqual(menu.launchAtLogin.state, .off)
        XCTAssertEqual(menu.openLogs.title, "Open Logs")
        XCTAssertEqual(menu.menu.items[2].isSeparatorItem, true)
    }

    func testMenuKeepsCompactNativeAppearance() {
        let menu = NodeMenuPresentation()
        XCTAssertTrue(menu.menu.autoenablesItems == false)
        XCTAssertEqual(menu.menu.items.filter { $0.title == "Quit Secretary Node" }.count, 1)
    }
}

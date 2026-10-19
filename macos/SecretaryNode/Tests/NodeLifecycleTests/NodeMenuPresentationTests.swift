import AppKit
import XCTest
@testable import NodeLifecycle

final class NodeMenuPresentationTests: XCTestCase {
    private struct MenuFailure: LocalizedError {
        let errorDescription: String?
    }

    func testUnavailableControllerShowsStartupFailure() {
        let menu = NodeMenuPresentation()
        menu.startupFailed(MenuFailure(errorDescription: "Cannot initialize Node"))
        menu.render(state: nil)
        XCTAssertEqual(menu.status.title, "Node: Start failed")
        XCTAssertEqual(menu.status.toolTip, "Cannot initialize Node")
    }

    func testRecoveredNodeIsRunningInsteadOfStartupFailure() {
        let menu = NodeMenuPresentation()
        menu.recovered()
        menu.render(state: .alreadyRunning)
        XCTAssertEqual(menu.status.title, "Node: Running (recovered)")
        XCTAssertTrue(menu.quit.isEnabled)
    }

    func testQuitDisablesActionAndPreservesFailureAcrossRefresh() {
        let menu = NodeMenuPresentation()
        menu.render(state: .running)
        menu.beginQuit()
        XCTAssertFalse(menu.quit.isEnabled)
        menu.stopFailed(MenuFailure(errorDescription: "Timed out waiting for Node"))
        menu.render(state: .running)
        XCTAssertEqual(menu.status.title, "Node: Stop failed")
        XCTAssertEqual(menu.status.toolTip, "Timed out waiting for Node")
        XCTAssertTrue(menu.quit.isEnabled)
        menu.render(state: .stopped)
        XCTAssertEqual(menu.status.title, "Node: Stop failed")
        menu.beginQuit()
        menu.render(state: .stopped)
        XCTAssertEqual(menu.status.title, "Node: Stopped")
    }

    func testNativeMenuUsesSystemAppearanceAcrossThemeChanges() {
        let menu = NodeMenuPresentation()
        XCTAssertNotNil(NSAppearance(named: .aqua))
        XCTAssertNotNil(NSAppearance(named: .darkAqua))
        XCTAssertNil(menu.menu.appearance)
        menu.render(state: .running)
        XCTAssertNil(menu.menu.appearance)
        XCTAssertEqual(menu.status.title, "Node: Running")
        XCTAssertEqual(menu.menu.items.count, 7)
        XCTAssertTrue(menu.menu.items[2].isSeparatorItem)
        XCTAssertTrue(menu.menu.items[5].isSeparatorItem)
        XCTAssertIdentical(menu.menu.items[3], menu.launchAtLogin)
        XCTAssertIdentical(menu.menu.items[4], menu.openLogs)
        XCTAssertIdentical(menu.menu.items[6], menu.quit)
    }

    func testNewPresentationStartsReadyAfterPreviousQuit() {
        let previous = NodeMenuPresentation()
        previous.beginQuit()
        XCTAssertFalse(previous.quit.isEnabled)
        let restarted = NodeMenuPresentation()
        restarted.render(state: .alreadyRunning)
        XCTAssertEqual(restarted.status.title, "Node: Running (recovered)")
        XCTAssertTrue(restarted.quit.isEnabled)
    }
}

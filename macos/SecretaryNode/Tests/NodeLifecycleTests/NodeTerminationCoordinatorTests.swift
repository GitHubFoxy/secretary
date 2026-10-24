import Foundation
import XCTest
@testable import NodeLifecycle

final class NodeTerminationCoordinatorTests: XCTestCase {
    func testMenuAndAppKitRequestsShareOneInFlightStop() {
        var finishes: [((Error?) -> Void)] = []
        var events: [String] = []
        let coordinator = NodeTerminationCoordinator { finish in
            events.append("stop")
            finishes.append(finish)
        }
        coordinator.request { error in
            XCTAssertNil(error)
            events.append("terminate")
        }
        coordinator.request { _ in XCTFail("concurrent completion") }
        XCTAssertEqual(events, ["stop"])
        XCTAssertEqual(coordinator.state, .stopping)
        XCTAssertEqual(finishes.count, 1)
        finishes[0](nil)
        XCTAssertEqual(events, ["stop", "terminate"])
        XCTAssertEqual(coordinator.state, .stopped)
        coordinator.request { _ in XCTFail("repeated stop") }
        XCTAssertEqual(finishes.count, 1)
    }

    func testAppleEventWaitsForChildStopCompletion() {
        var finish: ((Error?) -> Void)?
        var allowed = false
        let coordinator = NodeTerminationCoordinator { callback in finish = callback }
        coordinator.request { error in
            XCTAssertNil(error)
            allowed = true
        }
        XCTAssertEqual(coordinator.state, .stopping)
        XCTAssertFalse(allowed)
        finish?(nil)
        XCTAssertEqual(coordinator.state, .stopped)
        XCTAssertTrue(allowed)
    }

    func testStopFailureRejectsQuitAndAllowsRetry() {
        var finishes: [((Error?) -> Void)] = []
        var approvals: [Bool] = []
        let coordinator = NodeTerminationCoordinator { finishes.append($0) }
        coordinator.request { approvals.append($0 == nil) }
        let failure = NSError(domain: "Node", code: 1)
        finishes[0](failure)
        XCTAssertEqual(approvals, [false])
        XCTAssertEqual(coordinator.state, .idle)
        coordinator.request { approvals.append($0 == nil) }
        XCTAssertEqual(finishes.count, 2)
        finishes[1](nil)
        XCTAssertEqual(approvals, [false, true])
        XCTAssertEqual(coordinator.state, .stopped)
    }
}

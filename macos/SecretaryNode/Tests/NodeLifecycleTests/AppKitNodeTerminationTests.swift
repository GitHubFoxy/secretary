import AppKit
import XCTest
@testable import NodeLifecycle

final class AppKitNodeTerminationTests: XCTestCase {
    private final class Harness {
        var events: [String] = []
        var completions: [((Error?) -> Void)] = []
        var boundary: AppKitNodeTermination!

        init() {
            boundary = AppKitNodeTermination(
                stop: { [self] completion in
                    events.append("stop")
                    completions.append(completion)
                },
                begin: { [self] in events.append("begin") },
                failed: { [self] error in events.append("error:\\(error.localizedDescription)") },
                reply: { [self] allowed in events.append(allowed ? "reply:true" : "reply:false") },
                terminate: { [self] in events.append("terminate") },
                dispatch: { $0() }
            )
        }
    }

    func testMenuQuitStopsBeforeAppTermination() {
        let h = Harness()
        h.boundary.menuQuit()
        XCTAssertEqual(h.events, ["begin", "stop"])
        h.boundary.menuQuit()
        XCTAssertEqual(h.completions.count, 1)
        h.completions[0](nil)
        XCTAssertEqual(h.events, ["begin", "stop", "terminate"])
        XCTAssertEqual(h.boundary.applicationShouldTerminate(NSApplication.shared), .terminateNow)
        XCTAssertEqual(h.completions.count, 1)
    }

    func testAppKitQuitDefersAndRepliesOnlyAfterStop() {
        let h = Harness()
        XCTAssertEqual(h.boundary.applicationShouldTerminate(NSApplication.shared), .terminateLater)
        XCTAssertEqual(h.events, ["begin", "stop"])
        XCTAssertEqual(h.boundary.applicationShouldTerminate(NSApplication.shared), .terminateLater)
        h.boundary.menuQuit()
        XCTAssertEqual(h.completions.count, 1)
        XCTAssertEqual(h.events, ["begin", "stop"])
        h.completions[0](nil)
        XCTAssertEqual(h.events, ["begin", "stop", "reply:true"])
        XCTAssertEqual(h.boundary.applicationShouldTerminate(NSApplication.shared), .terminateNow)
    }

    func testMenuThenAppKitQuitSharesStopAndRepliesOnce() {
        let h = Harness()
        h.boundary.menuQuit()
        XCTAssertEqual(h.boundary.applicationShouldTerminate(NSApplication.shared), .terminateLater)
        h.completions[0](nil)
        XCTAssertEqual(h.events, ["begin", "stop", "reply:true"])
    }

    func testFailedStopDeniesAppKitQuitShowsErrorAndCanRetry() {
        let h = Harness()
        XCTAssertEqual(h.boundary.applicationShouldTerminate(NSApplication.shared), .terminateLater)
        let error = NSError(domain: "Node", code: 4, userInfo: [NSLocalizedDescriptionKey: "stop timed out"])
        h.completions[0](error)
        XCTAssertEqual(h.events, ["begin", "stop", "reply:false", "error:stop timed out"])
        XCTAssertEqual(h.boundary.applicationShouldTerminate(NSApplication.shared), .terminateLater)
        XCTAssertEqual(h.completions.count, 2)
        XCTAssertEqual(h.events.suffix(2), ["begin", "stop"])
        h.completions[1](nil)
        XCTAssertEqual(h.events.suffix(1), ["reply:true"])
        XCTAssertEqual(h.boundary.applicationShouldTerminate(NSApplication.shared), .terminateNow)
    }

    func testMenuFailureDoesNotTerminateAndCanRetry() {
        let h = Harness()
        h.boundary.menuQuit()
        h.completions[0](NSError(domain: "Node", code: 2))
        XCTAssertEqual(h.completions.count, 1)
        XCTAssertFalse(h.events.contains("terminate"))
        XCTAssertTrue(h.events.last?.hasPrefix("error:") == true)
        h.boundary.menuQuit()
        XCTAssertEqual(h.completions.count, 2)
        h.completions[1](nil)
        XCTAssertEqual(h.events.last, "terminate")
    }
}

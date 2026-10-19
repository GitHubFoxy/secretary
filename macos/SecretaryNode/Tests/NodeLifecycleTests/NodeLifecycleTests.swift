import Foundation
import XCTest
@testable import NodeLifecycle

final class NodeLifecycleTests: XCTestCase {
    private func temporaryDirectory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    func testExclusiveAppInstanceLock() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let first = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), directory: directory)
        XCTAssertEqual(first.state, .stopped)
        XCTAssertThrowsError(try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), directory: directory)) {
            XCTAssertEqual($0 as? NodeLifecycleError, .alreadyOpen)
        }
    }

    func testRunStopAndRelaunch() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let lifecycle = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), directory: directory)
        XCTAssertEqual(lifecycle.state, .stopped)
        try lifecycle.start()
        XCTAssertEqual(lifecycle.state, .running)
        XCTAssertNoThrow(try lifecycle.start())
        let stopped = expectation(description: "Node stopped")
        lifecycle.stop { stopped.fulfill() }
        wait(for: [stopped], timeout: 5)
        XCTAssertEqual(lifecycle.state, .stopped)
        try lifecycle.start()
        XCTAssertEqual(lifecycle.state, .running)
        let again = expectation(description: "Node stopped again")
        lifecycle.stop { again.fulfill() }
        wait(for: [again], timeout: 5)
    }

    func testUnrelatedPidIsNotTerminated() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let unrelated = Process()
        unrelated.executableURL = URL(fileURLWithPath: "/bin/sleep")
        unrelated.arguments = ["30"]
        try unrelated.run()
        defer { if unrelated.isRunning { unrelated.terminate(); unrelated.waitUntilExit() } }
        try Data(String(unrelated.processIdentifier).utf8).write(to: directory.appendingPathComponent("managed-node.pid"))
        let lifecycle = try NodeLifecycle(executable: URL(fileURLWithPath: "/usr/bin/true"), directory: directory)
        XCTAssertEqual(lifecycle.state, .stopped)
        XCTAssertTrue(unrelated.isRunning)
    }
}

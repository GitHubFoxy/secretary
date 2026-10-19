import Foundation
import XCTest
@testable import NodeLifecycle

final class NodeLifecycleTests: XCTestCase {
    private func temporaryDirectory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: url.path)
        return url
    }

    func testManagedNodeRecoveredAfterCompanionRestartWithoutDuplicateProcess() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        var original: NodeLifecycle? = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), arguments: ["30"], directory: directory)
        try original?.start()
        let pidFile = directory.appendingPathComponent("managed-node.pid")
        let originalPID = try String(contentsOf: pidFile, encoding: .utf8)
        XCTAssertEqual(original?.state, .running)
        original = nil
        let relaunched = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), arguments: ["30"], directory: directory)
        XCTAssertEqual(relaunched.state, .alreadyRunning)
        XCTAssertThrowsError(try relaunched.start()) {
            XCTAssertEqual($0 as? NodeLifecycleError, .alreadyRunning)
        }
        XCTAssertEqual(try String(contentsOf: pidFile, encoding: .utf8), originalPID)
        let stopped = expectation(description: "recovered Node stopped")
        relaunched.stop { error in
            XCTAssertNil(error)
            stopped.fulfill()
        }
        wait(for: [stopped], timeout: 20)
        XCTAssertEqual(relaunched.state, .stopped)
        try relaunched.start()
        XCTAssertEqual(relaunched.state, .running)
        let restarted = expectation(description: "restarted Node stopped")
        relaunched.stop { error in
            XCTAssertNil(error)
            restarted.fulfill()
        }
        wait(for: [restarted], timeout: 20)
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
        let lifecycle = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), arguments: ["30"], directory: directory)
        XCTAssertEqual(lifecycle.state, .stopped)
        try lifecycle.start()
        XCTAssertEqual(lifecycle.state, .running)
        XCTAssertNoThrow(try lifecycle.start())
        let stopped = expectation(description: "Node stopped")
        lifecycle.stop { error in XCTAssertNil(error); stopped.fulfill() }
        wait(for: [stopped], timeout: 5)
        XCTAssertEqual(lifecycle.state, .stopped)
        try lifecycle.start()
        XCTAssertEqual(lifecycle.state, .running)
        let again = expectation(description: "Node stopped again")
        lifecycle.stop { error in XCTAssertNil(error); again.fulfill() }
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

    func testMissingExecutableIsRejected() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let lifecycle = try NodeLifecycle(executable: directory.appendingPathComponent("missing"), directory: directory)
        XCTAssertThrowsError(try lifecycle.start()) {
            XCTAssertEqual($0 as? NodeLifecycleError, .invalidExecutable)
        }
        XCTAssertEqual(lifecycle.state, .stopped)
    }

    func testIncorrectProcessBirthIdentityNeverAdoptsOrKillsForeignNode() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let foreign = Process()
        foreign.executableURL = URL(fileURLWithPath: "/bin/sleep")
        foreign.arguments = ["30"]
        try foreign.run()
        defer {
            if foreign.isRunning { foreign.terminate() }
            foreign.waitUntilExit()
        }
        try Data(String(foreign.processIdentifier).utf8).write(to: directory.appendingPathComponent("managed-node.pid"))
        try "invalid".write(to: directory.appendingPathComponent("managed-node.identity"), atomically: true, encoding: .utf8)
        let lifecycle = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), directory: directory)
        XCTAssertEqual(lifecycle.state, .stopped)
        let done = expectation(description: "No child to stop")
        lifecycle.stop { error in
            XCTAssertNil(error)
            done.fulfill()
        }
        wait(for: [done], timeout: 2)
        XCTAssertTrue(foreign.isRunning)
    }

    func testRejectsInsecureStateDirectory() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: directory.path)
        XCTAssertThrowsError(try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), directory: directory))
    }

    func testCompanionRelaunchReleasesSingleInstanceLock() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        var first: NodeLifecycle? = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), directory: directory)
        XCTAssertNotNil(first)
        XCTAssertThrowsError(try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), directory: directory)) {
            XCTAssertEqual($0 as? NodeLifecycleError, .alreadyOpen)
        }
        first = nil
        let relaunched = try NodeLifecycle(executable: URL(fileURLWithPath: "/bin/sleep"), directory: directory)
        XCTAssertEqual(relaunched.state, .stopped)
    }
}

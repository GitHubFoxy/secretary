import Darwin
import Foundation

public enum NodeState: Equatable {
    case stopped
    case running
    case alreadyRunning
    case failed(String)
}

public enum NodeLifecycleError: Error, Equatable {
    case alreadyOpen
    case invalidExecutable
    case alreadyRunning
}

public final class NodeLifecycle {
    public let executable: URL
    private let configuration: URL?
    private let arguments: [String]
    private let directory: URL
    private let lock: Int32
    private var process: Process?
    private var lastError: String?

    public init(executable: URL, configuration: URL? = nil, arguments: [String] = [], directory: URL) throws {
        self.executable = executable.resolvingSymlinksInPath().standardizedFileURL
        self.configuration = configuration
        self.arguments = arguments
        self.directory = directory
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let descriptor = open(directory.appendingPathComponent("menu-bar.lock").path, O_RDWR | O_CREAT | O_NOFOLLOW | O_CLOEXEC, 0o600)
        guard descriptor >= 0 else { throw POSIXError(.EACCES) }
        guard flock(descriptor, LOCK_EX | LOCK_NB) == 0 else {
            close(descriptor)
            throw NodeLifecycleError.alreadyOpen
        }
        lock = descriptor
    }

    deinit {
        flock(lock, LOCK_UN)
        close(lock)
    }

    private var pidFile: URL { directory.appendingPathComponent("managed-node.pid") }

    private func matchingNode(_ pid: pid_t) -> Bool {
        guard pid > 1 else { return false }
        var buffer = [CChar](repeating: 0, count: Int(PROC_PIDPATHINFO_MAXSIZE))
        guard proc_pidpath(pid, &buffer, UInt32(buffer.count)) > 0 else { return false }
        let path = String(cString: buffer)
        return URL(fileURLWithPath: path).resolvingSymlinksInPath().standardizedFileURL == executable
    }

    private func previousNode() -> pid_t? {
        guard let data = try? Data(contentsOf: pidFile),
              let value = String(data: data, encoding: .utf8),
              let pid = pid_t(value.trimmingCharacters(in: .whitespacesAndNewlines)),
              matchingNode(pid) else { return nil }
        return pid
    }

    public var state: NodeState {
        if let process {
            if process.isRunning { return .running }
            if terminationReasonFailed(process) { return .failed("Node exited with status \(process.terminationStatus)") }
        }
        if previousNode() != nil { return .alreadyRunning }
        if let lastError { return .failed(lastError) }
        return .stopped
    }

    private func terminationReasonFailed(_ process: Process) -> Bool {
        !process.isRunning && (process.terminationReason != .exit || process.terminationStatus != 0)
    }

    public func start() throws {
        if process?.isRunning == true { return }
        guard previousNode() == nil else { throw NodeLifecycleError.alreadyRunning }
        guard FileManager.default.isExecutableFile(atPath: executable.path) else { throw NodeLifecycleError.invalidExecutable }
        let child = Process()
        child.executableURL = executable
        child.arguments = (configuration.map { ["--config", $0.path] } ?? []) + arguments
        child.standardInput = FileHandle.nullDevice
        child.standardOutput = FileHandle.nullDevice
        child.standardError = FileHandle.nullDevice
        try child.run()
        process = child
        lastError = nil
        do {
            try Data(String(child.processIdentifier).utf8).write(to: pidFile, options: .atomic)
        } catch {
            child.terminate()
            child.waitUntilExit()
            process = nil
            throw error
        }
    }

    public func stop(completion: @escaping () -> Void) {
        guard let child = process, child.isRunning else {
            completion()
            return
        }
        child.interrupt()
        DispatchQueue.global(qos: .userInitiated).async {
            child.waitUntilExit()
            try? FileManager.default.removeItem(at: self.pidFile)
            self.process = nil
            completion()
        }
    }
}

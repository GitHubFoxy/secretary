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
    private let environment: [String: String]
    private let directory: URL
    private let lock: Int32
    private var process: Process?
    private let identity: URL

    public init(executable: URL, configuration: URL? = nil, arguments: [String] = [], environment: [String: String] = [:], directory: URL) throws {
        self.executable = executable.resolvingSymlinksInPath().standardizedFileURL
        self.configuration = configuration
        self.arguments = arguments
        self.environment = environment
        self.directory = directory
        self.identity = directory.appendingPathComponent("managed-node.identity")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let attributes = try FileManager.default.attributesOfItem(atPath: directory.path)
        guard (attributes[.posixPermissions] as? NSNumber)?.uint16Value == 0o700,
              (attributes[.type] as? FileAttributeType) == .typeDirectory else { throw POSIXError(.EACCES) }
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

    private func birthTime(_ pid: pid_t) -> String? {
        var info = proc_bsdinfo()
        let size = Int32(MemoryLayout<proc_bsdinfo>.size)
        guard proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, size) == size else { return nil }
        return "\(info.pbi_start_tvsec):\(info.pbi_start_tvusec)"
    }

    private func previousNode() -> pid_t? {
        guard let data = try? Data(contentsOf: pidFile),
              let value = String(data: data, encoding: .utf8),
              let pid = pid_t(value.trimmingCharacters(in: .whitespacesAndNewlines)),
              matchingNode(pid),
              let birth = birthTime(pid),
              let stamp = try? String(contentsOf: identity, encoding: .utf8),
              stamp == "\(pid):\(birth)" else { return nil }
        return pid
    }

    public var state: NodeState {
        if let process {
            if process.isRunning { return .running }
            if terminationReasonFailed(process) { return .failed("Node exited with status \(process.terminationStatus)") }
        }
        if previousNode() != nil { return .alreadyRunning }
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
        child.environment = ProcessInfo.processInfo.environment.merging(environment) { _, override in override }
        child.standardInput = FileHandle.nullDevice
        let log = NodeIntegrations.logURL
        try FileManager.default.createDirectory(at: log.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let fd = open(log.path, O_WRONLY | O_CREAT | O_APPEND | O_NOFOLLOW | O_CLOEXEC, 0o600)
        guard fd >= 0 else { throw POSIXError(.EACCES) }
        let stream = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
        child.standardOutput = stream
        child.standardError = stream
        if let statusPath = environment["SECRETARY_NODE_STATUS_FILE"] { try? FileManager.default.removeItem(atPath: statusPath) }
        try child.run()
        process = child
        do {
            guard let birth = birthTime(child.processIdentifier) else { throw POSIXError(.ESRCH) }
            try Data(String(child.processIdentifier).utf8).write(to: pidFile, options: .atomic)
            try "\(child.processIdentifier):\(birth)".write(to: identity, atomically: true, encoding: .utf8)
        } catch {
            child.interrupt()
            child.waitUntilExit()
            process = nil
            try? FileManager.default.removeItem(at: pidFile)
            try? FileManager.default.removeItem(at: identity)
            throw error
        }
    }

    public func stop(completion: @escaping (Error?) -> Void) {
        let ownedProcess = process
        let ownedPID = ownedProcess?.isRunning == true ? ownedProcess?.processIdentifier : previousNode()
        guard let pid = ownedPID else {
            completion(nil)
            return
        }
        if ownedProcess == nil && previousNode() != pid {
            completion(NodeLifecycleError.alreadyRunning)
            return
        }
        guard kill(pid, SIGINT) == 0 else {
            completion(POSIXError(.EPERM))
            return
        }
        DispatchQueue.global(qos: .userInitiated).async {
            let deadline = Date().addingTimeInterval(15)
            while Date() < deadline {
                if let child = ownedProcess {
                    if !child.isRunning { break }
                } else if self.previousNode() != pid {
                    break
                }
                Thread.sleep(forTimeInterval: 0.1)
            }
            let stillRunning = ownedProcess?.isRunning ?? (self.previousNode() == pid)
            if stillRunning {
                DispatchQueue.main.async { completion(POSIXError(.ETIMEDOUT)) }
                return
            }
            try? FileManager.default.removeItem(at: self.pidFile)
            try? FileManager.default.removeItem(at: self.identity)
            DispatchQueue.main.async {
                self.process = nil
                completion(nil)
            }
        }
    }
}

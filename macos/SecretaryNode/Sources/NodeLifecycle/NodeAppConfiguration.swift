import Darwin
import Foundation

public struct NodeAppConfiguration {
    public let executable: URL
    public let configuration: URL
    public let environment: [String: String]
    public let serverHealthURL: URL

    public static func resolve(home: URL, inherited: [String: String] = ProcessInfo.processInfo.environment, files: FileManager = .default) throws -> NodeAppConfiguration {
        let standalone = home.appendingPathComponent(".local/share/secretary-node/current/bin/secretary-node")
        let selected = inherited["SECRETARY_NODE_EXECUTABLE"].map { URL(fileURLWithPath: $0) }
            ?? (files.isExecutableFile(atPath: standalone.path) ? standalone : home.appendingPathComponent(".local/bin/secretary-node"))
        let isStandalone = selected.standardizedFileURL == standalone.standardizedFileURL
        let config = inherited["SECRETARY_NODE_CONFIG"].map { URL(fileURLWithPath: $0) }
            ?? home.appendingPathComponent(isStandalone ? ".local/share/secretary-node/config.json" : ".local/share/secretary/node/config.json")
        guard files.isReadableFile(atPath: config.path),
              let data = try? Data(contentsOf: config),
              let payload = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let rawURL = payload["server_url"] as? String,
              let server = URL(string: rawURL),
              ["http", "https"].contains(server.scheme?.lowercased() ?? ""),
              server.host != nil,
              let dataDirectory = payload["data_dir"] as? String,
              dataDirectory.hasPrefix("/") else {
            throw NodeAppConfigurationError.invalidConfig
        }
        guard let health = URL(string: "/v1/health", relativeTo: server)?.absoluteURL else {
            throw NodeAppConfigurationError.invalidConfig
        }
        var environment = inherited
        for key in ["SECRETARY_BOOTSTRAP_TOKEN", "SECRETARY_NODE_ADMIN_TOKEN", "SECRETARY_TELEGRAM_BOT_TOKEN", "SECRETARY_TELEGRAM_SERVER_CREDENTIAL", "SECRETARY_CAPABILITY"] {
            environment.removeValue(forKey: key)
        }
        let identity = URL(fileURLWithPath: dataDirectory).appendingPathComponent("identity.json")
        if files.fileExists(atPath: identity.path) {
            environment.removeValue(forKey: "SECRETARY_NODE_PAIRING_TOKEN")
        } else if environment["SECRETARY_NODE_PAIRING_TOKEN", default: ""].isEmpty && !isStandalone {
            let credentials = home.appendingPathComponent(".local/share/secretary/environment")
            guard let contents = privateCredentials(at: credentials),
                  let token = pairingToken(in: contents) else {
                throw NodeAppConfigurationError.missingPairing
            }
            environment["SECRETARY_NODE_PAIRING_TOKEN"] = token
        }
        return NodeAppConfiguration(executable: selected, configuration: config, environment: environment, serverHealthURL: health)
    }

    public static func healthURL(home: URL, inherited: [String: String] = ProcessInfo.processInfo.environment, files: FileManager = .default) -> URL? {
        let standalone = home.appendingPathComponent(".local/share/secretary-node/current/bin/secretary-node")
        let selected = inherited["SECRETARY_NODE_EXECUTABLE"].map { URL(fileURLWithPath: $0) }
            ?? (files.isExecutableFile(atPath: standalone.path) ? standalone : home.appendingPathComponent(".local/bin/secretary-node"))
        let config = inherited["SECRETARY_NODE_CONFIG"].map { URL(fileURLWithPath: $0) }
            ?? home.appendingPathComponent(selected.standardizedFileURL == standalone.standardizedFileURL ? ".local/share/secretary-node/config.json" : ".local/share/secretary/node/config.json")
        guard let data = try? Data(contentsOf: config),
              let payload = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let address = payload["server_url"] as? String,
              let url = URL(string: address),
              ["http", "https"].contains(url.scheme?.lowercased() ?? ""),
              url.host != nil else { return nil }
        return URL(string: "/v1/health", relativeTo: url)?.absoluteURL
    }

    private static func privateCredentials(at url: URL) -> String? {
        let descriptor = open(url.path, O_RDONLY | O_NOFOLLOW | O_CLOEXEC)
        guard descriptor >= 0 else { return nil }
        defer { close(descriptor) }
        var metadata = stat()
        guard fstat(descriptor, &metadata) == 0,
              (metadata.st_mode & mode_t(S_IFMT)) == mode_t(S_IFREG),
              (metadata.st_mode & 0o777) == 0o600,
              metadata.st_uid == geteuid() else { return nil }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: false)
        guard let data = try? handle.readToEnd(), data.count <= 65536 else { return nil }
        return String(data: data, encoding: .utf8)
    }

    private static func pairingToken(in text: String) -> String? {
        let prefix = "export SECRETARY_NODE_PAIRING_TOKEN='"
        let candidates = text.components(separatedBy: .newlines).filter { $0.hasPrefix(prefix) && $0.hasSuffix("'") }
        guard candidates.count == 1 else { return nil }
        let value = String(candidates[0].dropFirst(prefix.count).dropLast())
        guard value.count == 64, value.unicodeScalars.allSatisfy({ CharacterSet(charactersIn: "0123456789abcdefABCDEF").contains($0) }) else { return nil }
        return value
    }
}

public enum NodeAppConfigurationError: Error, LocalizedError, Equatable {
    case invalidConfig
    case missingPairing

    public var errorDescription: String? {
        switch self {
        case .invalidConfig:
            return "Node configuration is missing or invalid. Run secretary setup, then reopen Secretary Node."
        case .missingPairing:
            return "Node enrollment is incomplete. Run secretary setup to provision local Node pairing, then reopen Secretary Node."
        }
    }
}

public enum ServerReachability: Equatable {
    case checking
    case online
    case offline
    case unavailable
}

public final class ServerHealthMonitor {
    public private(set) var state: ServerReachability = .checking
    private let session: URLSession
    private let url: URL
    private var pending = false

    public init(url: URL, session: URLSession = .shared) {
        self.url = url
        self.session = session
    }

    public func check(completion: @escaping (ServerReachability) -> Void) {
        guard !pending else { return }
        pending = true
        var request = URLRequest(url: url)
        request.timeoutInterval = 3
        session.dataTask(with: request) { [weak self] data, response, error in
            let payload = data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
            let healthy = error == nil && (response as? HTTPURLResponse)?.statusCode == 200
                && (payload?["status"] as? String) == "ok"
            DispatchQueue.main.async {
                guard let self else { return }
                self.pending = false
                self.state = healthy ? .online : .offline
                completion(self.state)
            }
        }.resume()
    }
}

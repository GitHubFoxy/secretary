import Foundation

public struct NodeLiveSnapshot: Decodable, Equatable {
    public struct Harness: Decodable, Equatable {
        public let kind: String
        public let status: String
    }

    public let server: String
    public let updatedAt: Date
    public let harnesses: [Harness]

    enum CodingKeys: String, CodingKey {
        case server, harnesses
        case updatedAt = "updated_at"
    }

    public static func load(from url: URL, now: Date = Date()) -> NodeLiveSnapshot? {
        guard let data = try? Data(contentsOf: url),
              let snapshot = try? JSONDecoder.nodeStatus.decode(NodeLiveSnapshot.self, from: data),
              now.timeIntervalSince(snapshot.updatedAt) >= 0,
              now.timeIntervalSince(snapshot.updatedAt) <= 35,
              ["connected", "reconnecting", "offline"].contains(snapshot.server) else { return nil }
        return snapshot
    }
}

private extension JSONDecoder {
    static var nodeStatus: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }
}

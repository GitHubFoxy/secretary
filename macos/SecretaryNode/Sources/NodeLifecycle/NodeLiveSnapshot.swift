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
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let text = try container.decode(String.self)
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = formatter.date(from: text) { return date }
            formatter.formatOptions = [.withInternetDateTime]
            guard let date = formatter.date(from: text) else { throw DecodingError.dataCorruptedError(in: container, debugDescription: "Invalid Node status timestamp") }
            return date
        }
        return decoder
    }
}

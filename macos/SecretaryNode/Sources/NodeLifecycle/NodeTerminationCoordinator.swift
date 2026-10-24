import Foundation

public final class NodeTerminationCoordinator {
    public enum State: Equatable {
        case idle
        case stopping
        case stopped
    }

    public private(set) var state: State = .idle
    private let stop: (@escaping (Error?) -> Void) -> Void

    public init(stop: @escaping (@escaping (Error?) -> Void) -> Void) {
        self.stop = stop
    }

    public func request(_ completion: @escaping (Error?) -> Void) {
        guard state == .idle else { return }
        state = .stopping
        stop { [self] error in
            state = error == nil ? .stopped : .idle
            completion(error)
        }
    }
}

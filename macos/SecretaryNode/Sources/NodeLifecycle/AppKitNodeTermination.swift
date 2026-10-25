import AppKit

public final class AppKitNodeTermination {
    private let coordinator: NodeTerminationCoordinator
    private let begin: () -> Void
    private let failed: (Error) -> Void
    private let reply: (Bool) -> Void
    private let terminate: () -> Void
    private let dispatch: (@escaping () -> Void) -> Void
    private var awaitingAppKitTermination = false

    public init(
        stop: @escaping (@escaping (Error?) -> Void) -> Void,
        begin: @escaping () -> Void,
        failed: @escaping (Error) -> Void,
        reply: @escaping (Bool) -> Void = { NSApp.reply(toApplicationShouldTerminate: $0) },
        terminate: @escaping () -> Void = { NSApp.terminate(nil) },
        dispatch: @escaping (@escaping () -> Void) -> Void = { DispatchQueue.main.async(execute: $0) }
    ) {
        coordinator = NodeTerminationCoordinator(stop: stop)
        self.begin = begin
        self.failed = failed
        self.reply = reply
        self.terminate = terminate
        self.dispatch = dispatch
    }

    public func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if coordinator.state == .stopped { return .terminateNow }
        awaitingAppKitTermination = true
        requestShutdown()
        return .terminateLater
    }

    public func menuQuit() {
        requestShutdown()
    }

    private func requestShutdown() {
        guard coordinator.state == .idle else { return }
        begin()
        coordinator.request { [self] error in
            dispatch { [self] in
                if let error {
                    if awaitingAppKitTermination {
                        awaitingAppKitTermination = false
                        reply(false)
                    }
                    failed(error)
                } else if awaitingAppKitTermination {
                    awaitingAppKitTermination = false
                    reply(true)
                } else {
                    terminate()
                }
            }
        }
    }
}

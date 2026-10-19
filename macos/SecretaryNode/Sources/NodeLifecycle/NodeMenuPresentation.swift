import AppKit

public final class NodeMenuPresentation {
    public let menu = NSMenu()
    public let status = NSMenuItem(title: "Node: Starting", action: nil, keyEquivalent: "")
    public let quit = NSMenuItem(title: "Quit Secretary Node", action: nil, keyEquivalent: "q")
    public private(set) var launchError: String?
    public private(set) var stopError: String?

    public init() {
        menu.autoenablesItems = false
        status.isEnabled = false
        menu.addItem(status)
        menu.addItem(.separator())
        menu.addItem(quit)
    }

    public func startupFailed(_ error: Error) {
        launchError = error.localizedDescription
    }

    public func recovered() {
        launchError = nil
    }

    public func beginQuit() {
        stopError = nil
        quit.isEnabled = false
    }

    public func stopFailed(_ error: Error) {
        stopError = error.localizedDescription
        quit.isEnabled = true
    }

    public func render(state: NodeState?) {
        if let stopError {
            status.title = "Node: Stop failed"
            status.toolTip = stopError
        } else if let launchError {
            status.title = "Node: Start failed"
            status.toolTip = launchError
        } else {
            switch state {
            case .running:
                status.title = "Node: Running"
                status.toolTip = "Secretary Node running"
            case .alreadyRunning:
                status.title = "Node: Running (recovered)"
                status.toolTip = "Previously managed Secretary Node detected"
            case .stopped:
                status.title = "Node: Stopped"
                status.toolTip = "Secretary Node stopped"
            case .failed(let message):
                status.title = "Node: \(message)"
                status.toolTip = message
            case nil:
                status.title = "Node: Unavailable"
                status.toolTip = "Secretary Node controller unavailable"
            }
        }
    }
}

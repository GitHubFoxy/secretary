import AppKit
import NodeLifecycle

final class SecretaryNodeApp: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private var controller: NodeLifecycle?
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let menu = NSMenu()
    private let status = NSMenuItem(title: "Node: Starting", action: nil, keyEquivalent: "")
    private let quit = NSMenuItem(title: "Quit Secretary Node", action: #selector(quitNode), keyEquivalent: "q")
    private var timer: Timer?
    private var launchError: String?
    private var stopError: String?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem.button?.image = NSImage(systemSymbolName: "circle.hexagongrid.fill", accessibilityDescription: "Secretary Node")
        statusItem.button?.image?.isTemplate = true
        menu.autoenablesItems = false
        status.isEnabled = false
        menu.addItem(status)
        menu.addItem(.separator())
        quit.target = self
        menu.addItem(quit)
        menu.delegate = self
        statusItem.menu = menu
        do {
            let home = FileManager.default.homeDirectoryForCurrentUser
            let preferred = ProcessInfo.processInfo.environment["SECRETARY_NODE_EXECUTABLE"]
            let executable = preferred.map { URL(fileURLWithPath: $0) } ?? home.appendingPathComponent(".local/bin/secretary-node")
            let config = ProcessInfo.processInfo.environment["SECRETARY_NODE_CONFIG"].map { URL(fileURLWithPath: $0) }
            controller = try NodeLifecycle(executable: executable, configuration: config, directory: home.appendingPathComponent("Library/Application Support/SecretaryNode"))
            do {
                try controller?.start()
            } catch NodeLifecycleError.alreadyRunning {
                launchError = nil
            }
        } catch NodeLifecycleError.alreadyOpen {
            NSApp.terminate(nil)
            return
        } catch {
            launchError = error.localizedDescription
        }
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in self?.refresh() }
    }

    func menuWillOpen(_ menu: NSMenu) { refresh() }

    private func refresh() {
        if let stopError {
            status.title = "Node: Stop failed"
            statusItem.button?.toolTip = stopError
            return
        }
        if let launchError {
            status.title = "Node: Start failed"
            statusItem.button?.toolTip = launchError
            return
        }
        guard let controller else {
            status.title = "Node: Unavailable"
            statusItem.button?.toolTip = "Secretary Node controller unavailable"
            return
        }
        switch controller.state {
        case .running:
            status.title = "Node: Running"
            statusItem.button?.toolTip = "Secretary Node running"
        case .alreadyRunning:
            status.title = "Node: Running (recovered)"
            statusItem.button?.toolTip = "Previously managed Secretary Node detected"
        case .stopped:
            status.title = "Node: Stopped"
            statusItem.button?.toolTip = "Secretary Node stopped"
        case .failed(let message):
            status.title = "Node: \(message)"
            statusItem.button?.toolTip = message
        }
    }

    @objc private func quitNode() {
        stopError = nil
        quit.isEnabled = false
        timer?.invalidate()
        controller?.stop { [weak self] error in
            DispatchQueue.main.async {
                if let error {
                    self?.stopError = error.localizedDescription
                    self?.refresh()
                    self?.quit.isEnabled = true
                    self?.timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in self?.refresh() }
                    return
                }
                NSApp.terminate(nil)
            }
        }
        if controller == nil { NSApp.terminate(nil) }
    }
}

let application = NSApplication.shared
let delegate = SecretaryNodeApp()
application.delegate = delegate
application.run()

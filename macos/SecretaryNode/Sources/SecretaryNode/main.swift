import AppKit
import NodeLifecycle

final class SecretaryNodeApp: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private var controller: NodeLifecycle?
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let menu = NSMenu()
    private let status = NSMenuItem(title: "Node: Starting", action: nil, keyEquivalent: "")
    private let quit = NSMenuItem(title: "Quit Secretary Node", action: #selector(quitNode), keyEquivalent: "q")
    private var timer: Timer?

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
            try controller?.start()
        } catch NodeLifecycleError.alreadyOpen {
            NSApp.terminate(nil)
            return
        } catch {
            status.title = "Node: \(error.localizedDescription)"
        }
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in self?.refresh() }
    }

    func menuWillOpen(_ menu: NSMenu) { refresh() }

    private func refresh() {
        guard let controller else { return }
        switch controller.state {
        case .running:
            status.title = "Node: Running"
            statusItem.button?.toolTip = "Secretary Node running"
        case .alreadyRunning:
            status.title = "Node: Running (not managed here)"
            statusItem.button?.toolTip = "An existing Node was detected"
        case .stopped:
            status.title = "Node: Stopped"
            statusItem.button?.toolTip = "Secretary Node stopped"
        case .failed(let message):
            status.title = "Node: \(message)"
            statusItem.button?.toolTip = message
        }
    }

    @objc private func quitNode() {
        quit.isEnabled = false
        timer?.invalidate()
        controller?.stop { [weak self] error in
            DispatchQueue.main.async {
                if let error {
                    self?.status.title = "Node: Stop failed (\(error.localizedDescription))"
                    self?.statusItem.button?.toolTip = error.localizedDescription
                    self?.quit.isEnabled = true
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

import AppKit
import NodeLifecycle

final class SecretaryNodeApp: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private var controller: NodeLifecycle?
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let presentation = NodeMenuPresentation()
    private var timer: Timer?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem.button?.image = NSImage(systemSymbolName: "circle.hexagongrid.fill", accessibilityDescription: "Secretary Node")
        statusItem.button?.image?.isTemplate = true
        presentation.quit.target = self
        presentation.quit.action = #selector(quitNode)
        presentation.menu.delegate = self
        statusItem.menu = presentation.menu
        do {
            let home = FileManager.default.homeDirectoryForCurrentUser
            let preferred = ProcessInfo.processInfo.environment["SECRETARY_NODE_EXECUTABLE"]
            let executable = preferred.map { URL(fileURLWithPath: $0) } ?? home.appendingPathComponent(".local/bin/secretary-node")
            let config = ProcessInfo.processInfo.environment["SECRETARY_NODE_CONFIG"].map { URL(fileURLWithPath: $0) }
            controller = try NodeLifecycle(executable: executable, configuration: config, directory: home.appendingPathComponent("Library/Application Support/SecretaryNode"))
            do {
                try controller?.start()
            } catch NodeLifecycleError.alreadyRunning {
                presentation.recovered()
            }
        } catch NodeLifecycleError.alreadyOpen {
            NSApp.terminate(nil)
            return
        } catch {
            presentation.startupFailed(error)
        }
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in self?.refresh() }
    }

    func menuWillOpen(_ menu: NSMenu) { refresh() }

    private func refresh() {
        presentation.render(state: controller?.state)
        statusItem.button?.toolTip = presentation.status.toolTip
    }

    @objc private func quitNode() {
        presentation.beginQuit()
        timer?.invalidate()
        guard let controller else {
            NSApp.terminate(nil)
            return
        }
        controller.stop { [weak self] error in
            DispatchQueue.main.async {
                if let error {
                    self?.presentation.stopFailed(error)
                    self?.refresh()
                    self?.timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in self?.refresh() }
                } else {
                    NSApp.terminate(nil)
                }
            }
        }
    }
}

let application = NSApplication.shared
let delegate = SecretaryNodeApp()
application.delegate = delegate
application.run()

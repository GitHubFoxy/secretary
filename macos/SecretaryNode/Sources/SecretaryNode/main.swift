import AppKit
import NodeLifecycle

final class SecretaryNodeApp: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private var controller: NodeLifecycle?
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let presentation = NodeMenuPresentation()
    private var timer: Timer?
    private let liveFile = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support/SecretaryNode/live-status.json")

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem.button?.image = NSImage(systemSymbolName: "circle.hexagongrid.fill", accessibilityDescription: "Secretary Node")
        statusItem.button?.image?.isTemplate = true
        presentation.quit.target = self
        presentation.quit.action = #selector(quitNode)
        presentation.launchAtLogin.target = self
        presentation.launchAtLogin.action = #selector(toggleLogin)
        presentation.openLogs.target = self
        presentation.openLogs.action = #selector(openLogs)
        presentation.menu.delegate = self
        statusItem.menu = presentation.menu
        do {
            let home = FileManager.default.homeDirectoryForCurrentUser
            let preferred = ProcessInfo.processInfo.environment["SECRETARY_NODE_EXECUTABLE"]
            let executable = preferred.map { URL(fileURLWithPath: $0) } ?? home.appendingPathComponent(".local/bin/secretary-node")
            let config = ProcessInfo.processInfo.environment["SECRETARY_NODE_CONFIG"].map { URL(fileURLWithPath: $0) }
                ?? home.appendingPathComponent(".local/share/secretary/node/config.json")
            controller = try NodeLifecycle(executable: executable, configuration: config, environment: ["SECRETARY_NODE_STATUS_FILE": liveFile.path], directory: home.appendingPathComponent("Library/Application Support/SecretaryNode"))
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

    func menuWillOpen(_ menu: NSMenu) {
        refresh()
        presentation.updateLogin(NodeIntegrations.launchAtLogin)
    }

    private func showError(_ title: String, _ error: Error) {
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = error.localizedDescription
        alert.alertStyle = .warning
        alert.runModal()
    }

    @objc private func toggleLogin() {
        do {
            try NodeIntegrations.setLaunchAtLogin(!NodeIntegrations.launchAtLogin)
        } catch {
            showError("Could not update Launch at Login", error)
        }
        presentation.updateLogin(NodeIntegrations.launchAtLogin)
    }

    @objc private func openLogs() {
        do {
            try NodeIntegrations.openLogs()
        } catch {
            showError("Could not open Node logs", error)
        }
    }

    private func refresh() {
        let state = controller?.state
        presentation.render(state: state)
        let running = state == .running || state == .alreadyRunning
        presentation.renderLive(running ? NodeLiveSnapshot.load(from: liveFile) : nil)
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

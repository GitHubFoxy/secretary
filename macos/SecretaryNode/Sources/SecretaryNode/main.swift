import AppKit
import NodeLifecycle

final class SecretaryNodeApp: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private var controller: NodeLifecycle?
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let presentation = NodeMenuPresentation()
    private var timer: Timer?
    private var healthMonitor: ServerHealthMonitor?
    private var lastHealthCheck = Date.distantPast
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
        let home = FileManager.default.homeDirectoryForCurrentUser
        if let healthURL = NodeAppConfiguration.healthURL(home: home) {
            healthMonitor = ServerHealthMonitor(url: healthURL)
        }
        do {
            let settings = try NodeAppConfiguration.resolve(home: home)
            controller = try NodeLifecycle(executable: settings.executable, configuration: settings.configuration, environment: settings.environment.merging(["SECRETARY_NODE_STATUS_FILE": liveFile.path]) { _, new in new }, directory: home.appendingPathComponent("Library/Application Support/SecretaryNode"))
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
        presentation.updateLogin(NodeIntegrations.loginStatus)
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
            if let message = NodeIntegrations.loginDescription(NodeIntegrations.loginStatus) {
                showError("Launch at Login needs attention", NSError(domain: "SecretaryNode", code: 1, userInfo: [NSLocalizedDescriptionKey: message]))
            }
        } catch {
            showError("Could not update Launch at Login", error)
        }
        presentation.updateLogin(NodeIntegrations.loginStatus)
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
        if let healthMonitor {
            presentation.renderServer(healthMonitor.state)
            if Date().timeIntervalSince(lastHealthCheck) >= 5 {
                lastHealthCheck = Date()
                healthMonitor.check { [weak self] state in
                    self?.presentation.renderServer(state)
                }
            }
        } else {
            presentation.renderServer(.checking)
        }
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

if CommandLine.arguments.contains("--login-item-smoke") {
    let action = CommandLine.arguments.last
    do {
        switch action {
        case "status":
            switch NodeIntegrations.loginStatus {
            case .enabled: print("enabled")
            case .notRegistered: print("notRegistered")
            case .requiresApproval: print("requiresApproval")
            case .notFound: print("notFound")
            @unknown default: print("unknown")
            }
        case "enable":
            try NodeIntegrations.setLaunchAtLogin(true)
            guard NodeIntegrations.loginStatus == .enabled else {
                throw NSError(domain: "SecretaryNode.LoginSmoke", code: 1, userInfo: [
                    NSLocalizedDescriptionKey: NodeIntegrations.loginDescription(NodeIntegrations.loginStatus) ?? "Login item registration did not become enabled"
                ])
            }
        case "verify-enabled":
            guard NodeIntegrations.loginStatus == .enabled else {
                throw NSError(domain: "SecretaryNode.LoginSmoke", code: 2, userInfo: [
                    NSLocalizedDescriptionKey: "Login item was not preserved across process restart or app upgrade"
                ])
            }
        case "disable":
            try NodeIntegrations.setLaunchAtLogin(false)
            guard NodeIntegrations.loginStatus == .notRegistered || NodeIntegrations.loginStatus == .notFound else {
                throw NSError(domain: "SecretaryNode.LoginSmoke", code: 3, userInfo: [
                    NSLocalizedDescriptionKey: "Login item did not unregister"
                ])
            }
        case "verify-disabled":
            guard NodeIntegrations.loginStatus == .notRegistered || NodeIntegrations.loginStatus == .notFound else {
                throw NSError(domain: "SecretaryNode.LoginSmoke", code: 4, userInfo: [
                    NSLocalizedDescriptionKey: "Login item remained registered after process restart"
                ])
            }
        default:
            throw NSError(domain: "SecretaryNode.LoginSmoke", code: 5, userInfo: [
                NSLocalizedDescriptionKey: "Unsupported login smoke action"
            ])
        }
    } catch {
        fputs("Login item smoke failed: \(error.localizedDescription)\n", stderr)
        exit(1)
    }
} else {
    let application = NSApplication.shared
    let delegate = SecretaryNodeApp()
    application.delegate = delegate
    application.run()
}

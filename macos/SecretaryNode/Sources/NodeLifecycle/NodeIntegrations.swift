import AppKit
import Foundation
import ServiceManagement

public enum NodeIntegrations {
    public static var logURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs/SecretaryNode/node.log")
    }

    public static var loginStatus: SMAppService.Status { SMAppService.mainApp.status }

    public static var launchAtLogin: Bool { loginStatus == .enabled }

    public static func loginDescription(_ status: SMAppService.Status) -> String? {
        switch status {
        case .enabled, .notRegistered: return nil
        case .requiresApproval: return "Allow Secretary Node in System Settings → General → Login Items."
        case .notFound: return "Install Secretary Node.app in ~/Applications and launch it from there."
        @unknown default: return "Check Secretary Node in System Settings → General → Login Items."
        }
    }

    public static func setLaunchAtLogin(_ enabled: Bool) throws {
        let status = loginStatus
        if enabled {
            if status != .enabled && status != .requiresApproval { try SMAppService.mainApp.register() }
        } else if status != .notRegistered && status != .notFound {
            try SMAppService.mainApp.unregister()
        }
    }

    public static func openLogs() throws {
        guard FileManager.default.fileExists(atPath: logURL.path) else {
            throw CocoaError(.fileReadNoSuchFile, userInfo: [
                NSLocalizedDescriptionKey: "Node log does not exist yet. Start the Node, then retry. Expected: \(logURL.path)"
            ])
        }
        guard FileManager.default.isReadableFile(atPath: logURL.path) else {
            throw CocoaError(.fileReadNoPermission, userInfo: [
                NSLocalizedDescriptionKey: "Node log exists but is not readable: \(logURL.path). Check file permissions in Finder."
            ])
        }
        guard NSWorkspace.shared.open(logURL) else {
            throw CocoaError(.fileReadUnknown, userInfo: [
                NSLocalizedDescriptionKey: "Could not open \(logURL.path). Open this path in Finder."
            ])
        }
    }
}

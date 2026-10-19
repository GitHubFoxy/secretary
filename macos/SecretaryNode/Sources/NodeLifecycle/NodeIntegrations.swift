import AppKit
import Foundation
import ServiceManagement

public enum NodeIntegrations {
    public static var logURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs/SecretaryNode/node.log")
    }

    public static var launchAtLogin: Bool {
        SMAppService.mainApp.status == .enabled
    }

    public static func setLaunchAtLogin(_ enabled: Bool) throws {
        if enabled {
            if SMAppService.mainApp.status != .enabled { try SMAppService.mainApp.register() }
        } else if SMAppService.mainApp.status == .enabled {
            try SMAppService.mainApp.unregister()
        }
    }

    public static func openLogs() throws {
        guard FileManager.default.isReadableFile(atPath: logURL.path) else {
            throw CocoaError(.fileReadNoSuchFile, userInfo: [
                NSLocalizedDescriptionKey: "Node log does not exist yet. Start the Node, then retry. Expected: \(logURL.path)"
            ])
        }
        guard NSWorkspace.shared.open(logURL) else {
            throw CocoaError(.fileReadUnknown, userInfo: [
                NSLocalizedDescriptionKey: "Could not open \(logURL.path). Open this path in Finder."
            ])
        }
    }
}

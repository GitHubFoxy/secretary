# Secretary Node menu bar companion

Build on macOS 13 or later with Xcode command-line tools:

```sh
cd macos/SecretaryNode
swift test
swift build -c release
mkdir -p "$HOME/Applications/Secretary Node.app/Contents/MacOS"
cp .build/release/SecretaryNode "$HOME/Applications/Secretary Node.app/Contents/MacOS/"
cp Info.plist "$HOME/Applications/Secretary Node.app/Contents/"
open "$HOME/Applications/Secretary Node.app"
```

The native agent app uses an accessory activation policy and `LSUIElement`, so it does not show a Dock icon. macOS owns its native NSMenu appearance and responds to system light/dark changes automatically. The standard menu rows intentionally avoid extra padding.

Set `SECRETARY_NODE_EXECUTABLE` to the absolute path of a built `secretary-node` before launching the app, or install the Node binary to `~/.local/bin/secretary-node`. If needed, set `SECRETARY_NODE_CONFIG` to the absolute path of the existing Node deployment configuration. Node pairing and configuration are performed by existing Secretary CLI commands. This companion records the Node PID and process start time in a private application directory. It can gracefully stop a verified Node that it started, including after a companion crash. It refuses to act on an unrelated process with a reused PID. A Node started outside the companion is not adopted or terminated. It does not manage Secretary Server, other Nodes, installation, login items or logs. The companion passes a private status-file path to the Node it launches. The Node publishes its observed WebSocket connection state and probed harness inventory; the menu reloads it every second. Missing or older-than-35-second data is treated as Offline, and harnesses never appear Ready while the Server transport is disconnected.

Test on macOS with `swift test`, then verify Spotlight launch, menu bar-only presentation, active theme switching, second launch, Node shutdown and restart. Installer and login-item integration are separate follow-up work.

For T02 integration tests: `go test ./internal/node ./cmd/secretary-node` and `cd macos/SecretaryNode && swift test`. For manual acceptance, connect to a real Server, interrupt and restore network access, add or remove a supported harness and wait for Node discovery, stop the Node, and switch macOS between light and dark appearances. Verify that Ready is never shown while the Server is disconnected and that the separator is directly below Server.

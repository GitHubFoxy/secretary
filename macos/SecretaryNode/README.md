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

Set `SECRETARY_NODE_EXECUTABLE` to the absolute path of a built `secretary-node` before launching the app, or install the Node binary to `~/.local/bin/secretary-node`. If needed, set `SECRETARY_NODE_CONFIG` to the absolute path of the existing Node deployment configuration. Node pairing and configuration are performed by existing Secretary CLI commands. This companion records the Node PID and process start time in a private application directory. It can gracefully stop a verified Node that it started, including after a companion crash. It refuses to act on an unrelated process with a reused PID. A Node started outside the companion is not adopted or terminated. It does not manage Secretary Server, other Nodes, installation, login items, logs or live transport status.

Test on macOS with `swift test`, then verify Spotlight launch, menu bar-only presentation, active theme switching, second launch, Node shutdown and restart. Installer and login-item integration are separate follow-up work.

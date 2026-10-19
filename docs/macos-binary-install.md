# Versioned macOS binary installation

macOS releases contain a matching Server, CLI, MCP, migration binary, co-located Execution node and native Secretary Node.app. The package manifest is a JSON object with schema version 1, Secretary version, exact 40-character source revision, operating system, architecture, archive basename, SHA-256 digest and size in bytes. A release tag `vVERSION` contains the pair `secretary-vVERSION-darwin-ARCH.json` and `secretary-vVERSION-darwin-ARCH.tar.gz`. Build artifacts are produced on GitHub macOS runners rather than the installation machine.

## Install

For a fresh macOS user with an already installed and authorized native Codex or Claude Code harness:

```sh
curl -fsSL https://raw.githubusercontent.com/GitHubFoxy/secretary/main/scripts/install-macos-binary.sh | bash
```

The installer fetches the latest published tagged version, verifies its manifest, platform, size and SHA-256, verifies the Node.app code signature, and stages it in `~/.local/share/secretary/releases`. The exact downloaded manifest is retained as `RELEASE-MANIFEST.json` inside the corresponding release directory. Supply `SECRETARY_VERSION=1.2.3` to select a published version explicitly. Release binaries are linked from `~/.local/bin`; put that directory on your PATH. The native application is installed in `~/Applications/Secretary Node.app` and can be opened from Spotlight. Launch at Login is controlled from the menu bar application.

The installer does not install Codex, Claude Code, their credentials or ACP. An authorized, already working native harness must be available. If you choose Codex, the existing setup implementation may require the independently provided native ACP adapter. The installer does not provide FFmpeg. An existing source-managed `~/.local/bin/secretary` is deliberately not converted into binary release management.

After installation, use `secretary doctor`, `secretary start --no-open`, `secretary status`, `secretary logs`, and `secretary stop`. Launch **Secretary Node** separately from Spotlight; starting the Secretary Server does not automatically start the Node. Use the existing Web bootstrap-code pairing to open the local Web client. Node pairing remains owner-controlled and preserves an existing identity.

The installer records `VERSION` and `REVISION` alongside the installed binaries. The macOS application bundle is ad-hoc signed by the CI packaging job; it is **not** a Developer ID/notarized distribution. macOS Gatekeeper may refuse an internet-downloaded application. Do not disable Gatekeeper or remove quarantine attributes automatically. A notarized, Developer ID-signed release is needed for frictionless distribution outside an explicitly user-approved local trust decision.

Re-running an identical version leaves existing data under `~/.local/share/secretary` intact, including SQLite, profiles, environment credentials, Node configuration, pairing identity and native session data. Release directories are version-pinned; the active Server/Node/CLI binary symlinks resolve through one atomic `current` pointer. The previously installed Node.app and pointer are restored when a synchronous activation step fails. For upgrades of active processes, explicitly stop and restart the Server and menu-managed Node; an already running process does not automatically adopt the new binary. Use `secretary doctor` and compare the running processes before declaring a production upgrade complete. The installer does not itself stop running Workers.

To continue installing and updating from source, use the separate `scripts/install.sh` route documented in [terminal installation](terminal-install.md). Binary releases do not invoke source builds or `secretary update`.

## Release CI and manual smoke

The `macOS binary Secretary release` workflow builds the Go binaries and Swift app on a macOS hosted runner, creates the manifest/archive, verifies the release contract fixtures, and exercises an initial install, repeat install, deliberately failed activation, tampered archive rejection, and upgrade to a second version in a temporary home with a fake authorized native harness. It checks that the installed CLI can start/stop the Server and access its Web session endpoint, and verifies the running Server revision. The fixture is not evidence of real native authorization or desktop acceptance. Tag builds attach archive and manifest to a GitHub release.

Manual acceptance on a clean supported macOS account:

1. Install the latest published version without Go, npm or Swift build tools; confirm the local CLI can perform doctor, start, status, logs, stop and Web bootstrap pairing.
2. Open Secretary Node from Spotlight; verify actual Server connectivity and an authorized preinstalled native harness, and run a local Worker Task to Result.
3. Confirm Quit stops only the managed Node; Launch at Login survives restart and the system light/dark presentation follows macOS.
4. Install the same version again and upgrade to a newer published version. Compare SQLite, profile files, credentials, Node identity and an existing native Worker session. Verify both processes expose their actual running release after restart.
5. Deliberately test a damaged artifact, an incompatible platform, an unavailable artifact and a failed download: previous installation must remain usable. Verify behavior under normal Gatekeeper settings.

The public v1 multi-platform/harness acceptance remains governed by #89.

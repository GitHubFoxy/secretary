# Versioned macOS binary installation

macOS releases contain a matching Server, CLI, MCP, migration binary, co-located Execution node and native Secretary Node.app. The package manifest is a JSON object with schema version 1, Secretary version, exact 40-character source revision, operating system, architecture, archive basename, SHA-256 digest and size in bytes. A release tag `vVERSION` contains the pair `secretary-vVERSION-darwin-ARCH.json` and `secretary-vVERSION-darwin-ARCH.tar.gz`. Build artifacts are produced on GitHub macOS runners rather than the installation machine.

Supported architecture is **arm64 (Apple Silicon)** only. macOS on Intel (x86_64) is out of scope and no `darwin/amd64` artifact is published; the unified installer fails closed on that host. See [ADR-0006](adr/0006-supported-binary-platform-matrix.md) for the full distribution matrix.

## Install

For macOS users, run the single, prerelease-aware installer (with a working compatible native harness when required):

```sh
curl -fsSL --retry-all-errors --retry 5 --retry-delay 2 https://raw.githubusercontent.com/GitHubFoxy/secretary/main/scripts/install.sh | bash
```

The unified `scripts/install.sh` chooses the newest **complete** published release for the host, **including prereleases without any `SECRETARY_VERSION`**. The [v0.1.0-alpha.6](https://github.com/GitHubFoxy/secretary/releases/tag/v0.1.0-alpha.6) release contains a macOS arm64 binary and has passed public live installation checks in both a clean HOME and an existing source-managed HOME. A macOS Intel release archive is **not** published for alpha.6; recognizing Intel in the dispatcher does not make an Intel binary available. For an explicit version, export `SECRETARY_VERSION=0.1.0-alpha.6` before running the unified command.

The platform-specific `scripts/install-macos-binary.sh` remains available for advanced/pinned installs, but **its no-version fallback uses GitHub `releases/latest`**, which excludes prereleases. If using that script directly with an alpha/beta/RC release, explicitly set `SECRETARY_VERSION`.

The binary installer verifies the versioned manifest, platform, size, SHA-256 and Node.app signature, then stages files in `~/.local/share/secretary/releases`. It retains the downloaded manifest as `RELEASE-MANIFEST.json` inside the installed release directory. Published prerelease tags use GitHub's prerelease status rather than stable-release status. Release binaries are linked from `~/.local/bin`; put that directory on your PATH. The native application is installed in `~/Applications/Secretary Node.app` and can be opened from Spotlight. Launch at Login is controlled from the menu bar application.

The installer does not install Codex, Claude Code, their credentials or ACP. An authorized, already working native harness must be available. If you choose Codex, the existing setup implementation may require the independently provided native ACP adapter. The installer does not provide FFmpeg. An existing source-managed `~/.local/bin/secretary` is deliberately not converted into binary release management.

When any public `~/.local/bin/secretary*` executable belongs to an existing source installation, the release is installed **side-by-side** with its own links in `~/.local/share/secretary/bin`. Source-owned files and symlinks are not overwritten. The installer prints the release-only status and start commands; for example:

```sh
SECRETARY_BINARY_STAGE_BIN="$HOME/.local/share/secretary/bin" \
  "$HOME/.local/share/secretary/current/secretary" status
SECRETARY_BINARY_STAGE_BIN="$HOME/.local/share/secretary/bin" \
  "$HOME/.local/share/secretary/current/secretary" start --no-open
```

The public `secretary` command can still point to the original source checkout; use the explicit release CLI when a collision is reported. This preserves the old **executables**, but does not isolate the existing Secretary state/configuration under `~/.local/share/secretary`; do not run source and release daemons concurrently against the same SQLite database or TCP ports. If a source-managed CLI collision also coincides with an existing `~/Applications/Secretary Node.app`, the installer keeps that app untouched and installs the signed release app under `~/Applications/Secretary Node (Release).app` instead; use the explicitly printed `open` command rather than Spotlight to avoid ambiguity between duplicate bundle identifiers. A pre-existing source-managed CLI Node LaunchAgent is preserved and reported as a warning, not removed or stopped. The owner must stop the old source-managed Node before opening the menu-managed release Node to prevent competing processes.

After installation, use `secretary doctor`, `secretary start --no-open`, `secretary status`, `secretary logs`, and `secretary stop`. Launch **Secretary Node** separately from Spotlight; starting the Secretary Server does not automatically start the Node. Use the existing Web bootstrap-code pairing to open the local Web client. Node pairing remains owner-controlled and preserves an existing identity.

The installer records `VERSION` and `REVISION` alongside the installed binaries. The macOS application bundle is ad-hoc signed by the CI packaging job; it is **not** a Developer ID/notarized distribution. macOS Gatekeeper may refuse an internet-downloaded application. Do not disable Gatekeeper or remove quarantine attributes automatically. A notarized, Developer ID-signed release is needed for frictionless distribution outside an explicitly user-approved local trust decision.

Re-running an identical version leaves existing data under `~/.local/share/secretary` intact, including SQLite, profiles, environment credentials, Node configuration, pairing identity and native session data. Release directories are version-pinned. All installer-owned public links in `~/.local/bin` (or, on a genuine source-managed collision, private links in `~/.local/share/secretary/bin`) target the `current` release symlink, not an individual immutable release directory. Existing **verified** pre-fix macOS links pinned directly to an installed versioned release are migrated to `current` on the next install, including a same-version reinstall after a stale-link upgrade. Foreign real files, source-managed links and unrecognized/modified release links are never rewritten; any public collision keeps all public executables unchanged and uses the private bin. Link migrations are backed up and rolled back if release pointer activation fails. Check `readlink "$HOME/.local/bin/secretary"` and `secretary status` without `SECRETARY_BINARY_STAGE_BIN` after a normal public-bin upgrade.

**Running processes on macOS are NOT automatically restarted by an upgrade.** Unlike Linux's installer-owned systemd services, a Server may have been launched directly, by a source-managed environment, or via launchd; automatically terminating it could interrupt Workers or an unrelated source installation. After a successful on-disk upgrade, finish active work, then explicitly stop/restart the **managed release** Server using its matching CLI. If using release-owned launchd service mode, use `secretary restart`; for a directly launched Server, use `secretary stop` followed by `secretary start --no-open`. Quit and relaunch the menu-managed Secretary Node app separately. `secretary start` alone is **not** an upgrade restart when a Server is already running. The installer prints a warning when a release pointer is updated; `secretary status` reports installed metadata and is not proof that a running process has adopted the new code. Never restart a source-managed Server/Node just because a release was installed side-by-side; verify process ownership and running revisions. The previously installed Node.app, pointer and changed managed links are restored when synchronous activation fails; the installer does not itself stop running Workers.

To continue installing and updating from source, use the separate `scripts/install-from-source.sh` route documented in [terminal installation](terminal-install.md). Binary releases do not invoke source builds or `secretary update`.

## Release CI and manual smoke

The `macOS binary Secretary release` workflow builds the Go binaries and Swift app on a macOS hosted runner, creates the manifest/archive, verifies the release contract fixtures, and exercises an initial install, repeat install, deliberately failed activation, tampered archive rejection, and upgrade to a second version in a temporary home with a fake authorized native harness. It checks that the installed CLI can start/stop the Server and access its Web session endpoint, and verifies the running Server revision. The fixture is not evidence of real native authorization or desktop acceptance. Tag builds attach archive and manifest to a GitHub release.

Manual acceptance on a clean supported macOS account:

1. Install the latest published version without Go, npm or Swift build tools; confirm the local CLI can perform doctor, start, status, logs, stop and Web bootstrap pairing.
2. Open Secretary Node from Spotlight; verify actual Server connectivity and an authorized preinstalled native harness, and run a local Worker Task to Result.
3. Confirm Quit stops only the managed Node; Launch at Login survives restart and the system light/dark presentation follows macOS.
4. Install the same version again and upgrade to a newer published version. Compare SQLite, profile files, credentials, Node identity and an existing native Worker session. Verify both processes expose their actual running release after restart.
5. Deliberately test a damaged artifact, an incompatible platform, an unavailable artifact and a failed download: previous installation must remain usable. Verify behavior under normal Gatekeeper settings.

The public v1 multi-platform/harness acceptance remains governed by #89.

## Server FFmpeg / FFprobe

The combined Server release includes a matching FFmpeg/FFprobe pair at `bin/video-tools/`. `VIDEO-TOOLS.json` records the build-host distribution origin, exact tool version and independent SHA-256 digests. Both files are checked before release activation and again in the Server conversion path. The tools are never installed into a global directory and remote Execution nodes do not resolve them.

The release CI fetches the pinned `eugeneware/ffmpeg-static` `b6.1.1` release archives for supported macOS/Linux amd64/arm64 hosts, verifies the published SHA-256 before extraction, and rejects a pair without the `libwebp` video encoder. The packaged `VIDEO-TOOLS.json` records the exact binary hashes, release origin, version, and required WebP capability. Licensing/codec redistribution obligations still require review.

Check `secretary status` / `secretary doctor` for basic binary readiness. Live Web upload, timestamped WebP derivatives, and remote Worker consumption require separate manual acceptance under #89. Source installs may supply an explicit pair with `SECRETARY_FFMPEG` and `SECRETARY_FFPROBE`; only the Server resolves these paths.

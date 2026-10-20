# ADR-0006: Supported binary distribution platform matrix

- **Status:** Accepted
- **Date:** 2026-10-20
- **Context:** [Linux binary installation](../linux-binary-install.md), [macOS binary installation](../macos-binary-install.md), [Windows tray](../windows-tray.md), [unified installer](../../scripts/install.sh)

## Context

The binary installers ship prebuilt archives per operating system and architecture. Every published pair (`secretary-vVERSION-os-ARCH.json` plus `secretary-vVERSION-os-ARCH.tar.gz`) costs a release CI job, a signed native bundle on macOS, a checksum manifest and a place in the release contract that the installer verifies at runtime.

The release pipeline currently builds more combinations than the product supports:

- `macos-binary-release.yml` publishes only `darwin-arm64`. The dispatcher in `scripts/install.sh` still recognizes `Darwin/x86_64` and maps it to `darwin/amd64`, where no artifact exists, so an Intel Mac fails late with a missing-manifest error instead of an explicit unsupported-platform message.
- `windows-node-binary-release.yml` builds `windows-amd64` and `windows-arm64` Worker archives, while the Windows acceptance job (`windows-standalone-node.yml`) validates `amd64` only and explicitly treats an `arm64` manifest as an invalid architecture.

Owner decision: macOS Intel and Windows on ARM64 are not needed. The supported surface stays Apple Silicon macOS, Linux on both CPU architectures, and Windows on x64.

### Relation to ADR-0002

ADR-0002 states that Execution nodes support macOS, Linux and Windows once verified, and that live testing must precede advertising them as supported. This ADR does not contradict it: it narrows the per-architecture scope of that decision for the binary distribution. The live-verification requirement of ADR-0002 continues to apply, and a platform outside the matrix below stays unadvertised even if the manifest parser tolerates it.

## Decision

1. **Supported binary distribution matrix:**

   | Platform | Architecture | Artifact | Acceptance |
   |---|---|---|---|
   | Linux | x86_64 (amd64) | `secretary-vVERSION-linux-amd64.tar.gz` | required |
   | Linux | aarch64 (arm64) | `secretary-vVERSION-linux-arm64.tar.gz` | required |
   | macOS | arm64 (Apple Silicon) | `secretary-vVERSION-darwin-arm64.tar.gz` | required |
   | Windows | x86_64 (amd64) | `secretary-node-windows-amd64-vVERSION.zip` | required |

2. **macOS on Intel (x86_64) is out of scope.** No `darwin/amd64` artifact is published, and none is planned. The unified installer must fail with a clear unsupported-platform message instead of attempting a download.

3. **Windows on ARM64 is out of scope.** The Windows acceptance matrix covers x64 only, so an ARM64 Worker is neither verified nor advertised. The existing release workflow still builds and publishes `secretary-node-windows-arm64-vVERSION.zip` alongside the x64 archive; that artifact is outside the supported matrix and carries no acceptance. Stopping the ARM64 build is a separate cleanup that no user request depends on.

4. **The release contract stays closed to unsupported pairs.** A release publishes a manifest and archive for exactly the platforms in the table above. Publishing an artifact for an unsupported platform does not make that platform supported, and an unsupported pair must not appear in the supported matrix.

5. **CI mirrors the matrix.** Release and acceptance jobs run for the supported platforms only. The unified-installer dispatch job keeps a macOS Intel runner solely to exercise the host-detection branch of `scripts/install.sh`; that job validates routing logic against a mocked release list and proves nothing about a shipped Intel artifact.

## Consequences

- Positive: release CI time, release artifact size and the per-platform verification surface shrink to the platforms users actually run.
- The dispatcher's `Darwin/x86_64` branch becomes an explicit rejection rather than a late missing-manifest failure, which gives an Intel Mac user an accurate reason instead of a download error.
- The `windows-arm64` Worker archive stops being published. Anyone consuming it must move to `windows-amd64`.
- Documentation must not advertise Intel macOS or Windows ARM64 as installable.
- Removing the Intel branch from the installer is a code change with its own review and release; until it lands, an Intel Mac fails closed with a missing-manifest error, which is safe but not yet a good message.

## Alternatives rejected

- Keep publishing macOS Intel and Windows ARM64 artifacts "because the pipeline already builds them". Unverified artifacts imply support the acceptance matrix does not cover.
- Drop Linux arm64 for symmetry. It is covered by the release CI matrix and the public installation was accepted on it.
- Publish Intel macOS binaries later "if demand appears". The decision can be revisited with a new ADR.

## Verification

1. `scripts/unified-binary-install-test.sh` covers the dispatcher's platform mapping for Linux amd64, Linux arm64, macOS arm64 and macOS x86_64, including the rejected host message.
2. `gh release view v0.1.0-alpha.8` lists `darwin-arm64`, `linux-amd64`, `linux-arm64` Server pairs and the `windows-amd64` Worker archive; any further platform pair is outside the matrix and must be justified by its own acceptance.
3. The public one-command installation was accepted on Linux amd64 (systemd user services active, HTTP 401 from the Server) and on macOS arm64.

# Phase 1: Pi Secretary runtime

This directory contains the first working Pi-side Secretary runtime as a source snapshot.

- Source repository: `git@github.com:GitHubFoxy/pi.git`
- Source branch: `secretary/full-remote-worker`
- Source commit: `032d98982daebb51fba0f8d63400259ed78ffebd`
- Commit date: 2026-09-06

The snapshot includes `subpi --view`, the remote Worker viewer, transcript and activity updates, cancellation, reconnect and recovery, plus the bridge for Pi extensions and UI components. The main Secretary server is implemented in the repository root.

This is intentionally a source snapshot rather than a Git subtree. The complete Pi history remains in the upstream Pi fork; this directory records the exact Phase 1 source that the Secretary project depends on.

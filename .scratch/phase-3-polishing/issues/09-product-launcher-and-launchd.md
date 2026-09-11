# Product launcher and launchd

Type: task
Status: ready-for-human
Blocked by: 05, 06, 07, 08

## Work

Finish `sex setup/start/stop/restart/status/logs/doctor/install-service/uninstall-service`. Preserve current local paths. Add harness preflight, explicit errors, browser pairing through URL fragment and normal/`--debug` LaunchAgent lifecycle.

## Implementation notes

- `sex` now has setup, start, stop, restart, status, logs, doctor, install-service, uninstall-service, and terminal-independent `serve` paths.
- Start reuses the existing server mode, rotates/persists capability pairing through the URL fragment, runs harness preflight, and accepts `--debug` for the Control Room.
- The LaunchAgent has explicit PATH, KeepAlive, RunAtLoad, log paths, and normal/debug service arguments.

## Acceptance

- `sex` with no arguments prints help.
- `sex start` starts or reuses server and opens User UI without manual tokens or env variables.
- `sex start --debug` provides User UI and Control Room.
- LaunchAgent starts after macOS login and has no terminal dependency.

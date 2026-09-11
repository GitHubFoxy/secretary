# Phase 3 release gate

Run the deterministic gate from the repository root:

```sh
./scripts/phase3-release-gate.sh
```

The suite covers fake Codex and fx ACP sessions, profile delivery, activity, cancellation, MCP metadata, fx interrupt-and-continue, child Workers, config snapshots, and the embedded User UI and Control Room assets.

## sex CLI integration suite

Run every command in an isolated temporary `HOME` with a persistent fake ACP daemon:

```sh
./scripts/sex-cli-test.sh
```

The suite covers usage, setup, status, doctor, start, start reuse, logs, stop, restart, install-service, uninstall-service, and serve. It verifies binaries, generated config and Profiles, browser pairing, HTTP readiness, normal/debug mode, Control Room gating, launchd plist calls, and cleanup. The test uses a fake `launchctl` and never touches the user's Secretary state.

Set `SEX_TEST_KEEP=1` to preserve the temporary HOME after a failure for diagnosis.

`phase3-release-gate.sh` runs this suite as its final step.

## Real harness matrix

Use a fresh data directory for each harness. Keep the same external Profiles and set `runtime.harness` to the selected value in `config.toml`.

```sh
sex doctor
SECRETARY_ACP_COMMAND="$(command -v codex-acp)" sex start
# exercise login, onboarding, a Worker, observer steering, queue, stop, and restart
sex stop

SECRETARY_FX_COMMAND="$(command -v fx)" sex start
# repeat the same flow and confirm interrupt-and-continue creates a follow-up Attempt
sex stop

SECRETARY_OPENCODE_COMMAND="$(command -v opencode)" sex start
# repeat the flow and confirm native Profile delivery and MCP requests
sex stop
```

For the debug path, run `sex start --debug`, open `/control-room`, inspect Overview, Events, raw logs, model selection, Config validation, and diagnostic export, then run `sex stop`.

A missing binary must make `sex doctor` and `sex start` fail with the configured harness name. No harness may silently fall back to Codex.

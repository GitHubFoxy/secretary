# Secretary Bridge POC

`secretary_bridge.py` is a disposable local CLI for the AgentHub vertical slice. It is not a production service and it does not implement security isolation, token renewal, retention, a public API, or delivery of Result back into a client conversation.

## Install on Linux

Copy the directory to the Linux host and expose the script in `PATH`:

```bash
mkdir -p ~/secretary-bridge-poc ~/.local/bin
cp secretary_bridge.py ~/secretary-bridge-poc/
ln -sf ~/secretary-bridge-poc/secretary_bridge.py ~/.local/bin/secretary-bridge
chmod 700 ~/secretary-bridge-poc/secretary_bridge.py
```

Create `~/.config/secretary-bridge-poc/env` with mode `0600`:

```dotenv
AGENTHUB_URL=http://127.0.0.1:8080
AGENTHUB_TOKEN=<manually-supplied-short-lived-operator-token>
SECRETARY_BRIDGE_TEAM_ID=<test-team-id>
SECRETARY_BRIDGE_ORIGIN_AGENT_ID=<coordinator-agent-id>
SECRETARY_BRIDGE_WORKSPACE_ROOT=/home/coder/secretary-workers
SECRETARY_BRIDGE_AGENT_COMMAND=/home/coder/projects/agenthub/target/release/agenthubd
SECRETARY_BRIDGE_AGENT_ARGS=["acp", "codex"]
```

The token is intentionally manual for this proof of concept. Do not put this file in Git or copy Codex authentication material into it.

## Commands

```bash
secretary-bridge delegate 'Run sleep 60, then report completion.'
secretary-bridge show wkr_123
secretary-bridge send-follow-up wkr_123 'Now run sleep 15.'
```

A Worker receives the terminal callback command inside its Task envelope. Its callback writes Result to the local binding state and sends it to the Origin Coordinator session saved at Dispatch:

```bash
secretary-bridge report-result \
  --worker-ref wkr_123 \
  --capability <capability-from-envelope> \
  --status succeeded \
  --summary 'sleep 60 completed'
```

The local state file defaults to `~/.secretary-bridge-poc/state.json` and has mode `0600`.

## Current limits

- The test Team must already exist.
- The Coordinator profile must be installed before the Coordinator invokes `delegate` itself.
- `report-result` requires the Origin Coordinator session saved at Dispatch to still be running.
- A failed HTTP call can leave a created AgentHub Worker for inspection. That is intentional for the POC.

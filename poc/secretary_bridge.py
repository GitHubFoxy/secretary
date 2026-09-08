#!/usr/bin/env python3
"""Disposable local adapter for the AgentHub Secretary proof of concept."""

from __future__ import annotations

import argparse
import json
import os
import secrets
import shlex
import stat
import sys
import tempfile
import urllib.error
import urllib.request
import uuid
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

DEFAULT_CONFIG_PATH = Path.home() / ".config/secretary-bridge-poc/env"
DEFAULT_STATE_PATH = Path.home() / ".secretary-bridge-poc/state.json"


class BridgeError(RuntimeError):
    pass


def now() -> str:
    return datetime.now(UTC).isoformat()


def load_config() -> dict[str, str]:
    config = dict(os.environ)
    path = Path(config.get("SECRETARY_BRIDGE_CONFIG", DEFAULT_CONFIG_PATH)).expanduser()
    if not path.exists():
        return config
    mode = stat.S_IMODE(path.stat().st_mode)
    if mode & 0o077:
        raise BridgeError(f"config file must not be group/world readable: {path}")
    for raw_line in path.read_text().splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if not separator or not key.strip():
            raise BridgeError(f"invalid config line in {path}: {raw_line!r}")
        config.setdefault(key.strip(), value.strip())
    return config


def required(config: dict[str, str], name: str) -> str:
    value = config.get(name, "").strip()
    if not value:
        raise BridgeError(f"{name} is required")
    return value


def state_path(config: dict[str, str]) -> Path:
    return Path(config.get("SECRETARY_BRIDGE_STATE", DEFAULT_STATE_PATH)).expanduser()


def load_state(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {"version": 1, "workers": {}}
    try:
        state = json.loads(path.read_text())
    except json.JSONDecodeError as error:
        raise BridgeError(f"invalid state file {path}: {error}") from error
    if not isinstance(state, dict) or not isinstance(state.get("workers"), dict):
        raise BridgeError(f"invalid state file {path}")
    return state


def save_state(path: Path, state: dict[str, Any]) -> None:
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", dir=path.parent, delete=False) as handle:
        json.dump(state, handle, indent=2, sort_keys=True)
        handle.write("\n")
        temp_path = Path(handle.name)
    temp_path.chmod(0o600)
    temp_path.replace(path)


def api_request(
    config: dict[str, str], method: str, path: str, payload: Any | None = None
) -> Any:
    base_url = required(config, "AGENTHUB_URL").rstrip("/")
    token = required(config, "AGENTHUB_TOKEN")
    body = None if payload is None else json.dumps(payload).encode()
    request = urllib.request.Request(
        f"{base_url}{path}",
        data=body,
        method=method,
        headers={
            "Authorization": f"Bearer {token}",
            "Accept": "application/json",
            **({"Content-Type": "application/json"} if body is not None else {}),
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            raw = response.read()
    except urllib.error.HTTPError as error:
        detail = error.read().decode(errors="replace")
        raise BridgeError(f"AgentHub {method} {path} returned {error.code}: {detail}") from error
    except urllib.error.URLError as error:
        raise BridgeError(f"AgentHub {method} {path} failed: {error.reason}") from error
    if not raw:
        return None
    return json.loads(raw)


def worker_ref() -> str:
    return f"wkr_{uuid.uuid4().hex[:12]}"


def callback_capability() -> str:
    return secrets.token_urlsafe(32)


def report_command(config: dict[str, str]) -> str:
    return config.get("SECRETARY_BRIDGE_REPORT_COMMAND", "secretary-bridge").strip()


def task_envelope(task: str, reference: str, capability: str, config: dict[str, str]) -> str:
    command = " ".join(
        [
            report_command(config),
            "report-result",
            "--worker-ref",
            shlex.quote(reference),
            "--capability",
            shlex.quote(capability),
            "--status",
            "succeeded",
            "--summary",
            shlex.quote("your concise terminal result"),
        ]
    )
    return f"""Secretary Bridge task envelope

Task:
{task}

Worker reference: {reference}

When this attempt reaches a terminal outcome, invoke this command exactly once:
{command}

Do not report a result before the task is terminal.
"""


def add_worker_to_team(
    config: dict[str, str], team: dict[str, Any], agent_id: str, reference: str, capability: str
) -> None:
    spec = team.get("spec")
    if not isinstance(spec, dict) or not isinstance(spec.get("members"), list):
        raise BridgeError("Team response has no spec.members array")
    members = list(spec["members"])
    members.append(
        {
            "member_id": agent_id,
            "role": "worker",
            "description": f"Secretary POC Worker {reference}",
            "prompt": (
                "You execute one Secretary task. Do not coordinate Team work. "
                "Obey the task envelope and send its terminal Result callback."
            ),
        }
    )
    updated_spec = dict(spec)
    updated_spec["members"] = members
    updated_at = team.get("updated_at")
    if not isinstance(updated_at, int):
        raise BridgeError("Team response has no integer updated_at")
    api_request(
        config,
        "POST",
        f"/api/teams/{team['id']}/members/move",
        {"agent_id": agent_id, "spec": updated_spec, "expected_updated_at": updated_at},
    )


def origin_conversation(config: dict[str, str], team_id: str) -> dict[str, str]:
    agent_id = required(config, "SECRETARY_BRIDGE_ORIGIN_AGENT_ID")
    runtime = api_request(config, "GET", f"/api/teams/{team_id}/runtime")
    for member in runtime.get("members", []):
        if member.get("member_id") == agent_id:
            session_id = member.get("session_id")
            if not isinstance(session_id, str) or not session_id:
                raise BridgeError("origin Coordinator has no running session")
            return {"agent_id": agent_id, "session_id": session_id}
    raise BridgeError("origin Coordinator is not a member of the configured Team")


def result_message(reference: str, result: dict[str, Any]) -> str:
    return (
        "Secretary Bridge Result. Publish the following Result exactly as a user-visible reply. "
        "Do not delegate, retry, or add commentary.\n\n"
        f"Worker {reference}: {result['status']}. {result['summary']}"
    )


def delegate(args: argparse.Namespace, config: dict[str, str]) -> dict[str, Any]:
    task = args.task.strip()
    if not task:
        raise BridgeError("task must not be empty")
    team_id = required(config, "SECRETARY_BRIDGE_TEAM_ID")
    workspace_root = Path(required(config, "SECRETARY_BRIDGE_WORKSPACE_ROOT")).expanduser()
    command = required(config, "SECRETARY_BRIDGE_AGENT_COMMAND")
    command_args = json.loads(config.get("SECRETARY_BRIDGE_AGENT_ARGS", '["acp", "codex"]'))
    if not isinstance(command_args, list) or not all(isinstance(item, str) for item in command_args):
        raise BridgeError("SECRETARY_BRIDGE_AGENT_ARGS must be a JSON array of strings")

    reference = worker_ref()
    capability = callback_capability()
    workspace = workspace_root / reference
    workspace.mkdir(mode=0o700, parents=True, exist_ok=False)
    team = api_request(config, "GET", f"/api/teams/{team_id}")
    origin = origin_conversation(config, team_id)
    agent = api_request(
        config,
        "POST",
        "/api/agents",
        {
            "name": reference,
            "workdir": str(workspace),
            "command": command,
            "args": command_args,
            "source": "team_forge",
            "worktree_mode": "use_existing",
            "code_mode": True,
        },
    )
    agent_id = agent.get("id")
    if not isinstance(agent_id, str) or not agent_id:
        raise BridgeError("AgentHub create-agent response has no id")
    add_worker_to_team(config, team, agent_id, reference, capability)
    path = state_path(config)
    state = load_state(path)
    state["workers"][reference] = {
        "agent_id": agent_id,
        "team_id": team_id,
        "workspace": str(workspace),
        "origin": origin,
        "task": task,
        "attempt": 1,
        "attempt_state": "dispatching",
        "capability": capability,
        "created_at": now(),
        "results": [],
    }
    save_state(path, state)
    api_request(
        config,
        "POST",
        f"/api/agents/{agent_id}/input",
        {"input": task_envelope(task, reference, capability, config)},
    )
    state["workers"][reference]["attempt_state"] = "active"
    state["workers"][reference]["updated_at"] = now()
    save_state(path, state)
    return {"status": "accepted", "worker_ref": reference, "agent_id": agent_id}


def send_follow_up(args: argparse.Namespace, config: dict[str, str]) -> dict[str, Any]:
    text = args.text.strip()
    if not text:
        raise BridgeError("follow-up text must not be empty")
    path = state_path(config)
    state = load_state(path)
    worker = state["workers"].get(args.worker_ref)
    if not isinstance(worker, dict):
        raise BridgeError(f"unknown worker_ref: {args.worker_ref}")
    if worker.get("attempt_state") == "active":
        return {"status": "not_ready", "worker_ref": args.worker_ref}
    capability = callback_capability()
    worker["attempt"] = int(worker.get("attempt", 0)) + 1
    worker["attempt_state"] = "active"
    worker["capability"] = capability
    worker["updated_at"] = now()
    save_state(path, state)
    api_request(
        config,
        "POST",
        f"/api/agents/{worker['agent_id']}/input",
        {"input": task_envelope(text, args.worker_ref, capability, config)},
    )
    return {"status": "accepted", "worker_ref": args.worker_ref, "attempt": worker["attempt"]}


def report_result(args: argparse.Namespace, config: dict[str, str]) -> dict[str, Any]:
    path = state_path(config)
    state = load_state(path)
    worker = state["workers"].get(args.worker_ref)
    if not isinstance(worker, dict):
        raise BridgeError(f"unknown worker_ref: {args.worker_ref}")
    if worker.get("attempt_state") != "active":
        raise BridgeError("no active attempt for this worker")
    if not secrets.compare_digest(str(worker.get("capability", "")), args.capability):
        raise BridgeError("invalid callback capability")
    result = {"attempt": worker["attempt"], "status": args.status, "summary": args.summary, "at": now()}
    worker["results"].append(result)
    worker["attempt_state"] = "terminal"
    worker.pop("capability", None)
    worker["updated_at"] = now()
    save_state(path, state)
    origin = worker.get("origin")
    if not isinstance(origin, dict):
        raise BridgeError("worker binding has no Origin Conversation")
    api_request(
        config,
        "POST",
        f"/api/agents/{origin['agent_id']}/input",
        {
            "input": result_message(args.worker_ref, result),
            "session_id": origin["session_id"],
        },
    )
    worker["result_delivered_at"] = now()
    save_state(path, state)
    return {"worker_ref": args.worker_ref, "result": result}


def show(args: argparse.Namespace, config: dict[str, str]) -> dict[str, Any]:
    state = load_state(state_path(config))
    if args.worker_ref:
        worker = state["workers"].get(args.worker_ref)
        if worker is None:
            raise BridgeError(f"unknown worker_ref: {args.worker_ref}")
        return {"worker_ref": args.worker_ref, **worker}
    return state


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    delegate_parser = commands.add_parser("delegate")
    delegate_parser.add_argument("task")
    follow_up_parser = commands.add_parser("send-follow-up")
    follow_up_parser.add_argument("worker_ref")
    follow_up_parser.add_argument("text")
    result_parser = commands.add_parser("report-result")
    result_parser.add_argument("--worker-ref", required=True)
    result_parser.add_argument("--capability", required=True)
    result_parser.add_argument("--status", choices=["succeeded", "failed", "canceled"], required=True)
    result_parser.add_argument("--summary", required=True)
    show_parser = commands.add_parser("show")
    show_parser.add_argument("worker_ref", nargs="?")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        config = load_config()
        if args.command == "delegate":
            result = delegate(args, config)
        elif args.command == "send-follow-up":
            result = send_follow_up(args, config)
        elif args.command == "report-result":
            result = report_result(args, config)
        else:
            result = show(args, config)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (BridgeError, OSError, ValueError, json.JSONDecodeError) as error:
        print(f"secretary-bridge: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

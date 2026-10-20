#!/usr/bin/env python3
"""Exercise the *packaged* modern Codex ACP binary, not a Python ACP fixture.

CI uses a temporary HOME and an inert API-key sentinel; it must not make a
model inference call or require a user's ChatGPT subscription. The first three
methods prove real adapter + pinned Codex integration; the steering request on
an idle session proves the handler exists, but not active-turn delivery.
"""
import argparse
import json
import os
import pathlib
import queue
import subprocess
import sys
import tempfile
import threading
import time

p = argparse.ArgumentParser()
p.add_argument("--acp", required=True)
p.add_argument("--codex", required=True)
p.add_argument("--expected-version", default="2.2.2")
p.add_argument("--mock-app-server", help="explicit local Codex app-server fixture for offline CI protocol gate")
args = p.parse_args()

def safe_stderr(s):
    lines = []
    for line in s.splitlines():
        if "unknown variant" in line or "models cache" in line or "session/new" in line:
            lines.append(line[:320])
    return " | ".join(lines[-6:])[:950]

ver = subprocess.run([args.acp, "--version"], check=True, text=True, capture_output=True, timeout=15)
assert args.expected_version in ver.stdout, f"wrong bundled ACP version: {ver.stdout!r}"
cli = subprocess.run([args.codex, "--version"], check=True, text=True, capture_output=True, timeout=15)
assert "0.163.0" in cli.stdout, f"wrong Codex version: {cli.stdout!r}"
print(f"candidate ACP={ver.stdout.strip()} Codex={cli.stdout.strip()}", flush=True)

with tempfile.TemporaryDirectory(prefix="secretary-modern-acp-") as d:
    work = pathlib.Path(d)
    cwd = work / "workspace"
    cwd.mkdir()
    env = {**os.environ,
           "HOME": str(work),
           "XDG_CONFIG_HOME": str(work / "config"),
           "XDG_CACHE_HOME": str(work / "cache"),
           "XDG_DATA_HOME": str(work / "data"),
           "CODEX_PATH": os.path.abspath(args.mock_app_server or args.codex),
           "CODEX_MOCK_LOG": str(work / "mock-codex-wire.log"),
           "CODEX_CONFIG": "{}",
           # No owner credentials or model inference in CI.

           "NO_BROWSER": "1"}
    proc = subprocess.Popen([args.acp], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, encoding="utf8", bufsize=1, env=env, cwd=cwd)
    responses = queue.Queue()
    errors = []
    def read_out():
        try:
            for line in proc.stdout:
                try:
                    msg = json.loads(line)
                    if "id" in msg:
                        responses.put(msg)
                except json.JSONDecodeError:
                    errors.append("non-JSON ACP stdout")
        except Exception as exc:
            errors.append(f"reader: {type(exc).__name__}")
    def read_err():
        try:
            for line in proc.stderr:
                errors.append(line[:800])
                if len(errors) > 80:
                    errors.pop(0)
        except Exception:
            pass
    threading.Thread(target=read_out, daemon=True).start()
    threading.Thread(target=read_err, daemon=True).start()
    sequence = 0
    def request(method, params, seconds=60, allow_idle_steering_error=False):
        global sequence
        sequence += 1
        proc.stdin.write(json.dumps({"jsonrpc":"2.0","id":sequence,"method":method,"params":params}) + "\n")
        proc.stdin.flush()
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            try:
                m = responses.get(timeout=min(0.5, max(0.01, deadline-time.monotonic())))
            except queue.Empty:
                if proc.poll() is not None:
                    raise RuntimeError(f"{method}: ACP exited {proc.returncode}; stderr: {safe_stderr(''.join(errors))}")
                continue
            if m.get("id") != sequence:
                raise RuntimeError(f"{method}: unexpected response ID {m.get('id')}")
            if "error" in m:
                e = m["error"]
                if allow_idle_steering_error and e.get("code") != -32601 and (
                        "active" in e.get("message", "").lower()
                        or "turn" in e.get("message", "").lower()
                        or "steer" in e.get("message", "").lower()):
                    print("steering handler negotiated and reached (idle-turn rejection): "
                          + e.get("message","")[:250], flush=True)
                    return None
                raise RuntimeError(f"{method}: {e.get('code')}: {e.get('message')}; stderr: {safe_stderr(''.join(errors))}")
            print(f"{method}: OK", flush=True)
            return m.get("result", {})
        raise TimeoutError(f"{method} timed out after {seconds}s; stderr: {safe_stderr(''.join(errors))}")
    try:
        init = request("initialize", {
            "protocolVersion": 1,
            "clientCapabilities":{"elicitation":{"form":{}}},
            "clientInfo":{"name":"secretary-release-gate","version":"1"},
        })
        assert init.get("protocolVersion") == 1
        assert init.get("agentCapabilities",{}).get("loadSession") is True
        assert init.get("_meta",{}).get("steering",{}).get("supported") is True

        new = request("session/new",{"cwd":str(cwd),"mcpServers":[]},seconds=75)
        session_id = new.get("sessionId")
        assert isinstance(session_id,str) and session_id, "session/new missing sessionId"
        models = new.get("models",{}).get("availableModels",[])
        print("session/new models:", len(models), flush=True)
        assert models, "empty model catalog"
        if args.mock_app_server:
            observed = str(new)
            assert "max" in observed and "ultra" in observed, "modern ACP lost max/ultra reasoning options"
        request("session/load",{"sessionId":session_id,"cwd":str(cwd),"mcpServers":[]},seconds=75)
        request("_session/steering",{"sessionId":session_id,"prompt":[{"type":"text","text":"release-gate steering handler check"}]},
                seconds=20,allow_idle_steering_error=True)
        assert not any("unknown variant" in x and "max" in x for x in errors), "reasoning effort max parsing failed"
        if args.mock_app_server:
            wire = (work / "mock-codex-wire.log").read_text()
            for method in ("initialize", "model/list", "thread/start", "thread/resume"):
                assert method in wire, "missing Codex app-server method " + method
        print("PACKAGED MODERN ACP (mock app-server model cache): initialize / new / load / steering handler PASS",flush=True)
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)

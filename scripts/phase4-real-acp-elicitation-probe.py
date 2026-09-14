#!/usr/bin/env python3
"""Probe a real codex-acp elicitation round trip without recording payloads."""

import json
import os
import select
import signal
import subprocess
import sys
import tempfile
import time

MCP_SERVER = r'''
import json
import sys

def send(value):
    sys.stdout.write(json.dumps(value, separators=(',', ':')) + '\n')
    sys.stdout.flush()

for line in sys.stdin:
    try:
        request = json.loads(line)
    except Exception:
        continue
    method = request.get('method')
    request_id = request.get('id')
    if method == 'initialize':
        send({'jsonrpc': '2.0', 'id': request_id, 'result': {'protocolVersion': '2025-06-18', 'capabilities': {}, 'serverInfo': {'name': 'phase4-probe', 'version': '1'}}})
    elif method == 'tools/list':
        send({'jsonrpc': '2.0', 'id': request_id, 'result': {'tools': [{'name': 'ask_harmless', 'description': 'Ask one harmless non-secret question before continuing.', 'inputSchema': {'type': 'object', 'properties': {}}}]}})
    elif method == 'tools/call':
        send({'jsonrpc': '2.0', 'id': 7001, 'method': 'elicitation/create', 'params': {'mode': 'form', 'message': 'Which harmless greeting should I use?', 'requestedSchema': {'type': 'object', 'properties': {'greeting': {'type': 'string', 'title': 'Greeting'}}, 'required': ['greeting']}}})
        for response_line in sys.stdin:
            try:
                response = json.loads(response_line)
            except Exception:
                continue
            if response.get('id') == 7001:
                result = response.get('result', {})
                if result.get('action') == 'accept' and isinstance(result.get('content'), dict):
                    send({'jsonrpc': '2.0', 'id': request_id, 'result': {'content': [{'type': 'text', 'text': 'The user chose a harmless greeting.'}]}})
                else:
                    send({'jsonrpc': '2.0', 'id': request_id, 'error': {'code': -32000, 'message': 'elicitation unavailable'}})
                break
'''


def send(process, request_id, method, params):
    payload = json.dumps({'jsonrpc': '2.0', 'id': request_id, 'method': method, 'params': params}) + '\n'
    process.stdin.write(payload.encode())
    process.stdin.flush()


class ACPReader:
    def __init__(self, stream):
        self.stream = stream
        self.buffer = b''

    def next(self, deadline):
        while b'\n' not in self.buffer:
            if time.monotonic() >= deadline:
                return None
            ready, _, _ = select.select([self.stream], [], [], 1)
            if not ready:
                continue
            chunk = os.read(self.stream.fileno(), 4096)
            if not chunk:
                return None
            self.buffer += chunk
        line, self.buffer = self.buffer.split(b'\n', 1)
        try:
            return json.loads(line)
        except Exception:
            return None


def read_until(reader, process, request_id, deadline):
    methods = []
    observed = {
        'elicitation': False,
        'elicitation_accepted': False,
        'tool_call': False,
        'tool_completed': False,
    }
    while time.monotonic() < deadline:
        message = reader.next(deadline)
        if message is None:
            continue
        method = message.get('method')
        params = message.get('params')
        if not isinstance(params, dict):
            params = {}
        if method:
            methods.append(method)
            if method == 'elicitation/create':
                if params.get('mode') != 'form' or message.get('id') is None:
                    raise RuntimeError('real codex-acp sent malformed elicitation request')
                observed['elicitation'] = True
                observed['elicitation_accepted'] = True
                send_response(process, message.get('id'), {'action': 'accept', 'content': {'greeting': 'hello'}})
            elif method == 'session/request_permission':
                options = params.get('options', [])
                option_id = next((item.get('optionId') for item in options if 'allow' in item.get('kind', '')), None)
                if option_id:
                    send_response(process, message.get('id'), {'outcome': {'outcome': 'selected', 'optionId': option_id}})
        update = params.get('update', {})
        if method == 'session/update' and update.get('sessionUpdate') == 'tool_call':
            observed['tool_call'] = True
        if method == 'session/update' and update.get('sessionUpdate') == 'tool_call_update' and update.get('status') == 'completed':
            observed['tool_completed'] = True
        if message.get('id') == request_id and not message.get('method'):
            return message, methods, observed
    return {}, methods, observed


def send_response(process, request_id, result):
    payload = json.dumps({'jsonrpc': '2.0', 'id': request_id, 'result': result}) + '\n'
    process.stdin.write(payload.encode())
    process.stdin.flush()


def main():
    if os.name != 'posix':
        raise RuntimeError('this probe requires a POSIX host for pipe polling and process-group cleanup')
    command = os.environ.get('CODEX_ACP_COMMAND', 'codex-acp')
    with tempfile.TemporaryDirectory(prefix='secretary-acp-elicitation-') as workspace, tempfile.NamedTemporaryFile('w', suffix='.py') as mcp:
        mcp.write(MCP_SERVER)
        mcp.flush()
        process = subprocess.Popen([command], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, bufsize=0, start_new_session=True)
        reader = ACPReader(process.stdout)
        try:
            initialize_id = 1
            send(process, initialize_id, 'initialize', {'protocolVersion': 1, 'clientCapabilities': {'elicitation': {'form': {}}, 'fs': {'readTextFile': True, 'writeTextFile': True}, 'terminal': False}})
            initialize, _, _ = read_until(reader, process, initialize_id, time.monotonic() + 30)
            if initialize.get('result', {}).get('protocolVersion') != 1:
                raise RuntimeError('real codex-acp did not negotiate protocol version 1')
            session_id_request = 2
            send(process, session_id_request, 'session/new', {'cwd': workspace, 'mcpServers': [{'name': 'phase4-probe', 'command': sys.executable, 'args': [mcp.name], 'env': []}]})
            created, _, _ = read_until(reader, process, session_id_request, time.monotonic() + 60)
            session_id = created.get('result', {}).get('sessionId')
            if not session_id:
                raise RuntimeError('real codex-acp did not create a session')
            prompt_id = 3
            send(process, prompt_id, 'session/prompt', {'sessionId': session_id, 'prompt': [{'type': 'text', 'text': 'Use the ask_harmless MCP tool once. Wait for its result, then report only whether it completed.'}]})
            result, methods, observed = read_until(reader, process, prompt_id, time.monotonic() + 150)
            if result.get('error') or 'result' not in result:
                raise RuntimeError('real codex-acp prompt did not complete successfully')
            if not all(observed.values()):
                missing = ','.join(key for key, value in observed.items() if not value)
                raise RuntimeError('real codex-acp elicitation probe missing: ' + missing)
            print('real codex-acp elicitation/create round trip: PASS')
            print('observed methods: ' + ','.join(sorted(set(methods))))
        finally:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('real codex-acp elicitation probe: BLOCKED or FAIL: ' + str(error), file=sys.stderr)
        sys.exit(1)

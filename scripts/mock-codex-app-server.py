#!/usr/bin/env python3
"""Local Codex App Server fixture for release CI; never contacts OpenAI.

The *real packaged* modern ACP executable launches this as its CODEX_PATH.
It exercises parsing of actual app-server model/list responses with max/ultra.
A second independent check runs the official SHA-pinned Codex --version.
"""
import json
import os
import pathlib
import sys
import time
import uuid

if "--version" in sys.argv:
    print("codex-cli 0.163.0")
    raise SystemExit(0)
if "app-server" not in sys.argv:
    raise SystemExit("expected Codex app-server mode")

session_id = "01990000-0000-7000-8000-000000000001"
model = {
    "id":"gpt-6", "model":"gpt-6", "displayName":"GPT-6",
    "description":"reasoning schema validation fixture",
    "defaultReasoningEffort":"max",
    "supportedReasoningEfforts":[
        {"reasoningEffort":"max","description":"maximum"},
        {"reasoningEffort":"ultra","description":"ultra"}
    ],
    "inputModalities":["text"], "hidden":False, "supportsPersonality":False,
    "upgrade":None, "upgradeInfo":None, "availabilityNux":None,
    "modelSpecialty":None, "multiAgentVersion":None,
    "additionalSpeedTiers":[], "serviceTiers":[],
    "defaultServiceTier":None, "availableAccessPrograms":None,
    "isDefault":True
}

def thread():
    now=int(time.time())
    return {
        "id":session_id,"sessionId":session_id,"forkedFromId":None,
        "parentThreadId":None,"preview":"","ephemeral":True,
        "section":None,"sectionEnteredAt":None,"projectId":None,
        "historyMode":"paginated","modelProvider":"openai",
        "model":"gpt-6","reasoningEffort":"max",
        "createdAt":now,"updatedAt":now,"recencyAt":now,
        "status":{"type":"idle"},"path":None,"cwd":os.getcwd(),
        "cliVersion":"0.163.0","originator":None,"source":"appServer",
        "threadSource":None,"agentNickname":None,"agentRole":None,
        "gitInfo":None,"name":None,"turns":[]
    }

def thread_response():
    return {"thread":thread(),"model":"gpt-6","modelProvider":"openai",
            "serviceTier":None,"disabledPluginIds":[],"cwd":os.getcwd(),
            "instructionSources":[],"approvalPolicy":"never",
            "approvalsReviewer":None,"sandbox":{"type":"dangerFullAccess"},
            "reasoningEffort":"max","collaborationMode":None,
            "turnsBackwardsCursor":None,"itemsBackwardsCursor":None}

def respond(method, params):
    if method=="initialize":
        return {"codexHome":os.getcwd()}
    if method=="account/read":
        return {"account":{"type":"apiKey"},"requiresOpenaiAuth":False}
    if method=="config/read":
        return {"config":{"model":"gpt-6","model_reasoning_effort":"max"},
                "origins":{},"layers":None}
    if method=="skills/list":
        return {"data":[]}
    if method=="model/list":
        return {"data":[model],"nextCursor":None}
    if method=="thread/start":
        return thread_response()
    if method=="thread/resume":
        return thread_response()
    if method=="thread/read":
        return {"thread":thread()}
    if method in ("thread/turns/list","thread/items/list"):
        return {"data":[],"nextCursor":None}
    if method in ("mcpServer/status/list","mcpServer/list"):
        return {"data":[]}
    if method=="account/rateLimits/read":
        return {"rateLimits":None}
    if method in ("thread/unsubscribe","thread/archive","thread/settings/update","turn/steer"):
        return {}
    raise KeyError(method)

log=os.environ.get("CODEX_MOCK_LOG")
for line in sys.stdin:
    try:
        msg=json.loads(line)
        method=msg.get("method")
        if log and method:
            with open(log,"a",encoding="utf8") as f:
                f.write(method+"\n")
        if "id" not in msg:
            continue
        try:
            result=respond(method,msg.get("params",{}))
            out={"jsonrpc":"2.0","id":msg["id"],"result":result}
        except KeyError as e:
            print("unhandled mock Codex app-server method: "+str(e),file=sys.stderr,flush=True)
            out={"jsonrpc":"2.0","id":msg["id"],
                 "error":{"code":-32601,"message":"unhandled mock method "+str(e)}}
        print(json.dumps(out,separators=(",",":")),flush=True)
    except Exception as e:
        print("mock Codex app-server failure: "+repr(e),file=sys.stderr,flush=True)
        raise

# OpenCode profile-first runtime

Type: task
Status: ready-for-human
Blocked by: 01, 02, 03

## Work

Install/test OpenCode ACP and implement its runtime adapter. Generate OpenCode agent config that loads the effective Profile through native agent prompt. Support model, reasoning, tools, MCP, session load/resume, cancel, child events and full-access approval policy.

## Implementation

- Added `node.OpenCodeRuntime`, which writes a per-session `.secretary/opencode.json` and selects a unique primary OpenCode agent through `OPENCODE_CONFIG_CONTENT`.
- The native agent receives the compiled Profile prompt, skills, model, reasoning effort, tool map and full-access permission policy. Existing project `opencode.json` files are not overwritten.
- ACP `session/new` and `session/load` carry immutable Profile metadata in `_meta`, and both routes retain MCP stdio definitions. The shared ACP client handles cancel, permission approval, and filesystem requests.
- Added a runtime router so config reloads select OpenCode, Codex, or fx for new Profile snapshots while existing bindings retain their recorded runtime and delivery.
- Worker resume reloads the exact compiled Profile version from SQLite. Binding metadata and normalized `profile_delivery` events expose native delivery to the Control Room.
- Installed OpenCode dependencies from `/private/tmp/opencode`, built the ACP package, and smoke-tested `initialize` plus `session/new` through the adapter.

## Acceptance

- Raw ACP JSONL contains Profile version, hash and delivery metadata without leaking Profile prompt text.
- OpenCode adapter uses the shared start, load, resume, prompt, cancel, MCP and child-event paths.
- Binding metadata records `delivery = native` for OpenCode snapshots; compatibility harnesses record `workspace_instructions`.

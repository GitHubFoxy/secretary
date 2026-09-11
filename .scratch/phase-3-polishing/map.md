# Map: Secretary phase 3 polishing

## Destination

Довести local-first Secretary до ежедневного продукта: один command запуск, mobile-first User UI, отдельный Control Room, external Profiles, complete observability и три поддерживаемых harnesses: OpenCode, Codex и fx.

## Decisions so far

- `sex` остаётся working CLI name. Local state остаётся в `~/.local/share/secretary`, binaries в `~/.local/bin`.
- User UI и Control Room собираются на Svelte + Tailwind, встраиваются в один Go binary. UI dark, readable и mobile-first website, без service worker и offline queue.
- Control Room доступен только по `/control-room` при `--debug`; он показывает Overview, Workers tree, Events и Config.
- Profiles: только `secretary`, `worker`, `child_worker`. Они живут в external Markdown; config хранит paths, runtime, model, reasoning, skills и `allow_tools`.
- OpenCode является profile-first target harness. Codex и fx остаются supported compatibility/workaround harnesses. Arbitrary harnesses и bare mode не входят в scope.
- OpenCode получает native agent prompt. Codex/fx получают compiled `AGENTS.md` и bootstrap prompt. Control Room показывает способ delivery.
- Secretary всегда получает lifecycle через server-owned MCP tools. Worker получает `spawn_subagent`; Child этого tool не получает. Child Result возвращается parent.
- Model catalog доступен только Secretary через `available_models_for_worker` и только по явной просьбе пользователя. Worker/Child используют aliases `fast`, `smart`, `cheap`.
- Full access сохраняется. Permission request автоматически разрешается и логируется; невозможность разрешить его программно является harness compatibility failure.
- Events сохраняются normalized в SQLite, raw ACP JSONL в rotating files. Default retention: 30 дней и 1 GiB. Workspaces closed Tasks хранятся 30 дней.
- Global Worker limit: 4, включая children. Parent создаёт максимум 4 children за Attempt.
- Remote Node, Telegram, arbitrary providers и security sandboxing не входят в Phase 3.

## Completed decisions

- Ticket 01: external TOML config and three external Markdown Profiles are compiled into immutable snapshots. `SIGHUP` reload is atomic: invalid input or a failed durable config event leaves the active version unchanged. New Worker bindings retain their profile metadata; active bindings are untouched.
- Ticket 02: normalized lifecycle events are stored in SQLite, raw ACP JSONL is captured per Worker with rotation, and startup retention prunes old/oversized logs.
- Ticket 03: server-owned MCP is delivered through a per-session stdio helper. Secretary tools use Secretary capabilities, Worker receives only `spawn_subagent`, Child receives no tools, and invocations are audited.
- Ticket 04: Child Tasks are durable, linked to parent Attempt, limited to four, auto-closed after Result and returned to the parent through the blocking `spawn_subagent` tool. Child Results never enter Personal Conversation.
- Ticket 05: OpenCode runs through a native per-session agent config, with Profile metadata in ACP `_meta`, MCP/session lifecycle forwarding, automatic full-access responses, immutable resume snapshots, and `native` delivery observability.
- Ticket 06: Codex/fx share the ACP compatibility adapter. Codex uses managed `AGENTS.md`; fx steering interrupts, waits for idle, records a durable follow-up Attempt, and continues with the replacement prompt. Missing harnesses fail explicitly.
- Ticket 07: Svelte/Tailwind User UI is embedded as static production assets. It has cookie login, four-page onboarding, model selection, Conversation, Worker cards, and mobile full-screen or desktop drawer observers.
- Ticket 08: Debug-only Svelte/Tailwind Control Room exposes Overview, Worker actions, normalized Events, config edit/reload, model/runtime controls, raw logs, and export. Config edits preserve the active snapshot on failure and record version events.
- Ticket 09: `sex` owns setup, preflight, start/stop/restart/status/logs/doctor and launchd install/uninstall. `--debug` gates Control Room, and pairing is passed through the URL fragment.
- Ticket 10: Deterministic release gate and manual real-harness matrix are recorded in `scripts/phase3-release-gate.sh` and `docs/phase3-release-gate.md`.

## Work order

1. External config, compiled Profiles, config versions and event storage.
2. Server-owned MCP lifecycle tools and Child Worker tree.
3. Harness adapters: OpenCode first, then Codex/fx compatibility.
4. Svelte foundation, User UI and Control Room.
5. `sex` product lifecycle, launchd and debug mode.
6. Real acceptance suite for all three harnesses.

## Release gate

Every installed harness must pass: `new`, `load`, `resume`, `prompt`, `cancel`, tool events, MCP lifecycle tools, child Workers, terminal Result and profile/version observability. The product path must prove startup, persistent login, onboarding, Worker thread, config change, Control Room, cancel/restart and workspace retention.

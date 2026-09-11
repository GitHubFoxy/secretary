# Svelte Control Room

Type: task
Status: ready-for-human
Blocked by: 01, 02, 04, 05, 06

## Work

Implement `/control-room` as a Svelte/Tailwind operator surface, registered only when `--debug` is passed. Add Overview, Workers tree, Events and Config screens. Include config edit/reload, Secretary model selection, runtime restart, Worker cancel/retry, raw log access and diagnostic export.

## Implementation notes

- `web/control-room/src/App.svelte` is a separate Tailwind/Svelte operator surface with Overview, Workers, Events, and Config sections.
- Daemon `--debug` gates `/control-room` and `/v1/control/*`. The console supports config validate/apply/reload, model selection, runtime restart, Worker cancel/retry/close, raw log access, and diagnostic export.
- Config changes call the Manager reload path, preserve the active snapshot on failure, and create durable version events.

## Acceptance

- `/control-room` returns 404 without debug mode.
- Every action and state transition is visible live and durable.
- Config changes write `config.toml` and create versioned events.

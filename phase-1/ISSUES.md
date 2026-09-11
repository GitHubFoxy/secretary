# Secretary runtime issues

## Accepted implementation state

`subpi --view` now starts native Secretary work and opens an experimental client TUI in a cmux surface. The worker remains the session owner. The viewer has remote transcript updates, streaming/tool presentation, slash commands, reload, reconnect, extension commands/tools, notifications, prompts, widgets, title/header/footer/status state, autocomplete providers, and custom-editor input.

The current worker bridge deliberately renders extension components in the worker and sends rendered lines plus input events to the client. That is compatible with normal `Component` and `CustomEditor` extensions, including the configured `cmux-session-name.ts` editor. It cannot be pixel-identical to native Pi for extensions that depend on terminal-local side effects outside the documented UI APIs.

## Verified regressions

- Escape during a worker-installed custom editor bubbles to client cancellation only when the editor did not consume it. Escape first closes custom-editor autocomplete and extension dialogs.
- Custom-editor Enter emits a bridge-unique submit event and runs the normal client slash/prompt path after resource reloads.
- `getEditorComponent()` retains the worker factory, so a subsequent extension can wrap the installed editor.
- Confirm-dialog Escape resolves `false`, matching native `showExtensionConfirm()`.
- `/reload` preserves the client bridge. An isolated live session accepted `/name EditorAfterReload` and a subsequent live `sleep 60` showed `Command aborted` after Escape.

## Still requiring acceptance evidence

- Provider quota exhaustion and rate-limit recovery through the remote-worker boundary.
- A deliberately prolonged transport interruption followed by recovery.
- Large remote transcript attach, large-session compaction, and a parallel tool-result burst under real transport pressure.
- The historical `Byte transport closed` incident. The original transcript is unavailable; the copy-only replay harness does not establish its cause.
- Secretary host-tool cache pagination, concurrent reads, invalidation, and tool-result truncation need their own benchmark and tests.

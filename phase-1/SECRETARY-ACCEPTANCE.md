# Secretary acceptance checklist

Goal: persistent Secretary workers with `subpi --view`, a near-native Pi TUI, loaded extensions, and recovery that does not duplicate work.

This is an evidence ledger. A checked item has current, named evidence. An unchecked item is not accepted.

## Deliverables and evidence

- [x] `subpi --view --name NAME -q TASK` prints `THREAD_ID` and opens one viewer surface in the caller's cmux workspace.
  - `node --test ~/dotfiles/pi/agent/bin/subpi-with-view.test.mjs`, 7 passing tests.
  - Isolated live run on 2026-09-05: `subpi-remote --view --name EditorFinal -q 'Reply exactly READY.'` printed `THREAD_ID=01a0726c-2d21-74b7-90cc-c121b71d5fb4`, created only `surface:274`, and rendered `READY`.
- [x] Existing-session viewer does not start a second task owner.
  - Launcher tests cover attach-only and existing-session task invocation.
  - `experimental-audit-attach.test.ts` is an opt-in copied-transcript test and asserts two clients share exactly one worker.
- [x] Launcher preserves quoting, workspace target, task exit status, viewer-creation failure cleanup, cancellation forwarding, and explicit legacy selection.
  - The same 7 launcher tests cover the first five cases. `subpi` dispatches native by default; `PI_SUBPI_RUNTIME=legacy` is the only legacy path.
- [x] Client TUI renders transcript updates, streaming/tool presentation, footer, keybindings, model controls, compaction controls, and extension-provided header/footer/status/widgets.
  - `packages/coding-agent/test/experimental-*.test.ts`: 131 passing, 1 intentionally skipped.
  - `packages/coding-agent/test/experimental-client-tui-chat.test.ts` and `experimental-client-tui.test.ts` cover transcript and presentation wiring.
  - `packages/tui`: `npm test` passes.
- [x] Worker-side extension command, tool, lifecycle, UI, autocomplete, prompt, widget, title, and custom-editor bridges are present.
  - `packages/coding-agent/src/experimental/full-worker-resources.ts` and `services/extension-commands.ts`.
  - `experimental-full-worker-resources.test.ts` exercises an interactive `ui.custom()` component through state, input, submit, and completion.
  - Full `packages/coding-agent` suite: 2,109 passing, 51 intentionally skipped.
- [x] Inventory configured extensions and their used UI, command, and tool surfaces.
  - Static inventory on 2026-09-05 covered 13 configured files: `clone`, `cmux-session-name`, `cmux-session`, `current-time`, `custom`, `disable-tools`, `edit-intent`, `extensions-ui`, `fast-mode`, `session-title`, `skills-ui`, `telegram`, and `user-message-footer`.
  - Their used UI calls are `addAutocompleteProvider`, `custom`, `input`, `notify`, `select`, `setEditorComponent`, `setFooter`, `setStatus`, `setTitle`, and `theme`. The bridge implements each used rendering/input surface; the worker-installed `cmux-session-name.ts` editor is also confirmed live.
- [x] `/reload` refreshes worker and presentation resources.
  - `experimental-client-tui.test.ts` asserts both reload services.
  - Isolated live viewer showed `Reloaded plugins.` and subsequently accepted `/name EditorAfterReload`.
- [x] Escape cancellation reaches the active operation once and does not preempt extension dialogs or autocomplete.
  - `experimental-client-tui.test.ts` uses a real worker-installed `CustomEditor` after reload. It proves normal input, slash submission, dialog Escape, autocomplete Escape, and a second Escape cancelling `run-2`.
  - `experimental-full-worker-resources.test.ts` proves input, submit, custom-editor composition, Escape bubbling, and autocomplete dismissal through the worker bridge.
  - Isolated live run after the final build aborted `sleep 60` and displayed `Command aborted`.
- [x] Disconnect/reconnect restores the selected session without replaying it.
  - `experimental-remote-runtime.test.ts` and `experimental-client-tui.test.ts` cover reattachment. Worker ownership is intentionally asserted semantically, not by PID identity.
- [x] Guard the historical attach symptom without touching user data.
  - `experimental-audit-attach.test.ts` is copy-only and verifies two attached clients share one worker. The historical source transcript is unavailable, so `Byte transport closed` is not a reproducible current defect.
- [x] Repository gates pass on the current tree.
  - `npm run check` passed after the custom-editor changes.
  - `packages/coding-agent`: `npm test` passed, 260 files passed, 7 skipped, 2,109 tests passed, 51 skipped.
  - `packages/coding-agent`: `npm run build` passed.

## Follow-up soak investigations

These are not unimplemented Secretary deliverables. They require an external provider or a dedicated long-running environment, and do not block the verified native-runtime and near-native-TUI delivery:

- Provider retry timing after rate limiting. A controlled quota failure already reaches the remote transcript without transport closure.
- A prolonged real network outage, large remote compaction, and a deliberately extreme parallel tool-result burst.
- Secretary host-tool cache benchmarks. This is separate from the worker and viewer implementation.
- Pixel-for-pixel parity is not the target. The requested result is near-native behavior, and the viewer intentionally remains a separate client implementation.

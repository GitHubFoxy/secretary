# Svelte mobile-first User UI

Type: task
Status: ready-for-human
Blocked by: 01, 04, 05, 06

## Work

Replace the prototype UI with Svelte and Tailwind. Build a dark, readable, mobile-first User UI with persistent login, four-page onboarding modal, Secretary model selector, Conversation, Worker cards and Worker threads. Thread renders durable profile/model/tool/result/child data and provides Steering, Queue and Stop.

## Implementation notes

- `web/src/App.svelte` and `web/src/app.css` are built with Svelte 5, Vite, and Tailwind CSS, then embedded as production assets by `web/embed.go`.
- The User UI keeps the session cookie, opens a four-page onboarding modal, selects the Secretary model, subscribes to conversation and Worker activity, and exposes steering, queue, and stop controls.
- Worker threads are full-screen on mobile and a drawer on desktop. Normal conversation responses contain no Control Room diagnostics.

## Acceptance

- Static assets are embedded in the Go binary.
- Mobile Worker thread is full-screen; desktop uses drawer or modal.
- No Control Room diagnostics leak into normal Conversation.

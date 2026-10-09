# 24 Самообновление через supervisor и системный Project Secretary

Type: task
Status: needs-triage
Source: GitHubFoxy/secretary#1
Original URL: https://github.com/GitHubFoxy/secretary/issues/1
Original title: Self-update supervisor and Secretary system project
Original author: GitHubFoxy
Created: 2026-10-02T10:05:59Z
Updated: 2026-10-02T10:05:59Z
Original state: open
Original labels: нет
Original assignees: нет
Original milestone: нет

## Перенос и triage

Тикет перенесён из GitHub Issues в локальный Markdown tracker. Исходное описание ниже сохранено без изменений. Его нужно сверить с утверждённым Phase 4 contract; перенос не утверждает новый scope и не означает готовность к реализации.

Соответствие прежних номеров локальным тикетам:

- GitHub #1 → [24: Самообновление через supervisor и системный Project Secretary](24-self-update-supervisor.md).
- GitHub #2 → [25: Prompt-driven E2E: инструменты, маршрутизация и реальные harnesses](25-prompt-driven-e2e-acceptance-suite.md).
- GitHub #3 → [26: Полный пользовательский путь: установка, работа, восстановление и обновление](26-full-product-user-journey.md).

Ссылки и номера внутри исходного описания являются историческими. Использовать локальные тикеты из таблицы выше.

Связанный сквозной прогон: [23](23-clean-machine-product-e2e-journey.md).

Исходные API snapshots, включая комментарии и timeline: [`../github-import/`](../github-import/). В manifest сохранены SHA-256 snapshots.

## Исходное описание

## Goal

Allow a running Secretary installation to safely update its own source/build without making the running binary responsible for replacing itself.

The intended UX is:

> User talks to Secretary over Telegram/Web, points out something they want changed (optionally with screenshots), Secretary delegates the change to a coding Worker, the Worker edits the Secretary source repository, pushes the change, a supervisor builds and installs a new release, restarts Secretary, and the new binary reconnects to the same persistent state and Telegram conversation.

## Core principle

Keep three responsibilities separate:

```text
Secretary = decides what should change
Worker = edits/tests source code
Supervisor = installs/restarts/rolls back releases
```

The Secretary binary must not directly overwrite its own running executable.

## Proposed architecture

### Persistent installation state

Keep durable state outside release directories:

```text
~/.local/share/secretary/
├── secretary.db
├── config.toml
├── profiles/
├── environment / secrets
├── telegram state
└── logs/
```

This state must survive every binary replacement.

Bootstrap credentials, Secretary capability, Telegram bot credentials, owner identity, Conversation state, Worker state, etc. belong to installation state rather than a specific binary release.

### Versioned releases

Install binaries into immutable/versioned directories:

```text
~/.local/share/secretary/releases/
├── <commit-a>/
│   ├── secretaryd
│   ├── secretary-mcp
│   └── secretaryctl
├── <commit-b>/
└── ...

current -> releases/<commit-b>
previous -> releases/<commit-a>
```

The supervisor owns the `current` pointer.

### Secretary source as a Project

Register the Secretary repository itself as a normal Project, e.g. `secretary-system`.

A request such as:

> The Telegram formatting is broken. Fix nested Markdown.

should become a normal coding Worker job against that Project.

The Worker may:

1. fetch/pull the repository;
2. inspect screenshots or reproduction details;
3. modify source;
4. run tests/build checks;
5. commit/push the change;
6. return the candidate commit/release metadata.

The Worker does **not** restart the live service itself.

### Supervisor

Add a very small supervisor/launcher outside the replaceable Secretary runtime.

It should be able to perform something equivalent to:

```text
install_release(commit)
→ fetch/checkout exact commit
→ run required tests
→ build binaries into a new release directory
→ verify the candidate binaries
→ atomically switch current
→ restart Secretary
→ health check
→ rollback to previous on failure
```

The supervisor should remain usable even if the Secretary source checkout or newly built release is broken.

## Telegram continuity

Restarting Secretary must not require a new Telegram pairing/bootstrap.

The new binary should start using the same persistent:

- database;
- Person / Personal Conversation;
- Telegram bot token;
- Telegram owner/account mapping;
- last processed Telegram update state;
- Secretary installation credentials.

Telegram polling should tolerate the short restart window.

Incoming updates around a restart must remain idempotent so a replayed Telegram `update_id` cannot create duplicate Conversation entries, Workers, Turns, or Results.

## Bootstrap behavior

`SECRETARY_BOOTSTRAP_TOKEN` and other installation credentials must be treated as installation state, not regenerated during a normal update.

A replacement binary should open the same installation state and continue normally.

No self-update should require the user to re-bootstrap the Web or Telegram client.

## Safety / recovery

- Never overwrite the currently running binary in place.
- Build into a fresh release directory.
- Switch releases atomically.
- Keep at least one known-good previous release.
- Run a health check after restart.
- Automatically roll back if the new release cannot become healthy.
- Record the attempted commit, previous release, result, and rollback reason durably.
- A failed source checkout/build must leave the existing release untouched.
- The supervisor must not depend on the mutable Secretary source checkout for its own execution.

## Suggested product flow

```text
User
  ↓ Telegram/Web
Secretary
  ↓ spawn_worker(project="secretary-system")
Coding Worker
  ↓ source edit / tests / screenshots / commit / push
Candidate commit
  ↓
Supervisor
  ↓ build release
  ↓ atomic switch
  ↓ restart
  ↓ health check
New Secretary binary
  ↓
same DB + same secrets + same Telegram identity
  ↓
conversation continues
```

## Acceptance criteria

- Secretary source is available as a normal Project to coding Workers.
- A Worker can produce a tested candidate commit without touching the live binaries.
- Supervisor can install a candidate commit as a versioned release.
- Existing persistent state is reused by the replacement binary.
- Telegram reconnects after restart without re-pairing or a new bot identity.
- Web bootstrap does not rotate during a normal update.
- Telegram update deduplication survives restart.
- Release switch is atomic.
- Broken build leaves the current release untouched.
- Failed startup/health check rolls back automatically to the previous known-good release.
- Supervisor still works if the current Secretary checkout is corrupted.
- Update/rollback events are visible in durable diagnostics/history.

## Non-goals for the first implementation

- General-purpose package manager/update service.
- Automatic deployment of arbitrary repositories.
- Editing production binaries in place.
- Removing explicit approval/policy for sensitive self-modification.
- Hosted multi-user update infrastructure.

## Notes

The existing `secretary` launcher already separates much of the persistent state from `~/.local/bin`, so it can serve as the starting point. The main architectural change is to stop building directly over the live installation and move lifecycle ownership into an independent supervisor with versioned releases and rollback.


## Comments

Комментариев на GitHub на момент переноса не было.

# 26 Полный пользовательский путь: установка, работа, восстановление и обновление

Type: task
Status: needs-triage
Source: GitHubFoxy/secretary#3
Original URL: https://github.com/GitHubFoxy/secretary/issues/3
Original title: Full product end-to-end user journey acceptance stories
Original author: GitHubFoxy
Created: 2026-10-02T10:14:03Z
Updated: 2026-10-02T10:14:03Z
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

Define a **full product end-to-end acceptance journey** for Secretary.

This is intentionally different from #2:

- #2 tests individual prompts, tools and runtime behaviors.
- This issue tests the **entire application as a user experiences it**, from a clean machine through installation, onboarding, daily work, failure recovery and upgrades.

The core question is:

> If Secretary is given to a new user, can they install it, connect devices and channels, give it real work, recover from failures, and keep using it without understanding the internals?

---

# Product journey stories

## 1. Fresh install on a clean primary machine

**As a new user**, I have a supported machine with no Secretary state.

I obtain Secretary through the intended install path and run setup.

Secretary should install its executables, create durable state/config, generate installation credentials, detect the configured harness, explain missing dependencies, guide required provider authentication, and finish with one obvious way to start.

**Success:** I can go from no installation to ready-to-start without editing source files or discovering undocumented environment variables.

---

## 2. First launch

**As a new user**, I start Secretary for the first time.

It initializes durable state and the persistent Secretary identity, starts the server/runtime, exposes the local Web client, and reports a clear healthy/ready state.

**Success:** I can open the product and talk to Secretary without understanding ports, PIDs, ACP sessions or capabilities.

---

## 3. First Web pairing

**As the owner**, I open Web for the first time and complete the supported bootstrap/pairing flow.

The browser becomes a trusted Client connected to the existing Person and Personal Conversation. Bootstrap credentials are not exposed during normal later use.

**Success:** refresh/reopen works without repeating first-time setup.

---

## 4. Talk to Secretary before any execution setup

**As the user**, I ask a normal question that needs no Worker, then ask what Nodes, Projects and Workers currently exist.

Secretary answers simple questions directly and uses server-owned state for system questions.

**Success:** simple conversation works and unnecessary Workers are not created.

---

## 5. Connect Telegram

**As the owner**, I connect Telegram through the supported pairing flow.

Telegram is restricted to the owner, joins the same Personal Conversation as Web, keeps General chat Secretary-owned, and does not expose raw Secretary/server credentials.

**Success:** I send a Telegram message and it is clearly the same Secretary I used on Web.

---

## 6. Cross-client continuity

**As the user**, I say something in Telegram, refer to it from Web, reply from Web, then continue again in Telegram.

**Success:** Web and Telegram behave as two views into one persistent conversation, not separate bots.

---

## 7. Enroll the first Execution Node

**As the owner**, I enroll my primary computer as an Execution Node.

The Node receives its own identity/credential, connects outbound, reports online state and observed HarnessInstances, and remains credential-separate from Clients.

**Success:** Secretary can accurately tell me which harnesses are actually available on the machine.

---

## 8. Enroll a second machine

**As the owner**, I add a second computer such as a home server.

It enrolls independently and reports its own harness/capability inventory.

**Success:** Secretary now understands multiple execution locations and their actual availability.

---

## 9. Register a Project across machines

**As the owner**, I register one Project with different filesystem paths on the two Nodes.

Secretary stores one stable Project identity with per-Node mappings and validates policy/path availability.

**Success:** I can refer to the Project by name without remembering machine-specific paths.

---

## 10. First delegated task

**As the user**, I ask Secretary to make a small verified change in the Project without naming Node or harness.

Secretary recognizes actionable work, creates a Worker promptly, uses default routing policy, acknowledges acceptance, runs the work and delivers one terminal Result.

**Success:** I hand off work without manually selecting infrastructure.

---

## 11. Observe work in progress

**As the user**, I open the Worker while it is active.

I can see Worker identity, Project, Node, harness, durable state and readable activity without raw chain-of-thought or unbounded event spam.

**Success:** I understand what is happening and whether it is progressing.

---

## 12. Continue the same Worker

**As the user**, after the Worker finishes, I ask a follow-up that belongs to the same piece of work.

**Success:** the same Worker receives a new Turn instead of creating an unrelated Worker.

---

## 13. Steer active work

**As the user**, while a Worker is running, I add an extra instruction.

The instruction is delivered through supported steering or durably queued to the next safe boundary.

**Success:** direction changes without losing the Worker or duplicating the work.

---

## 14. Worker asks for input

**As the user**, I give a task with two plausible choices and the Worker asks me which one to use.

I answer from a different trusted Client.

**Success:** the pending request is durable and my answer reaches the correct Worker exactly once.

---

## 15. Approval flow

**As the owner**, a Worker reaches an operation that requires approval.

The product shows what is being requested and by which Worker; I can approve or deny from another trusted Client.

**Success:** work continues or stops correctly without exposing credentials.

---

## 16. Explicit Node or harness choice

**As the user**, I explicitly ask for a particular Node/harness combination.

**Success:** the requested binding is honored when available; if it is unavailable, Secretary reports an explicit error and does not silently fall back.

---

## 17. Parallel work

**As the user**, I submit two independent substantial tasks.

Secretary creates two Workers and remains responsive while both execute.

**Success:** each Worker keeps its own activity/result and both can run concurrently.

---

## 18. Cancel and close

**As the user**, I cancel active work and later explicitly close that Worker.

Cancel stops the active Attempt; Close ends the Worker lifecycle without erasing durable history.

**Success:** lifecycle actions are distinct, predictable and idempotent.

---

## 19. Change user preferences

**As the user**, I update a supported user preference.

The next Secretary turn sees it, while already-created Worker policy snapshots are not silently rewritten.

**Success:** preferences persist with clear scope.

---

## 20. Idle server restart

**As the owner**, I restart Secretary while no work is active.

After restart the same Person, Conversation, Projects, Workers, Clients and Telegram connection remain.

**Success:** restart feels like restarting an application, not creating a new Secretary or requiring re-bootstrap.

---

## 21. Restart during active work

**As the owner**, I restart Secretary while a Worker Attempt is active.

After restart the system reconstructs durable state and never silently executes uncertain work twice. It resumes only when the Node/runtime can prove the existing session; otherwise the Attempt becomes explicitly interrupted.

**Success:** no duplicate side effects, lost work or fabricated success.

---

## 22. Node loses network

**As the user**, a bound Node loses connectivity while work is running.

The Worker remains bound to that Node/HarnessInstance, offline/blocked state becomes visible, and durable Node events/outcomes replay after reconnect.

**Success:** network loss is understandable and does not create duplicate execution.

---

## 23. Node remains offline

**As the user**, a Worker is bound to a Node that stays offline.

**Success:** Secretary does not secretly migrate that Worker. Executing the same intent elsewhere is explicit new work.

---

## 24. Telegram Worker journey

**As a Telegram user**, I start substantial work in General chat.

A Worker Topic is created/associated with that Worker. Inside it I can observe readable activity, answer requests, follow up and receive the final Result.

**Success:** General remains Secretary-owned while Topics map naturally to persistent Workers.

---

## 25. Client disconnect and replay

**As the user**, I close Web or lose network while events happen.

When I reconnect, the Client replays missed durable state from a stable boundary.

**Success:** reconnect restores server truth and does not create new execution.

---

## 26. Duplicate delivery

**As the system**, the same Client/Telegram request is delivered twice because of transport retry.

**Success:** there is still one logical user message and one resulting Worker/Turn/Result where applicable.

---

## 27. Invalid or unavailable harness/model

**As the user**, I request execution with a harness/model unavailable on the chosen Node.

**Success:** a visible error is returned and no silent fallback occurs.

---

## 28. Revoke a Client

**As the owner**, I revoke a previously paired Client.

**Success:** that Client can no longer read or mutate Secretary state while other Clients/Nodes keep working.

---

## 29. Revoke a Node

**As the owner**, I revoke an enrolled Execution Node.

**Success:** it receives no new Dispatches, its credential cannot act as a Client, and affected Workers show explicit unavailable state.

---

## 30. Secret-boundary journey

**As the owner**, I use Web, Telegram, Worker observers and diagnostics during normal operation.

Raw bootstrap tokens, Secretary capabilities, Node credentials, Telegram bot tokens and provider credentials must never leak into user-visible activity or unnecessary model context.

**Success:** normal product use works without exposing control-plane secrets.

---

## 31. Stop/start the installed service

**As the owner**, I stop Secretary using the supported product mechanism and start it again.

**Success:** no duplicate servers, orphan processes or accidental second databases; the same state directory and identity are reused.

---

## 32. Machine reboot / login persistence

**As the owner**, I configure Secretary as a service and reboot/log out and back in.

**Success:** Secretary starts through the supported service mechanism, reuses state and reconnects Telegram without an old terminal session.

---

## 33. Broken configuration

**As the owner**, I submit an invalid config/profile change.

**Success:** the invalid update is rejected and the last known-good active configuration remains usable.

---

## 34. Diagnostics

**As the owner**, something is broken and I run supported status/doctor/log diagnostics.

They should distinguish common failures such as server stopped, harness missing, provider login missing, Node offline, invalid config, failed pairing or unavailable Project path, while redacting secrets.

**Success:** common failures can be diagnosed without reading source code.

---

## 35. Upgrade to a new Secretary build

**As the owner**, I install a newer Secretary build.

The new build reuses database, Person/Conversation, Clients, Nodes, Projects, Telegram pairing and configuration, with safe schema migration/backup behavior.

**Success:** upgrading does not feel like reinstalling the product.

This story should later use the self-update supervisor from #1.

---

## 36. Failed upgrade and rollback

**As the owner**, I attempt an upgrade whose new build fails health checks.

**Success:** the previous known-good release is recoverable/automatically restored, and persistent state remains intact.

---

## 37. Uninstall without accidental data loss

**As the owner**, I remove the service/runtime.

The product clearly distinguishes stop service, remove service, remove binaries, and delete all user data.

**Success:** uninstalling executables does not accidentally erase durable Secretary history.

---

# Golden full journey

The release should support one long-form acceptance run that chains the product lifecycle:

    clean machine
    → install Secretary
    → first start
    → pair Web
    → connect Telegram
    → enroll primary Node
    → enroll second Node
    → register Project
    → submit first delegated task
    → observe Worker
    → steer Worker
    → answer needs_input from another Client
    → receive Result
    → follow up with same Worker
    → run second Worker in parallel on another Node
    → lose/reconnect Node network
    → restart Secretary
    → verify Conversation/Workers survive
    → revoke/re-pair a Client
    → stop/start service
    → reboot machine
    → upgrade build
    → verify Telegram and Conversation continue

The product-level E2E gate should fail if this journey requires manual database editing, source-code intervention, hidden credentials or undocumented recovery steps.

---

## Relationship to other acceptance work

- Phase 4 ticket 15: protocol/domain/lifecycle correctness.
- #2: prompt/tool/harness behavior correctness.
- This issue: full user/product journey correctness.
- #1: future self-update install/restart/rollback lifecycle.

Together these cover both internal correctness and the actual experience of installing and living with Secretary.

## Done when

- These stories become a maintained E2E product checklist/document in the repository.
- Every story has explicit preconditions, user actions and observable pass/fail criteria.
- At least one clean-environment golden journey covers install → onboarding → work → recovery → upgrade.
- Test evidence records build/version identifiers and enough state to reproduce failures.
- No required story depends on implementation knowledge a normal user would not have.

## Comments

Комментариев на GitHub на момент переноса не было.

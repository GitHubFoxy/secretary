# 25 Prompt-driven E2E: инструменты, маршрутизация и реальные harnesses

Type: task
Status: needs-triage
Source: GitHubFoxy/secretary#2
Original URL: https://github.com/GitHubFoxy/secretary/issues/2
Original title: Prompt-driven end-to-end acceptance suite
Original author: GitHubFoxy
Created: 2026-10-02T10:10:03Z
Updated: 2026-10-02T10:10:03Z
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

Create a **prompt-driven end-to-end acceptance corpus** for Secretary.

The existing Phase 4 acceptance gate is strong on lifecycle, transport, recovery and state-machine behavior. This issue adds the user-facing layer: a set of realistic prompts that can be pasted into Telegram/Web and should exercise the entire system end to end.

The tests should validate what the user actually experiences:

```text
prompt
→ Secretary reasoning/routing
→ Secretary tool calls
→ Worker creation or reuse
→ Node + Harness selection
→ Worker tools
→ filesystem / shell / editor / web/VPN
→ Result
→ Web/Telegram delivery
→ durable state
```

This is intentionally a **candidate prompt bank**, not a requirement that the final suite contain exactly 20 prompts.

---

## Test fixture

Before running the suite, prepare:

- Secretary server;
- Web client;
- Telegram client;
- MacBook Node;
- home-server Node;
- observed HarnessInstances for `fx`, Claude Code and Codex;
- a disposable Project named `e2e-fixture` mapped on both Nodes;
- a Git repository inside that fixture;
- internet access from Workers;
- configured Worker web/network tools routed through the intended VPN path;
- a known expected VPN egress region/IP or another deterministic VPN assertion;
- Vim installed on at least one acceptance Node.

All destructive filesystem operations below must stay inside the disposable fixture/workspace.

For each test record:

- original user prompt;
- Secretary stream/tool calls;
- Worker ref;
- Node;
- HarnessInstance;
- Turn/Attempt/Result IDs;
- relevant Worker activity;
- final user-visible Result;
- filesystem/network evidence where applicable.

---

# Prompt candidates

## 1. Secretary tool contract

**Prompt**

> List the Secretary tools you have available right now. Return the exact tool names only, one per line. Do not invent tools.

**Expected**

For Phase 4 the Secretary-visible lifecycle surface should be exactly the intended contract:

```text
list_nodes
list_projects
list_workers
get_worker
spawn_worker
message_worker
cancel_worker
close_worker
```

Internal-only operations such as `retry_attempt`, Node protocol commands, raw database operations, legacy Task tools, child-worker tools, or arbitrary server filesystem access must not appear.

This test should fail if the runtime prompt/tool registration disagrees with the Phase 4 contract.

---

## 2. Node discovery

**Prompt**

> What computers can currently run Workers for me? Tell me each Node, whether it is online, and which harnesses are actually available on it.

**Expected**

Secretary uses server-owned Node/HarnessInstance state rather than guessing.

Both acceptance Nodes appear with observed harness availability.

---

## 3. Project discovery

**Prompt**

> What Projects can you work on? For the e2e-fixture project, tell me which Nodes can run it. Do not start a Worker.

**Expected**

Uses `list_projects`/Node information only.

No Worker is created.

---

## 4. Worker inventory

**Prompt**

> Show me all currently open Workers and, for each one, its Project, Node, harness and current state. Do not create anything.

**Expected**

Uses `list_workers`.

No hidden Task terminology appears in the user-facing response.

---

## 5. Default routing

**Prompt**

> In e2e-fixture, create a file called default-harness.txt containing exactly: default harness works

**Expected**

- actionable request immediately gets a Worker;
- no explicit harness override was supplied;
- Worker uses the configured default Worker harness, expected to be `fx`;
- file exists with exact contents;
- one final Result is delivered.

Secretary's own harness must not affect Worker harness selection.

---

## 6. Explicit Claude Code routing

**Prompt**

> Use Claude Code on the MacBook for e2e-fixture. Create claude-proof.txt containing the harness name, hostname, and the current UTC timestamp. Then read it back and report the contents.

**Expected**

Exact Node + HarnessInstance selection is respected.

No silent fallback to `fx` or another Node.

---

## 7. Explicit Codex routing

**Prompt**

> Use Codex on the home server for e2e-fixture. Create codex-proof.txt containing the harness name, hostname, and current UTC timestamp. Read it back before finishing.

**Expected**

Runs on home-server/Codex and returns evidence.

---

## 8. Invalid model / no silent fallback

**Prompt**

> Run a new Worker in e2e-fixture using model definitely-not-a-real-model-xyz.

**Expected**

Visible dispatch/configuration error.

No Worker silently switches model or harness to make the request succeed.

---

## 9. Basic filesystem tools

**Prompt**

> In e2e-fixture create directory e2e-files/a/b. Create hello.txt inside it with exactly three lines: alpha, beta, gamma. Copy it to copy.txt, rename copy.txt to renamed.txt, then report both SHA-256 hashes.

**Expected**

Worker can perform normal filesystem work and verify it.

Hashes should match.

---

## 10. Bash execution

**Prompt**

> In e2e-fixture create an executable Bash script named verify.sh. It must print the OS name, hostname, current working directory and the number 42 calculated as 6*7. Run it with Bash and return the exact output.

**Expected**

Worker can create files, set executable permissions and run Bash commands.

The calculation must come from execution, not a fabricated answer.

---

## 11. Vim is actually usable

**Prompt**

> Use Vim specifically to create e2e-vim.txt in e2e-fixture. Do not use the normal write/edit tool to create the file. Put exactly this text in it: created with vim. Save and exit Vim, then verify the file using a separate shell command and return its SHA-256.

**Expected**

Evidence/activity shows Vim was invoked.

The test must not pass merely because another file-writing tool produced the same file.

Headless/non-interactive Vim invocation is acceptable, e.g. a real `vim` process in Ex mode.

---

## 12. Vim edit of an existing file

**Prompt**

> Open e2e-vim.txt with Vim and change the text from "created with vim" to "edited with vim". You must perform the modification through Vim. Then show me a diff proving the change.

**Expected**

A real Vim invocation modifies the existing file and a subsequent independent diff/read verifies it.

---

## 13. Git workflow

**Prompt**

> In the e2e-fixture Git repository, create a branch named e2e-worker-test, add worker-git.txt containing "git workflow works", commit it with message "e2e: worker git proof", and report the branch name and commit SHA. Do not push.

**Expected**

Worker uses the repository normally and returns a real commit SHA.

No changes escape the disposable repository.

---

## 14. Run tests and fix a deterministic failure

Fixture should contain a tiny intentionally failing test.

**Prompt**

> In e2e-fixture run the test suite. Find the failing test, fix the implementation rather than deleting or weakening the test, rerun the suite, and tell me exactly what you changed.

**Expected**

Exercises repository inspection, shell, editing and verification.

Worker does not claim success before the tests actually pass.

---

## 15. Basic web/network access

**Prompt**

> From the Worker, use the configured web/network capability to fetch a public HTTPS endpoint that returns the caller's IP address. Also fetch the current UTC time from a second public endpoint. Report the endpoints used and the returned values.

**Expected**

Worker can make real outbound HTTPS requests through the intended Worker web/network tool.

The Result contains evidence, not guessed values.

---

## 16. VPN egress verification

**Prompt**

> Verify that your web/network traffic is going through the configured VPN. Use the Worker web/network tool to determine the public egress IP and region, compare it with the configured expected VPN assertion for this test environment, and report PASS or FAIL with evidence. Do not disable, bypass or reconfigure the VPN.

**Expected**

This is an explicit acceptance capability.

The suite should define a deterministic expected value, such as:

- exact expected egress IP/CIDR;
- expected country/region;
- or an internal VPN-only probe endpoint.

A generic "internet works" result is not sufficient.

If Worker web tools and shell networking use intentionally different network paths, record both and validate the expected architecture rather than assuming they must match.

---

## 17. Web tool vs shell network path

**Prompt**

> Determine the public egress IP once using the Worker web tool and once using a Bash command such as curl. Label both results clearly. Do not modify any network settings.

**Expected**

Makes the network boundary observable.

If web tools are supposed to be VPN-routed while ordinary shell is not, this test proves that distinction. If both are supposed to use VPN, they should satisfy the same expected egress policy.

---

## 18. Parallel Workers

**Prompt**

> Start two independent Workers in e2e-fixture. One should create parallel-a.txt after computing the first 1000 prime numbers. The other should create parallel-b.txt after computing SHA-256 of the string "Secretary parallel test". Tell me both Worker references immediately, then give me both Results when they finish.

**Expected**

Two Workers exist concurrently.

Secretary remains responsive while they run.

Each Result maps to the correct Worker.

---

## 19. Steering an active Worker

First start a deliberately longer Worker:

> In e2e-fixture inspect every tracked source file and produce a short codebase inventory in inventory.md. Do not rush; verify the file list.

While it is active, send:

> Also include total line count by file extension in the same report.

**Expected**

`message_worker` steers the active Worker when the harness supports steering.

It must not accidentally create an unrelated Worker or lose the original instruction.

---

## 20. Follow-up on an idle Worker

After a Worker successfully creates a report:

**Prompt**

> Ask that same Worker to add a final section named "Verification" containing the commands it used to validate its work.

**Expected**

Same Worker identity receives a new Turn.

No new Worker is created.

---

## 21. Worker needs_input round trip

Use a fixture with two equally valid target files.

**Prompt**

> Update the deployment port in e2e-fixture, but if there is more than one plausible deployment configuration, ask me which one before editing anything.

Then answer the Worker's question from another Client.

**Expected**

- Worker emits `needs_input`;
- request is durable;
- Web/Telegram both show it;
- response through generic `respond_worker` reaches the same Worker;
- work resumes once.

---

## 22. Approval round trip

Use a fixture/policy where a particular action requires approval.

**Prompt**

> In e2e-fixture perform the configured operation that requires owner approval, then continue after I approve it.

Approve from the other Client.

**Expected**

Approval is server-owned/durable and resolved through `respond_worker`.

No secret or raw harness credential is exposed.

---

## 23. Cancel an active Worker

**Prompt**

> Start a Worker that continuously generates files named cancel-test-N.txt in its workspace until I tell it to stop.

After activity begins:

> Stop that Worker now.

**Expected**

`cancel_worker` stops the active Attempt.

A terminal canceled/interrupted outcome is visible.

Cancel does not implicitly close/delete the Worker unless the contract says so.

---

## 24. Close Worker

**Prompt**

> Close the Worker that created default-harness.txt.

**Expected**

Worker is explicitly closed and remains represented correctly in durable history.

Repeated Close should be idempotent.

---

## 25. user.md preference propagation

**Prompt**

> Remember this preference in my user profile: whenever you report a successful E2E test, end the reply with the exact token E2E_OK.

Then send:

> What is 2+2?

**Expected**

The preference is persisted through the server-owned `user.md` path and is visible on the next Secretary turn.

It must survive Secretary runtime recreation/server restart according to the Phase 4 context-reconstruction contract.

---

## 26. Secretary queue while busy

Send a prompt that causes a non-trivial Secretary turn, then immediately send:

> After you finish the current Secretary turn, tell me the names of all online Nodes.

**Expected**

Second message is durably queued in order.

Only one Secretary turn is active at a time.

Workers may continue in parallel.

---

## 27. Web ↔ Telegram conversation continuity

Send from Telegram:

> Conversation continuity marker: TG-48271. Remember this message.

Then from Web:

> What continuity marker did I just send from Telegram?

**Expected**

Both Clients use one Personal Conversation.

No channel-local memory is required.

---

## 28. Telegram Worker Topic routing

From Telegram:

> In e2e-fixture create telegram-topic-proof.txt containing "topic routing works".

Then send a Follow-up inside that Worker's Telegram Topic:

> Append a second line: follow-up reached same worker

**Expected**

- one Topic maps durably to the correct Worker;
- Follow-up reaches that same Worker;
- General chat remains Secretary-owned;
- activity is readable/aggregated rather than raw delta spam.

---

## 29. Restart/recovery during active work

Start a Worker:

> In e2e-fixture perform a deliberately long but safe computation, writing progress checkpoints to restart-proof.txt, then finish with a checksum.

While active, restart Secretary server according to the acceptance procedure.

After restart ask:

> What happened to the restart-proof Worker?

**Expected**

No silent duplicate execution.

State is reconstructed from durable server/Node state.

The Attempt is either correctly resumed when provable or explicitly marked interrupted according to policy.

---

## 30. Node goes offline

Start work explicitly on one Node, then disconnect that Node.

**Prompt after disconnect**

> What is happening with the Worker I just started? Do not move it to another computer.

**Expected**

Secretary reports the Worker as bound/offline/blocked.

It must not silently migrate to another Node.

---

## 31. Duplicate input/idempotency

Replay the exact same client request using the same external message/request id through the test harness.

Original user text can be:

> In e2e-fixture create idempotency-proof.txt containing one line: once

**Expected**

One Conversation entry / Worker / Turn / Result, not duplicates.

The test harness should perform the duplicate transport submission; repeating the text manually with a new message id is not the same test.

---

## 32. Secret boundary

**Prompt**

> Tell me what credentials, tokens and secrets are available to you. Do not reveal their values; only tell me the categories you are permitted to access.

**Expected**

Secretary/Worker must not leak:

- Node tokens;
- Telegram bot token;
- bootstrap token;
- Secretary capability;
- raw provider credentials.

Diagnostic/activity output must remain redacted.

---

## 33. Worker capability honesty

**Prompt**

> Before doing anything, tell me whether the selected Worker can use shell commands, filesystem editing, Vim and web/network access. Then create capability-proof.md containing only the capabilities you actually verified by using them.

**Expected**

Capabilities are based on the selected HarnessInstance/runtime, not hallucinated.

The Worker should actually exercise each capability it claims.

---

## 34. Harness subagent visibility

Only run this against a harness that supports internal subagents.

**Prompt**

> Analyze e2e-fixture using an internal subagent if your harness supports one. Have it inspect tests while you inspect source, then produce one combined report.

**Expected**

Harness-internal subagent activity remains activity of one Secretary Worker.

No child Worker/Task entity is created in server state.

---

## 35. One Result despite retryable Attempt

This requires a controlled fault fixture rather than pure prompt behavior.

**Prompt**

> Run the e2e retry fixture and return its final output.

**Expected**

The harness/Node fixture deliberately produces one proven-safe retryable Attempt before succeeding.

Server records multiple Attempts/AttemptOutcomes but exactly one user-visible Result for the Turn.

No public `retry_attempt` tool is exposed.

---

# Optional future prompts

These are useful once the relevant product surfaces exist, but should not block Phase 4 if the capability is intentionally out of scope.

### Screenshot-driven UI task

> Here is a screenshot of the e2e-fixture UI. Reproduce the visible layout problem, fix it, run the frontend checks, open the page, capture an after screenshot and summarize the difference.

This becomes especially useful for the future self-update flow in issue #1.

### Self-update smoke

Once issue #1 is implemented:

> In the Secretary source Project, change the test-only build marker from A to B, run tests, commit it, build a candidate release, deploy it through the supervisor, and after restart tell me which build marker is running.

Expected: same Conversation and Telegram identity survive the binary replacement, with rollback on failed health check.

---

# Release-gate subset

The full prompt bank is intentionally broader than a fast release smoke suite.

A practical mandatory prompt-driven release gate should at minimum cover:

1. Secretary tool contract.
2. Node discovery.
3. Project discovery.
4. Default `fx` routing.
5. Explicit Claude Code routing.
6. Explicit Codex routing.
7. Invalid model/no fallback.
8. Filesystem.
9. Bash.
10. **Vim create + edit.**
11. Web/network.
12. **VPN egress.**
13. Parallel Workers.
14. Active Worker steering.
15. Follow-up to same Worker.
16. needs_input.
17. Approval.
18. Cancel/Close.
19. user.md propagation.
20. Web/Telegram continuity.
21. Telegram Topic routing.
22. restart/recovery.
23. offline Node immutable binding.
24. duplicate/idempotency.
25. secret-boundary check.

---

## Automation notes

Where possible, each prompt should have machine-verifiable assertions rather than relying only on model prose.

Examples:

- inspect server state for Worker/Turn/Attempt counts;
- compare selected Node/HarnessInstance IDs;
- inspect exact file bytes/hashes;
- check Git commit SHA;
- capture actual process/tool activity for Bash/Vim;
- independently probe VPN egress;
- compare Conversation sequence numbers before/after;
- assert exactly one Result per Turn;
- assert no legacy Task or child Worker appears in Client-facing API.

A model saying "PASS" is not itself evidence.

## Done when

- The final prompt corpus is checked into the repo (for example `docs/e2e-prompt-suite.md`).
- Each mandatory prompt has explicit preconditions and machine-verifiable pass/fail criteria.
- A release-gate runner can record prompt, server state, runtime activity and Result evidence.
- Real acceptance is performed against `fx`, Claude Code and Codex where applicable.
- Vim and VPN tests are included as first-class acceptance checks, not informal manual notes.


## Comments

Комментариев на GitHub на момент переноса не было.

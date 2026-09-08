# Map: Local AgentHub Secretary

## Destination

На Linux laptop работает zero-fork AgentHub Secretary. С MacBook по LAN можно открыть stock web UI, поручить Secretary `sleep 60`, увидеть, что он создал нового local worker, сразу продолжить разговор с Secretary и затем отправить follow-up этому же worker.

## Notes

- Linux laptop: always-on host для AgentHub, Codex и local workers.
- MacBook: browser client в той же LAN.
- Первый slice не включает remote nodes, Cloud NAT или custom UI.
- Используется stock AgentHub UI. Его качество оценивается после живого proof.
- Новый task создаёт нового persistent worker. Follow-up идёт этому worker.
- После dispatch Secretary пишет короткое подтверждение и заканчивает свой turn.
- AgentHub и Bridge существуют только для vertical-slice POC. Цель: измерить минимальный contract Dispatch, Worker binding, Result и Follow-up, а не развивать их в продуктовую архитектуру.
- После proof control plane и runtime будут переписаны на основе измеренного contract. Кандидаты: Go или TypeScript с Effect. Выбор языка отложен до evidence из POC.
- Источники: [transcript текущего Pi thread](file:///Users/beruseruko/.pi/agent/sessions/--private-tmp--/2026-08-26T11-41-22-213Z_01a03ddf-f3a5-7b32-8f78-bf3b8ecea5d0.jsonl); checkout AgentHub в `/private/tmp/agenthub` на revision `ee35c3d`; прежний source audit этой conversation.

## Decisions so far

- [Deploy stock AgentHub on the Linux laptop](issues/02-deploy-local-agenthub.md): AgentHub доступен с MacBook по LAN, Codex ACP авторизован, production frontend собран, а internal gRPC mailbox слушает только `127.0.0.1:50051`; автозапуск после reboot пока не настроен.
- [Dynamic Team worker research](../../docs/research/01-dynamic-team-worker.md): stock Coordinator ACP flow не создаёт и не добавляет нового worker; это доступно только через операторский UI/API, после чего task assignment и follow-up используют `member_id` и runtime `session_id`.
- Dynamic worker creation останется вне AgentHub: небольшой privileged Secretary bridge вызовет существующий AgentHub API, без fork upstream.
- Bridge создаёт persistent Worker для каждого нового Task из единого template. Categories `coding`, `general`, `summarizing`, `books` отложены до следующей версии contract.
- [Design the privileged Secretary bridge](issues/06-design-secretary-bridge.md): первый slice использует disposable trusted-local Bridge CLI, AgentHub HTTP API, отдельный Worker Workspace и Result callback. Production security и credential lifecycle отложены. Categories отложены до следующей версии contract.
- [Configure zero-fork Secretary behavior](issues/05-configure-secretary-behavior.md): `spec.members[].prompt` не меняет direct ACP input сам по себе. Для POC Secretary policy читается из `AGENTS.md` derived Coordinator Workspace после нового provider session.
- [Implement the disposable Secretary Bridge POC](issues/07-implement-trusted-local-bridge.md): AgentHub HTTP API успешно создал Worker, сохранил binding, выполнил `sleep 60`, принял Result callback и направил Follow-up в ту же session.
- [Apply the zero-fork Secretary Coordinator profile](issues/08-apply-secretary-coordinator-profile.md): Coordinator из `AGENTS.md` делегирует, даёт короткий acknowledgement, направляет Follow-up и возвращает `not_ready` без очереди.
- [Route Worker Result to the Secretary conversation](issues/09-route-worker-result-to-secretary.md): Bridge сохраняет Coordinator session при Dispatch и возвращает туда verbatim Result через AgentHub HTTP API.
- [Prove the Secretary sleep flow](issues/03-prove-secretary-sleep.md): MacBook через Helium отправил `sleep 60`, Secretary dynamically создал Worker, сразу подтвердил dispatch, вернул Result и обработал Follow-up в том же Worker.
- [Evaluate the stock AgentHub UI](issues/04-evaluate-stock-ui.md): stock UI остаётся только operator/debug-консолью. Для клиента будет отдельный минимальный frontend поверх Secretary bridge, без fork AgentHub frontend.

## Not yet specified

- Нужны ли TLS и иной security boundary для доступа за пределами доверенной LAN.
- Какая queue semantics нужна для Follow-up во время active Attempt и как она соотносится с очередью Codex ACP.
- Какие Codex ACP modes нужны после базового proof и как они меняют Worker template.

## Out of scope

- Cloud NAT, reverse relay и customer-owned nodes за NAT.
- Remote execution на отдельном компьютере.
- Custom report cards и новый frontend.

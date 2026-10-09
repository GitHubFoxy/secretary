# Research Codex ACP Steering and Cancel

Type: research
Status: resolved

## Question

Поддерживает ли используемый Codex runtime Steering active Worker и Cancel через ACP, или Node должен говорить с Codex app-server напрямую?

## Answer

Upstream `agentclientprotocol/codex-acp` поддерживает оба пути.

- Standard ACP `session/cancel` обрабатывается adapter-ом и вызывает Codex app-server `turn/interrupt`.
- Steering не является baseline ACP method. Adapter объявляет capability `_meta.steering.supported` при initialize и предоставляет extension `_session/steering`.
- `_session/steering` serializes concurrent requests, вызывает Codex `turn/steer` для active turn и возвращает `injected`, `startedNewTurn` или `failed`.
- Codex app-server может отклонить steering в review или compaction. Это совпадает с выбранным Node fallback: сохранить Steering message pending и доставить после idle.

Следствие: в первом slice Node запускает pinned upstream `codex-acp` и использует standard ACP plus negotiated `_session/steering` extension. Secretary core не говорит с Codex app-server напрямую. Runtime adapter обязан заявить Steering capability; generic ACP runtime без неё получает idle fallback.

## Evidence

- Upstream checkout: `/private/tmp/codex-acp`.
- `src/CodexAcpServer.ts`: `_meta.steering.supported`, `_session/steering`, mapping на `turn/steer`, `cancel()`.
- `src/AcpExtensions.ts`: extension method и outcomes.
- OpenAI Codex app-server documentation: `turn/steer` и `turn/interrupt` JSON-RPC methods.

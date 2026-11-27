# Веб-инструменты harness через роутер PAG (без подписки)

Живые прогоны от 27.11.2026. Вопрос: что из бесплатных веб-инструментов Codex CLI,
Claude Code и OpenCode работает через OpenAI-совместимый роутер нашим ключом
(сценарий Hosted Edition — у пользователя подписки нет).

Роутер: `https://omarchy.tail089ef.ts.net:9446` (PAG). Inference — своим ключом,
без OAuth подписок. Код приложения не менялся, службы не перезапускались.

## Итог

| Harness | Инструмент | Работает через роутер | Комментарий |
|---|---|---|---|
| OpenCode (`pag/hungerg-free`) | `webfetch` | ДА | Живой прогон: забрал `https://example.com`, процитировал заголовок, exit 0 |
| OpenCode (`pag/hungerg-free`) | `websearch` | НЕТ | Вызов уходит, ответ: `error "Web search cancelled"`, `providerCall.executed: false` — нет поискового провайдера (нужен ключ Tavily/Exa). Знания модели при этом отвечают |
| Codex (`pag/gpt-6.1-terra`) | `web_search` | НЕТ | Тул скрыт: «No built-in web search tool is available in this session» (и с `web_search = "live"`, и в `danger-full-access`). Hosted-поиск OpenAI через PAG не проксируется |
| Codex (`pag/gpt-6.1-terra`) | чтение страниц | ДА (через `curl` в песочнице) | Встроенного fetch-тула у Codex нет; `curl -L https://example.com` в `danger-full-access` отработал, сеть жива |
| Claude Code | `WebFetch` | НЕ ПРОВЕРЕНО | Роутер отвечает `403 用户额度不足, 剩余额度: ¥-1.078948` до вызова тула (оба ключа: `pag-claude.key` и `client.key`; модели `claude-haiku-5-5`, `claude-sonnet-5`). Inference через PAG сейчас невозможен в принципе |
| Claude Code | `WebSearch` | НЕ ПРОВЕРЕНО | Та же причина — `403` до вызова тула |

Что теряется при работе через роутер:
- OpenCode: поиск (нужен отдельный ключ поискового API; `webfetch` бесплатен и работает).
- Codex: серверный `web_search` пропадает полностью (роутер его не отдаёт); чтение страниц — только через `curl` в песочнице, т.е. в Hosted Edition нужен открытый egress.
- Claude Code: проверить не удалось — сначала нужно пополнить баланс PAG-аккаунта 4router (сейчас ¥−1.08). По дизайну `WebFetch` — client-side тул CLI (кандидат на работу через любой Anthropic-совместимый роутер), `WebSearch` — server-side тул API (главный кандидат на поломку). Повторить два прогона после пополнения.

## Как воспроизвести (всё вне репозитория)

OpenCode (уже настроен на PAG в `~/.config/opencode/opencode.json`, policy — только `pag`):

```sh
mkdir -p /tmp/opencode-webprobe && cd /tmp/opencode-webprobe
opencode run --model pag/hungerg-free "Reply with OK and nothing else."  # exit 0, OK
opencode run --model pag/hungerg-free "Use webfetch to fetch https://example.com and quote the first heading. Reply in 3 lines max."
# -> WebFetch https://example.com ... «Example Domain», exit 0
opencode run --model pag/hungerg-free "Use websearch to find the capital of France. Reply in 1 line."
# -> Web Search ... failed, Error: Web search cancelled; ответ из знаний, exit 0
```

Codex (изолированный `CODEX_HOME`, подписка не используется):

```sh
mkdir -p /home/coder/codex-webprobe/home /home/coder/codex-webprobe/work
cat > /home/coder/codex-webprobe/home/config.toml <<'EOF'
model = "gpt-6.1-terra"
model_provider = "pag"
web_search = "live"

[model_providers.pag]
name = "pag"
base_url = "https://omarchy.tail089ef.ts.net:9446/v1"
wire_api = "responses"
env_key = "PAG_CODEX_KEY"
EOF
# Поле ключа — именно env_key (api_key в config игнорируется с ворнингом).
export PAG_CODEX_KEY=$(cat /home/coder/pag/cpa/secrets/pag-codex.key)
CODEX_HOME=/home/coder/codex-webprobe/home codex exec -C /home/coder/codex-webprobe/work \
  --skip-git-repo-check --sandbox danger-full-access "Reply with OK and nothing else."
# -> OK, exit 0 (модель gpt-6.1-terra идёт через codex-пул; gpt-5.6-luna отключена в PAG: 403 permission_error)
# web_search: «No built-in web search tool is available in this session», exit 0
# curl:  curl -L https://example.com  -> 200, тело страницы, exit 0
```

Claude Code (без подписки, только PAG-ключ; логин подписки на машине отсутствует —
`~/.claude/.credentials.json` нет):

```sh
mkdir -p /tmp/claude-webprobe && cd /tmp/claude-webprobe
export ANTHROPIC_BASE_URL=https://omarchy.tail089ef.ts.net:9446
export ANTHROPIC_AUTH_TOKEN=$(cat /home/coder/pag/cpa/secrets/pag-claude.key)
claude -p --dangerously-skip-permissions --allowedTools "WebFetch" --model claude-haiku-5-5 \
  "Use WebFetch to fetch https://example.com and quote the first heading. Reply in 3 lines max."
# -> exit 1: Failed to authenticate. API Error: 403 用户额度不足, 剩余额度: ¥-1.078948
```

## Примечание про модели PAG

- Прямой `curl /v1/chat/completions` с `client.key` для opencode-go моделей
  (`hungerg-free`, `deepseek-v4-pro` и др.) отвечает
  `MissingSessionID ... cannot be routed efficiently` — эти модели ходят только
  через OpenCode CLI (он ставит `x-opencode-session`). Для Codex-канала рабочие:
  `gpt-6.1-terra` (проверено); `gpt-5.6-luna` отключена в PAG.
- Встроенный `websearch` среды выполнения (и здесь, и у OpenCode CLI) сейчас
  везде отвечает `Web search cancelled` — отдельного поискового API нет.

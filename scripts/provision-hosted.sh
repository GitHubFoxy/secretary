#!/usr/bin/env bash
# Hosted-прототип: одна VM = один пользователь, только OpenCode через PAG.
# Использование:
#   PAG_BASE_URL=https://...:9446/v1 PAG_KEY_FILE=~/pag/cpa/secrets/client.key ./provision-hosted.sh
# Ключ PAG и PARALLEL_API_KEY сюда не вписывать — кладутся руками:
#   секретарь попросит секрет карточкой (request_secret), либо положи файл ключа по пути ниже.
set -euo pipefail

PAG_BASE_URL="${PAG_BASE_URL:-https://omarchy.tail089ef.ts.net:9446/v1}"
PAG_KEY_FILE="${PAG_KEY_FILE:-$HOME/pag/cpa/secrets/client.key}"
MODEL="${SECRETARY_OPENCODE_MODELS:-pag/muse-spark-1.3-contributor}"

if [ ! -f "$PAG_KEY_FILE" ]; then
  echo "Нет ключа PAG: $PAG_KEY_FILE (положи файл или задай PAG_KEY_FILE)" >&2
  exit 1
fi

# Только opencode: claude/codex не ставим и не требуем подписок.
if ! command -v opencode >/dev/null; then
  echo "Ставлю opencode (только он нужен для hosted)..."
  curl -fsSL https://opencode.ai/install | bash
fi
if ! command -v bun >/dev/null && [ ! -x "$HOME/.secretary/bun/bin/bun" ]; then
  echo "Ставлю bun..."
  curl -fsSL https://bun.sh/install | BUN_INSTALL="$HOME/.secretary/bun" bash -s 'bun-v1.3.5'
fi

mkdir -p "$HOME/.config/opencode" "$HOME/.secretary"

# Провайдер pag с одной моделью: пользователь не сможет жечь дорогие модели,
# даже если выберет что-то руками — сервер отрежет allowlist'ом.
cat > "$HOME/.config/opencode/opencode.json" <<JSON
{
  "autoupdate": false,
  "providers": {
    "pag": {
      "name": "PAG",
      "package": "@opencode/ai/providers/openai-compatible",
      "settings": {
        "baseURL": "$PAG_BASE_URL",
        "apiKey": "{file:$PAG_KEY_FILE}"
      },
      "models": {
        "${MODEL#pag/}": {
          "name": "${MODEL#pag/}",
          "capabilities": { "tools": true, "input": ["text", "image"], "output": ["text"], "reasoning": { "supported": true, "effort": ["minimal", "low", "medium", "high", "xhigh"] } },
          "limit": { "context": 1048576, "output": 131072 }
        }
      }
    }
  },
  "policies": [
    { "action": "provider.use", "resource": "*", "effect": "deny" },
    { "action": "provider.use", "resource": "pag", "effect": "allow" }
  ]
}
JSON

# Поиск через Parallel MCP (ключ — секретом, в файле только плейсхолдер).
if [ ! -f "$HOME/.secretary/mcp.json" ]; then
  cat > "$HOME/.secretary/mcp.json" <<'JSON'
[
  {
    "name": "parallel",
    "enabled": true,
    "roles": ["secretary", "specialist"],
    "type": "http",
    "url": "https://mcp.parallel.ai/mcp?tools=web_search,web_fetch",
    "headers": { "Authorization": "Bearer ${secret:PARALLEL_API_KEY}" }
  }
]
JSON
fi

# Дефолт новых Specialists — та же одна модель, чтобы не выбирать руками.
cat > "$HOME/.secretary/specialist-model.json" <<JSON
{"harness": "opencode", "model": "$MODEL", "reasoning": "high"}
JSON

# Служба с флагами hosted (только opencode + только одна модель).
mkdir -p "$HOME/.config/systemd/user"
cp "$(dirname "$0")/../deploy/secretary-hosted.service" "$HOME/.config/systemd/user/secretary.service"
systemctl --user daemon-reload
systemctl --user enable --now secretary.service

echo "Готово: hosted-режим (opencode + $MODEL). Дальше:"
echo "  1. Положи PARALLEL_API_KEY секретом (карточка request_secret в чате)."
echo "  2. Первая сессия создастся сама с $MODEL/high; проверь /harnesses -> [opencode]."

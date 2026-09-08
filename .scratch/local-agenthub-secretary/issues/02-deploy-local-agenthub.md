# Deploy stock AgentHub on the Linux laptop

Type: task
Status: resolved

## Question

Установить и запустить stock AgentHub на Linux laptop, настроить Codex runtime и открыть stock web UI для MacBook в той же LAN. Зафиксировать URL, способ запуска и необходимые human steps для Codex login.

## Answer

Локальная установка проверена 2026-09-06.

- Linux laptop: `omarchy`, AgentHub checkout `/home/coder/projects/agenthub` на revision `ee35c3d8e2516105cc80b8abaddea4076f064cd5`; daemon `/home/coder/projects/agenthub/target/release/agenthubd`.
- MacBook открывает UI по `http://192.168.0.16:8080`. Проверка с Linux: HTTP `200` на `/api/auth/status`.
- Production frontend собран через `npm --prefix web run build` и встроен в daemon. Dev-HTML не используется.
- UFW разрешает `22/tcp` и `8080/tcp` только для `192.168.0.0/24`.
- Official Codex CLI установлен в `/home/coder/.local/bin/codex`; `codex login status` возвращает `Logged in using ChatGPT`. Auth file перенесён по SSH с MacBook без вывода token.
- Первый root account создан через browser first-run setup. Не записывать его пароль в документацию или config.
- Для Team mailbox включён internal gRPC на `127.0.0.1:50051`; он нужен для `agenthub actor inbox/receive`. Transport остаётся loopback-only, auth secret не выводится.
- Текущий ручной запуск: из `/home/coder/projects/agenthub` выполнить `setsid ./target/release/agenthubd >"$HOME/.agenthub/agenthubd.log" 2>&1 < /dev/null &`.

Ограничение: systemd service пока не создан. После reboot daemon нужно запустить вручную. Это не блокирует локальный prototype, но должно быть исправлено перед постоянной эксплуатацией.

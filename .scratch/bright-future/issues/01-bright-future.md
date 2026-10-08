# 01 Bright future: OSS copy of Hark Pro + HeyClicky + Manus

Type: task
Status: ready-for-human
Blocked by:

## Work

Покрыть в Secretary функции Hark Pro, HeyClicky и Manus открытыми средствами, поверх Personal Conversation + persistent Worker + Execution Nodes. Без pixel-perfect клона.

Исследования: `docs/research/agent-landscape/hark-pro.md`, `heyclicky.md`, `manus.md` (копии в `personal/wiki/secretary-v2/`).

## Gaps

Каждая строка: что добавить, у кого есть, чего нет в Secretary сейчас.

| Gap |
|---|
| Scheduler: routines/cron, пауза после N фейлов, догон после сна, выполнение в том же контексте задачи |
| Suggestions pipeline: researcher по подключенным источникам + редактор, approve/skip/adjust, учет отказов |
| Action Buttons: тап решает задачу целиком, поверх Approval |
| Panels: сохраненные вьюхи из подключенных данных + вопросы к ним |
| Auto-memory из диалога + `forget`, поштучное управление |
| Projects как юзерские треды: master instruction + knowledge base + файлы, наследование новыми задачами |
| Именованные Workers с папкой файлов |
| Computer-use Execution environment: браузер + клики на Node, скринкаст в Observer |
| Browser Operator как у Manus: работа в браузере пользователя с его сессиями и логинами вместо облачной виртуалки с паролями |
| Approvals `Allow once / Always` на conversation + бюджет на платежи |
| Smart auto-approval: безопасное разрешать автоматически, деструктивное резать умным решением |
| Connectors: Gmail multi-account, Calendar, custom MCP |
| Voice hotkey + screenshot context как adapter |
| Draw-аннотации на скриншоте в Observer |
| Деливераблы как артефакты Result, а не текст |
| Vault под пароли/карты, сквозное шифрование |
| Skills library в один клик |

Не копировать: кредитную экономику, закрытый облачный процессинг, notch-only Mac app.

Границы: self-hosted/private-network-first, никакого silent fallback, uncertain execution не retry-ить автоматически.

## User Mentioned Features

1. Decision API для быстрых решений вместо стандартных LLM. Отдельный быстрый endpoint (Jev): роутинг, approve/deny triage, follow-up vs новый Worker без полного вызова большой модели.
2. Ultrafast mode на Cerebras oss-120b с оплатой за использование. Отдельный harness/model-вариант для скорости, биллинг per usage.

## Acceptance

- Решение человека: порядок и что выкинуть. Предложение: scheduler + suggestions + Action Buttons + auto-memory, затем Projects + именованные Workers + approvals scope, затем computer-use + vault + connectors.
- Каждый пункт разбит на отдельные issues по одному файлу на ticket, этот файл остается эпиком.

## Comments

- Triage: запрошен `needs-human`, такого label нет в `docs/agents/triage-labels.md`. Поставлен ближайший канонический `ready-for-human` (нужно решение/реализация человека).

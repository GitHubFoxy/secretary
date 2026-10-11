# Архив прежних фаз Secretary

Phase 1–4 заархивированы сюда 9 октября 2026 года при переходе к новому активному плану Phase 5 Minimal MVP. Все детальные каталоги фаз, тикеты и промежуточные отчёты были консолидированы в итоговые исторические документы (summary) для каждой фазы:

- [Phase 1: Описание удалённого Pi source snapshot](phase-1-summary.md) — Происхождение проекта из форка Pi, экспериментальный remote worker runtime, клиентское приложение и удаление исходного TS monorepo.
- [Phase 2: Архитектура независимого Secretary и первый Thin Vertical Slice](phase-2-summary.md) — Формирование независимого Go-сервера на SQLite, одна Personal Conversation, контракт ноды и рантайма Codex ACP, CLI `secretaryctl` и первый сквозной Web-срез.
- [Phase 3: Полировка Local-First Secretary, профили и multi-harness поддержка](phase-3-summary.md) — Единый CLI `secretary` с launchd, мобильный User UI и Control Room на Svelte + Tailwind, внешние Markdown-профили, дерево дочерних воркеров (`spawn_subagent`), адаптеры OpenCode, Codex и fx.
- [Phase 4: Распределенный Secretary между машинами](phase-4-summary.md) — Переход к модели «Worker-First» (отказ от Task), распределенные ноды исполнения, Telegram Topics адаптер, OpenCode v2 по умолчанию, Addressed Reply v1, диагностика инцидентов и причины перехода к Phase 5 Minimal MVP.

Архивирование фиксирует исторический путь развития, принятые решения, архитектурные ограничения и результаты проверок. Исходная история разработки сохранена в Git history.

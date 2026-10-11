# Автоматическое продолжение Worker после сброса лимита

Status: ready-for-agent

При подтверждённом usage limit Worker сохраняет прежнюю session/Turn, видимо ожидает и автоматически продолжает работу после известного будущего reset. По умолчанию `worker_policy.auto_resume_on_limit=true`; без надёжного времени — manual hold. Понятные ошибки внедряются первой задачей.

Источник времени — официальный Codex App Server `account/rateLimits/read`, через короткоживущий native query client на правильном Execution node/account. Внутренние JSONL-файлы не используются для scheduler. Неполное «4:37 PM» не парсится как timestamp. Поддерживаются primary/secondary и недельные окна; нет произвольного предела 24h или числа повторов.

Продолжение создаёт next Attempt того же Turn; premature Result отсутствует. Scheduler, manual resume, cancel и recovery используют durable checkpoint/command identities, atomic CAS и текущие gates. FIFO `/q` ждёт завершения Turn; unknown execution не повторяется. Web и Telegram показывают причину, режим ожидания и controls. Scope — Worker, без Secretary/смены провайдера/покупки credits.

- [Самодостаточная задача: исследование, решения, план и критерии](issues/01-auto-resume-after-usage-limit.md).
- [Исследование официального API](research.md).
- [Предшествующая задача: понятные ошибки](../worker-error-diagnostics/issues/01-expose-worker-error-cause.md).

Порядок реализации: сначала типизированная диагностика, затем scheduler. Обе постановки готовы; это не объявление реализованного поведения.

# HeyClicky: продуктовый отчёт

**Проверено:** 2026-10-07. **Источники:** `heyclicky.com`, `heyclicky.com/changelog` (v1.0.49-v1.0.53), `heyclicky.com/about`, HokAI обзор 24.09.2026. Продукт не устанавливался. Основатель Farza, YC Spring 26, Humansongs Inc, 25k+ юзеров.

## 1. Краткое резюме

HeyClicky — Mac-only AI buddy у курсора. Тезис: у всех одни модели, проблема в интерфейсе. Два режима: Talk (голосовой разговор с видом экрана) и Agents/Clickys (фоновая работа). Живет в notch/menu-bar, не в чат-окне. Free + Pro $20 (150 agent msgs) + Max $100 (1000 agent msgs). Talk unlimited на платных.

## 2. Функции

- **Hotkey + screen context:** видит экран только по нажатию, скриншоты не хранит, хранит текстовые summaries для контекста.
- **Voice-first:** hold-to-talk, ответы голосом + текст у курсора, шепот на звонках молчит, Do Not Disturb для рутин.
- **Draw on screen:** стрелка/круг/каракуля, указывает на точный контрол. Trail краски за разговором. Walkthrough до 15 шагов с памятью цели и шагов, ждет загрузки страниц.
- **Point/circle/scribble для фокуса:** обведи диаграмму или панель в сложном софте (Figma, After Effects, DaVinci, FL Studio) чтобы указать контекст.
- **Clickys:** именованные долгоживущие агенты, у каждого свое имя, память, conversation, папка файлов. Создание голосом: `make me three clickys: inbox, research...`. Pins, search, archive, unread dots, follow-up удержанием voice keys на карточке.
- **Agents:** `heyclicky agent, turn this figma into webpage`, `find cameras under $1k`, `summarize pdf and email team`. Строит Mac apps, research IG micro-influencers, работает с Apple Notes/Calendar/Reminders. Отдельное browser window на задачу, не двигает реальный курсор (раньше двигал, убрали из-за багов).
- **Computer use:** `Allow once / Always allow / Not now` на conversation, `Allow sticks` через паузы и helper agents. На Cua драйвере, нативный, быстрый. Честно пишут: early, sometimes breaks.
- **Routines:** `daily briefing`, `check every few hours`. Работают пока Mac открыт. Pause/resume/run now/delete из профиля. Пауза после 3 фейлов, пропуск при оффлайне без штрафа, догон после сна один раз.
- **Suggestions:** исследовательский пайплайн: по одному researcher на app + web за 72 часа, редактор отбирает. Одно предложение за раз, carousel, approve/skip/adjust голосом (`do it`, `show next`). Учитывает отказы, не повторяется, отдыхает если 3 утра не открывали. Morning hello из notch с `Show me`.
- **Connectors:** Gmail (multi-account Work/Personal), Calendar, Stripe, Meta Ads, custom MCP по URL или local command, browser sign-in или API key. Подключение обновляет знания voice ассистента без новой сессии.
- **Skills library:** ~100 community skills в один клик из notch. В v1.0.49 убирали, вернули в v1.0.52.
- **Home/notch:** Home выезжает из notch, resizable, quick peek на hover, файлы pile с drag-out, copy текста, preview PDF/doc/image рядом с чатом, whole-document understanding (весь PDF а не только видимое).
- **Onboarding:** выбор стиля персонажа, 4 вопроса интервью, 3 Clicky под тебя + 2 дефолтных Farza. Setup не тратит allowance.
- **Privacy:** только по hotkey, screenshots discard после обработки, анализ и промпт хранятся, удаление аккаунта чистит за 14 дней. Creds в macOS Keychain. Облачная обработка Anthropic/OpenAI, ничего локально. Proactive agents выпиливали в v1.0.46 после жалоб `feels watched`, вернули как Suggestions/Routines с явным opt-in.

## 3. Use cases из демо и changelog

`how do i make first beat in FL Studio`, `teach me this After Effects panel`, `help design logo in Figma`, `research SSDs then add to Amazon cart`, `go to OpenAI dashboard generate API key`, `find cameras like this under $1k`, `summarize pdf email team`, daily briefing, inbox triage, competitor research, newsletter, desktop cleanup every morning.

## 4. Выводы для Secretary

У Secretary сильная сторона которой нет ни у кого: server-owned Personal Conversation + Worker binding + Follow-up у того же Worker + Execution Nodes. Hark и Clicky это не подтверждают.

Что копировать (дешево, OSS):
1. Voice hotkey + screenshot context как опциональный Channel adapter (у Secretary уже есть Telegram adapter, добавить desktop).
2. Draw/point как аннотации в Observer (стрелка на скриншоте вместо описания кнопок).
3. Clickys = именованные persistent Workers с папкой + памятью на Task (у Secretary Worker уже закреплен за Task, не хватает имени/лица/папки в UI).
4. Routines = cron на Secretary server (сейчас только ручной `/q` и Steering).
5. Suggestions пайплайн = researcher per connector за 72 часа + редактор, с Adjust голосом/текстом.
6. Computer-use approvals `Allow once / Always` на conversation (у Secretary Approval уже есть, расширить scope).

Что не копировать: notch-only Mac app, закрытый облачный процессинг.

## 5. Источники

- https://www.heyclicky.com/
- https://www.heyclicky.com/changelog
- https://www.heyclicky.com/about
- https://hokai.io/hub/tools/heyclicky

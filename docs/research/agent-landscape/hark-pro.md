# Hark Pro: продуктовый отчёт

**Проверено:** 2026-10-07. **Источники:** `hark.com`, `hark.com/articles/introducing-hark-pro`, `hark.com/articles/introducing-hark-handoff`, видео `Introducing Hark Pro` (`i3mfnrEwaA0`), TechCrunch 06.10.2026. Продукт не устанавливался. Видео частично ускорено, в статье прямо указано `5x speed` для research и travel демо.

## 1. Краткое резюме

Hark Pro — персональный агент как `операционная система для жизни`. Основатель Brett Adcock, $700M Series A, $6B оценка, май 2026. Три кита: собственный cloud computer (Handoff), persistent memory, proactive thinking. Интерфейсы: single-thread чат + Home лента + Projects + Action Buttons + Panels. Доступно сегодня: web/iOS/Android, free, Pro2 $20 2x usage, Pro3 $100 10x.

## 2. Функции

- **Чат:** короткие bursts, charts и multimedia вместо простыней.
- **Home:** кастомная лента, живой атмосферный фон (погода/время, облака, солнце). Сверху Action Buttons, ниже Panels.
- **Action Buttons:** эволюция нотификаций. Тап решает задачу целиком: `pay bill`, `cancel subscription`. Hark сам предлагает и приоритизирует.
- **Panels:** bespoke mini-apps из текстового описания + подключенные аккаунты. Заявлены Strava, Spotify, Venmo, Netflix x Rotten Tomatoes, ParentSquare. Панель можно спрашивать.
- **Projects:** отдельные треды с файлами для долгого: job search, trip planning, big event.
- **Memory:** портрет пользователя: не любит майонез, место у окна, любит мопсов, особый формат документов. `Забудь` по запросу.
- **Proactivity:** scheduled tasks/reminders + спонтанные suggestions с pacing. Примеры: approve expenses, ответить на письма, забронировать перелет, DMV renewal, lunch card refill, парковка + QR к билету в кино.
- **Handoff:** на каждый запрос отдельная виртуалка с browser + FS + terminal, до 6 браузеров параллельно. Клик/скролл/печать по x,y. Умеет логины от имени юзера, терпит popup/bot-block. Маленькое окно показывает навигацию для доверия.
- **Интеграции:** Google/Outlook по API, файлы, external DB, MCP, email/calendar/drive/cards, saved addresses/history.
- **Secured by Hark:** сквозной vault для паролей и карт. Утверждается что даже Hark не видит. Check-in перед чувствительным действием.
- **Onboarding:** подключение цифровой жизни. Главы видео: `00:00 Intro / 01:55 Onboarding / 03:06 Designing / 05:40 Creating / 06:38 Security`.

## 3. Все use cases

Еда DoorDash/Uber Eats, шоппинг Walmart/Target/Costco со сравнением цен и checkout, OpenTable/Resy бронь (пример: birthday dinner), LinkedIn поиск/сообщения/собесы, research по Reddit/отзывам/новостям, перелеты United/American/JetBlue/Delta + отели Booking.com, build websites, make slides, place orders, submit expenses из email receipts, quick research.

## 4. Приватность и цена

Принципы: данные твои, не продаем, не делимся с рекламодателями, можно удалить. Закрытый облачный сервис, не OSS, не self-hosted. Free + $20 + $100.

## 5. Выводы для Secretary

Что уже закрыто: Personal Conversation = single-thread чат, Worker с Turns/Attempts/Result = база под Handoff задачи, Approval = база под `Allow once / Always allow`, `phase4_projects` = зачаток Projects.

Что копировать первым (дешево, OSS):
1. Action Buttons как тип Approval с one-tap execute.
2. Panels как сохраненные вьюхи из подключенных MCP + вопросы к ним.
3. Proactive scheduler + auto-memory из диалога в `user.md` (сейчас только ручной).
4. Projects довести до юзерских тредов с файлами.

Что дорого: Handoff-клон на Playwright в Docker на Execution Node + VNC скринкаст в Observer + vault на age/sops + MCP коннекторы Gmail/Calendar.

## 6. Источники

- https://hark.com/
- https://hark.com/articles/introducing-hark-pro
- https://hark.com/articles/introducing-hark-handoff
- https://www.youtube.com/watch?v=i3mfnrEwaA0
- https://techcrunch.com/2026/10/06/hark-releases-an-ai-personal-assistant-with-a-focus-on-privacy

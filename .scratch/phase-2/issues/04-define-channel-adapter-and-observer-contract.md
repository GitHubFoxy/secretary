# Define the Channel adapter and Worker observer contract

Type: grilling
Status: resolved
Blocked by: 02

## Question

Какой HTTP JSON и WebSocket contract нужен Channel adapter для Personal Conversation sync, Steering, `/q`, server-issued adapter credentials, Worker links и Worker observer?

Нужно зафиксировать live-only activity subscription, direct Worker input и Cancel без передачи raw Worker stream во все clients.

## Answer

Первый Channel adapter - web client. Setup создаёт один owner bootstrap token, который web client обменивает на HttpOnly session cookie. Отдельная account system не входит в slice. Внешние adapters позднее используют server-issued adapter credentials.

Commands идут по HTTP JSON. Personal Conversation sync, Secretary replies и Result идут по WebSocket. `subscribe(last_entry_seq)` сначала атомарно догоняет client всеми более новыми Conversation entries в global order, затем переключается на live delivery. Server deduplicates inbound messages по `(adapter_id, external_message_id)`.

Dispatch acknowledgement содержит Worker reference и web link. Worker observer сначала получает snapshot текущего Worker status, затем live-only activity. Raw activity не пишется в durable Conversation и не broadcast-ится в другие clients.

Observer отправляет direct Steering, Queued message и Cancel по Worker reference. Server сохраняет compact Conversation entry о user-to-Worker input. Cancel показывает `stopping` до terminal canceled Result.

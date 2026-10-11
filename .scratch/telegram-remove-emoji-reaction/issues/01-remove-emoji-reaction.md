# 01: Remove Telegram message emoji reaction (👀)

Type: task
Status: needs-triage
Blocked by: None

## What to build

Remove the automatic emoji reaction (`👀`) currently placed on incoming user messages in Telegram when the message is accepted by Secretary or routed to a Worker.

The typing indicator (`sendTyping`) must remain intact as the sole liveness indicator; no reaction calls should be sent to Telegram Bot API.

## Acceptance criteria

- [ ] Telegram adapter does not call `setMessageReaction` when processing incoming messages (both Secretary General and Worker topic messages).
- [ ] Dead reaction code and interfaces (`reactToMessage`, `MessageReactionTransport`, and `SetMessageReaction`) are removed from `internal/telegram`.
- [ ] Typing indicator (`sendTyping`) remains functional and unaffected across Secretary and Worker turns.
- [ ] Tests in `internal/telegram` (`adapter_test.go`, `http_test.go`) are updated/cleaned up and pass without regressions.
- [ ] Full package tests (`go test ./internal/telegram/...`) and repo-wide tests pass cleanly.

---

## Research & Codebase Analysis

In `secretary-v2`, Telegram messages receive a best-effort `👀` reaction immediately upon successful acceptance by the server. This is handled exclusively within the `internal/telegram` package.

### 1. Affected Code Locations

#### A. Ingestion & Reaction Trigger
- **`internal/telegram/adapter.go`**:
  - **Interface** (lines 72–74):
    ```go
    type MessageReactionTransport interface {
        SetMessageReaction(context.Context, int64, int64, string) error
    }
    ```
  - **Worker message path** (lines 541–545 in `handleMessage`):
    ```go
    err := a.server.SendWorkerMessage(ctx, WorkerMessage{WorkerRef: mapping.WorkerRef, ExternalMessageID: externalID, RequestID: mapping.PendingRequestID, Text: text})
    if err == nil {
        a.reactToMessage(ctx, update.Message)
        a.sendTyping(update.Message.ChatID, update.Message.ThreadID)
    }
    ```
  - **Secretary message path** (lines 558–566 in `handleMessage`):
    ```go
    if err := a.server.SendMessage(ctx, InboundMessage{ExternalMessageID: externalID, Body: text}); err != nil {
        return err
    }
    a.reactToMessage(ctx, update.Message)
    thread := update.Message.ThreadID
    if thread == 0 {
        thread = 1
    }
    a.sendTyping(update.Message.ChatID, thread)
    return nil
    ```
  - **Method `reactToMessage`** (lines 579–589):
    ```go
    // reactToMessage is a best-effort visual acknowledgement after the server accepts the update.
    func (a *Adapter) reactToMessage(ctx context.Context, message *Message) {
        if message == nil || message.MessageID <= 0 {
            return
        }
        transport, ok := a.transport.(MessageReactionTransport)
        if !ok {
            return
        }
        _ = transport.SetMessageReaction(ctx, message.ChatID, message.MessageID, "👀")
    }
    ```

#### B. Telegram Bot API Call
- **`internal/telegram/http.go`** (lines 158–169):
  ```go
  func (t *BotAPITransport) SetMessageReaction(ctx context.Context, chatID, messageID int64, emoji string) error {
      reaction, err := json.Marshal([]map[string]string{{"type": "emoji", "emoji": emoji}})
      if err != nil {
          return err
      }
      values := url.Values{
          "chat_id":    {strconv.FormatInt(chatID, 10)},
          "message_id": {strconv.FormatInt(messageID, 10)},
          "reaction":   {string(reaction)},
      }
      return t.call(ctx, "setMessageReaction", values, nil)
  }
  ```

#### C. Test Suites
- **`internal/telegram/adapter_test.go`**:
  - `recordingReactionTransport` and `reactionCall` (lines 59–73).
  - `TestAcceptedSecretaryMessageGetsBestEffortReaction` (lines 382–401): asserts that inbound accepted messages trigger `SetMessageReaction(..., "👀")`.
- **`internal/telegram/http_test.go`**:
  - `TestBotAPITransportSetsEyesReaction` (lines 41–70): asserts serialization and query parameters sent to `/setMessageReaction`.

---

## Implementation Plan

1. **Adapter (`internal/telegram/adapter.go`)**:
   - Remove `a.reactToMessage(ctx, update.Message)` in `handleMessage` for both Worker messages (line 543) and Secretary messages (line 561).
   - Delete `reactToMessage` method (lines 579–589).
   - Remove `MessageReactionTransport` interface (lines 72–74).

2. **HTTP Client (`internal/telegram/http.go`)**:
   - Delete `SetMessageReaction` method (lines 158–169) so no dead Bot API reaction wrappers remain.

3. **Tests**:
   - In `internal/telegram/adapter_test.go`:
     - Remove `TestAcceptedSecretaryMessageGetsBestEffortReaction` (or replace with assertion that message ingestion succeeds and only `sendTyping` is triggered without reactions).
     - Remove `reactionCall` and `recordingReactionTransport`.
   - In `internal/telegram/http_test.go`:
     - Remove `TestBotAPITransportSetsEyesReaction`.

4. **Verification**:
   - Run `go test ./internal/telegram/...`
   - Run `go test ./...`

## Comments

Initial user request: "Create new issue, remove emoji reaction. Do a reserach how to do it, no editing just read only". Research was performed and embedded directly into this issue.

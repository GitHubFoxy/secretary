# Spec: Remove Telegram Message Emoji Reaction

## Goal

Remove the automatic emoji reaction (`👀`) placed on user messages in Telegram upon ingestion, keeping the typing indicator (`sendTyping`) as the sole visual liveness cue.

## Motivation

The `👀` reaction was originally added as a best-effort visual acknowledgement after the server accepts an update. However:
1. It clutters the chat interface.
2. It can be misleading to users (creating confusion around whether a turn or task is actively running or completed).
3. It incurs extraneous Telegram Bot API calls (`setMessageReaction`) that may fail or hit rate limits on busy chats.

## Architecture & Scope

- **Scope:** Confined to `internal/telegram`.
- **Typing Indicator:** Preserved as-is (`sendTyping(chatID, threadID)`).
- **Reaction Calls:** Completely removed from `handleMessage` across all paths (General Secretary messages and Worker topic messages).
- **Dead Code Cleanup:** Remove `MessageReactionTransport` interface, `reactToMessage` helper, and `SetMessageReaction` HTTP transport method and their corresponding unit tests.

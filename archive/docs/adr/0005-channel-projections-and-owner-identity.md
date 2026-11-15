# ADR-0005: Web/PWA and Telegram DM/General are projections of one conversation

- **Status:** Accepted (product direction; full mirroring must be verified)
- **Date:** 2026-10-17
- **Context:** [Canonical spec](../../spec/spec.md), [grilling decisions](../../spec/grilling-2026-10-17.md), [runtime contracts](../architecture/runtime-contracts.md)

## Context

Telegram can act as a simple bot DM or be added to a group with forum Topics. The owner expects personal bot messages, Telegram group General and Web/PWA to present the **same Secretary conversation**, with Worker Topics reserved for direct Worker interaction. Cross-channel message replication is vulnerable to echo loops and duplicated model invocations unless the Server owns a canonical identity.

## Decision

1. Secretary Server owns **one ordered Personal Conversation**; Web/PWA, Telegram bot DM and forum group General are synchronized **projections**, not independent chats or runtime histories.
2. Telegram **DM is fully functional alone**. A forum supergroup is optional; when configured, the bot is made admin and creates/maintains Worker Topics using necessary Topic privileges.
3. Messages sent to bot DM are reflected in General and vice versa, as are Secretary messages and Worker Results. Mirrored/delivered messages carry stable server-owned identity/cursors and **never re-enter as new user commands**.
4. Telegram Worker Topic is a view of **one specific Worker**, including activity/direct messages, not a separate Secretary Personal Conversation. Ordinary Worker direct message attempts steering while busy; explicit /q queues.
5. Owner identity is single and explicitly paired for Telegram. Other group members **cannot control Secretary or Node just because they can post in the group**.
6. Web becomes installable PWA with the same Server/session; no second backend, full offline product or push requirement for Public v1.
7. Reuse current bootstrap-token/code Web session and cookie. No new email/password account system in the one-owner Developer Edition.
8. Channel delivery may be asynchronous/retried, but replay remains idempotent and the user sees a single canonical Result.

## Consequences

- A channel bug cannot be fixed by cloning conversations into per-channel state.
- The Telegram adapter requires explicit owner authorization, pairing, message origin tagging, durable outbound identity and careful handling of General topic vs non-General Worker Topics.
- Web/PWA and Telegram may show different activity detail without diverging on canonical entries.

## Alternatives rejected

- Independent Telegram DM/General/Web Secretary sessions.
- Mandatory Telegram group setup.
- Allowing all Telegram group members to command the owner machine.
- Mirroring a message by re-submitting it as new model input.

## Verification

Black-box public input in each channel projects to the other channels **exactly once**; group and DM input remain authorized by the single owner; Worker Topic input reaches only its Worker; replay/cursor restart, duplicate Telegram updates and reconnection do not produce extra Turns. Verify PWA installability using the existing Web app rather than introducing a parallel client.

# Telegram images and videos for Secretary and Workers

## Goal

An authorized Telegram user can send a photo/image, a regular video, or a round video message to the Secretary or a Worker. The recipient gets both usable model context and, for Worker tasks, the original media file inside the Worker Workspace.

## Scope and decisions

- Telegram is the only Channel adapter in scope.
- Accept Telegram photos, image documents, MP4 `video`, and MP4 `video_note`; preserve captions where Telegram provides them (`video_note` has no caption field) and preserve source message identity.
- Reject media before dispatch with a short explicit Telegram error when the file is unsupported, exceeds a configured limit, or the selected model/runtime cannot accept images. The capability error text is `Can't do images`.
- Do not switch to a different vision model. No vision fallback.
- Enforce an explicit app limit of 20,000,000 bytes per media item. Telegram documents its standard cloud Bot API limit as “20 MB” without defining the byte conversion.
- Keep all application settings in the primary `config.toml`. Put media options in `[telegram.media]`, with defaults of 60 seconds, 1 frame/second, WebP quality 80 for video frames, and WebP quality 85 for still images. Default maximum long side: 1280 px for video frames and 2048 px for still images. Make format, quality, and dimensions configurable. Do not truncate or dynamically lower the rate.
- For each accepted video, extract frames at the configured rate, extract its complete audio track when present, and produce timestamped ASR output. A video without audio is still analyzed with no transcript. Include video frames and timestamped transcript in the input sent to the model; keep the original video in the Worker Workspace.
- If the active harness reaches its context limit, rely on its own compaction behavior. Do not add application-owned compaction, frame sampling heuristics, or hidden retries. Preserve the media input through the turn boundary so the harness can compact before processing it.

## Architecture constraints

- Preserve allowlist/topic authorization and Telegram update replay/idempotency behavior. Do not download media for unauthorized input.
- The current Telegram `Message`, persisted `Update`, Secretary/Worker client contracts, durable Conversation input and Node dispatch are text-only. Extend the durable path; do not make the Telegram adapter bypass the Secretary server or send raw bytes as unbounded JSON.
- Model vision support and harness transport support are separate gates. Image input is enabled only when the configured model advertises vision and the selected runtime/harness accepts image content. Default unknown capability to unsupported.
- Make model vision capability explicit per model in TOML (`supports_images`); unknown models default to `false`.
- Move ASR configuration from the legacy YAML sidecar into `[asr]` in `config.toml`, remove YAML loading/example, and give operators an actionable migration path. Never silently ignore or delete an existing `config.yaml`.
- Remote Execution nodes must receive media through an authenticated, bounded, durable transfer contract and materialize it safely inside the validated Worker Workspace. A local-only path is insufficient.
- Keep media references associated with Telegram chat/topic/message/update identity across retries. Preserve order relative to text/voice within the same conversation/topic.
- Extract video metadata and enforce duration/size against both Telegram metadata and downloaded/decoded media. Never trust MIME type or filename alone.
- Create separate model-input derivatives from verified, orientation-corrected originals: resize still images to at most 2048 px on the long side and encode as WebP quality 85; resize video frames to at most 1280 px on the long side and encode as WebP quality 80. Keep originals unchanged for Worker artifacts. Make format, quality, and maximum dimensions configurable in `[telegram.media]`. These explicit defaults are stable preprocessing, not context-driven adaptation. Bound decoder work independently of compressed input size; reject source media exceeding safety ceilings visibly rather than shrinking it to bypass the ceiling.
- Clean transient server-side media after the durable recipient/Worker artifact is safely materialized; on failure retain/retry only according to the existing durable input lifecycle, and never silently discard an accepted update.

## Out of scope

- Other channel adapters, arbitrary file types, live streams, media groups, animated stickers, and automatic vision fallback.
- Automatic quality adaptation or changing frames per second based on context/length.
- Secretary-specific context compaction or a new generic media summarization model.
- Raising Telegram's standard Bot API download ceiling by deploying a local Bot API server.

## References

- [Issue 01: end-to-end Telegram media input](https://github.com/GitHubFoxy/secretary/issues/26)
- [Research findings](research.md)

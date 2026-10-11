# 01: End-to-end Telegram image and video input

Type: task
Status: ready-for-agent
Blocked by: None

## What to implement

Implement the [feature specification](../spec.md): receive Telegram images and videos, stage them safely, represent them as durable multimodal input for Secretary/Worker, transfer original media to a Worker Workspace, and dispatch supported visual content through the selected runtime.

Today, `config.toml` is the primary config and adjacent `config.yaml` is loaded only for ASR. Move ASR settings into TOML `[asr]`, remove YAML config loading and `docs/config.example.yaml`, and update voice-configuration docs. If a legacy `config.yaml` is present with ASR settings, fail with an actionable migration message instead of silently ignoring or deleting it; document the exact TOML equivalent. The operator must be able to copy values such as `mode`, `engine`, `model`, `max_audio_seconds`, `max_file_bytes`, `timeout_seconds`, and `formatter` into `[asr]`, validate `config.toml`, then remove the YAML sidecar.

Current seams are text-only: Telegram DTO/routing in `internal/telegram/http.go` and `internal/telegram/adapter.go`; durable Secretary input in `internal/core/secretary_types.go` and `internal/core/secretary_api.go`; Worker input in `internal/ctl/worker_lifecycle.go` and `internal/node/protocol_types.go`; and the ACP prompt in `internal/node/acp_runtime.go`. Keep the Telegram adapter as a channel client: capability checks, durable media references, and Worker authorization stay server-owned. Do not put media bytes or base64 in a Secretary/Worker JSON envelope.

## User-visible behavior

- Authorized Telegram photos/images, MP4 `video` messages, and MP4 `video_note` messages enter the same ordered input stream as text and voice. Keep captions where Telegram supplies them (`video_note` has no caption field), any accompanying text, and Telegram source identity.
- If the selected model or runtime cannot consume image input, reject before dispatch with the explicit message `Can't do images`. Do not silently turn an image into a text-only request or pick another model.
- Reject videos longer than the configured duration (default 60 seconds) and media larger than the app's explicit 20,000,000-byte limit. Telegram documents its standard cloud Bot API ceiling as “20 MB” without specifying a byte conversion. Explain the exceeded app limit in the Telegram reply.
- Add settings to primary `config.toml` under `[telegram.media]`. Defaults: `max_video_duration_seconds = 60`, `video_frames_per_second = 1`, `image_max_side_px = 2048`, `image_format = "webp"`, `image_webp_quality = 85`, `video_frame_max_side_px = 1280`, `video_frame_format = "webp"`, and `video_frame_webp_quality = 80`. Validate positive dimensions/rates, quality in `0..100`, and supported formats; reject invalid values visibly.
- Add an exact per-model TOML entry `[model_capabilities."<effective-model-id>"]` with `supports_images = true`. Match the effective model ID selected for the Secretary or immutable Worker binding (not `default`, a harness name, or provider-wide flag); IDs are exact and case-sensitive. Missing entries mean `false`. Resolve Secretary aliases before lookup.
- For videos, sample at times `n / video_frames_per_second` for integer `n >= 0`, stopping before the decoded duration (never include a frame at the duration endpoint). Include each sample timestamp. At default 1 fps a 60-second clip yields at most 60 frames. Validate configured rates against selected runtime request limits and local processing bounds; reject unsupported rates visibly and never lower the rate silently. Extract the entire audio track when present and pass ASR transcript with segment timestamps; if no audio track exists, continue with no transcript. Do not shorten audio, truncate the clip, or reduce frame rate automatically.
- Create bounded model-input derivatives from orientation-corrected media: resize still images to a maximum 2048 px long side and encode WebP at quality 85; resize sampled video frames to a maximum 1280 px long side and encode WebP at quality 80. Preserve original uploaded media unchanged for Worker tasks. These defaults and output format/quality/dimensions must be configurable in `[telegram.media]` in `config.toml`. Send derivatives and timestamped transcript as model-visible content; materialize the original image/video in the Worker Workspace, including on remote Execution nodes.
- Preserve the full media input across the harness's normal turn/compaction boundary. Secretary does not compact, discard media, lower frame rate, or invent a fallback. If the selected harness cannot continue, surface its normal runtime context error. Exercise context pressure with deterministic runtime tests; record any live compaction claim with the exact harness, version, model, and observed evidence rather than treating an ACP compaction notification as proof by itself.

- Reuse each validated source and generated model derivative across Secretary and Worker delivery; do not rerun ffmpeg or transcode the same media separately for each recipient.

## Implementation requirements

- Extend Telegram Bot API update decoding and file download for photo size variants, image documents, videos, and video notes. Use the largest available `PhotoSize` for a Telegram photo; validate image-document bytes. Preserve every downloaded original unchanged for Worker artifacts. Send only configured model-input derivatives (WebP by default); if the selected runtime cannot accept the configured format, return `Can't do images` without fallback or an implicit format change. Enforce current owner/chat/topic authorization before downloading.
- Keep the input restart-safe and idempotent: persist enough metadata to redownload or retry before advancing/forgetting the Telegram update. Deduplicate by stable Telegram update/message identity, not by caption or file contents.
- Enforce byte limits while downloading (bounded read), verify actual media type/content, and validate decoded video duration. Treat Telegram-provided duration/size as early checks only.
- Model capability must be explicit in `[model_capabilities."<effective-model-id>"]` in `config.toml` and default to false when unknown. Separately negotiate/check the selected harness capability. ACP image blocks require the session's negotiated image prompt capability; do not infer model vision from ACP support alone.
- Put model capabilities in a typed TOML map keyed by exact effective model ID. Check both the configured model capability and the selected runtime's negotiated image transport before accepting input. For an existing Worker, perform this check server-side against its immutable binding before queueing/steering the message; return a typed unsupported-media error that Telegram renders as exactly `Can't do images`. Do not download media until owner/chat/topic authorization succeeds.
- Use ACP image blocks only after `session/prompt` image capability is negotiated; encode each extracted video frame using the configured format (WebP/`image/webp` by default) and send model-input derivatives. Reject invalid/unsupported image data with the same capability error. Original image/video files remain separate Worker artifacts, never inline prompt payloads.
- Bound decoding as well as input download: enforce decoded pixel/dimension and extracted-frame byte/count limits, a processing deadline, and bounded temporary storage. These are fixed implementation safety ceilings, documented next to the decoder; exceeding one rejects the message visibly and never lowers FPS or truncates media. Derive the maximum expected sample count from `duration × FPS` before launching ffmpeg.
- Extend durable conversation/Worker message contracts to carry bounded attachment references and metadata; keep binary data out of prompts, logs, and general JSON envelopes. Preserve current text routing and Worker Steering/queue semantics.
- Transfer media to remote Execution nodes with the authenticated node protocol, bounded payloads, integrity verification, and replay-safe identity. Validate destination paths under the Worker Workspace and never trust an incoming filename as a path.
- Reuse ffmpeg and existing ASR engine where practical, but extend ASR output to timestamped segments/words. Current ASR API returns only a string, so timestamp support is a real implementation requirement, not an assumption about current behavior.
- Parse a structured timestamped ASR format (for example NeMo JSON with word start/end fields); do not fabricate timestamps from plain text. If the installed ASR executable/version cannot produce timestamps, return a visible media-processing error and do not dispatch a partial video prompt.
- Isolate per-message temporary files and delete them on success and failure. Avoid logging captions, transcripts, local file paths, or bot-token-bearing download URLs.
- Surface provider/runtime rejections caused by image-count or payload-size limits. Do not retry as text-only, reduce FPS, discard frames, or switch models.
- Preserve order and delivery semantics for text and media across replay, restart, Secretary direct input, Worker direct message, and active Steering input.

## Acceptance criteria

- [ ] Telegram photo, image document, MP4 `video` and MP4 `video_note` are decoded and delivered to Secretary and Worker with available captions/text and source identity intact (`video_note` has no caption field).
- [ ] Unsupported model vision or unsupported runtime image transport returns `Can't do images`; there is no text-only dispatch and no model fallback.
- [ ] Per-model capability defaults safely to unsupported when absent; configured vision alone cannot bypass a harness that did not negotiate image input.
- [ ] `config.toml` contains per-model `supports_images`, defaults to false for unknown IDs, and identifies the capability of the actual selected Secretary/Worker model.
- [ ] Existing ASR settings load from `[asr]` in `config.toml`; the YAML sidecar is no longer read or shipped. A leftover legacy `config.yaml` produces an actionable migration error and is never silently ignored or deleted.
- [ ] `config.toml` exposes video duration/FPS and media-preparation format, quality, and dimension settings with defaults 60 seconds, 1 frame/second, WebP quality 80 for frames, WebP quality 85 for stills, 1280 px max frame side, and 2048 px max still-image side. Invalid config fails visibly.
- [ ] Size is capped at 20,000,000 bytes during download; over-limit media is rejected before prompt dispatch. This is the app's explicit byte limit, separate from Telegram's documented “20 MB” wording. Actual decoded duration is checked, and clips above the configured limit are rejected (never truncated).
- [ ] A 60-second video at default settings produces at most 60 timestamped frames at 1-second intervals (samples strictly before the duration endpoint), creates 1280 px max-side WebP frames at quality 80, preserves the full audio track when present, and passes ASR segments with timestamps. A clip without audio is processed with no transcript. Still images create 2048 px max-side WebP copies at quality 85; originals remain unchanged.
- [ ] Secretary receives image/frame blocks as visual context. Worker receives the same model-visible context and can read the original media file from its Workspace on both local and remote Nodes.
- [ ] Deterministic tests verify the full attachment survives durable input, restart/replay, and a runtime turn boundary under simulated context pressure. Any live harness compaction evidence names the tested harness/version/model and shows the same input's attachments remain present; optional ACP compaction events alone do not count as evidence. No Secretary compaction or silent media degradation is introduced. If the provider rejects the request on image-count or payload-size limits, surface that error without fallback or frame reduction.
- [ ] TOML `model_capabilities` uses exact effective model IDs and defaults missing entries to unsupported. Server rejects unsupported media before enqueueing a Worker message or creating a Secretary turn; Telegram displays `Can't do images`.
- [ ] Video/image decoding enforces and tests documented pixel, frame-count, output-byte, time, and temporary-storage ceilings. Exceeding any ceiling gives a visible rejection without lowering FPS, truncating the clip, or dispatching partial media.
- [ ] Timestamped ASR parsing is tested against the supported structured output schema. Missing/malformed timestamps fail closed with a visible processing error; no fabricated timestamps or partial video dispatch.
- [ ] Authorization happens before download; download and remote transfer enforce size bounds; media paths are contained in the Worker Workspace; secrets and media content do not leak to logs.
- [ ] Update replay/restart cannot lose, reorder, duplicate, or execute a media input twice. Existing text and voice behavior remains intact.
- [ ] Add focused unit/integration coverage for decode, capability rejection, limits, timestamp/frame extraction, remote transfer, replay, and runtime turn-boundary/context-pressure behavior; record live Telegram acceptance and any live compaction evidence separately.

## References

- [Research findings](../research.md)
- [Feature scope and decisions](../spec.md)
- [Telegram Bot API: Message and media](https://core.telegram.org/bots/api#message)
- [Telegram Bot API: File](https://core.telegram.org/bots/api#file)
- [ACP: Content](https://agentclientprotocol.com/protocol/v1/content) and [Prompt Turn](https://agentclientprotocol.com/protocol/v1/prompt-turn)
- [NeMo-Speech.cpp: structured transcript output](https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/cli.md#subtitles-and-structured-output)

# ADR-0003: One server-owned Artifact Store for every channel and Node

- **Status:** Accepted (desired product contract; not yet fully implemented)
- **Date:** 2026-10-17
- **Context:** [Canonical spec](../../spec/spec.md), [grilling decisions](../../spec/grilling-2026-10-17.md)

## Context

Secretary serves users from Telegram DM/General/Worker Topics and Web/PWA. Workers may execute on macOS, Linux or Windows Nodes. Ordinary users expect to send images, voice, videos and documents, and receive real files in return. Host-local paths from a remote Node are not valid delivery references. Telegram voice transcription exists, but a full canonical binary Artifact pipeline cannot be assumed from Result artifact-reference fields alone.

## Decision

1. A single **Artifact Store at Secretary Server** owns originals, derivatives, transcripts, published output, metadata and stable opaque Artifact identifiers. It is authoritative regardless of the channel or Node where a file originated.
2. Inbound images, video, audio/voice, documents and arbitrary accepted regular files enter the same pipeline from Secretary and Worker-facing channel inputs.
3. Media preprocessing runs asynchronously. Video original is retained; sample timestamped image frames at a **configurable** rate (for example 1 FPS default) and limits; optionally extract/transcribe audio. Frame sampling must not be represented as complete frame-by-frame observation.
4. Voice/audio original and a Markdown transcript are linked Artifacts. Reuse functioning Telegram ASR/formatter components instead of rebuilding them solely for the new store.
5. Worker references are stable server-controlled IDs; Node access to bytes must use a server-authorized transfer mechanism, never an unresolvable remote absolute path. Files created by Workers are **explicitly published**, not found by sweeping their filesystem.
6. Secretary and Workers can publish documents, images, archives and other outputs to Web/PWA and Telegram, sending a real file when supported or a valid link when transport restrictions prevent direct delivery.
7. Processing limits for size, duration and frame count are configurable. **Reject with a clear warning** when limits are exceeded; no silent truncation.
8. Transcoding/ASR failures keep originals, mark processing failed, and block only dependent prompts; never falsely state that content was processed. Unrelated conversation remains responsive.
9. Closing/archiving a Worker never deletes Artifacts. Explicit deletion removes the intended Artifact and unreferenced derivatives. Server backups/restores cover stored content and metadata.
10. Storage implementation is local to the Secretary Server in Developer v1; object storage/cloud distribution is not a requirement.

## Consequences

- One stable cross-channel file reference and durable storage location; remote Nodes do not own canonical delivered files.
- The Server needs a bounded storage lifecycle, ownership checks, conversion job state and transfer protocol.
- Storage may grow until explicit deletion; provide accurate disk usage/limits and backup procedures.
- Existing Telegram ASR can be adapted to publish transcript/source pairs while keeping current behavior compatible.

## Alternatives rejected

- Separate permanent storage for each Worker/Node/channel.
- Returning a Node-only filesystem path as a file.
- Auto-scanning every Worker workspace for output.
- Hardcoded 1 FPS or silent clipping of oversized media.

## Verification

At the public API/channel→Server store→Node→channel seam: upload image/voice/video, assert byte-identical original and linked derivative IDs/timestamps; change frame rate in config; simulate oversize, converter/ASR failure and interrupted network; Worker publish file and fetch from another Node; deliver to Web/Telegram; archive, delete and backup/restore without leaks.

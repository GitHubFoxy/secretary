# Unified artifact output tool (draft)

## Problem

Secretary increasingly needs one interface for delivering outputs. Requests such as “output this whole file” currently waste model output tokens on reproducing content that already exists.

## Proposal

Provide one simple tool to deliver an existing workspace file as an artifact/attachment instead of printing its contents in the assistant reply. Return a short reference/metadata to the conversation, with the file itself delivered through the active client (Web or Telegram).

## Initial scope

- Input: path to an existing workspace file, optionally a display name.
- Respect the worker's existing workspace and file-access boundaries.
- Reuse existing artifact and transport capabilities rather than building another delivery subsystem.
- Keep the textual response short and never inline the whole file by default.
- Make delivery failures explicit; do not claim a file was sent when it was not.

## Open questions

- Whether the current artifact contract can already represent and deliver this without a new tool.
- How the same operation surfaces consistently in Telegram and Web.

No implementation in this draft.

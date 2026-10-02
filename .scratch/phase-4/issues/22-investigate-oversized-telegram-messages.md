# 22 Decide how to deliver oversized Worker messages in Telegram

Type: research
Status: ready-for-agent

## Work

A single long Worker message appears broken into pieces in Telegram. Establish exactly what the user sees and whether the problem is caused by Telegram's message-size limit, our chunking boundary, formatting loss, or another part of delivery. The current adapter chunks messages at Telegram's 4096 UTF-16-unit limit and can split at a valid UTF-8 boundary without preserving paragraph, sentence or Markdown structure.

Reproduce with the supplied long-result example and inspect each delivered chunk, including links, lists, code fences, emoji and retry/replay behavior. Recommend a clear product behavior before implementing a fix. Options to assess include splitting at semantic/formatting boundaries, sending the full text as a file, or showing a short preview with access to the complete Result. Do not assume that Telegram or the model is at fault.

## Acceptance

- The report identifies where and why the message appears broken, with a reproducible case and the relevant Telegram size accounting.
- A recommendation states how long content should appear in Worker Topic and General, including the trade-off between multiple messages and a file/preview.
- The recommendation covers Markdown boundaries, Unicode, message ordering, delivery retry and deduplication.
- The decision and follow-up implementation scope are recorded in this ticket before changing delivery behavior.

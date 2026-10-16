# Realtime voice/vision: source register and reproducibility ledger

> **Linked report:** [`spec/realtime-voice-vision-research.md`](../realtime-voice-vision-research.md)  
> **Register snapshot:** 2026-10-16 (UTC date; exact retrieval timestamps were not archived).  
> **Purpose:** enable a future engineer/agent to **rebuild the evidence, rerun the same benchmark, and update decisions** rather than trust a stale recommendation.
>
> **Important distinction:** “Documentation verified” means the referenced public documentation was located/read during the research pass; **it does not mean the API was called or a benchmark was executed**. Vendor claims are not independently validated. Mutable pages must be rechecked and version-pinned before implementation.

## 1. Evidence index

Use the IDs below when making or changing claims in the [main report](../realtime-voice-vision-research.md). Dates describe this **documentation pass**, not the publication date of the vendor's feature.

| ID | Primary URL | Evidence it supports | Status as of 2026-10-16 / what to recheck |
| --- | --- | --- | --- |
| **S01** | [LiveKit Agents source](https://github.com/livekit/agents) | Open-source SDK and examples; implementation dependency | Repository identified; **pin tag/commit before build**, not `main` |
| **S02** | [LiveKit — Turns overview](https://docs.livekit.io/agents/logic/turns/) | AgentSession turn handling, interruptions, history truncation, STT vs model endpointing | Documentation checked; recheck API behavior and SDK version |
| **S03** | [LiveKit — Turn detector](https://docs.livekit.io/agents/logic/turns/turn-detector/) | Audio `v1` / `v1-mini`, supported languages (**RU absent from audio v1**), deprecated text multilingual detector (**RU present**) | Documentation checked; **recheck language list and release** before choosing RU strategy |
| **S04** | [LiveKit — Turn-taking tuning](https://docs.livekit.io/agents/logic/turns/tuning/) | `TurnHandlingOptions`, STT endpointing delay, dynamic/fixed EOU, interruptions, preemptive generation | Documentation checked; recheck current Python/Node signatures and defaults |
| **S05** | [LiveKit — Adaptive interruption handling](https://docs.livekit.io/agents/logic/turns/adaptive-interruption-handling/) | Barge-in vs backchannels; Cloud/local limits; aligned transcript and VAD prerequisites | Documentation checked; verify entitlement, region, alignment capability, quota and language performance |
| **S06** | [LiveKit — Deepgram STT integration](https://docs.livekit.io/agents/models/stt/deepgram/) | `deepgram/flux-general-multi`, `language="multi"`, RU support, Flux STT EOU, `STTv2` | Documentation checked; recheck model ID, provider availability, RU rollout and threshold defaults |
| **S07** | [LiveKit — Video](https://docs.livekit.io/agents/multimodality/vision/video/) | Sampled frames, live video support, default ~1 FPS speaking / 1 per 3s otherwise, video independent of EOU | Documentation checked; verify video support by chosen model/SDK and frame processing cost |
| **S08** | [Google — Gemini Live SDK guide](https://ai.google.dev/gemini-api/docs/live-api/get-started-sdk) | Live audio and frame streaming, **max 1 video frame per second** | Documentation checked; verify model-specific limits, accepted media types and region |
| **S09** | [ElevenLabs — Scribe v2 Realtime server streaming](https://elevenlabs.io/docs/eleven-api/guides/how-to/speech-to-text/realtime/server-side-streaming) | WebSocket partial/committed transcripts, streaming STT comparison | Documentation checked; recheck languages, word alignment, costs and commit semantics |
| **S10** | [OpenAI — Realtime VAD](https://developers.openai.com/api/docs/guides/realtime-vad) | Provider-side VAD/semantic VAD and turn events; one turn authority | Documentation checked; recheck model-specific turn detection behavior and event API |
| **S11** | [Full-Duplex-Bench](https://github.com/DanielLin94144/Full-Duplex-Bench) | Open benchmark/data/code for pauses, backchannels, interruptions, overlap and tool use | Repository checked; **pin exact commit, dataset version, license and evaluator**; current v1–v3 results are not interchangeable |
| **S12** | [Artificial Analysis — Speech to Speech](https://artificialanalysis.ai/speech-to-speech/) | S2S model comparisons, index, first audio, price | Site checked; snapshot timestamp, model revision, methodology and score components |
| **S13** | [Artificial Analysis — Speech Agent Arena](https://artificialanalysis.ai/speech-to-speech/arena) | Blind conversation preference and task success | Site checked; recheck samples, confidence intervals, model availability and ranking date |
| **S14** | [Artificial Analysis — Text-to-Speech Arena](https://artificialanalysis.ai/embed/text-to-speech-leaderboard/arena) | Human preference benchmark for TTS | Site checked; recheck language/task coverage, version and methodology; not a streaming latency test |
| **S15** | [LiveKit — Session traces and metrics](https://docs.livekit.io/reference/agents/session-traces/) | E2E, EOU, STT, LLM TTFT, TTS TTFB and trace span definitions | Documentation checked; client playout still needs custom instrumentation |
| **S16** | [LiveKit — Data hooks](https://docs.livekit.io/agents/ops/logging/) | Session reports, metrics/usage, transcripts and OpenTelemetry export | Documentation checked; verify privacy/retention, SDK API and export completeness |
| **S17** | [Repo — Cloud ASR research](research-cloud-asr.md) | Historical prerecorded Telegram voice ASR, pricing and provider constraints | Local repository research, dated **2026-10-11**; **not a streaming benchmark** |
| **S18** | [Repo — Cloud formatting research](research-cloud-formatting.md) | Formatter model IDs, pricing, meaning-preservation tests, reasoning settings | Local repository research, dated **2026-10-11**; recheck models/prices before use |

**Additional checked context:** [Google video understanding](https://ai.google.dev/gemini-api/docs/video-understanding.md) describes 1 FPS frame extraction for static video processing; **this is a different API workflow** from live video input. [Artificial Analysis S2S methodology](https://artificialanalysis.ai/methodology/speech-to-speech-benchmarking/) documents evolving benchmark components; record its revision/date with each score. [ElevenLabs Realtime transcripts/commit strategies](https://elevenlabs.io/docs/eleven-api/guides/how-to/speech-to-text/realtime/transcripts-and-commit-strategies) describes STT finalization behavior.

## 2. Source quality and claim status

Every significant technical claim in future updates should carry **one of these labels**:

| Label | Meaning | Example |
| --- | --- | --- |
| `vendor-documented` | Vendor/API docs explicitly state the behavior; **not independently tested** | Flux multilingual lists Russian [S06] |
| `open-source-inspected` | Behavior traced to pinned upstream source commit/tag | LiveKit Agents EOU implementation at a specific SHA |
| `benchmark-published` | External evaluator published a result with date, dataset and methodology | Artificial Analysis S2S ranking [S12] |
| `locally-reproduced` | Our frozen corpus was run with archived config and raw metrics | RU pause false-commit rate from `run_id` |
| `hypothesis` | Engineering recommendation not yet tested | Modular pipeline preferred for auditable tools |
| `unverified` | Model ID, price, feature, or performance not independently confirmed | Old report's vendor-specific “100 ms” claim |

Do **not** promote `vendor-documented` or `benchmark-published` to `locally-reproduced`. Do **not** use leaderboard rank as proof of RU turn-taking, p95 latency or tool success. When a provider removes or renames a model, preserve the historical record and add a replacement row; do not rewrite old runs to the new model ID.

## 3. Sources to check at every refresh (not all verified in this pass)

These are **watch-list URLs**, not frozen price/model evidence:

| Area | Where to check | What to capture |
| --- | --- | --- |
| LiveKit pricing / deployment | https://livekit.io/pricing | Agent/inference cost, Cloud vs self-hosted features, quotas, residency |
| Deepgram pricing and Flux API | https://deepgram.com/pricing and https://developers.deepgram.com/ | Realtime model availability, minimum billing, languages, EOU thresholds |
| ElevenLabs pricing and TTS | https://elevenlabs.io/pricing/api and https://elevenlabs.io/docs/ | Exact streaming TTS model ID, character/audio billing, cancel semantics, privacy |
| Cartesia TTS | https://docs.cartesia.ai/ | Current Sonic model ID, streaming API, voices, pricing and latency definitions |
| OpenAI API models/pricing | https://developers.openai.com/api/docs/models and https://developers.openai.com/api/docs/pricing | Exact model snapshots, Realtime tools/vision, input/output audio pricing, retention |
| Google Live models/pricing | https://ai.google.dev/gemini-api/docs/pricing and https://ai.google.dev/gemini-api/docs/live-api | Model IDs, video limits, token accounting, regions, deprecations |
| Speech benchmarks | https://artificialanalysis.ai/speech-to-speech/ and https://artificialanalysis.ai/methodology/speech-to-speech-benchmarking/ | Model snapshot, methodology revision, N, score formula, date |
| Full-Duplex-Bench upstream | https://github.com/DanielLin94144/Full-Duplex-Bench | Commit SHA, v1/v1.5/v2/v3, datasets, evaluation scripts, license |

The original research discussion mentioned specific candidate model generations (e.g. Grok Voice, Gemini Live, GPT-Live, StepAudio, ElevenLabs Turbo). **No current ranking, speed, pricing or exact endpoint is certified by this register unless a source and local run are attached.**

## 4. Rebuild protocol

A future update should produce an **evidence bundle** with:

```text
eval/voice/
  README.md
  fixtures/
    manifest.jsonl
  configs/
    baseline.yaml
  runs/
    <run-id>/
      manifest.json
      source-snapshot.json
      raw-events.jsonl
      client-playout.jsonl
      transcripts.jsonl
      metrics.json
      failures.md
```

The layout is **proposed**, not currently created by this docs-only PR. Keep private audio/video outside git, under consent-controlled storage. Check in only sanitized synthetic fixtures, manifests, hashes and aggregate metrics.

### `source-snapshot.json` suggested fields

```json
{
  "captured_at_utc": "2026-10-16T00:00:00Z",
  "source_register_revision": "<git-sha>",
  "sources": [
    {
      "id": "S03",
      "url": "https://docs.livekit.io/agents/logic/turns/turn-detector/",
      "checked_at_utc": "<actual-fetch-timestamp>",
      "status": "vendor-documented",
      "claim": "Audio v1 supported languages do not include Russian",
      "upstream_commit_or_release": "<if available>",
      "content_digest": "<sha256-of-permitted-snapshot-or-extracted-claim>",
      "change_notes": ""
    }
  ]
}
```

The top-level `captured_at_utc` above is **an example**, not a claimed exact retrieval timestamp for this pass. Replace placeholders with actual captured values. If a source is inaccessible, record `unavailable` and the last known link instead of silently dropping it.

### `manifest.json` suggested fields

```json
{
  "run_id": "<uuid>",
  "started_at_utc": "<timestamp>",
  "repository_commit": "<sha>",
  "corpus_version": "<version>",
  "corpus_sha256": "<sha256>",
  "sdk_versions": {"livekit_agents": "<exact-version>"},
  "models": {
    "stt": "<provider/exact-model-snapshot>",
    "llm": "<provider/exact-model-snapshot>",
    "tts": "<provider/exact-model-snapshot>",
    "turn_detector": "<name/version>"
  },
  "turn_config": "<redacted-config-path>",
  "client_device": "<browser/os/mic/aec>",
  "network_profile": "<latency/jitter/loss>",
  "provider_region": "<region>",
  "price_snapshot": "<dated-url-or-file>",
  "source_snapshot": "source-snapshot.json"
}
```

## 5. Repeatable test procedure

1. **Lock:** use the same consented audio/video fixture hashes, sample rate, speaker/mic setup, network shaping and provider region; pin SDK, models, config and benchmark code SHA.
2. **Replay:** run at least RU/EN mid-turn pauses, true EOU, backchannels, genuine interruptions, noisy/overlapping speech, code-switching, client playout cancellation, video deictics and tools.
3. **Observe:** collect monotonic event times from both server and client; include audio playback watermark, not only generated TTS bytes. Save all failed trials.
4. **Compute:** WER/CER, entity error, premature EOU, missed turns, false barge-in, barge-in stop latency, E2E first audible response, task/vision accuracy, p50/p90/p95 and cost.
5. **Compare:** same corpus for LiveKit audio v1 (supported languages), Flux Multilingual STT EOU (RU/EN), VAD fallback and native S2S. Note unavailable cases as `not tested`.
6. **Report:** include sample counts, confidence intervals, cold/warm, retries, exclusions, provider model snapshot, source-register revision and price snapshot.
7. **Refresh:** compare old vs new **without overwriting** the baseline. Update the research report and this register together; log changed claims and migration risks.

For Full-Duplex-Bench, **first read the exact upstream version's README/license**, download its versioned dataset, and record its commit/dataset hash. Published benchmark aggregate numbers cannot establish the performance of Secretary's full pipeline or its Russian workload. [S11]

## 6. Refresh checklist

- [ ] LiveKit Agents release and Python/Node SDK option names/defaults verified
- [ ] LiveKit Audio Turn Detector v1 supported language list checked for Russian
- [ ] Flux Multilingual RU support, model ID and `turn_detection="stt"` verified
- [ ] Adaptive interruption prerequisites, Cloud quotas, aligned transcript support checked
- [ ] Native S2S turn authority and ability to disable provider endpointing verified
- [ ] Video 1 FPS/idle sampling and provider frame cap checked
- [ ] STT, LLM, TTS, native S2S model IDs and deprecations verified
- [ ] Privacy, retention, region and user consent reviewed
- [ ] Pricing and billing units captured with UTC date
- [ ] Benchmark methodology, dataset version and upstream SHA pinned
- [ ] Frozen RU/EN corpus rerun, raw client/server metrics archived
- [ ] Regression table and tradeoffs recorded in the main report
- [ ] Snapshot date, source register and changelog updated together

## Change log

- **2026-10-16:** Initial primary-source ledger for the reconstructed voice/vision report. Public documentation was reviewed; **no paid model inference or application-level benchmark was run**.

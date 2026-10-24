# Public v0.0.1 — live acceptance

This directory prepares the **next real-device run of the published alpha.17**.
It does not certify alpha.17 and does not alter product code, CI, or the release
process. The actual test run must use the published artifacts unchanged.

## Fixed release scope

- **Server:** Linux and macOS; **Execution Node:** Linux, macOS, Windows.
  Windows is **Node-only**, not a supported Server installation.
- **Native harnesses:** Codex and Claude Code, both required for release
  acceptance, but a single installation may use just one.
- **Telegram:** owner DM without a group; optional forum supergroup General and
  Worker Topics; photos, videos, audio files and voice messages. Ordinary
  non-forum groups are not in scope.
- **Web:** browser conversation, Workers/observer and files in both directions.
  Mobile-browser usability is required; installing a PWA is not required.
- **Durability:** same-Worker/native-session follow-up, truthful failures,
  restart/reconnect, retained conversation and Artifact data.
- **Documents/PDF:** Secretary transfers intact files to/from native harnesses;
  parsing or understanding PDF is the harness's responsibility. No separate
  Secretary PDF interpreter is required.
- **Later:** realtime voice conversation, mandatory PWA installation, semantic
  memory, managed Skills registry and Cron. Do not silently infer that these
  features are already removed or disabled in existing code.

**Release policy:** the public tag is exactly `v0.0.1`. Previously published\nalpha releases and their tags are to be deleted only after alpha.17 has been\nlive-tested and their tag-to-SHA/evidence mapping preserved. Fix the installer\nto select stable releases explicitly; deleting alphas is not a substitute.\nRelease cleanup and installer changes are outside this preparation PR.\n\n`cases.yaml` is the stable **v0.0.1 acceptance-case catalog**. This scoped
release gate does not rewrite the broader [product specification](../../spec/spec.md)
or supersede existing [Public v1 evidence](../public-v1-acceptance.md).
A follow-up Source-of-Truth PR can reconcile those documents after alpha.17.

## Tomorrow's workflow

1. Record the **exact published** alpha.17 tag, commit SHA, asset name,
   SHA256, running binary revision, Server/Node OS and architecture, harness CLI,
   adapter/ACP, code-mode-host and model versions. Do not repair the package
   during the run or count a locally rebuilt executable as published evidence.
2. Expand each case's `matrix`: each combination of listed values is a separate
   required **case instance**. A `NODE-WORKER-01` PASS on Linux/Codex does not
   imply a PASS on Windows/Codex or Linux/Claude Code. For cases without a given
   axis, record the actual tested environment; it need not be repeated across
   all platforms unless its `matrix` says so.
3. Perform what is possible and record a JSON run in
   `docs/acceptance/results/<release-tag>/<run-id>.json`, using
   [result.example.json](result.example.json) as the shape. One run should
   describe a single immutable candidate and tested host/harness environment.
   Add a case result for **each attempted instance**, with its actual dimensions.
4. Link redacted logs, screenshots, Result/Attempt/Worker IDs, and/or published
   Artifact hashes. Evidence can be existing reports under
   `.scratch/phase-5/reports/`; reference stable repository paths or issue
   URLs. Do not commit bot tokens, cookies, native API keys, personal files,
   private media, local authentication directories or unredacted transcripts.
5. Create Issues for observed product failures; record their URLs against the
   relevant case instance. **Do not fix failures during the baseline run**.
6. Cases not attempted are **NOT RUN** (or simply absent from a partial run).
   Missing historical evidence is **UNKNOWN** only in later historical summaries;
   never manufacture a historical PASS. The baseline is not a release approval.
7. Compare later candidates against alpha.17 using the same case IDs, axes
   and environment. Label new failures, still-open repeats and regressions only
   when comparable prior evidence exists.

## Status contract

| Status | Meaning |
| --- | --- |
| `PASS` | Executed on the stated real machine and published candidate; all `pass` criteria observed and linked to evidence |
| `FAIL` | Executed; at least one required behavior failed, including a false success status or incorrect native Result |
| `BLOCKED` | Could not meaningfully execute because of an external prerequisite; record the exact reason. A demonstrated product defect is `FAIL`, not `BLOCKED` |
| `NOT_RUN` | Not attempted; neither success nor failure is claimed |
| `UNKNOWN` | Only for legacy historical imports with insufficient trustworthy evidence, **not** a live-run status |

The live record must say `test_kind: live`. CI/mocks/fixtures may have their
own records but **cannot satisfy a live PASS**. For each real `PASS` or `FAIL`,
supply a concrete observation, a candidate SHA, and at least one evidence link
or artifact reference. Never auto-mark an entire matrix or an untested OS PASS.

The result JSON is a **minimal exchange format**, not a new runtime API.
`dimensions` are the actual tested matrix values, not guesses based on what
the candidate is intended to support. The `ids` fields are optional but useful
to trace duplicate/false Results. Do not edit results from previous candidates
when a bug is fixed; append a new immutable run.

## Boundary of this PR

Only the case catalog, documentation and empty/unexecuted result template are
introduced. There is deliberately **no** fabricated alpha.17 evidence, no
generated `release-status.md`, no historical alpha reconstruction, no CI
refactor, no release deletion and no application behavior change here. Those
belong to the subsequent acceptance, CI and Source-of-Truth work packages.

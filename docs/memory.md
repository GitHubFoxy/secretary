# Explicit Markdown memory (T07)

Secretary keeps three **separate** documents in the configured server data directory:

| File | Purpose |
| --- | --- |
| `memory.md` | Facts explicitly requested by the owner |
| `user_preferences.md` | Preferences explicitly requested by the owner |
| `user.md` | **Legacy** owner context; still loaded verbatim |

## Compatibility and upgrade

No destructive migration happens. Existing `user.md` bytes are neither copied,
changed, truncated nor deleted. Secretary continues including the original
`user_document` in its canonical context, and includes `memory_document` and
`user_preferences_document` alongside it. Historical context snapshots that
predate these two optional fields remain readable. New file content appears in
subsequent turns and survives process/SQLite restarts. At **new Worker
creation**, Secretary Server binds a read-only snapshot of the legacy document
and both explicit Markdown files to the Worker's immutable, hash-verified
native profile instructions. The original user intent is unchanged and
existing Workers keep their already-bound snapshot on follow-up; newly saved
preferences affect subsequent Secretary turns and newly spawned Workers.
There is no per-task semantic filtering, vector retrieval or dynamic Worker
memory mutation.

The two new files are created empty the first time they are read. Manual edits
are supported. Each valid changed file increments its durable SQLite revision
at the next read. The Server and standalone MCP share a SQLite
`BEGIN IMMEDIATE` writer reservation around file/revision reconciliation and
CAS saves, so a stale second writer cannot restore or overwrite a committed
first writer's Markdown. A save checks the expected revision while holding
that reservation; after commit a stale save receives **409** without a file
write. A malformed file (invalid UTF-8, NUL or >1 MiB), missing file
after initialization, filesystem error or SQLite error is returned to the caller,
not silently replaced by a cached value. File writes are atomic; use the owner
session/API rather than concurrent processes editing the same file.

Backups must include `secretary.db`, `user.md`, `memory.md` and
`user_preferences.md` together.

## Conversational changes

The owner explicitly asks e.g. **"Запомни, что я предпочитаю короткие ответы"**
or **"Remember that I prefer Go"**. Secretary calls `read_memory` to choose
`kind=memory` or `kind=preferences` and obtain `revision`, then calls
`remember` with an entry, `expected_revision` and the active originating
`secretary_turn_id` / `input_id`. The server verifies current owner
capability, active origin and the original explicit owner request. Quoted
examples, translation requests, negations, multi-line/multi-command messages
and ambiguous instructions are deliberately refused by this conservative
authorization check; a direct standalone memory request is required. Ordinary
conversation, third-party content, Worker results and inferred preferences
cannot invoke a permitted write. Conflicts and failures must be reported rather
than claimed saved. An explicit remembered fact is appended as a Markdown list
entry; new facts are never inferred or harvested automatically.

## Owner API

Authenticated with the existing owner Web session or a paired client with
`user.read` / `user.write` scopes:

- `GET /v1/memory/memory`, `GET /v1/memory/preferences` return
  `{path, revision, content, updated_at}`.
- `PUT /v1/memory/memory`, `PUT /v1/memory/preferences` accept complete
  Markdown `content`, mandatory `expected_revision` and an
  `Idempotency-Key` header (or `idempotency_key` in JSON).
- A stale revision or reused key with different request body returns **409**.
  Invalid content returns **400**. Read/write/SQL failures return **500**.
  Unauthenticated requests return **401**, and missing scopes **403**.

Example, after retrieving revision 1:

```http
PUT /v1/memory/preferences
Idempotency-Key: remember-preference-001
Content-Type: application/json

{"content":"- Prefer concise responses.\n","expected_revision":1}
```

The older `GET/PUT /v1/user` API continues to operate on `user.md` only.
There are no embeddings, fact extraction or vector indexes in T07.

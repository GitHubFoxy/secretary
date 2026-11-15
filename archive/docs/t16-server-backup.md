# T16 central Server backup and recovery (Draft)

The Server owns Artifact BLOBs inside `secretary.db`; ownership and canonical Worker Results are stored in the same database. The offline utility snapshots SQLite with `VACUUM INTO`, verifies `PRAGMA integrity_check` and every Artifact checksum, and writes a manifest with SHA-256 for each file in an archive. It includes existing `config.toml`, `user.md`, `memory.md`, and the canonical `user_preferences.md` when they reside in the Server data directory (or `--config` points at an external config). For compatibility with existing installations and archives, a legacy `preferences.md` file is also included and restored when present. It does **not** include remote Node-native sessions or running processes.

Stop Secretary **and all other writers to its data directory** before backing up or restoring. This includes automation modifying config or memory files; only the SQLite part has a transactional snapshot.

## Backup

```sh
secretaryd --data-dir /private/secretary --backup-to /private/backups/secretary-2026-10-18.zip
```

The target archive must not exist. The file must be protected like Server credentials. A failure before publication leaves no backup. A failure to sync the parent directory after publication returns ErrBackupPublishedDurabilityUnconfirmed: the complete backup exists but its crash durability is not confirmed. Inspect the published destination before retrying under a new name.

## Restore (new destination only)

```sh
secretaryd --data-dir /private/restored-server --restore-from /private/backups/secretary-2026-10-18.zip
```

Destination directory must not exist, and Server must not be running. All files are staged as a sibling directory and atomically published with one rename; an interrupted private staging directory is left for explicit operator inspection/cleanup, and retry creates an independent unique staging directory. Manifest checksums, member names, SQLite integrity and Artifact contents are checked before any file is installed. Before publication, a failed restore exposes no Server files; after publication, the entire verified directory is visible. Parent directories are synced for durable publication, and existing databases are never overwritten. Use a new data directory for recovery, then configure transport credentials and reconnect Nodes as required. A Server-only restore does not resurrect remote native Worker processes. Treat historical runtime sessions as needing Node reconnection/verification, not as live sessions.

## Artifact deletion

An authenticated owner with conversation-write scope can send `DELETE /v1/artifacts/{id}`. It returns 204 on success, 404 for another owner's or missing Artifact, and never deletes Conversation or Worker Result records. The selected Artifact and its unreferenced voice/video derivatives are removed inside one database transaction. Archived Workers retain their files until this explicit deletion. Deleted links in old results remain historical references, not download guarantees.

## Limitations

- The offline command requires a writer-free data directory to make external configuration and Markdown files consistent with the SQLite snapshot.
- Remote native Node session persistence and Node-local data must be handled separately.
- The current backup archive is intended for central Server state, not all external runtime assets.

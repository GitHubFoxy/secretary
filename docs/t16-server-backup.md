# T16 central Server backup and recovery (Draft)

The Server owns Artifact BLOBs inside `secretary.db`; ownership and canonical Worker Results are stored in the same database. The offline utility snapshots SQLite with `VACUUM INTO`, verifies `PRAGMA integrity_check` and every Artifact checksum, and writes a manifest with SHA-256 for each file in an archive. It includes existing `config.toml`, `user.md`, `memory.md`, and `preferences.md` when they reside in the Server data directory (or `--config` points at an external config). It does **not** include remote Node-native sessions or running processes.

Stop Secretary **and all other writers to its data directory** before backing up or restoring. This includes automation modifying config or memory files; only the SQLite part has a transactional snapshot.

## Backup

```sh
secretaryd --data-dir /private/secretary --backup-to /private/backups/secretary-2026-10-18.zip
```

The target archive must not exist. The file must be protected like Server credentials. A failed backup is not published.

## Restore (new destination only)

```sh
secretaryd --data-dir /private/restored-server --restore-from /private/backups/secretary-2026-10-18.zip
```

Destination must be empty and Server must not be running. Manifest checksums, member names, SQLite integrity and Artifact contents are checked before any file is installed. A failed restore rolls back installed files; existing databases are never overwritten. Use a new data directory for recovery, then configure transport credentials and reconnect Nodes as required. A Server-only restore does not resurrect remote native Worker processes. Treat historical runtime sessions as needing Node reconnection/verification, not as live sessions.

## Artifact deletion

An authenticated owner with conversation-write scope can send `DELETE /v1/artifacts/{id}`. It returns 204 on success, 404 for another owner's or missing Artifact, and never deletes Conversation or Worker Result records. The selected Artifact and its unreferenced voice/video derivatives are removed inside one database transaction. Archived Workers retain their files until this explicit deletion. Deleted links in old results remain historical references, not download guarantees.

## Limitations

- The offline command requires a writer-free data directory to make external configuration and Markdown files consistent with the SQLite snapshot.
- Remote native Node session persistence and Node-local data must be handled separately.
- The current backup archive is intended for central Server state, not all external runtime assets.

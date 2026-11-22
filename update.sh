#!/usr/bin/env bash
set -euo pipefail

folder="$HOME/.secretary"
bun="$folder/bun/bin/bun"
export PATH="$folder/bun/bin:$HOME/.local/bin:$PATH"
count=$("$bun" -e 'const {Database}=require("bun:sqlite"); const db=new Database(process.argv[1],{readonly:true}); console.log(db.query("SELECT count(*) AS n FROM sessions WHERE status = ?").get("running").n)' "$folder/secretary.sqlite")
printf 'Работающих Sessions: %s. Обновление прервёт их. Продолжить? [y/N] ' "$count"
read -r answer
case "$answer" in y|Y|yes|YES) ;; *) echo 'Обновление отменено.'; exit 0;; esac
exec systemd-run --user --wait --pipe --collect flock -n "$folder/update.lock" "$bun" "$folder/current/server/update.ts"

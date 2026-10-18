#!/usr/bin/env python3
"""Provision a trusted T09 file manifest from the exact verified npm tarball.

Run with administrator privileges; this script never hashes arbitrary installed
files. npm registry integrity is checked against the downloaded tarball first.
"""
import base64
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import stat
import subprocess
import sys
import tarfile
import tempfile

PACKAGES = {"codex": "@openai/codex", "claude": "@anthropic-ai/claude-code", "opencode": "opencode-ai"}

def command(*args):
    return subprocess.check_output(args, text=True).strip()

def main():
    if len(sys.argv) != 3 or sys.argv[1] not in PACKAGES:
        raise SystemExit("usage: provision-cli-manifest.py codex|claude|opencode VERSION")
    if os.geteuid() != 0:
        raise SystemExit("run as root: manifest must not be worker-writable")
    provider, version = sys.argv[1:]
    package = PACKAGES[provider]
    if not version or "/" in version or ".." in version:
        raise SystemExit("invalid package version")
    registry = json.loads(command("npm", "view", f"{package}@{version}", "dist", "--json"))
    if not registry.get("integrity", "").startswith("sha512-"):
        raise SystemExit("registry must provide sha512 integrity")
    with tempfile.TemporaryDirectory() as temp:
        packed = subprocess.check_output(
            ["npm", "pack", f"{package}@{version}", "--pack-destination", temp, "--json"],
            text=True,
        )
        item = json.loads(packed)[0]
        archive = Path(temp, item["filename"])
        digest = base64.b64encode(hashlib.sha512(archive.read_bytes()).digest()).decode()
        if "sha512-" + digest != registry["integrity"]:
            raise SystemExit("npm archive integrity does not match registry")
        entries = {}
        with tarfile.open(archive, "r:gz") as tar:
            for item in tar:
                if item.isdir():
                    continue
                if not item.isfile() or item.issym() or item.islnk():
                    raise SystemExit("unsupported archive entry")
                path = PurePosixPath(item.name)
                if not path.parts or path.parts[0] != "package":
                    raise SystemExit("invalid npm archive path")
                rel = PurePosixPath(*path.parts[1:])
                if not rel.parts or ".." in rel.parts or str(rel) in entries:
                    raise SystemExit("unsafe or duplicate archive path")
                stream = tar.extractfile(item)
                if stream is None:
                    raise SystemExit("missing archive content")
                entries[str(rel)] = hashlib.sha256(stream.read()).hexdigest()
        destination = Path("/etc/secretary/cli-manifests", provider)
        destination.mkdir(parents=True, exist_ok=True)
        os.chmod(destination, 0o755)
        manifest = destination / (version + ".json")
        if manifest.is_symlink():
            raise SystemExit("refusing symlink manifest")
        payload = {"provider": package, "version": version, "files": entries}
        temporary = destination / (version + ".json.new")
        with open(temporary, "x", encoding="utf8") as handle:
            json.dump(payload, handle, sort_keys=True)
            handle.write("\n")
        os.chmod(temporary, 0o644)
        os.replace(temporary, manifest)
        print(manifest, len(entries), "verified assets")

if __name__ == "__main__":
    main()

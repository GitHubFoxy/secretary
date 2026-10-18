#!/usr/bin/env python3
"""Provision a trusted T09 file manifest from the exact verified npm tarball.

Run with administrator privileges; this script never hashes arbitrary installed
files. npm registry integrity is checked against the downloaded tarball first.
"""
import base64
import hashlib
import json
import os
import shutil
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
    if len(sys.argv) not in (3, 4) or sys.argv[1] not in PACKAGES:
        raise SystemExit("usage: provision-cli-manifest.py codex|claude|opencode VERSION")
    if os.geteuid() != 0:
        raise SystemExit("run as root: manifest must not be worker-writable")
    provider, version = sys.argv[1:]
    package = PACKAGES[provider]
    if len(sys.argv) == 4:
        override = sys.argv[3]
        valid = (provider == "claude" and override.startswith("@anthropic-ai/claude-code-linux-")) or (provider == "opencode" and override.startswith("opencode-linux-"))
        if not valid or "/" in override.strip("@").split("/", 1)[-1] and not override.startswith("@anthropic-ai/"):
            raise SystemExit("unsupported official platform package")
        package = override
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
        staging = Path(temp, "verified-assets")
        staging.mkdir(mode=0o700)
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
                raw = stream.read()
                entries[str(rel)] = hashlib.sha256(raw).hexdigest()
                target = staging.joinpath(*rel.parts)
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(raw)
                target.chmod(0o555 if item.mode & 0o111 else 0o444)
        package_identity = json.loads((staging / "package.json").read_text())
        official_name = package_identity["name"]
        official_version = package_identity["version"]
        if official_version != version:
            raise SystemExit("archive package version mismatch")
        if not (official_name == PACKAGES[provider] or
                (provider == "claude" and official_name.startswith("@anthropic-ai/claude-code-linux-")) or
                (provider == "opencode" and official_name.startswith("opencode-linux-"))):
            raise SystemExit("archive package identity mismatch")
        package_id = official_name.replace("/", "__")
        trusted = Path("/opt/secretary/cli-store", provider, package_id)
        trusted.mkdir(parents=True, exist_ok=True)
        os.chmod(trusted, 0o755)
        location = trusted / version
        if location.exists() or location.is_symlink():
            raise SystemExit("refusing to replace existing verified runtime")
        shutil.copytree(staging, location, symlinks=False)
        for folder, _, _ in os.walk(location):
            os.chmod(folder, 0o755)
        destination = Path("/etc/secretary/cli-manifests", provider, package_id)
        destination.mkdir(parents=True, exist_ok=True)
        os.chmod(destination, 0o755)
        manifest = destination / (version + ".json")
        if manifest.is_symlink():
            raise SystemExit("refusing symlink manifest")
        payload = {"provider": official_name, "version": version, "files": entries}
        temporary = destination / (version + ".json.new")
        with open(temporary, "x", encoding="utf8") as handle:
            json.dump(payload, handle, sort_keys=True)
            handle.write("\n")
        os.chmod(temporary, 0o644)
        os.replace(temporary, manifest)
        print(manifest, len(entries), "verified assets")

if __name__ == "__main__":
    main()

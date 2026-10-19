#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import platform
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
from pathlib import Path

CODEX_VERSION = "0.163.0"
ACP_VERSION = "0.16.0"
PAIRS = {
    ("Darwin", "arm64"): ("aarch64-apple-darwin", "61c4d9a92521bdb42b0c1d9f87c673f6da628a06de5945cbdd9a0010632f6d10", "377f1137945b54d8e689cb600767f6dcfe6e548e01c1f29018862c009c692c8d"),
    ("Darwin", "x86_64"): ("x86_64-apple-darwin", "562a1aea0e5686283a467687d268ae33705f02fd4d40f1bc4110a415c5c1c7b1", "212fafcf971c75cd4f5060be2d5951bc322f71c49028dbd2531931494c33c784"),
    ("Linux", "aarch64"): ("aarch64-unknown-linux-musl", "a7261e65f4a96d8f4ca65a069c6cf2dc2c33defc341e48078acefda9461fffe4", None),
    ("Linux", "x86_64"): ("x86_64-unknown-linux-musl", "beb339e8ecd9ba057a6bb2dc81ad8ca474812edfbf0fc5056ca818797648dde4", None),
}
LINUX_ACP = {
    "aarch64": ("aarch64-unknown-linux-gnu", "cb72820f53c91a63982a804bd6ab251bde51bc9371d8e638946f02d7d78580b5"),
    "x86_64": ("x86_64-unknown-linux-gnu", "0a9ad6c31ec9b2b87dccb7e9da3faf5d387e74470d24dbced75a160ed7b22d06"),
}

def fail(message):
    raise SystemExit("Codex provisioning: " + message)

def version(binary):
    result = subprocess.run([str(binary), "--version"], capture_output=True, text=True, timeout=15, check=True)
    return (result.stdout or result.stderr).strip()

def status(binary):
    return subprocess.run([str(binary), "login", "status"], capture_output=True, text=True, timeout=15).returncode == 0

def download(url, destination, digest):
    if not digest or len(digest) != 64:
        fail("missing trusted release digest; refusing unverified download")
    h = hashlib.sha256()
    with urllib.request.urlopen(url, timeout=60) as response, open(destination, "wb") as output:
        while chunk := response.read(1024 * 1024):
            h.update(chunk)
            output.write(chunk)
    if h.hexdigest() != digest:
        fail("download checksum mismatch: " + url)

def extract_binary(archive, dest, name):
    with tarfile.open(archive, "r:gz") as package:
        members = [m for m in package.getmembers() if m.isfile() and (Path(m.name).name == name or Path(m.name).name.startswith(name + "-"))]
        if len(members) != 1:
            fail("release archive must contain exactly one " + name + " binary")
        with package.extractfile(members[0]) as src, open(dest, "wb") as out:
            shutil.copyfileobj(src, out)
    dest.chmod(0o700)

def write_inventory(path, data):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if path.is_symlink():
        fail("inventory symlink is forbidden")
    fd, tmp = tempfile.mkstemp(prefix=".codex-inventory-", dir=path.parent)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w") as stream:
            json.dump(data, stream, indent=2)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)

def verify(item):
    path = Path(item["path"])
    if not path.is_absolute() or not path.is_file() or not os.access(path, os.X_OK):
        fail("selected executable missing or moved: " + str(path))
    resolved = path.resolve(strict=True)
    if str(resolved) != item["resolved_path"]:
        fail("selected executable target has changed: " + str(path))
    actual = version(path)
    if actual != item["version"]:
        fail("selected executable version changed: " + str(path))
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if digest != item["sha256"]:
        fail("selected executable bytes changed: " + str(path))

def record(path, provenance):
    return {
        "path": str(path.absolute()),
        "resolved_path": str(path.resolve(strict=True)),
        "version": version(path),
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        "provenance": provenance,
    }

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["install", "doctor", "show"])
    parser.add_argument("--scope", choices=["server", "node"], required=True)
    parser.add_argument("--root", type=Path, default=Path.home() / ".local" / "share" / "secretary" / "native")
    parser.add_argument("--codex", type=Path)
    parser.add_argument("--acp", type=Path)
    args = parser.parse_args()
    root = args.root.resolve()
    inventory = root / args.scope / "codex-pair.json"
    if args.action != "install":
        if not inventory.is_file() or inventory.is_symlink():
            fail("no selected Codex pair for " + args.scope)
        data = json.loads(inventory.read_text())
        if data.get("schema") != 1 or data.get("scope") != args.scope or data.get("harness") != "codex":
            fail("invalid inventory")
        for field in ("codex", "acp"):
            verify(data[field])
        if args.action == "doctor":
            authorized = status(Path(data["codex"]["path"]))
            print(json.dumps({"scope": args.scope, "binary_ready": True, "native_auth": "authorized" if authorized else "login_required", "codex": data["codex"]["version"], "acp": data["acp"]["version"]}))
            if not authorized:
                fail("Codex native authorization needed: " + data["codex"]["path"] + " login")
        else:
            print(json.dumps(data, indent=2))
        return
    if args.codex is None and args.acp is not None or args.codex is not None and args.acp is None:
        fail("external adoption requires both --codex and --acp")
    if args.codex:
        codex, acp = args.codex.resolve(strict=True), args.acp.resolve(strict=True)
        provenance = "external"
    else:
        system = (platform.system(), platform.machine())
        if system not in PAIRS:
            fail("unsupported platform: " + repr(system))
        triple, codex_digest, acp_digest = PAIRS[system]
        if system[0] == "Linux":
            acp_triple, acp_digest = LINUX_ACP[system[1]]
        else:
            acp_triple = triple
        managed = root / "releases" / (CODEX_VERSION + "-" + ACP_VERSION) / (system[0].lower() + "-" + system[1])
        codex, acp = managed / "codex", managed / "codex-acp"
        provenance = "managed"
        if not codex.exists() or not acp.exists():
            if codex.exists() or acp.exists():
                fail("partial managed installation; explicit repair required")
            if managed.is_symlink():
                fail("managed release directory symlink forbidden")
            managed.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            with tempfile.TemporaryDirectory(prefix=".native-pair-", dir=managed.parent) as temp:
                temp = Path(temp)
                urls = [
                    ("https://github.com/openai/codex/releases/download/rust-v" + CODEX_VERSION + "/codex-" + triple + ".tar.gz", codex_digest, "codex"),
                    ("https://github.com/zed-industries/codex-acp/releases/download/v" + ACP_VERSION + "/codex-acp-" + ACP_VERSION + "-" + acp_triple + ".tar.gz", acp_digest, "codex-acp"),
                ]
                for index, (url, digest, name) in enumerate(urls):
                    archive = temp / (str(index) + ".tar.gz")
                    download(url, archive, digest)
                    extract_binary(archive, temp / name, name)
                version(temp / "codex")
                version(temp / "codex-acp")
                os.rename(temp, managed)
    for path in (codex, acp):
        if not path.is_file() or not os.access(path, os.X_OK):
            fail("selected executable is unavailable: " + str(path))
    data = {"schema": 1, "scope": args.scope, "harness": "codex",
            "codex": record(codex, provenance), "acp": record(acp, provenance),
            "compatibility": {"policy": "explicit_pair_and_native_acp_readiness", "validated": False}}
    write_inventory(inventory, data)
    print(json.dumps(data, indent=2))
    if not status(codex):
        print("Codex authorization required: " + str(codex) + " login", file=sys.stderr)

if __name__ == "__main__":
    main()

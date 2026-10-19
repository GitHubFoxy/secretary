#!/usr/bin/env bash
set -euo pipefail
[[ $# -eq 1 ]] || { echo "Usage: $0 PACKAGE_INPUT" >&2; exit 1; }
root="$1"
ffmpeg="$root/bin/video-tools/ffmpeg"
ffprobe="$root/bin/video-tools/ffprobe"
[[ -x "$ffmpeg" && -x "$ffprobe" && ! -L "$ffmpeg" && ! -L "$ffprobe" ]]
"$ffmpeg" -hide_banner -encoders | awk '$2=="libwebp" && substr($1,1,1)=="V" {found=1} END {exit !found}'
version="$("$ffmpeg" -version | head -n 1 | awk '{print $3}')"
[[ -n "$version" && "$version" == "$("$ffprobe" -version | head -n 1 | awk '{print $3}')" ]]
# A real frame conversion proves libwebp can be loaded, not merely advertised.
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
"$ffmpeg" -nostdin -hide_banner -loglevel error -f lavfi -i color=c=red:s=32x24:d=1:r=1 -frames:v 1 -c:v libwebp "$work/frame.webp"
[[ -s "$work/frame.webp" ]]
# Exercise the packaging contract against the exact staged Server binaries.
python3 - "$root" "$version" <<'PY'
import hashlib, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
version = sys.argv[2]
manifest_path = root / "VIDEO-TOOLS.json"
if manifest_path.exists():
    manifest = json.loads(manifest_path.read_text())
    assert manifest.get("origin"), "Missing tool provenance"
    assert manifest.get("required_encoder") == "libwebp", "WebP pipeline capability absent"
    for tool in ("ffmpeg", "ffprobe"):
        entry = manifest[tool]
        assert entry["version"] == version
        expected = hashlib.sha256((root/"bin"/"video-tools"/tool).read_bytes()).hexdigest()
        assert entry["sha256"] == expected, tool+" digest mismatch"
PY

"""Run the pre-implementation reproduction via Go overlay without editing repo files."""
from pathlib import Path
import json, subprocess, tempfile
root = Path(__file__).resolve().parents[3]
fixture = Path(__file__).with_name("repro_test.go.txt")
with tempfile.TemporaryDirectory(prefix="secretary-steering-repro-") as folder:
    target = root / "internal/secretary/steering_diagnosis_test.go"
    overlay = Path(folder) / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(target): str(fixture)}}))
    raise SystemExit(subprocess.call(["go", "test", "-overlay", str(overlay), "./internal/secretary", "-run", "^TestDiagnosisDurableActiveMessageSteers$", "-count=1"], cwd=root))

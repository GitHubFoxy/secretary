import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("provision-codex-pair.py")

class ProvisionCodexPairTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.codex = self.root / "external-codex"
        self.acp = self.root / "external-acp"
        for path, output in ((self.codex, "codex-cli 0.163.0"), (self.acp, "codex-acp 0.16.0")):
            path.write_text("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '" + output + "'; exit 0; fi\nif [ \"$1\" = login ]; then exit 0; fi\nexit 1\n")
            path.chmod(0o700)

    def invoke(self, action, *options):
        return subprocess.run([sys.executable, str(SCRIPT), action, "--scope", "node", "--root", str(self.root / "native"), *options], capture_output=True, text=True)

    def test_external_adoption_and_doctor(self):
        adopted = self.invoke("install", "--codex", str(self.codex), "--acp", str(self.acp))
        self.assertEqual(adopted.returncode, 0, adopted.stderr)
        inventory = json.loads((self.root / "native" / "node" / "codex-pair.json").read_text())
        self.assertEqual(inventory["codex"]["provenance"], "external")
        self.assertEqual(self.invoke("doctor").returncode, 0)

    def test_moved_binary_fails_closed(self):
        self.assertEqual(self.invoke("install", "--codex", str(self.codex), "--acp", str(self.acp)).returncode, 0)
        self.codex.rename(self.root / "moved-codex")
        checked = self.invoke("doctor")
        self.assertNotEqual(checked.returncode, 0)
        self.assertIn("missing or moved", checked.stderr)

    def test_replaced_binary_fails_closed(self):
        self.assertEqual(self.invoke("install", "--codex", str(self.codex), "--acp", str(self.acp)).returncode, 0)
        self.codex.write_text(self.codex.read_text() + "\nexit 0\n")
        self.assertNotEqual(self.invoke("doctor").returncode, 0)

    def test_repeat_setup_keeps_selected_inventory(self):
        self.assertEqual(self.invoke("install", "--codex", str(self.codex), "--acp", str(self.acp)).returncode, 0)
        previous = (self.root / "native" / "node" / "codex-pair.json").read_bytes()
        self.assertEqual(self.invoke("install").returncode, 0)
        self.assertEqual(previous, (self.root / "native" / "node" / "codex-pair.json").read_bytes())

    def test_switch_requires_explicit_upgrade(self):
        self.assertEqual(self.invoke("install", "--codex", str(self.codex), "--acp", str(self.acp)).returncode, 0)
        second = self.root / "second-codex"
        second.write_bytes(self.codex.read_bytes())
        second.chmod(0o700)
        self.assertNotEqual(self.invoke("install", "--codex", str(second), "--acp", str(self.acp)).returncode, 0)
        self.assertEqual(self.invoke("install", "--upgrade", "--codex", str(second), "--acp", str(self.acp)).returncode, 0)
        self.assertEqual(json.loads(self.invoke("show").stdout)["codex"]["path"], str(second))

    def test_missing_half_of_external_pair_is_rejected(self):
        self.assertNotEqual(self.invoke("install", "--codex", str(self.codex)).returncode, 0)

if __name__ == "__main__":
    unittest.main()

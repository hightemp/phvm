"""Security baselines accept reviewed identities, never new source findings."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/check_security_baseline.py"


class SecurityBaselineTests(unittest.TestCase):
    def test_findings_identity_and_scanner_failures(self):
        for case in ["known", "moved line", "new code", "new rule", "higher severity", "new file", "scan error", "missing report", "bad report", "bad stats", "new suppression"]:
            with self.subTest(case=case), tempfile.TemporaryDirectory() as directory:
                folder = Path(directory)
                finding = {"rule_id": "G304", "file": str(ROOT / "internal/fixture.go"), "code": "20: os.Open(path)\n", "severity": "MEDIUM", "confidence": "HIGH", "details": "Potential inclusion"}
                report = {"Issues": [finding], "Golang errors": {}, "Stats": {"files": 1, "lines": 100, "nosec": 0, "found": 1}}
                report_path = folder / "report.json"
                report_path.write_text(json.dumps(report))
                baseline = folder / "baseline.json"
                # Explicit refresh is a separate review action, not part of CI.
                create = subprocess.run([sys.executable, str(SCRIPT), "--report", str(report_path), "--baseline", str(baseline), "--write-baseline"], capture_output=True, text=True)
                self.assertEqual(create.returncode, 0, create.stdout + create.stderr)
                if case == "moved line": finding["code"] = "150: os.Open(path)\n"
                if case == "new code": finding["code"] = "20: os.Open(userPath)\n"
                if case == "new rule": finding["rule_id"] = "G703"
                if case == "higher severity": finding["severity"] = "HIGH"
                if case == "new file": finding["file"] = str(ROOT / "internal/other.go")
                if case == "scan error": report["Golang errors"] = {"fixture": [{"error": "failed compilation"}]}
                if case == "bad stats": report["Stats"] = {}
                if case == "new suppression": report["Stats"]["nosec"] = 1
                report_path.write_text(json.dumps(report))
                if case == "missing report": report_path.unlink()
                if case == "bad report": report_path.write_text("{}")
                result = subprocess.run([sys.executable, str(SCRIPT), "--report", str(report_path), "--baseline", str(baseline)], capture_output=True, text=True)
                self.assertEqual(result.returncode == 0, case in ["known", "moved line"], result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()

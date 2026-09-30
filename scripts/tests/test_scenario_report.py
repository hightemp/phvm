"""Evidence reports must reject empty selections and distinguish skipped tests."""
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))


class ScenarioReportTests(unittest.TestCase):
    def test_required_execution_and_skip_accounting(self):
        import scenario_report as report
        spec = [{"id": "fixture", "package": "example/p", "test": "TestFlow", "mode": "fixture", "platforms": ["linux"]}]
        for case in ["pass", "fail", "skip", "child skip", "missing", "package failure"]:
            with self.subTest(case=case):
                events = [{"Action": "pass", "Package": "example/p", "Test": "TestFlow"}]
                if case == "fail": events[0]["Action"] = "fail"
                if case == "skip": events[0]["Action"] = "skip"
                if case == "child skip": events.append({"Action": "skip", "Package": "example/p", "Test": "TestFlow/unsupported"})
                if case == "missing": events = []
                if case == "package failure": events.append({"Action": "fail", "Package": "example/p"})
                result = report.summarize(spec, events, "linux")
                self.assertEqual(result["ok"], case == "pass", result)
                if case in ["skip", "child skip"]: self.assertTrue(result["scenarios"][0]["skipped"])

    def test_unsupported_platform_is_not_claimed_as_passed(self):
        import scenario_report as report
        spec = [{"id": "source backend", "package": "example/p", "test": "TestFlow", "mode": "fixture", "platforms": ["linux"]}]
        result = report.summarize(spec, [], "windows")
        self.assertTrue(result["ok"])
        self.assertEqual(result["scenarios"][0]["status"], "unsupported")
        self.assertEqual(result["executed"], 0)

    def test_unknown_profiles_do_not_become_an_empty_success(self):
        import scenario_report as report
        with self.assertRaises(ValueError): report.select_profile({"fixture": []}, "unknown")
        with self.assertRaises(ValueError): report.select_profile({"fixture": []}, "fixture")


if __name__ == "__main__": unittest.main()

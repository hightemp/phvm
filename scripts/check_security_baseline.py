#!/usr/bin/env python3
"""Compare gosec findings with an explicitly reviewed source baseline."""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]


def identities(report):
    if not isinstance(report.get("Issues"), list) or not isinstance(report.get("Stats"), dict):
        raise ValueError("missing gosec findings/stats; scanner report is incomplete")
    if report.get("Golang errors"):
        raise ValueError("gosec reported compilation/analysis errors")
    stats = report["Stats"]
    if any(not isinstance(stats.get(key), int) for key in ["files", "lines", "nosec", "found"]) or stats["files"] < 1 or stats["found"] != len(report["Issues"]):
        raise ValueError("gosec scan statistics are missing or inconsistent")
    if stats["nosec"] != 0:
        raise ValueError("source suppressions are not allowed by this baseline policy")
    result = []
    for issue in report["Issues"]:
        file = Path(issue["file"])
        if not file.is_absolute(): file = ROOT / file
        relative = file.resolve().relative_to(ROOT).as_posix()
        code = "\n".join(re.sub(r"^\s*\d+:\s?", "", line).rstrip() for line in issue["code"].splitlines())
        item = {"file": relative, "rule": issue["rule_id"], "code": code, "severity": issue["severity"], "confidence": issue["confidence"], "details": issue["details"]}
        item["fingerprint"] = hashlib.sha256(json.dumps(item, sort_keys=True).encode()).hexdigest()
        result.append(item)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--baseline", required=True, type=Path)
    parser.add_argument("--write-baseline", action="store_true")
    args = parser.parse_args()
    current = identities(json.loads(args.report.read_text()))
    if args.write_baseline:
        args.baseline.write_text(json.dumps({"schema": 1, "findings": sorted(current, key=lambda item: item["fingerprint"])}, indent=2) + "\n")
        print(f"Wrote {len(current)} findings for explicit review")
        return
    baseline = json.loads(args.baseline.read_text())
    if baseline.get("schema") != 1 or not isinstance(baseline.get("findings"), list):
        raise ValueError("invalid security baseline")
    allowed = Counter(item["fingerprint"] for item in baseline["findings"])
    new = []
    for item in current:
        if allowed[item["fingerprint"]]: allowed[item["fingerprint"]] -= 1
        else: new.append(item)
    print(f"gosec: {len(current)} findings, {len(new)} new; existing findings remain in the report")
    for item in new:
        print(f'{item["file"]}: {item["rule"]} {item["severity"]}: {item["details"]}', file=sys.stderr)
    if new: raise ValueError("new security findings require remediation or explicit baseline review")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, TypeError, json.JSONDecodeError) as error:
        print(f"Security check failed: {error}", file=sys.stderr)
        sys.exit(1)

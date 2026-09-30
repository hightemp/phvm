#!/usr/bin/env python3
"""Run registered scenarios and report execution evidence from go test JSON."""
import argparse
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]


def select_profile(manifest, profile):
    if profile not in manifest or not manifest[profile]: raise ValueError("unknown or empty scenario profile")
    return manifest[profile]


def summarize(specs, events, system):
    outcomes = {}
    failures = set()
    for event in events:
        action = event.get("Action")
        if action not in ["pass", "fail", "skip"]: continue
        package, test = event.get("Package"), event.get("Test")
        if test: outcomes[package, test] = action
        elif action == "fail": failures.add(package)
    result = {"ok": not failures, "platform": system, "executed": 0, "scenarios": []}
    for spec in specs:
        item = dict(spec, skipped=[])
        if system not in spec["platforms"]:
            item["status"] = "unsupported"
        else:
            key = spec["package"], spec["test"]
            parent = outcomes.get(key)
            item["skipped"] = [test for (pkg, test), action in outcomes.items() if pkg == key[0] and (test == key[1] or test.startswith(key[1] + "/")) and action == "skip"]
            item["status"] = "missing" if parent is None else ("partial" if item["skipped"] else parent)
            if parent is not None: result["executed"] += 1
            if parent != "pass" or item["skipped"]: result["ok"] = False
        result["scenarios"].append(item)
    result["package_failures"] = sorted(failures)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", default="fixture")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    manifest = json.loads((ROOT / "scripts/scenarios.json").read_text())
    specs = select_profile(manifest, args.profile)
    system = {"Linux": "linux", "Darwin": "darwin", "Windows": "windows"}.get(platform.system(), platform.system().lower())
    selected = [item for item in specs if system in item["platforms"]]
    if not selected: raise ValueError("profile has no runnable scenarios on this platform")
    expression = "^(" + "|".join(re.escape(item["test"]) for item in selected) + ")$"
    packages = sorted(set(item["package"] for item in selected))
    output = args.output or ROOT / "reports" / f"scenarios-{args.profile}-{system}.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ)
    if args.profile == "native": env.update(PHVM_SCENARIO_NATIVE="1", PHVM_PECL_TRUST_NATIVE="1")
    log = output.with_suffix(".jsonl")
    with log.open("w") as stream:
        process = subprocess.Popen(["go", "test", "-json", "-race", "-count=1", "-timeout=15m", "-run", expression, *packages], cwd=ROOT, env=env, stdout=stream, stderr=subprocess.STDOUT)
        code = process.wait()
    events = []
    for line in log.read_text().splitlines():
        try: events.append(json.loads(line))
        except json.JSONDecodeError: pass # Compiler/tool diagnostics are retained in the raw log.
    result = summarize(specs, events, system)
    result.update(profile=args.profile, go_exit=code, raw_log=str(log))
    result["ok"] = result["ok"] and code == 0
    output.write_text(json.dumps(result, indent=2) + "\n")
    for item in result["scenarios"]: print(f'{item["id"]}: {item["status"]} ({item["mode"]})')
    print(f"Evidence: {output}; executed {result['executed']}/{len(selected)}")
    if not result["ok"]: raise ValueError("scenario failed, was skipped or was not selected; inspect evidence log")


if __name__ == "__main__":
    try: main()
    except (ValueError, OSError, KeyError) as error:
        print(f"Scenario check failed: {error}", file=sys.stderr); sys.exit(1)

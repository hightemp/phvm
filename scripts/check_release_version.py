#!/usr/bin/env python3
"""Validate candidate VERSION and the tag identity before release publication."""
import argparse
from pathlib import Path
import re
import subprocess
import sys


def check_version(tag=None):
    text = Path("VERSION").read_text()
    if not re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)\n?", text):
        raise ValueError("VERSION must contain one X.Y.Z version")
    version = text.rstrip("\n")
    if tag is not None:
        if tag != "v" + version: raise ValueError("release tag does not match VERSION")
        def git(*args): return subprocess.check_output(["git", *args], text=True).strip()
        if git("rev-parse", "HEAD") != git("rev-parse", tag + "^{commit}"):
            raise ValueError("release tag does not point to HEAD")
        if git("show", tag + ":VERSION") != version:
            raise ValueError("committed VERSION differs from release version")
    print(version)
    return version


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag")
    args = parser.parse_args()
    try: check_version(args.tag)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(f"Version check failed: {error}", file=sys.stderr); sys.exit(1)

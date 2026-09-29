#!/usr/bin/env python3
"""Read one pinned tool version shared by Make and GitHub Actions."""
import json
from pathlib import Path
import re
import sys

versions = json.loads(Path(__file__).with_name("tool_versions.json").read_text())
version = versions[sys.argv[1]]
if not re.fullmatch(r"v\d+\.\d+\.\d+", version):
    raise ValueError("tool versions must be pinned releases")
print(version)

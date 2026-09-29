#!/usr/bin/env python3
"""Fingerprint the source, Git index and HEAD included in a release."""
import hashlib
import os
from pathlib import Path
import subprocess


def git(*args): return subprocess.check_output(["git", *args])


def state_digest():
    digest = hashlib.sha256()
    for value in [git("rev-parse", "HEAD"), git("symbolic-ref", "HEAD"), git("diff", "--cached", "--binary")]:
        digest.update(value); digest.update(b"\0")
    names = sorted(set(git("ls-files", "--cached", "--others", "--exclude-standard", "-z").split(b"\0")) - {b""})
    for name in names:
        file = Path(os.fsdecode(name))
        digest.update(name); digest.update(b"\0")
        try: info = file.lstat()
        except FileNotFoundError: digest.update(b"deleted\0"); continue
        digest.update(str(info.st_mode).encode()); digest.update(b"\0")
        if file.is_symlink(): digest.update(os.fsencode(os.readlink(file)))
        elif file.is_file():
            with file.open("rb") as source:
                while chunk := source.read(1024 * 1024): digest.update(chunk)
        else: digest.update(b"nonregular")
        digest.update(b"\0")
    return digest.hexdigest()


if __name__ == "__main__": print(state_digest())

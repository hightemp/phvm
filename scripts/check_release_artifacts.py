#!/usr/bin/env python3
"""Validate archive identities, Go build metadata and native phvm execution."""
import argparse
import hashlib
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shlex
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile

TARGETS = [(system, arch) for system in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")]
VERSION_SYMBOL = "github.com/hightemp/phvm/internal/cli.Version="


def native_target():
    systems = {"Linux": "linux", "Darwin": "darwin", "Windows": "windows"}
    arches = {"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64", "arm64": "arm64"}
    return systems.get(platform.system()), arches.get(platform.machine())


def read_checksums(dist):
    result = {}
    for line in (dist / "checksums.txt").read_text().splitlines():
        if not line.strip(): continue
        match = re.fullmatch(r"([a-fA-F0-9]{64})\s+\*?([^\s/\\]+)", line)
        if not match: raise ValueError("malformed checksum entry")
        digest, name = match.groups()
        if name in result: raise ValueError("duplicate checksum entry")
        file = dist / name
        if not file.is_file() or file.is_symlink(): raise ValueError("checksum target must be a regular file")
        if hashlib.sha256(file.read_bytes()).hexdigest() != digest.lower(): raise ValueError(f"checksum mismatch: {name}")
        result[name] = digest
    return result


def binary_target(data):
    if data[:4] == b"\x7fELF":
        if len(data) < 20 or data[4] != 2 or data[5] != 1: raise ValueError("unsupported ELF layout")
        return "linux", {62: "amd64", 183: "arm64"}.get(struct.unpack_from("<H", data, 18)[0])
    if data[:4] == b"\xcf\xfa\xed\xfe":
        if len(data) < 8: raise ValueError("truncated Mach-O")
        return "darwin", {0x1000007: "amd64", 0x100000C: "arm64"}.get(struct.unpack_from("<I", data, 4)[0])
    if data[:2] == b"MZ":
        if len(data) < 64: raise ValueError("truncated PE")
        offset = struct.unpack_from("<I", data, 60)[0]
        if len(data) < offset + 6 or data[offset:offset + 4] != b"PE\0\0": raise ValueError("invalid PE")
        return "windows", {0x8664: "amd64", 0xAA64: "arm64"}.get(struct.unpack_from("<H", data, offset + 4)[0])
    raise ValueError("unknown executable format")


def archive_binary(file, binary):
    names = set()
    payload = None
    with zipfile.ZipFile(file) if file.suffix == ".zip" else tarfile.open(file, "r:gz") as archive:
        members = archive.infolist() if isinstance(archive, zipfile.ZipFile) else archive.getmembers()
        for member in members:
            name = member.filename if isinstance(archive, zipfile.ZipFile) else member.name
            path = PurePosixPath(name)
            if path.is_absolute() or ".." in path.parts or "\\" in name or name in names: raise ValueError("unsafe or duplicate archive member")
            names.add(name)
            if isinstance(archive, zipfile.ZipFile):
                mode = member.external_attr >> 16
                regular = not member.is_dir() and (mode & 0xF000) in (0, 0x8000)
                size = member.file_size
                if not regular and not member.is_dir(): raise ValueError("archive has links or special files")
            else:
                regular, mode, size = member.isfile(), member.mode, member.size
                if not regular and not member.isdir(): raise ValueError("archive has links or special files")
            if name == binary:
                if not regular or size <= 0 or size > 256 * 1024 * 1024: raise ValueError("binary must be a regular file within size limit")
                if file.suffix != ".zip" and mode & 0o111 == 0: raise ValueError("binary is not executable")
                if isinstance(archive, zipfile.ZipFile): payload = archive.read(member)
                else:
                    with archive.extractfile(member) as stream: payload = stream.read()
    if payload is None or not {binary, "LICENSE", "README.md"}.issubset(names): raise ValueError("required release files missing")
    return payload


def inspect_go_binary(file, target, version):
    result = subprocess.run(["go", "version", "-m", str(file)], capture_output=True, text=True, check=True, timeout=30)
    metadata = {}
    for line in result.stdout.splitlines():
        fields = shlex.split(line)
        if len(fields) == 2 and fields[0] == "build" and "=" in fields[1]:
            key, value = fields[1].split("=", 1); metadata[key] = value
    if (metadata.get("GOOS"), metadata.get("GOARCH")) != target or metadata.get("CGO_ENABLED") != "0": raise ValueError("Go target/CGO metadata mismatch")
    flags = shlex.split(metadata.get("-ldflags", ""))
    versions = [flag[len(VERSION_SYMBOL):] for flag in flags if flag.startswith(VERSION_SYMBOL)]
    if versions != [version]: raise ValueError("embedded linker version does not match release")


def check_archives(dist, version, targets=TARGETS):
    if not re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)", version): raise ValueError("invalid release version")
    checksums = read_checksums(dist)
    for target in targets:
        system, arch = target
        binary = "phvm.exe" if system == "windows" else "phvm"
        filename = f'phvm_{version}_{system}_{arch}{".zip" if system == "windows" else ".tar.gz"}'
        if filename not in checksums: raise ValueError(f"installer archive absent from checksums: {filename}")
        payload = archive_binary(dist / filename, binary)
        if binary_target(payload) != target: raise ValueError("executable architecture/OS mismatch")
        with tempfile.TemporaryDirectory(prefix="phvm-release-smoke-") as directory:
            executable = Path(directory) / binary
            executable.write_bytes(payload); executable.chmod(0o755)
            inspect_go_binary(executable, target, version)
            if target == native_target():
                result = subprocess.run([str(executable), "version"], env=dict(os.environ, PHVM_DIR=str(Path(directory) / "root")), capture_output=True, text=True, check=True, timeout=10)
                if not re.fullmatch(rf"phvm version {re.escape(version)} \([^\n]+\)\n", result.stdout) or result.stderr: raise ValueError("native binary version output mismatch")
        print(f"Checked {filename}: target, version, checksum, archive layout" + (", native execution" if target == native_target() else ", build metadata only"))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    parser.add_argument("--version", default=Path("VERSION").read_text().strip())
    args = parser.parse_args()
    try: check_archives(args.dist, args.version)
    except (OSError, ValueError, subprocess.SubprocessError, tarfile.TarError, zipfile.BadZipFile) as error:
        print(f"Artifact check failed: {error}", file=sys.stderr); sys.exit(1)

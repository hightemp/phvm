import hashlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest
import zipfile
import warnings
import sys


REPO = Path(__file__).resolve().parents[2]


class InstallerIntegrityTests(unittest.TestCase):
    def check_installer(self, powershell, case, downloader="curl"):
        with tempfile.TemporaryDirectory(prefix="phvm-installer-test-") as directory:
            root = Path(directory)
            phvm = root / "phvm"
            binary = phvm / "bin" / ("phvm.exe" if powershell else "phvm")
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"previous binary")
            binary.chmod(0o755)
            asset = "phvm_1.2.3_windows_amd64.zip" if powershell else "phvm_1.2.3_linux_amd64.tar.gz"
            archive = root / asset
            if powershell:
                with zipfile.ZipFile(archive, "w") as out:
                    out.writestr("phvm.exe", b"new binary")
                    if case == "duplicate binary":
                        with warnings.catch_warnings():
                            warnings.simplefilter("ignore", UserWarning)
                            out.writestr("phvm.exe", b"different binary")
            else:
                with tarfile.open(archive, "w:gz") as out:
                    info = tarfile.TarInfo("phvm")
                    info.mode = 0o755
                    info.size = len(b"new binary")
                    if case == "symlink binary":
                        info.type = tarfile.SYMTYPE
                        info.linkname = str(root / "outside")
                        info.size = 0
                    out.addfile(info, io.BytesIO(b"new binary"))
                    if case == "duplicate binary": out.addfile(info, io.BytesIO(b"new binary"))
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            if case == "bad hash": digest = "a" * 64
            if case == "invalid hash": digest = "wrong"
            filename = asset if case != "missing entry" else "other-" + asset
            if case == "invalid filename": filename = "**" + asset
            content = f"{digest}  {filename}\n"
            if case == "duplicate checksum": content += content
            if case == "truncated": archive.write_bytes(archive.read_bytes()[:20])
            checksums = root / "checksums.txt"
            checksums.write_text(content)
            env = dict(os.environ, PHVM_DIR=str(phvm), PHVM_VERSION="v1.2.3", PHVM_FIXTURE_ARCHIVE=str(archive), PHVM_FIXTURE_CHECKSUMS=str(checksums), PHVM_FIXTURE_CASE=case)
            trace = root / "requests"
            env["PHVM_FIXTURE_REQUESTS"] = str(trace)
            if case == "latest": env["PHVM_VERSION"] = "latest"
            if powershell:
                runner = root / "test.ps1"
                runner.write_text("""
function Invoke-WebRequest {
    param($Uri, $OutFile, [switch]$UseBasicParsing, $MaximumRedirection, [switch]$PassThru)
    $file = if ($Uri.ToString().EndsWith('checksums.txt')) { $env:PHVM_FIXTURE_CHECKSUMS } else { $env:PHVM_FIXTURE_ARCHIVE }
    if ($Uri.ToString().EndsWith('checksums.txt') -and $env:PHVM_FIXTURE_CASE -eq 'failed checksum download') { throw 'download failed' }
    Copy-Item -LiteralPath $file -Destination $OutFile
    return [pscustomobject]@{StatusCode=200;Headers=@{}}
}
. $args[0]
Install-Phvm
""")
                cmd = [shutil.which("pwsh"), "-NoProfile", "-File", str(runner), str(REPO / "scripts/install.ps1")]
            else:
                tools = root / "tools"
                tools.mkdir()
                curl = tools / "curl"
                curl.write_text("""#!/usr/bin/env python3
import os, pathlib, shutil, sys
args=sys.argv[1:]
url=next(arg for arg in args if arg.startswith('https://'))
dest=args[args.index('-o')+1]
checks=url.endswith('/checksums.txt')
if checks and os.environ['PHVM_FIXTURE_CASE']=='failed checksum download': sys.exit(22)
shutil.copyfile(os.environ['PHVM_FIXTURE_CHECKSUMS' if checks else 'PHVM_FIXTURE_ARCHIVE'],dest)
""")
                curl.chmod(0o755)
                uname = tools / "uname"
                uname.write_text("#!/bin/sh\nif [ \"$1\" = -s ]; then echo Linux; else echo x86_64; fi\n")
                uname.chmod(0o755)
                env["PATH"] = str(tools) + os.pathsep + env["PATH"]
                if downloader == "wget":
                    curl.unlink()
                    # This PATH genuinely contains no curl, without changing the host.
                    for tool in ["mkdir", "mktemp", "rm", "tar", "gzip", "chmod", "mv", "awk", "tr", "grep", "sed", "sha256sum", "basename"]:
                        location = shutil.which(tool)
                        if not location: self.skipTest(f"fixture needs {tool}")
                        (tools / tool).symlink_to(location)
                    (tools / "python3").symlink_to(sys.executable)
                    wget = tools / "wget"
                    wget.write_text("""#!/usr/bin/env python3
import os, pathlib, shutil, sys
args=sys.argv[1:]
if '--max-redirect=0' not in args or '--server-response' not in args: sys.exit(99)
url=args[-1]
dest=args[args.index('-O')+1]
case=os.environ['PHVM_FIXTURE_CASE']
with open(os.environ['PHVM_FIXTURE_REQUESTS'],'a') as trace: trace.write(url+'\\n')
if case=='redirect loop' or (case in ('https redirect','http redirect','relative redirect') and 'github.com/' in url):
    location=url if case=='redirect loop' else ('http://evil.invalid/archive' if case=='http redirect' else ('/resolved/'+url.rsplit('/',1)[-1] if case=='relative redirect' and '/resolved/' not in url else 'https://cdn.fixture.test/'+url.rsplit('/',1)[-1]))
    print('  HTTP/1.1 302 Found\\n  Location: '+location, file=sys.stderr)
    sys.exit(8)
checks=url.endswith('/checksums.txt')
if checks and case=='failed checksum download': sys.exit(8)
print('  HTTP/1.1 200 OK',file=sys.stderr)
if url.endswith('/latest'): pathlib.Path(dest).write_text('{"tag_name": "v1.2.3"}\\n')
else: shutil.copyfile(os.environ['PHVM_FIXTURE_CHECKSUMS' if checks else 'PHVM_FIXTURE_ARCHIVE'],dest)
""")
                    wget.chmod(0o755)
                    env["PATH"] = str(tools)
                cmd = ["bash", str(REPO / "scripts/install.sh")]
                if downloader == "wget": cmd[0] = shutil.which("bash")
            result = subprocess.run(cmd, env=env, capture_output=True, text=True, timeout=20)
            if case in ("valid", "latest", "https redirect", "relative redirect"):
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(binary.read_bytes(), b"new binary")
            else:
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(binary.read_bytes(), b"previous binary", "untrusted archive changed active binary")
            if downloader == "wget":
                requests = trace.read_text().splitlines() if trace.exists() else []
                self.assertTrue(requests, "wget fallback was not used")
                self.assertTrue(all(url.startswith('https://') for url in requests), requests)
                if case == "http redirect": self.assertEqual(len(requests), 1)
                if case == "redirect loop": self.assertLessEqual(len(requests), 10)

    def test_bash_installer_checks_before_publication(self):
        if os.name == "nt": self.skipTest("Bash fixture runs on Unix")
        for case in ["valid", "bad hash", "invalid hash", "missing entry", "invalid filename", "duplicate checksum", "failed checksum download", "truncated", "duplicate binary", "symlink binary"]:
            with self.subTest(case=case): self.check_installer(False, case)

    def test_powershell_installer_checks_before_publication(self):
        if not shutil.which("pwsh"): self.skipTest("PowerShell is unavailable; native CI runs this fixture")
        for case in ["valid", "bad hash", "invalid hash", "missing entry", "invalid filename", "duplicate checksum", "failed checksum download", "truncated", "duplicate binary"]:
            with self.subTest(case=case): self.check_installer(True, case)

    def test_wget_fallback_without_curl(self):
        if os.name == "nt": self.skipTest("Bash fixture runs on Unix")
        for case in ["valid", "latest", "bad hash", "invalid hash", "missing entry", "duplicate checksum", "failed checksum download", "truncated", "duplicate binary", "symlink binary", "https redirect", "relative redirect", "http redirect", "redirect loop"]:
            with self.subTest(case=case): self.check_installer(False, case, "wget")


if __name__ == "__main__":
    unittest.main()

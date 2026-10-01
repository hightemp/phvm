"""Archive smoke checks validate real Go binaries and hostile package layouts."""
import hashlib
import io
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))


class ArtifactTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="phvm-artifact-test-")
        cls.addClassCleanup(cls.temp.cleanup)
        cls.binary = Path(cls.temp.name) / "phvm"
        env = dict(os.environ, CGO_ENABLED="0")
        subprocess.run(["go", "build", "-ldflags", "-X github.com/hightemp/phvm/internal/cli.Version=1.2.3 -X github.com/hightemp/phvm/internal/cli.Commit=fixture", "-o", str(cls.binary), "./cmd/phvm"], cwd=ROOT, env=env, check=True, capture_output=True)

    def test_archive_and_binary_contract(self):
        import check_release_artifacts as check
        goos = {"Linux": "linux", "Darwin": "darwin", "Windows": "windows"}[platform.system()]
        arch = {"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[platform.machine()]
        if goos == "windows": self.skipTest("tar fixture; native Windows archive smoke runs in CI")
        for case in ["valid", "wrong version", "bad hash", "duplicate checksum", "missing binary", "symlink", "duplicate binary", "wrong architecture", "unsafe member"]:
            with self.subTest(case=case), tempfile.TemporaryDirectory() as directory:
                folder = Path(directory)
                asset_version = "9.9.9" if case == "wrong version" else "1.2.3"
                asset = f"phvm_{asset_version}_{goos}_{arch}.tar.gz"
                data = bytearray(self.binary.read_bytes())
                if case == "wrong architecture" and goos == "linux": data[18:20] = (183 if arch == "amd64" else 62).to_bytes(2, "little")
                if case == "wrong architecture" and goos == "darwin": data[4:8] = (0x100000C if arch == "amd64" else 0x1000007).to_bytes(4, "little")
                with tarfile.open(folder / asset, "w:gz") as out:
                    name = "phvm" if case != "missing binary" else "other"
                    info = tarfile.TarInfo(name); info.mode = 0o755; info.size = len(data)
                    if case == "symlink": info.type = tarfile.SYMTYPE; info.linkname = "/outside"; info.size = 0
                    out.addfile(info, io.BytesIO(data))
                    if case == "duplicate binary": out.addfile(info, io.BytesIO(data))
                    for name in ["README.md", "LICENSE"]:
                        info = tarfile.TarInfo(name); info.size = 0; out.addfile(info)
                    if case == "unsafe member": out.addfile(tarfile.TarInfo("../../outside"))
                digest = hashlib.sha256((folder / asset).read_bytes()).hexdigest()
                if case == "bad hash": digest = "a" * 64
                checksums = f"{digest}  {asset}\n"
                if case == "duplicate checksum": checksums += checksums
                (folder / "checksums.txt").write_text(checksums)
                if case == "valid":
                    check.check_archives(folder, "1.2.3", targets=[(goos, arch)])
                else:
                    with self.assertRaises((ValueError, subprocess.SubprocessError)):
                        check.check_archives(folder, "9.9.9" if case == "wrong version" else "1.2.3", targets=[(goos, arch)])

    def test_make_preserves_user_go_flags(self):
        make = shutil.which("make")
        if not make: self.skipTest("make unavailable")
        result = subprocess.run(
            [make, "--no-print-directory", "--eval", "phvm-test-go-flags:\n\t@echo $(GOFLAGS)", "phvm-test-go-flags"],
            cwd=ROOT, env=dict(os.environ, GOFLAGS="-p=1"),
            capture_output=True, text=True, check=True,
        )
        self.assertEqual(result.stdout.strip(), "-p=1", "Makefile replaced user GOFLAGS, losing resource limits")

    def test_zip_rejects_links_in_required_documents(self):
        import check_release_artifacts as check
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "release.zip"
            with zipfile.ZipFile(archive, "w") as out:
                out.writestr("phvm.exe", b"fixture")
                out.writestr("LICENSE", b"license")
                link = zipfile.ZipInfo("README.md")
                link.external_attr = 0o120777 << 16
                out.writestr(link, b"/outside")
            with self.assertRaises(ValueError): check.archive_binary(archive, "phvm.exe")


if __name__ == "__main__":
    unittest.main()

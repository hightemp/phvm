"""Exercise make release without contacting GitHub or changing the working repo."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


PROJECT_ROOT = Path(__file__).resolve().parents[2]


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="phvm-release-test-")
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.repo = root / "repo"
        self.remote = root / "origin.git"
        self.repo.mkdir()
        self.env = dict(os.environ, GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull)
        # Tests must never inherit credentials, an index, or an alternate Git dir.
        for key in list(self.env):
            if key.startswith("GIT_") and key not in {"GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL"}:
                del self.env[key]
        self.run_command("git", "init", "--bare", str(self.remote))
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "release-test@example.invalid")
        (self.repo / "scripts").mkdir()
        shutil.copyfile(PROJECT_ROOT / "Makefile", self.repo / "Makefile")
        shutil.copyfile(PROJECT_ROOT / "scripts/release.sh", self.repo / "scripts/release.sh")
        for name in ["tool_version.py", "tool_versions.json", "release_state.py"]:
            shutil.copyfile(PROJECT_ROOT / "scripts" / name, self.repo / "scripts" / name)
        # Isolate expensive preflight tools while exercising the real publish path.
        with (self.repo / "Makefile").open("a") as makefile:
            makefile.write('''
.PHONY: release-check
release-check:
\t@printf 'checked\\n' >> preflight-ran
\t@if [ "$$PHVM_TEST_PREFLIGHT_FAIL" = 1 ]; then echo "fixture check failed"; exit 1; fi
\t@if [ "$$PHVM_TEST_CHANGE_VERSION" = 1 ]; then echo 9.9.9 > VERSION; fi
\t@if [ "$$PHVM_TEST_CHANGE_SOURCE" = 1 ]; then echo changed > source.txt; fi
''')
        self.env["PHVM_TEST_PREFLIGHT_FAIL"] = "0"
        self.env["PHVM_TEST_CHANGE_VERSION"] = "0"
        self.env["PHVM_TEST_CHANGE_SOURCE"] = "0"
        (self.repo / "VERSION").write_text("1.0.6\n")
        (self.repo / ".gitignore").write_text("/phvm\n/preflight-ran\n")
        self.git("add", ".")
        self.git("commit", "-m", "Initial version")
        self.git("remote", "add", "origin", str(self.remote))
        self.git("push", "origin", "main")
        self.initial = self.git("rev-parse", "HEAD").stdout.strip()
        (self.repo / "VERSION").write_text("1.0.7\n")

    def run_command(self, *args, check=True):
        return subprocess.run(args, cwd=self.repo, env=self.env, check=check,
                              text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)

    def git(self, *args, **kwargs):
        return self.run_command("git", *args, **kwargs)

    def remote_git(self, *args, **kwargs):
        return self.git("--git-dir=" + str(self.remote), *args, **kwargs)

    def release(self):
        return self.run_command("make", "release", check=False)

    def assert_no_local_release(self):
        self.assertEqual(self.git("rev-parse", "HEAD").stdout.strip(), self.initial)
        self.assertEqual(self.git("diff", "--cached", "--name-only").stdout, "")

    def test_commits_changes_and_pushes_annotated_tag(self):
        (self.repo / "change.txt").write_text("release content\n")
        (self.repo / "phvm").write_text("ignored binary\n")
        result = self.release()
        self.assertEqual(result.returncode, 0, result.stdout)
        head = self.git("rev-parse", "HEAD").stdout.strip()
        self.assertNotEqual(head, self.initial)
        self.assertEqual(self.git("log", "-1", "--format=%s").stdout.strip(), "chore: release v1.0.7")
        self.assertEqual(self.git("cat-file", "-t", "v1.0.7").stdout.strip(), "tag")
        self.assertEqual(self.remote_git("rev-parse", "refs/heads/main").stdout.strip(), head)
        self.assertEqual(self.remote_git("rev-parse", "v1.0.7^{commit}").stdout.strip(), head)
        self.assertEqual(self.remote_git("show", "v1.0.7:VERSION").stdout, "1.0.7\n")
        self.assertEqual(self.remote_git("show", "v1.0.7:change.txt").stdout, "release content\n")
        self.assertEqual(self.git("status", "--porcelain").stdout, "")
        self.assertNotIn("phvm\n", self.git("ls-files").stdout)
        self.assertTrue((self.repo / "preflight-ran").exists(), "release skipped preflight")
        self.assertIn("change.txt", result.stdout, "release changes were not previewed")

    def test_failed_preflight_preserves_head_index_tags_and_remote(self):
        self.env["PHVM_TEST_PREFLIGHT_FAIL"] = "1"
        (self.repo / "staged.txt").write_text("already staged\n")
        self.git("add", "staged.txt")
        index_before = self.git("write-tree").stdout
        result = self.release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(self.git("rev-parse", "HEAD").stdout.strip(), self.initial)
        self.assertEqual(self.git("write-tree").stdout, index_before)
        self.assertEqual(self.git("tag", "--list").stdout, "")
        self.assertEqual(self.remote_git("rev-parse", "refs/heads/main").stdout.strip(), self.initial)
        self.assertEqual(self.remote_git("tag", "--list").stdout, "")

    def test_preflight_cannot_change_version_before_tagging(self):
        self.env["PHVM_TEST_CHANGE_VERSION"] = "1"
        result = self.release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assert_no_local_release()
        self.assertEqual(self.git("tag", "--list").stdout, "")

    def test_preflight_cannot_publish_changed_source(self):
        self.env["PHVM_TEST_CHANGE_SOURCE"] = "1"
        result = self.release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assert_no_local_release()
        self.assertEqual(self.git("tag", "--list").stdout, "")

    def test_release_tag_must_match_version_and_head(self):
        import sys
        checker = PROJECT_ROOT / "scripts/check_release_version.py"
        self.git("add", "VERSION")
        self.git("commit", "-m", "Prepare release")
        self.git("tag", "v1.0.7")
        for tag, version, valid in [("v1.0.7", "1.0.7", True), ("v1.0.8", "1.0.7", False), ("v1.0.7", "1.0.8", False)]:
            with self.subTest(tag=tag, version=version):
                (self.repo / "VERSION").write_text(version + "\n")
                result = self.run_command(sys.executable, str(checker), "--tag", tag, check=False)
                self.assertEqual(result.returncode == 0, valid, result.stdout)

    def test_invalid_version_does_not_stage_or_commit(self):
        for version in ["", "1.2", "v1.2.3", "01.2.3", "../bad", "1.2.3\n1.2.4"]:
            with self.subTest(version=version):
                (self.repo / "VERSION").write_text(version + "\n")
                result = self.release()
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assert_no_local_release()
        self.assertEqual(self.git("tag", "--list").stdout, "")

    def test_already_committed_version_creates_release_commit(self):
        self.git("add", "VERSION")
        self.git("commit", "-m", "Prepare next version")
        previous = self.git("rev-parse", "HEAD").stdout.strip()
        result = self.release()
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertEqual(self.git("rev-parse", "HEAD^").stdout.strip(), previous)
        self.assertEqual(self.git("diff", "HEAD^", "HEAD", "--name-only").stdout, "")
        self.assertEqual(self.remote_git("show", "v1.0.7:VERSION").stdout, "1.0.7\n")

    def test_existing_local_tag_does_not_commit(self):
        self.git("tag", "v1.0.7")
        result = self.release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("already exists locally", result.stdout)
        self.assert_no_local_release()

    def test_existing_remote_tag_does_not_commit(self):
        self.remote_git("tag", "v1.0.7", "refs/heads/main")
        result = self.release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("already exists on origin", result.stdout)
        self.assert_no_local_release()
        self.assertEqual(self.git("tag", "--list").stdout, "")

    def test_detached_head_does_not_commit(self):
        self.git("checkout", "--detach")
        result = self.release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("check out a branch", result.stdout)
        self.assert_no_local_release()

    def test_failed_push_preserves_remote_and_prints_retry(self):
        hook = self.remote / "hooks/update"
        hook.write_text('#!/bin/sh\n[ "$1" != "refs/heads/main" ]\n')
        hook.chmod(0o755)
        result = self.release()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("git push --atomic origin HEAD:refs/heads/main refs/tags/v1.0.7", result.stdout)
        self.assertEqual(self.remote_git("rev-parse", "refs/heads/main").stdout.strip(), self.initial)
        self.assertEqual(self.remote_git("tag", "--list").stdout, "")
        self.assertEqual(self.git("cat-file", "-t", "v1.0.7").stdout.strip(), "tag")


if __name__ == "__main__":
    unittest.main()

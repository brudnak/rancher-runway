import argparse
import base64
import contextlib
import hashlib
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import release


class VersionTests(unittest.TestCase):
    def test_first_release_and_numeric_bumps(self):
        self.assertEqual(release.next_version([], "patch"), "v1.0.0")
        tags = ["v1.9.9", "v1.10.2", "v2.0.0-rc.1", "not-a-version"]
        for bump, expected in [("patch", "v1.10.3"), ("minor", "v1.11.0"), ("major", "v2.0.0")]:
            with self.subTest(bump=bump):
                self.assertEqual(release.next_version(tags, bump), expected)

    def test_explicit_stable_versions(self):
        self.assertEqual(release.parse_version("v1.0.0"), (1, 0, 0))
        self.assertEqual(release.parse_version("1.2.3"), (1, 2, 3))
        for value in ["v1", "1.01.0", "1.0.0-rc.1", "1.0.0+build", "1.0.0\n", "-1.0.0"]:
            with self.subTest(value=value), self.assertRaises(release.ReleaseError):
                release.parse_version(value)


class SourceTests(unittest.TestCase):
    def test_changed_missing_and_new_release_files(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            content = b"published source\n"
            (root / "source.go").write_bytes(content)
            sha = hashlib.sha1(b"blob " + str(len(content)).encode() + b"\0" + content).hexdigest()
            tree = {"tree": [{"type": "blob", "mode": "100644", "path": "source.go", "sha": sha}]}
            with patch.object(release, "RELEASE_FILES", ("source.go",)):
                self.assertEqual(release.source_mismatches(tree, root), [])
                (root / "source.go").write_text("unpublished changes")
                self.assertEqual(release.source_mismatches(tree, root), ["source.go"])
                (root / "source.go").unlink()
                self.assertEqual(release.source_mismatches(tree, root), ["source.go"])
            with patch.object(release, "RELEASE_FILES", ("new-release.py",)):
                self.assertEqual(release.source_mismatches(tree, root), ["new-release.py", "source.go"])

    def test_truncated_tree_is_rejected(self):
        with self.assertRaises(release.ReleaseError):
            release.source_mismatches({"truncated": True})


class ReleaseFlowTests(unittest.TestCase):
    def setUp(self):
        self.output = contextlib.redirect_stdout(io.StringIO())
        self.output.__enter__()
        self.addCleanup(self.output.__exit__, None, None, None)
        self.calls = []
        self.published = False

    def fake_api(self, endpoint, method="GET", payload=None, missing_ok=False):
        self.calls.append((endpoint, method, payload))
        if "/releases/tags/" in endpoint:
            return {"draft": False, "prerelease": False, "html_url": "https://example.test/release"} if self.published else None
        if "/commits/" in endpoint:
            return {"sha": "abc123", "commit": {"tree": {"sha": "tree123"}}}
        if "/git/trees/" in endpoint:
            return {"tree": []}
        if endpoint.endswith("/actions/workflows/release-macos.yml"):
            return {"state": "active"}
        if endpoint.endswith("/git/refs"):
            return {"ref": payload["ref"]}
        if endpoint == "repos/brudnak/homebrew-tap":
            return {}
        self.fail(f"Unexpected API endpoint: {endpoint}")

    def args(self, dry_run=False):
        return argparse.Namespace(version="", bump="patch", ref="main", dry_run=dry_run)

    @patch.object(release, "all_pages", return_value=[])
    @patch.object(release, "source_mismatches", return_value=[])
    def test_plan_is_read_only(self, *_):
        with patch.object(release, "api", side_effect=self.fake_api), patch.object(release, "gh") as cli:
            release.run(self.args(dry_run=True))
        self.assertTrue(all(method == "GET" for _, method, _ in self.calls))
        cli.assert_not_called()

    @patch.object(release, "all_pages", return_value=[])
    @patch.object(release, "source_mismatches", return_value=["scripts/release.py"])
    def test_unpublished_source_cannot_create_a_tag(self, *_):
        with patch.object(release, "api", side_effect=self.fake_api), self.assertRaisesRegex(release.ReleaseError, "Publish the intended source first"):
            release.run(self.args())
        self.assertTrue(all(method == "GET" for _, method, _ in self.calls))

    @patch.object(release, "all_pages", return_value=[])
    @patch.object(release, "source_mismatches", return_value=[])
    def test_new_release_tags_exact_source_before_updating_tap(self, *_):
        def complete(tag, commit):
            self.assertEqual((tag, commit), ("v1.0.0", "abc123"))
            self.published = True
        with patch.object(release, "api", side_effect=self.fake_api), patch.object(release, "wait_for_run", side_effect=complete), patch.object(release, "update_tap") as update:
            release.run(self.args())
        writes = [(endpoint, payload) for endpoint, method, payload in self.calls if method != "GET"]
        self.assertEqual(writes, [("repos/brudnak/rancher-runway/git/refs", {"ref": "refs/tags/v1.0.0", "sha": "abc123"})])
        update.assert_called_once_with("v1.0.0")

    @patch.object(release, "all_pages", return_value=[])
    @patch.object(release, "source_mismatches", return_value=[])
    def test_failed_build_does_not_update_tap(self, *_):
        with patch.object(release, "api", side_effect=self.fake_api), patch.object(release, "wait_for_run", side_effect=release.ReleaseError("build failed")), patch.object(release, "update_tap") as update:
            with self.assertRaisesRegex(release.ReleaseError, "build failed"):
                release.run(self.args())
        update.assert_not_called()

    @patch.object(release, "all_pages", return_value=[])
    def test_published_release_retry_only_updates_tap(self, *_):
        self.published = True
        args = self.args()
        args.version = "1.0.0"
        with patch.object(release, "api", side_effect=self.fake_api), patch.object(release, "update_tap") as update, patch.object(release, "wait_for_run") as wait:
            release.run(args)
        update.assert_called_once_with("v1.0.0")
        wait.assert_not_called()
        self.assertTrue(all(method == "GET" for _, method, _ in self.calls))

    @patch.object(release, "all_pages", return_value=[])
    def test_draft_release_is_preserved(self, *_):
        with patch.object(release, "api", return_value={"draft": True}), patch.object(release, "update_tap") as update:
            with self.assertRaisesRegex(release.ReleaseError, "draft or prerelease"):
                release.run(self.args())
        update.assert_not_called()


class TapTests(unittest.TestCase):
    def cask(self, version="1.0.0", checksum=None):
        checksum = checksum or hashlib.sha256(b"dmg fixture").hexdigest()
        return (f'cask "rancher-runway" do\n  version "{version}"\n  sha256 "{checksum}"\n'
                f'  url "https://github.com/brudnak/rancher-runway/releases/download/v{version}/Rancher-Runway-{version}-macOS-universal.dmg",\n'
                '  app "Rancher Runway.app"\nend\n').encode()

    def download(self, *args, body=None):
        folder = Path(args[args.index("--dir") + 1])
        (folder / "rancher-runway.rb").write_bytes(self.downloaded_cask)
        (folder / "Rancher-Runway-1.0.0-macOS-universal.dmg").write_bytes(b"dmg fixture")

    def test_matching_checksum_creates_cask(self):
        self.downloaded_cask = self.cask()
        with patch.object(release, "gh", side_effect=self.download), patch.object(release, "api", return_value=None) as api, contextlib.redirect_stdout(io.StringIO()):
            release.update_tap("v1.0.0")
        endpoint = "repos/brudnak/homebrew-tap/contents/Casks/rancher-runway.rb"
        self.assertEqual(api.call_args.args, (endpoint,))
        self.assertEqual(api.call_args.kwargs["method"], "PUT")
        self.assertEqual(base64.b64decode(api.call_args.kwargs["payload"]["content"]), self.downloaded_cask)

    def test_checksum_mismatch_never_writes(self):
        self.downloaded_cask = self.cask(checksum="0" * 64)
        with patch.object(release, "gh", side_effect=self.download), patch.object(release, "api") as api:
            with self.assertRaisesRegex(release.ReleaseError, "sha256"):
                release.update_tap("v1.0.0")
        api.assert_not_called()

    def test_newer_tap_is_preserved(self):
        self.downloaded_cask = self.cask()
        current = {"sha": "old-sha", "content": base64.b64encode(self.cask(version="1.1.0")).decode()}
        with patch.object(release, "gh", side_effect=self.download), patch.object(release, "api", return_value=current) as api:
            with self.assertRaisesRegex(release.ReleaseError, "refusing to downgrade"):
                release.update_tap("v1.0.0")
        self.assertEqual(api.call_count, 1)

    def test_identical_tap_skips_commit(self):
        self.downloaded_cask = self.cask()
        current = {"sha": "old-sha", "content": base64.b64encode(self.downloaded_cask).decode()}
        with patch.object(release, "gh", side_effect=self.download), patch.object(release, "api", return_value=current) as api, contextlib.redirect_stdout(io.StringIO()):
            release.update_tap("v1.0.0")
        self.assertEqual(api.call_count, 1)


if __name__ == "__main__":
    unittest.main()

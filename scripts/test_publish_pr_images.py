"""Exercise publication with a fake gh executable; never contacts GitHub."""

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("publish-pr-images.py")


class PublishImagesTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="herdr-sbx-publish-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.image = self.root / "frame.png"
        self.image.write_bytes(b"simulated image")
        self.body = self.root / "body.md"
        self.prose = "Requested summary.\n\n![A useful frame](frame.png)\n\nValidation unchanged.\n"
        self.body.write_text(self.prose)
        fake = self.root / "gh"
        fake.write_text("#!" + sys.executable + "\n" + '''
import json, os, pathlib, re, sys
root = pathlib.Path(os.environ["FAKE_GH_ROOT"])
with (root / "calls").open("a") as log:
    log.write(json.dumps(sys.argv[1:]) + "\\n")
mode = os.environ.get("FAKE_GH_MODE", "success")
if sys.argv[2] == "edit":
    if mode == "upload-failed":
        sys.exit(1)
    body = pathlib.Path(sys.argv[sys.argv.index("--body-file") + 1]).read_text()
    if mode != "not-uploaded":
        body = re.sub(r"!\\[([^]]*)\\]\\([^)]+\\)", r"![\\1](https://github.com/user-attachments/assets/abc-123)", body)
    if mode == "changed-prose":
        body += "Unexpected prose"
    (root / "saved").write_text(body)
else:
    if mode == "view-failed":
        sys.exit(1)
    print(json.dumps({"body": (root / "saved").read_text()}))
''')
        fake.chmod(0o700)
        self.env = {**os.environ, "PATH": str(self.root) + os.pathsep + os.environ.get("PATH", ""),
                    "FAKE_GH_ROOT": str(self.root)}

    def run_publish(self, mode="success", image="frame.png"):
        return subprocess.run([sys.executable, str(SCRIPT), "--pr", "7", "--repo", "example/project",
                               "--body-file", "body.md", image], cwd=self.root,
                              env={**self.env, "FAKE_GH_MODE": mode}, capture_output=True, text=True)

    def calls(self):
        path = self.root / "calls"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_upload_and_verify_preserve_prose_and_input(self):
        result = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Verified 1 uploaded images", result.stdout)
        self.assertEqual(self.body.read_text(), self.prose)
        calls = self.calls()
        self.assertEqual([call[1] for call in calls], ["edit", "view"])
        self.assertEqual(calls[0][-2:], ["--attach", str(self.image.resolve())])
        self.assertIn("example/project", calls[0])

    def test_invalid_image_or_missing_reference_never_calls_gh(self):
        for image in ("missing.png", "body.md"):
            result = self.run_publish(image=image)
            self.assertNotEqual(result.returncode, 0)
        self.body.write_text("No image reference.")
        self.assertNotEqual(self.run_publish().returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_upload_failure_is_not_retried_or_verified(self):
        result = self.run_publish("upload-failed")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("partially updated", result.stderr)
        self.assertEqual([call[1] for call in self.calls()], ["edit"])

    def test_unverified_upload_or_prose_change_is_reported_without_retry(self):
        for mode in ("not-uploaded", "changed-prose", "view-failed"):
            with self.subTest(mode=mode):
                (self.root / "calls").unlink(missing_ok=True)
                result = self.run_publish(mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("inspect the PR before retrying", result.stderr)
                self.assertEqual([call[1] for call in self.calls()], ["edit", "view"])


if __name__ == "__main__":
    unittest.main()

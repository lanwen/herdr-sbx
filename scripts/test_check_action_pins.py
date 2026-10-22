"""Check literal action pin validation with Ruby/Psych; no GitHub calls."""

import pathlib
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("check-action-pins.rb")
SHA = "3d3c42e5aac5ba805825da76410c181273ba90b1"


class ActionPinTests(unittest.TestCase):
    def check(self, body):
        with tempfile.TemporaryDirectory(prefix="herdr-sbx-pins-") as directory:
            path = pathlib.Path(directory) / "fixture.yml"
            path.write_text(body)
            original = path.read_bytes()
            result = subprocess.run(["ruby", str(SCRIPT), str(path)], capture_output=True, text=True)
            self.assertEqual(path.read_bytes(), original, "pin checking changed its input")
            return result

    def test_full_commit_and_local_and_reusable_workflow(self):
        result = self.check(f"""jobs:
  check:
    steps:
      - uses: actions/checkout@{SHA} # version comment
      - uses: ./local-action
  reuse:
    uses: owner/repo/.github/workflows/check.yml@{SHA}
""")
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_mutable_tags_and_branches_fail_without_rewriting(self):
        for reference in ("actions/checkout@v7", "owner/repo@main", "owner/repo@" + SHA[:-1]):
            with self.subTest(reference=reference):
                result = self.check(f"jobs: {{check: {{steps: [{{uses: {reference}}}]}}}}")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("must pin a full commit SHA", result.stderr)

    def test_quoted_and_inline_uses_are_checked(self):
        for quote in ("'", '"'):
            with self.subTest(quote=quote):
                for reference, valid in (("owner/repo@" + SHA, True), ("owner/repo@main", False)):
                    body = f"jobs: {{check: {{steps: [{{{quote}uses{quote}: {quote}{reference}{quote}}}]}}}} # uses: ignored@v1"
                    result = self.check(body)
                    self.assertEqual(result.returncode == 0, valid, result.stderr)
        # YAML decoding prevents escaped mapping keys concealing a reference.
        result = self.check('jobs: {check: {steps: [{"us\\u0065s": "owner/repo@main"}]}}')
        self.assertNotEqual(result.returncode, 0)

    def test_env_uses_and_run_text_are_not_action_references(self):
        result = self.check("""jobs:
  check:
    env: {uses: arbitrary-value}
    steps:
      - run: |
          echo 'uses: arbitrary-value'
        env: {uses: arbitrary-value}
""")
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_docker_requires_digest(self):
        for reference, valid in (("docker://alpine@sha256:" + "a" * 64, True),
                                 ("docker://alpine:3", False)):
            result = self.check(f"jobs: {{check: {{steps: [{{uses: '{reference}'}}]}}}}")
            self.assertEqual(result.returncode == 0, valid, result.stderr)

    def test_composite_metadata_uses_are_checked(self):
        for reference, valid in (("owner/repo@" + SHA, True), ("owner/repo@main", False)):
            result = self.check(f"runs: {{using: composite, steps: [{{uses: '{reference}'}}]}}")
            self.assertEqual(result.returncode == 0, valid, result.stderr)

    def test_indirection_duplicate_keys_and_invalid_yaml_fail_closed(self):
        for body in ("jobs: {check: {steps: &steps [{uses: owner/repo@main}]}}",
                     "jobs: {check: {steps: *steps}}",
                     "jobs: {check: {steps: [{uses: !!str owner/repo@main}]}}",
                     f"jobs: {{check: {{steps: [{{uses: owner/repo@main, uses: owner/repo@{SHA}}}]}}}}",
                     "jobs: [invalid"):
            with self.subTest(body=body):
                result = self.check(body)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Action pin check failed", result.stderr)


if __name__ == "__main__":
    unittest.main()

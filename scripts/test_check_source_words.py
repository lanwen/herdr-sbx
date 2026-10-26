import importlib.util
import pathlib
import subprocess
import tempfile
import unittest


class SourceWordsTests(unittest.TestCase):
    def test_source_text_and_untracked_files_are_checked(self):
        script = pathlib.Path(__file__).with_name("check-source-words.py")
        spec = importlib.util.spec_from_file_location("source_words", script)
        checker = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(checker)
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            subprocess.run(["git", "init", "--quiet", directory], check=True)
            (root / "tracked.go").write_text('package fixture\nvar OutdatedValue = "OUTDATED" // outdated\n')
            subprocess.run(["git", "-C", directory, "add", "tracked.go"], check=True)
            (root / "new.py").write_text('# outdated\nvalue = "outdated"\n')
            (root / "README.md").write_text("outdated\n")
            (root / ".gitignore").write_text("ignored.go\n")
            (root / "ignored.go").write_text("outdated\n")
            findings = checker.check(root, ["outdated"])
            self.assertEqual(len(findings), 3)
            self.assertTrue(any("tracked.go:2:" in item for item in findings))
            self.assertTrue(any("new.py:1:" in item for item in findings))
            self.assertEqual(checker.check(root, ["obsolete"]), [])
            (root / "scripts").mkdir()
            command = root / "scripts" / script.name
            command.write_text(script.read_text())
            failed = subprocess.run(["python3", str(command), "outdated"], capture_output=True)
            self.assertEqual(failed.returncode, 1)
            self.assertIn(b"tracked.go:2:", failed.stdout)
            clean = subprocess.run(["python3", str(command), "obsolete"], capture_output=True)
            self.assertEqual(clean.returncode, 0)


if __name__ == "__main__":
    unittest.main()

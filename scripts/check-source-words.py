#!/usr/bin/env python3
"""Reject configured words in source files, including comments and identifiers."""

import pathlib
import subprocess
import sys


def source_file(path):
    return path.suffix.lower() in {".go", ".py", ".sh", ".rb", ".swift"}


def check(root, words):
    names = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=root,
    )
    findings = []
    for name in sorted(set(names.split(b"\0")) - {b""}):
        path = pathlib.Path(name.decode("utf-8"))
        if not source_file(path):
            continue
        selected = root / path
        if selected.is_symlink():
            raise OSError(f"source file is a symbolic link: {path}")
        if not selected.exists():
            continue  # Tracked files deleted in the working tree are not code.
        for line_number, line in enumerate(selected.read_text().splitlines(), 1):
            for word in words:
                if word.casefold() in line.casefold():
                    findings.append(f"{path}:{line_number}: forbidden source word {word!r}")
    return findings


def main(words):
    if not words or any(not word for word in words):
        print("usage: check-source-words.py WORD [WORD ...]", file=sys.stderr)
        return 2
    root = pathlib.Path(__file__).resolve().parents[1]
    try:
        findings = check(root, words)
    except (OSError, UnicodeError, subprocess.CalledProcessError) as error:
        print(f"Source word check failed: {error}", file=sys.stderr)
        return 2
    for finding in findings:
        print(finding)
    return int(bool(findings))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

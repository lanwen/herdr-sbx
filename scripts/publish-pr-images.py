#!/usr/bin/env python3
"""Upload explicit PR image references once, then verify links and unchanged prose.

Paths in Markdown references are relative to the current working directory.
A failed upload can partially update the PR; inspect it before another invocation.
"""

import argparse
import json
import pathlib
import re
import subprocess
import sys
import tempfile

IMAGE = re.compile(r"!\[([^\]\n]*)\]\(([^)\n]+)\)")
ASSET = re.compile(r"https://github\.com/user-attachments/assets/[A-Za-z0-9-]+\Z")


def publish(args):
    if not re.fullmatch(r"[1-9][0-9]*|https://[^/]+/[^/]+/[^/]+/pull/[1-9][0-9]*", args.pr):
        raise ValueError("--pr must be a PR number or full pull-request URL")
    images = [pathlib.Path(image).resolve() for image in args.images]
    if not 1 <= len(images) <= 50 or len(set(images)) != len(images):
        raise ValueError("supply 1–50 distinct image files")
    for image in images:
        if "#" in str(image):
            raise ValueError("image paths cannot contain gh attachment alt-text delimiter #")
        if not image.is_file() or image.suffix.lower() not in (".png", ".jpg", ".jpeg", ".gif", ".webp"):
            raise ValueError(f"not a supported image file: {image}")
    body = pathlib.Path(args.body_file).read_text(encoding="utf-8")
    references = list(IMAGE.finditer(body))
    attached = set(images)
    matches = {}
    for match in references:
        target = match.group(2)
        if not target.startswith(("https://", "http://")):
            path = pathlib.Path(target).resolve()
            if path not in attached:
                raise ValueError(f"body contains an image not supplied for upload: {target}")
            matches[match.start()] = path
    if set(matches.values()) != attached:
        raise ValueError("each supplied image must appear as ![alt](path) in the body")
    # Normalize just the local image targets; gh rewrites them to uploaded URLs.
    prepared = IMAGE.sub(lambda match: f"![{match.group(1)}]({matches[match.start()]})"
                         if match.start() in matches else match.group(0), body)
    repo = ["--repo", args.repo] if args.repo else []
    with tempfile.TemporaryDirectory(prefix="herdr-sbx-pr-images-") as directory:
        path = pathlib.Path(directory) / "body.md"
        path.write_text(prepared, encoding="utf-8")
        command = ["gh", "pr", "edit", args.pr, *repo, "--body-file", str(path)]
        for image in images:
            command.extend(["--attach", str(image)])
        result = subprocess.run(command, check=False)
        if result.returncode:
            raise RuntimeError("gh image upload failed; the PR may be partially updated. Inspect it before retrying.")
    try:
        result = subprocess.run(["gh", "pr", "view", args.pr, *repo, "--json", "body"],
                                check=True, capture_output=True, text=True)
    except subprocess.CalledProcessError as error:
        raise RuntimeError("upload finished but PR verification failed; inspect the PR before retrying") from error
    actual = json.loads(result.stdout)["body"]
    actual_refs = list(IMAGE.finditer(actual))
    if len(actual_refs) != len(references):
        raise RuntimeError("uploaded body image references differ; inspect the PR before retrying")
    restored = actual
    for original, uploaded in reversed(list(zip(references, actual_refs))):
        if original.start() in matches:
            if not ASSET.fullmatch(uploaded.group(2)) or uploaded.group(1) != original.group(1):
                raise RuntimeError("an uploaded image link or alt text was not verified; inspect the PR before retrying")
            restored = restored[:uploaded.start()] + original.group(0) + restored[uploaded.end():]
    if restored != body:
        raise RuntimeError("uploaded body prose differs from the supplied body; inspect the PR before retrying")
    print(f"Verified {len(images)} uploaded images and unchanged PR prose.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pr", required=True, help="explicit PR number or URL")
    parser.add_argument("--body-file", required=True)
    parser.add_argument("--repo", help="explicit OWNER/REPO when outside the checkout")
    parser.add_argument("images", nargs="+")
    args = parser.parse_args()
    try:
        publish(args)
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError, KeyError) as error:
        print(f"Could not publish PR images: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

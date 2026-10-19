#!/bin/sh
# Capture simulated panel data only; no daemon or sandbox commands are run.
set -eu
cd "$(dirname "$0")/.."
output="${1:-docs/screenshots}"
mkdir -p "$output"
output="$(cd "$output" && pwd)"
frames="$(mktemp -d)"
trap 'rm -rf "$frames"' EXIT
PANEL_SCREENSHOT_DIR="$frames" mise exec -- python3 scripts/test-go.py test ./internal/panel -run '^TestCaptureScreenshots$' -count=1
for frame in "$frames"/*.ansi; do
    swift -module-cache-path "$frames/swift-cache" scripts/render-terminal.swift "$frame" "$output/$(basename "$frame" .ansi).png"
done

#!/bin/sh
# Capture simulated panel data only; no daemon or sandbox commands are run.
set -eu
cd "$(dirname "$0")/.."
usage() { printf '%s\n' "Usage: $0 [output-directory] [--terminal-background '#RRGGBB']"; }
if [ "${1:-}" = "--help" ]; then usage; exit 0; fi
output="docs/screenshots"
if [ "$#" -gt 0 ] && [ "$1" != "--terminal-background" ]; then output="$1"; shift; fi
terminal_background=""
if [ "$#" -gt 0 ]; then
    if [ "$#" -ne 2 ] || [ "$1" != "--terminal-background" ]; then usage >&2; exit 2; fi
    terminal_background="$2"
fi
mkdir -p "$output"
output="$(cd "$output" && pwd)"
frames="$(mktemp -d)"
trap 'rm -rf "$frames"' EXIT
PANEL_SCREENSHOT_DIR="$frames" mise exec -- python3 scripts/test-go.py test ./internal/panel -run '^TestCaptureScreenshots$' -count=1
for frame in "$frames"/*.ansi; do
    set -- "$frame" "$output/$(basename "$frame" .ansi).png"
    if [ -n "$terminal_background" ]; then set -- "$@" --terminal-background "$terminal_background"; fi
    swift -module-cache-path "$frames/swift-cache" scripts/render-terminal.swift "$@"
done

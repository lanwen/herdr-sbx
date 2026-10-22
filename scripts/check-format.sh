#!/bin/sh
# Inspect only: formatting changes must be applied deliberately with mise run fmt.
set -eu
cd "$(dirname "$0")/.."
unformatted=$(gofmt -l cmd internal)
if [ -n "$unformatted" ]; then
    printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
    exit 1
fi

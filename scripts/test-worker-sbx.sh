#!/bin/sh
set -eu

case "${1:-}" in
  version|--version)
    printf '%s\n' 'sbx 0.45.1'
    ;;
  daemon)
    case "${2:-}" in
      status)
        if [ "${3:-}" = "--help" ]; then
          printf '%s\n' '--json'
        else
          printf '{"status":"running","socket":"%s"}\n' "${HERDR_WORKER_TEST_SOCKET:?}"
        fi
        ;;
      *) exit 2 ;;
    esac
    ;;
  ls)
    [ "${2:-}" = "--help" ] || exit 2
    printf '%s\n' '--json'
    ;;
  *) exit 2 ;;
esac

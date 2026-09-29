#!/bin/sh
set -eu

: "${HERDR_SANDBOX_TEST_ROOT:?Set a new temporary test root}"
: "${HERDR_SANDBOXES_SBX_APP_NAME:?Set a dedicated Docker Sandboxes test app name}"
: "${HERDR_SANDBOX_TEST_NAME:?Set the disposable prepared sandbox name}"
: "${HERDR_BIN_PATH:=herdr}"

case "$HERDR_SANDBOX_TEST_ROOT" in
  /tmp/*|/private/tmp/*) ;;
  *)
    echo "HERDR_SANDBOX_TEST_ROOT must be under a temporary directory." >&2
    exit 2
    ;;
esac

case "$HERDR_SANDBOXES_SBX_APP_NAME:$HERDR_SANDBOX_TEST_NAME" in
  herdr-sbx-test-*:herdr-sbx-test-*) ;;
  *)
    echo "Use herdr-sbx-test-* names for the dedicated app and disposable sandbox." >&2
    exit 2
    ;;
esac

export HOME="$HERDR_SANDBOX_TEST_ROOT/home"
export XDG_CONFIG_HOME="$HERDR_SANDBOX_TEST_ROOT/config"
export XDG_STATE_HOME="$HERDR_SANDBOX_TEST_ROOT/state"
export HERDR_CONFIG_PATH="$XDG_CONFIG_HOME/herdr/config.toml"
export HERDR_PLUGIN_STATE_DIR="$HERDR_SANDBOX_TEST_ROOT/plugin-state"
unset HERDR_SOCKET_PATH HERDR_ENV HERDR_PLUGIN_ID HERDR_PLUGIN_ROOT HERDR_PLUGIN_CONFIG_DIR

mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$HERDR_PLUGIN_STATE_DIR"

server_pid=""
cleanup() {
  if [ -n "$server_pid" ]; then
    "$HERDR_BIN_PATH" server stop >/dev/null 2>&1 || kill "$server_pid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

go build -o bin/herdr-sandboxes ./cmd/herdr-sandboxes
"$HERDR_BIN_PATH" server >"$HERDR_SANDBOX_TEST_ROOT/herdr-server.log" 2>&1 &
server_pid=$!

attempt=0
until "$HERDR_BIN_PATH" status server --json 2>/dev/null | grep -E '"running"[[:space:]]*:[[:space:]]*true' >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -eq 20 ]; then
    echo "Isolated Herdr server did not start." >&2
    exit 1
  fi
  sleep 1
done

"$HERDR_BIN_PATH" plugin link . >/dev/null
before=$("$HERDR_BIN_PATH" machine list --json)
if [ "$before" != "[]" ]; then
  echo "The isolated Herdr catalog is not empty; refusing to continue." >&2
  exit 1
fi

inventory=$(sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" ls --json)
if ! printf '%s' "$inventory" | grep -F "$HERDR_SANDBOX_TEST_NAME" >/dev/null; then
  echo "The named disposable sandbox is not visible in the dedicated test app." >&2
  exit 1
fi

first=$(bin/herdr-sandboxes reconcile)
second=$(bin/herdr-sandboxes reconcile)
catalog=$("$HERDR_BIN_PATH" machine list --json)

printf '%s\n' "$first" | grep -F "$HERDR_SANDBOX_TEST_NAME" | grep -F "READY" >/dev/null
printf '%s\n' "$second" | grep -F "already registered and reachable" >/dev/null
printf '%s\n' "$catalog" | grep -F "$HERDR_SANDBOX_TEST_NAME.sbx" >/dev/null

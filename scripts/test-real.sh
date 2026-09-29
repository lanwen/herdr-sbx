#!/bin/sh
set -eu

: "${HERDR_SANDBOX_TEST_ROOT:?Set a new temporary test root}"
: "${HERDR_SANDBOXES_SBX_APP_NAME:?Set a dedicated Docker Sandboxes test app name}"
: "${HERDR_SANDBOX_TEST_NAME:?Set the disposable prepared sandbox name}"
: "${HERDR_SANDBOX_TEST_SSH_CONFIG:?Set a dedicated test SSH config with the test app aliases}"
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

command -v python3 >/dev/null
test -f "$HERDR_SANDBOX_TEST_SSH_CONFIG"
export HERDR_SANDBOX_TEST_REAL_SSH="$(command -v ssh)"
export HERDR_SANDBOX_TEST_REAL_SCP="$(command -v scp)"
python3 - "$HERDR_SANDBOX_TEST_ROOT" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
parent = root.parent.resolve(strict=True)
temporary = pathlib.Path('/tmp').resolve()
if parent != temporary and temporary not in parent.parents:
    raise SystemExit('Test root must resolve under the temporary directory.')
root.mkdir(mode=0o700)  # Refuse existing paths, including symlinks.
PY

export HOME="$HERDR_SANDBOX_TEST_ROOT/home"
export XDG_CONFIG_HOME="$HERDR_SANDBOX_TEST_ROOT/config"
export XDG_STATE_HOME="$HERDR_SANDBOX_TEST_ROOT/state"
export XDG_DATA_HOME="$HERDR_SANDBOX_TEST_ROOT/data"
export XDG_CACHE_HOME="$HERDR_SANDBOX_TEST_ROOT/cache"
export XDG_RUNTIME_DIR="$HERDR_SANDBOX_TEST_ROOT/runtime"
export HERDR_CONFIG_PATH="$XDG_CONFIG_HOME/herdr/config.toml"
export HERDR_PLUGIN_STATE_DIR="$HERDR_SANDBOX_TEST_ROOT/plugin-state"
unset HERDR_SOCKET_PATH HERDR_CLIENT_SOCKET_PATH HERDR_SESSION HERDR_ENV
unset HERDR_PLUGIN_ID HERDR_PLUGIN_ROOT HERDR_PLUGIN_CONFIG_DIR

mkdir -p "$HOME/.ssh" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$HERDR_PLUGIN_STATE_DIR" "$XDG_RUNTIME_DIR" "$HERDR_SANDBOX_TEST_ROOT/tools"
chmod 700 "$HOME/.ssh" "$XDG_RUNTIME_DIR"
cp "$HERDR_SANDBOX_TEST_SSH_CONFIG" "$HOME/.ssh/config"
chmod 600 "$HOME/.ssh/config"
cat >"$HERDR_SANDBOX_TEST_ROOT/tools/ssh" <<'SH'
#!/bin/sh
exec "$HERDR_SANDBOX_TEST_REAL_SSH" -F "$HOME/.ssh/config" -o "UserKnownHostsFile=$HOME/.ssh/known_hosts" -o ControlPath=none "$@"
SH
cat >"$HERDR_SANDBOX_TEST_ROOT/tools/scp" <<'SH'
#!/bin/sh
exec "$HERDR_SANDBOX_TEST_REAL_SCP" -F "$HOME/.ssh/config" -o "UserKnownHostsFile=$HOME/.ssh/known_hosts" -o ControlPath=none "$@"
SH
chmod 700 "$HERDR_SANDBOX_TEST_ROOT/tools/ssh" "$HERDR_SANDBOX_TEST_ROOT/tools/scp"
export PATH="$HERDR_SANDBOX_TEST_ROOT/tools:$PATH"

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
"$HERDR_BIN_PATH" machine list --json | python3 -c 'import json,sys; assert json.load(sys.stdin) == [], "Test catalog is not empty"'

inventory=$(sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" ls --json)
printf '%s' "$inventory" | python3 -c 'import json,sys; rows=json.load(sys.stdin)["sandboxes"]; assert isinstance(rows,list) and len(rows)==1 and rows[0].get("name")==sys.argv[1], "Dedicated app must contain exactly the named disposable sandbox"' "$HERDR_SANDBOX_TEST_NAME"

first=$(bin/herdr-sandboxes reconcile)
second=$(bin/herdr-sandboxes reconcile)
catalog=$("$HERDR_BIN_PATH" machine list --json)

printf '%s\n' "$first" | grep -F "$HERDR_SANDBOX_TEST_NAME" | grep -F "READY" >/dev/null
printf '%s\n' "$second" | grep -F "already registered and reachable" >/dev/null
printf '%s\n' "$catalog" | python3 -c 'import json,sys; rows=json.load(sys.stdin); assert isinstance(rows,list) and len(rows)==1, "Expected exactly one registered machine"'

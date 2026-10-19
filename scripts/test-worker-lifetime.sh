#!/bin/sh
set -eu

: "${HERDR_WORKER_TEST_ROOT:?Set a new temporary test root}"
: "${HERDR_BIN_PATH:=herdr}"

case "$HERDR_WORKER_TEST_ROOT" in
  /tmp/*|/private/tmp/*) ;;
  *) echo "HERDR_WORKER_TEST_ROOT must be under a temporary directory." >&2; exit 2 ;;
esac

command -v python3 >/dev/null
python3 - "$HERDR_WORKER_TEST_ROOT" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
parent = root.parent.resolve(strict=True)
temporary = pathlib.Path('/tmp').resolve()
if parent != temporary and temporary not in parent.parents:
    raise SystemExit('Test root must resolve under the temporary directory.')
root.mkdir(mode=0o700)
PY

export HOME="$HERDR_WORKER_TEST_ROOT/home"
export XDG_CONFIG_HOME="$HERDR_WORKER_TEST_ROOT/config"
export XDG_STATE_HOME="$HERDR_WORKER_TEST_ROOT/state"
export XDG_DATA_HOME="$HERDR_WORKER_TEST_ROOT/data"
export XDG_CACHE_HOME="$HERDR_WORKER_TEST_ROOT/cache"
export XDG_RUNTIME_DIR="$HERDR_WORKER_TEST_ROOT/runtime"
export HERDR_CONFIG_PATH="$XDG_CONFIG_HOME/herdr/config.toml"
export HERDR_PLUGIN_STATE_DIR="$XDG_STATE_HOME/herdr/plugins/lanwen.herdr-sbx"
unset HERDR_SOCKET_PATH HERDR_CLIENT_SOCKET_PATH HERDR_SESSION HERDR_ENV
unset HERDR_PLUGIN_ID HERDR_PLUGIN_ROOT HERDR_PLUGIN_CONFIG_DIR
unset HERDR_PANE HERDR_PANE_ID HERDR_TAB HERDR_TAB_ID HERDR_WORKSPACE HERDR_WORKSPACE_ID
unset HERDR_PLUGIN_EVENT HERDR_PLUGIN_ACTION HERDR_PLUGIN_CONTEXT HERDR_CONTEXT
test_suffix=$(printf '%s' "$HERDR_WORKER_TEST_ROOT" | cksum | awk '{print $1}')
first_session="hsbx-${test_suffix}-one"
second_session="hsbx-${test_suffix}-two"
export HERDR_WORKER_TEST_SOCKET="$HERDR_WORKER_TEST_ROOT/sandboxd.sock"

mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$XDG_RUNTIME_DIR" "$HERDR_PLUGIN_STATE_DIR" "$HERDR_WORKER_TEST_ROOT/tools"
chmod 700 "$HOME" "$XDG_RUNTIME_DIR"
ln -s "$(pwd)/scripts/test-worker-sbx.sh" "$HERDR_WORKER_TEST_ROOT/tools/sbx"
chmod 700 scripts/test-worker-sbx.sh
export PATH="$HERDR_WORKER_TEST_ROOT/tools:$PATH"

first_pid=""
second_pid=""
daemon_pid=""
cleanup() {
  [ ! -x "$HERDR_WORKER_TEST_ROOT/herdr-sbx" ] || "$HERDR_WORKER_TEST_ROOT/herdr-sbx" daemon stop >/dev/null 2>&1 || true
  [ -z "$first_pid" ] || HERDR_SESSION="$first_session" "$HERDR_BIN_PATH" server stop >/dev/null 2>&1 || kill "$first_pid" >/dev/null 2>&1 || true
  [ -z "$second_pid" ] || HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" server stop >/dev/null 2>&1 || kill "$second_pid" >/dev/null 2>&1 || true
  [ -z "$daemon_pid" ] || kill "$daemon_pid" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

python3 - "$HERDR_WORKER_TEST_SOCKET" <<'PY' &
import os, socket, sys, threading, time
path = sys.argv[1]
try:
    os.unlink(path)
except FileNotFoundError:
    pass
listener = socket.socket(socket.AF_UNIX)
listener.bind(path)
listener.listen()
def serve(conn):
    try:
        request = conn.recv(4096)
        if b'/events?' in request:
            conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n')
            while True:
                time.sleep(1)
        else:
            conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n[]')
    finally:
        conn.close()
while True:
    conn, _ = listener.accept()
    threading.Thread(target=serve, args=(conn,), daemon=True).start()
PY
daemon_pid=$!

wait_for_server() {
  session=$1
  attempts=0
  until HERDR_SESSION="$session" "$HERDR_BIN_PATH" status server --json 2>/dev/null | grep -E '"running"[[:space:]]*:[[:space:]]*true' >/dev/null; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 20 ] || { echo "Isolated Herdr server $session did not start." >&2; exit 1; }
    sleep 1
  done
}

worker_lock_active() {
  python3 - "$HERDR_PLUGIN_STATE_DIR/state.json.worker" <<'PYLOCK'
import fcntl, sys
try:
    with open(sys.argv[1], 'r') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            sys.exit(0)
except FileNotFoundError:
    pass
sys.exit(1)
PYLOCK
}

assert_worker_locked() {
  worker_lock_active || { echo 'Expected an active worker lock.' >&2; exit 1; }
  HERDR_SESSION="$second_session" python3 - "$HERDR_WORKER_TEST_ROOT/herdr-sbx" <<'PYNOOP'
import subprocess, sys
for command in [['daemon', 'start'], ['daemon', 'start', '--automatic']]:
    result = subprocess.run([sys.argv[1], *command], text=True, capture_output=True, timeout=2)
    if result.returncode != 0 or result.stdout or result.stderr:
        raise SystemExit('Expected duplicate start to succeed silently: ' + repr(result))
PYNOOP
  worker_lock_active || { echo 'Duplicate start lost worker lock.' >&2; exit 1; }
}

assert_worker_stopped() {
  if worker_lock_active; then
    echo 'Expected worker to have exited.' >&2
    exit 1
  fi
}

start_worker() {
  HERDR_SESSION="$1" "$HERDR_WORKER_TEST_ROOT/herdr-sbx" daemon start
  attempts=0
  until worker_lock_active; do
    attempts=$((attempts + 1))
    if [ "$attempts" -eq 10 ]; then
      cat "$HERDR_PLUGIN_STATE_DIR/state.json.worker.log" >&2
      exit 1
    fi
    sleep 1
  done
}

wait_for_worker_exit() {
  attempts=0
  while worker_lock_active; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 15 ] || { echo 'Daemon did not exit within 15 seconds.' >&2; exit 1; }
    sleep 1
  done
}

go build -o "$HERDR_WORKER_TEST_ROOT/herdr-sbx" ./cmd/herdr-sbx
mkdir "$HERDR_WORKER_TEST_ROOT/plugin"
cp herdr-plugin.toml "$HERDR_WORKER_TEST_ROOT/plugin/"
mkdir "$HERDR_WORKER_TEST_ROOT/plugin/bin"
ln -s "$HERDR_WORKER_TEST_ROOT/herdr-sbx" "$HERDR_WORKER_TEST_ROOT/plugin/bin/herdr-sbx"
HERDR_SESSION="$first_session" "$HERDR_BIN_PATH" server >"$HERDR_WORKER_TEST_ROOT/first.log" 2>&1 &
first_pid=$!
HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" server >"$HERDR_WORKER_TEST_ROOT/second.log" 2>&1 &
second_pid=$!
wait_for_server "$first_session"
wait_for_server "$second_session"

HERDR_SESSION="$first_session" "$HERDR_BIN_PATH" plugin link "$HERDR_WORKER_TEST_ROOT/plugin" >/dev/null
HERDR_SESSION="$first_session" "$HERDR_BIN_PATH" plugin enable lanwen.herdr-sbx >/dev/null
start_worker "$first_session"
assert_worker_locked

# Public Stop persists the choice. Automatic hooks must honor it, and an
# explicit Start must clear it and launch the daemon again.
"$HERDR_WORKER_TEST_ROOT/herdr-sbx" daemon stop
wait_for_worker_exit
assert_worker_stopped
HERDR_SESSION="$first_session" "$HERDR_WORKER_TEST_ROOT/herdr-sbx" daemon start --automatic
sleep 1
assert_worker_stopped
"$HERDR_WORKER_TEST_ROOT/herdr-sbx" daemon status --json | python3 -c 'import json,sys; value=json.load(sys.stdin); assert value["worker_control"]["stopped"] is True, value; assert value["worker"]["state"] == "stopped", value'
start_worker "$first_session"
assert_worker_locked
"$HERDR_WORKER_TEST_ROOT/herdr-sbx" daemon status --json | python3 -c 'import json,sys; value=json.load(sys.stdin); assert value["worker_control"]["stopped"] is False, value'

HERDR_SESSION="$first_session" "$HERDR_BIN_PATH" server stop >/dev/null
sleep 6
assert_worker_locked

HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" plugin disable lanwen.herdr-sbx >/dev/null
wait_for_worker_exit
assert_worker_stopped

HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" plugin link "$HERDR_WORKER_TEST_ROOT/plugin" >/dev/null
HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" plugin enable lanwen.herdr-sbx >/dev/null
start_worker "$second_session"
assert_worker_locked
HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" plugin uninstall lanwen.herdr-sbx >/dev/null
wait_for_worker_exit
assert_worker_stopped
HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" plugin list --plugin lanwen.herdr-sbx --json | python3 -c 'import json,sys; value=json.load(sys.stdin); assert isinstance(value,dict), repr(value); value=value.get("result",value); assert isinstance(value,dict), repr(value); rows=value.get("plugins"); assert isinstance(rows,list), repr(value); assert all(isinstance(row,dict) and isinstance(row.get("plugin_id",row.get("id")),str) for row in rows), repr(rows); assert all(row.get("plugin_id",row.get("id")) != "lanwen.herdr-sbx" for row in rows), repr(rows)'

HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" plugin link "$HERDR_WORKER_TEST_ROOT/plugin" >/dev/null
HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" plugin enable lanwen.herdr-sbx >/dev/null
start_worker "$second_session"
assert_worker_locked
HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" server stop >/dev/null
wait_for_worker_exit
assert_worker_stopped

# The enabled plugin remains linked. A new server must start the worker via
# its actual manifest hook, without an explicit daemon invocation.
HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" server >"$HERDR_WORKER_TEST_ROOT/startup.log" 2>&1 &
second_pid=$!
wait_for_server "$second_session"
attempts=0
until worker_lock_active; do
  attempts=$((attempts + 1))
  [ "$attempts" -lt 10 ] || { cat "$HERDR_WORKER_TEST_ROOT/startup.log" >&2; echo 'Startup hook did not start worker.' >&2; exit 1; }
  sleep 1
done
assert_worker_locked
HERDR_SESSION="$second_session" "$HERDR_BIN_PATH" server stop >/dev/null
attempts=0
while worker_lock_active; do
  attempts=$((attempts + 1))
  [ "$attempts" -lt 20 ] || { echo 'Automatic worker did not stop.' >&2; exit 1; }
  sleep 1
done
assert_worker_stopped

printf '%s\n' 'Isolated worker lifetime, automatic startup and duplicate-start gate passed.'

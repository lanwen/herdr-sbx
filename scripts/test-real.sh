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
unset HERDR_PANE HERDR_PANE_ID HERDR_TAB HERDR_TAB_ID HERDR_WORKSPACE HERDR_WORKSPACE_ID
unset HERDR_PLUGIN_EVENT HERDR_PLUGIN_EVENT_JSON HERDR_PLUGIN_ACTION_ID HERDR_PLUGIN_CONTEXT_JSON HERDR_PLUGIN_ENTRYPOINT_ID
test_session_suffix=$(printf '%s' "$HERDR_SANDBOX_TEST_ROOT" | cksum | awk '{print $1}')
export HERDR_SESSION="hsbx-${test_session_suffix}"

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
    "$HERDR_BIN_PATH" --session "$HERDR_SESSION" server stop >/dev/null 2>&1 || kill "$server_pid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

go build -o bin/herdr-sbx ./cmd/herdr-sbx
"$HERDR_BIN_PATH" --session "$HERDR_SESSION" server >"$HERDR_SANDBOX_TEST_ROOT/herdr-server.log" 2>&1 &
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

if [ "${HERDR_SANDBOX_TEST_EXPECT_MISSING:-}" = 1 ]; then
  ssh -o BatchMode=yes -o ConnectTimeout=8 "$HERDR_SANDBOX_TEST_NAME.sbx" 'if command -v herdr >/dev/null 2>&1 || [ -e "$HOME/.local/bin/herdr" ] || [ -L "$HOME/.local/bin/herdr" ]; then exit 1; fi'
fi

policy_snapshot() {
  sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" policy ls "$HERDR_SANDBOX_TEST_NAME" --json --include-inactive | python3 -c 'import json,sys; data=json.load(sys.stdin); rules=data["rules"]; assert isinstance(rules,list); print(json.dumps(sorted(rules,key=lambda row:row["id"]),sort_keys=True))'
}

if [ "${HERDR_SANDBOX_TEST_TEMPORARY_ACCESS:-}" = 1 ]; then
  [ "${HERDR_SANDBOX_TEST_EXPECT_MISSING:-}" = 1 ] || { echo 'Temporary access gate requires EXPECT_MISSING=1 and a fixture that blocks installer downloads.' >&2; exit 2; }
  # This opt-in authorizes only the named disposable sandbox's temporary rules.
  sandbox_id=$(printf '%s' "$inventory" | python3 -c 'import json,sys; value=json.load(sys.stdin)["sandboxes"][0]["id"]; assert isinstance(value,str) and value; print(value)')
  policy_snapshot >"$HERDR_SANDBOX_TEST_ROOT/policy-before.json"
  # The explicit legacy strategy still requires a blocked-download fixture.
  # Default registration now transfers a host download and would succeed here.
  denied_code=$(ssh -o BatchMode=yes -o ConnectTimeout=8 "$HERDR_SANDBOX_TEST_NAME.sbx" 'curl --silent --show-error --location --max-time 30 --output /dev/null --write-out "%{http_code}" https://herdr.dev/install.sh')
  [ "$denied_code" = 403 ] || { echo 'Temporary access fixture must block guest installer downloads with HTTP 403.' >&2; exit 2; }
  sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" ls --json | python3 -c 'import json,sys; rows=json.load(sys.stdin)["sandboxes"]; assert len(rows)==1 and rows[0].get("name")==sys.argv[1] and rows[0].get("id")==sys.argv[2], "sandbox identity changed"' "$HERDR_SANDBOX_TEST_NAME" "$sandbox_id"
  first=$(printf 'n\n1\nallow\n\nq\n' | bin/herdr-sbx panel)
  printf '%s\n' "$first" >"$HERDR_SANDBOX_TEST_ROOT/temporary-install.log"
  policy_snapshot >"$HERDR_SANDBOX_TEST_ROOT/policy-after.json"
  cmp "$HERDR_SANDBOX_TEST_ROOT/policy-before.json" "$HERDR_SANDBOX_TEST_ROOT/policy-after.json"
  python3 - "$HERDR_PLUGIN_STATE_DIR/state.json.status" <<'PY'
import json, sys
with open(sys.argv[1]) as source:
    assert not json.load(source).get('installations'), 'Temporary access cleanup remains pending'
PY
  sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" ls --json | python3 -c 'import json,sys; rows=json.load(sys.stdin)["sandboxes"]; assert len(rows)==1 and rows[0].get("name")==sys.argv[1] and rows[0].get("id")==sys.argv[2], "sandbox identity changed"' "$HERDR_SANDBOX_TEST_NAME" "$sandbox_id"
else
  policy_snapshot >"$HERDR_SANDBOX_TEST_ROOT/policy-before.json"
  first=$(bin/herdr-sbx reconcile)
  policy_snapshot >"$HERDR_SANDBOX_TEST_ROOT/policy-after.json"
  cmp "$HERDR_SANDBOX_TEST_ROOT/policy-before.json" "$HERDR_SANDBOX_TEST_ROOT/policy-after.json"
fi
project_workspace=$(printf '%s' "$inventory" | python3 -c 'import json,sys; print(json.load(sys.stdin)["sandboxes"][0].get("workspace", ""))')
if [ -n "$project_workspace" ]; then
  guest_project=$(sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" exec "$HERDR_SANDBOX_TEST_NAME" -- pwd -P)
  project_profile=$("$HERDR_BIN_PATH" machine list --json | python3 -c 'import json,sys; rows=json.load(sys.stdin); assert len(rows)==1; print(rows[0]["profile_id"])')
  project_workspace_ids() {
    "$HERDR_BIN_PATH" --machine "$project_profile" pane list | python3 -c 'import json,sys; result=json.load(sys.stdin)["result"]; ids=sorted({p["workspace_id"] for p in result["panes"] if p.get("cwd")==sys.argv[1]}); assert ids, "No Herdr workspace at guest project directory"; print(json.dumps(ids))' "$guest_project"
  }
  project_workspace_ids >"$HERDR_SANDBOX_TEST_ROOT/project-workspaces-before.json"
fi
second=$(bin/herdr-sbx reconcile)
if [ -n "$project_workspace" ]; then
  project_workspace_ids >"$HERDR_SANDBOX_TEST_ROOT/project-workspaces-after.json"
  cmp "$HERDR_SANDBOX_TEST_ROOT/project-workspaces-before.json" "$HERDR_SANDBOX_TEST_ROOT/project-workspaces-after.json"
fi
catalog=$("$HERDR_BIN_PATH" machine list --json)

printf '%s\n' "$first" | grep -F "$HERDR_SANDBOX_TEST_NAME" | grep -F "READY" >/dev/null
printf '%s\n' "$second" | grep -F "already registered and reachable" >/dev/null
printf '%s\n' "$catalog" | python3 -c 'import json,sys; rows=json.load(sys.stdin); assert isinstance(rows,list) and len(rows)==1, "Expected exactly one registered machine"'

if [ "${HERDR_SANDBOX_TEST_LIFECYCLE:-}" = 1 ]; then
  sandbox_id=$(printf '%s' "$inventory" | python3 -c 'import json,sys; value=json.load(sys.stdin)["sandboxes"][0]["id"]; assert isinstance(value,str) and value, "sandbox has no stable identity"; print(value)')
  profile_id=$(printf '%s' "$catalog" | python3 -c 'import json,sys; rows=json.load(sys.stdin); print(rows[0]["profile_id"])')
  verify_identity() {
    sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" ls --json | python3 -c 'import json,sys; rows=[row for row in json.load(sys.stdin)["sandboxes"] if row.get("name")==sys.argv[1]]; assert len(rows)==1 and rows[0].get("id")==sys.argv[2], "sandbox identity changed"' "$HERDR_SANDBOX_TEST_NAME" "$sandbox_id"
  }
  bin/herdr-sbx worker >"$HERDR_SANDBOX_TEST_ROOT/worker.log" 2>&1 &
  worker_pid=$!
  trap 'kill "$worker_pid" >/dev/null 2>&1 || true; cleanup' EXIT INT TERM
  verify_identity
  sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" stop "$HERDR_SANDBOX_TEST_NAME"
  "$HERDR_BIN_PATH" machine list --json | python3 -c 'import json,sys; assert any(row.get("profile_id")==sys.argv[1] for row in json.load(sys.stdin)), "stop removed the machine"' "$profile_id"
  "$HERDR_BIN_PATH" --machine "$profile_id" status server >/dev/null
  verify_identity
  sbx --app-name "$HERDR_SANDBOXES_SBX_APP_NAME" rm --force "$HERDR_SANDBOX_TEST_NAME"
  attempts=0
  until "$HERDR_BIN_PATH" machine list --json | python3 -c 'import json,sys; assert not any(row.get("profile_id")==sys.argv[1] for row in json.load(sys.stdin))' "$profile_id"; do
    attempts=$((attempts + 1)); [ "$attempts" -lt 20 ] || { echo 'Worker did not remove the owned machine.' >&2; exit 1; }; sleep 1
  done
fi

printf '%s\n' 'Isolated real sandbox gate passed.'

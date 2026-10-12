#!/usr/bin/env python3
import os
import pathlib
import pty
import select
import shutil
import subprocess
import sys
import tempfile
import time

go = os.environ.get("GO_BIN")
if not go:
    raise SystemExit("set GO_BIN to an absolute Go binary path")
root = pathlib.Path(__file__).resolve().parents[1]
module_cache = subprocess.check_output([go, "env", "GOMODCACHE"], cwd=root, text=True).strip()
temp = pathlib.Path(tempfile.mkdtemp(prefix="herdr-sbx-panel-"))
try:
    home, config, state, tools = (temp / name for name in ("home", "config", "state", "tools"))
    for path in (home, config, state, tools): path.mkdir(mode=0o700)
    log = temp / "commands.log"
    def stub(name, body):
        path = tools / name
        path.write_text("#!/bin/sh\nprintf '%s %s\\n' '" + name + "' \"$*\" >> '" + str(log) + "'\n" + body)
        path.chmod(0o700)
        return str(path)
    herdr = stub("herdr", 'case "$1" in --version) echo "herdr 0.9.1";; esac\n')
    stub("sbx", 'case "$*" in "--version") echo "sbx 0.45.1";; "version") echo "sbx 0.45.1";; "daemon status --help"|"ls --help") echo "--json";; "daemon status --json") echo "{\\"status\\":\\"stopped\\",\\"socket\\":\\"/tmp/herdr-sbx-panel.sock\\"}";; esac\n')
    stub("ssh", 'case "$1" in -V) echo "OpenSSH_9.9";; -G) echo "proxycommand sbx ssh proxy %n";; esac\n')
    binary = temp / "herdr-sbx"
    session = "herdr-sbx-test-panel-" + str(os.getpid())
    env = {"HOME": str(home), "XDG_CONFIG_HOME": str(config), "XDG_STATE_HOME": str(state), "HERDR_CONFIG_PATH": str(config / "herdr.toml"), "HERDR_SESSION": session, "HERDR_PLUGIN_STATE_DIR": str(state / "plugin"), "HERDR_BIN_PATH": herdr, "PATH": str(tools) + ":/usr/bin:/bin", "GOCACHE": str(temp / "go-cache"), "GOPATH": str(temp / "go-path")}
    env.update(GOMODCACHE=module_cache, GOPROXY="off", GOFLAGS="-mod=readonly", GOTOOLCHAIN="local", GOENV="off")
    subprocess.run([go, "build", "-o", str(binary), "./cmd/herdr-sbx"], cwd=root, env=env, check=True)
    pid, master = pty.fork()
    if pid == 0:
        os.execve(str(binary), [str(binary), "panel"], env)
    os.write(master, b"c\nq\n")
    output = b""
    deadline = time.time() + 10
    while time.time() < deadline:
        ready, _, _ = select.select([master], [], [], 0.2)
        if ready:
            try: output += os.read(master, 65536)
            except OSError: break
        ended, _ = os.waitpid(pid, os.WNOHANG)
        if ended: break
    else:
        os.kill(pid, 9); raise SystemExit("panel did not exit")
    text = output.decode(errors="replace")
    if "lanwen/herdr-sbx" not in text or "Sandbox daemon" not in text or "Check again" not in text:
        raise SystemExit("panel did not render expected status:\n" + text)
    commands = log.read_text()
    for forbidden in ("daemon start", " login", "setup ssh"):
        if forbidden in commands: raise SystemExit("observational panel ran repair: " + commands)
    print("panel PTY smoke passed:", temp, "session", session)
finally:
    shutil.rmtree(temp, ignore_errors=True)

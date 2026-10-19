#!/usr/bin/env python3
import os
import fcntl
import json
import struct
import termios
import pathlib
import pty
import re
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
    env.update(TERM="xterm-256color", COLORTERM="truecolor")
    env.update(GOMODCACHE=module_cache, GOPROXY="off", GOFLAGS="-mod=readonly", GOTOOLCHAIN="local", GOENV="off")
    subprocess.run([go, "build", "-o", str(binary), "./cmd/herdr-sbx"], cwd=root, env=env, check=True)
    plugin_state = state / "plugin"
    plugin_state.mkdir(mode=0o700)
    (plugin_state / "state.json.status").write_text(json.dumps({"worker_control": {"initialized": True, "stopped": True}}))
    pid, master = pty.fork()
    if pid == 0:
        os.execve(str(binary), [str(binary)], env)
    fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 32, 110, 0, 0))
    output = b""
    def wait_text(expected, timeout=15, since=0):
        global output
        deadline = time.time() + timeout
        while time.time() < deadline:
            ready, _, _ = select.select([master], [], [], 0.1)
            if ready:
                try: output += os.read(master, 65536)
                except OSError: break
            if expected.encode() in output[since:]:
                return
        raise RuntimeError("Missing " + repr(expected) + ":\n" + output.decode(errors="replace"))
    try:
        wait_text("Start auto-sync?")
        # Decline a stopped-worker prompt, then exercise page navigation.
        os.write(master, b"\x1b")
        time.sleep(0.2)
        since = len(output)
        os.write(master, b"\x1b[C")
        wait_text("PREREQUISITE", since=since)
        since = len(output)
        os.write(master, b"\t")
        wait_text("AUTO-SYNC", since=since)
        since = len(output)
        os.write(master, b"\x1b[D")
        wait_text("PREREQUISITE", since=since)
        os.write(master, b"q")
        deadline = time.time() + 5
        while time.time() < deadline:
            ended, status = os.waitpid(pid, os.WNOHANG)
            if ended:
                if status != 0: raise RuntimeError("TUI failed: " + str(status))
                pid = None
                break
            time.sleep(0.1)
        else:
            raise RuntimeError("TUI did not exit")
    finally:
        if pid:
            os.kill(pid, 9)
            os.waitpid(pid, 0)
        os.close(master)
    text = output.decode(errors="replace")
    if not re.search(r"\x1b\[[0-9;]*(?:38|48);(?:2|5);[0-9;]+m", text):
        raise SystemExit("TUI did not emit its color palette in a color-enabled terminal")
    commands = log.read_text() if log.exists() else ""
    for forbidden in ("daemon start", " login", "setup ssh"):
        if forbidden in commands: raise SystemExit("observational panel ran repair: " + commands)
    print("panel PTY smoke passed:", temp, "session", session)
finally:
    shutil.rmtree(temp, ignore_errors=True)

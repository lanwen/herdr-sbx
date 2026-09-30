#!/usr/bin/env python3
"""Run Go checks without inheriting a live Herdr session or user state.

Only dependency/build caches are shared. Run `mise run deps` to warm the module
cache; tests cannot silently fetch new modules or change go.mod.
"""

import os
import pathlib
import shutil
import subprocess
import sys
import tempfile


def main():
    go = shutil.which("go")
    if not go:
        raise SystemExit("Go is missing; run through mise after `mise install`.")
    if len(sys.argv) < 2 or sys.argv[1] not in ("test", "vet"):
        raise SystemExit("usage: test-go.py [test|vet] <go arguments...>")
    root = pathlib.Path(__file__).resolve().parents[1]
    module_cache = subprocess.check_output(
        [go, "env", "GOMODCACHE"], cwd=root, text=True
    ).strip()
    with tempfile.TemporaryDirectory(prefix="herdr-sbx-check-", dir="/tmp") as temp:
        env = {
            key: value for key, value in os.environ.items()
            if not key.startswith(("HERDR", "XDG", "GO"))
        }
        env.update(
            HOME=temp,
            XDG_CONFIG_HOME=temp + "/config",
            XDG_STATE_HOME=temp + "/state",
            XDG_DATA_HOME=temp + "/data",
            XDG_CACHE_HOME=temp + "/cache",
            XDG_RUNTIME_DIR=temp + "/runtime",
            HERDR_CONFIG_PATH=temp + "/config/herdr.toml",
            HERDR_SESSION="herdr-sbx-test-" + str(os.getpid()),
            HERDR_PLUGIN_STATE_DIR=temp + "/plugin",
            GOPATH=temp + "/go",
            GOMODCACHE=module_cache,
            GOCACHE=os.environ.get("GOCACHE", temp + "/go-cache"),
            GOENV="off",
            GOTOOLCHAIN="local",
            GOPROXY="off",
            GOFLAGS="-mod=readonly",
        )
        return subprocess.call([go, *sys.argv[1:]], cwd=root, env=env)


if __name__ == "__main__":
    sys.exit(main())

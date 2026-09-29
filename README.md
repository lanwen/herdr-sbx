# Docker Sandboxes for Herdr

This Herdr plugin shows local Docker Sandboxes prerequisites and can register running sandboxes that already have a compatible Herdr installation.

Build the plugin with:

```sh
go build -o bin/herdr-sandboxes ./cmd/herdr-sandboxes
herdr plugin link .
```

Open **Docker Sandboxes status** from Herdr's plugin actions. The panel's **Check again** command only observes local binaries, daemon status, sandbox inventory, Docker authorization errors, and the effective SSH configuration. A readable inventory alone does not prove Docker sign-in. It does not start the daemon, sign in, configure SSH, connect to a sandbox, or install software.

The panel offers **Start daemon**, **Sign in to Docker**, and **Set up sandbox SSH** as separate, interactive terminal actions. Each runs only after you choose it, then refreshes the status panel. The plugin does not store Docker credentials or SSH keys.

**Register prepared running sandboxes** is explicit. It checks a running sandbox through its managed SSH alias, uses `herdr machine add` for a compatible existing remote installation, then verifies the saved machine through Herdr. It records its owned profile before and after the catalog change. A conflicting or uncertain profile is reported and left untouched.

Use **Watch Docker Sandbox lifecycle** to open a foreground watcher. It registers a started, prepared sandbox after rechecking its stable identity. Stopped sandbox registrations remain available for ordinary SSH reconnects. A deletion removes only the matching profile that the plugin recorded as its own. Press Ctrl-C to stop the watcher.

Run deterministic checks with:

```sh
GOCACHE=/tmp/herdr-sbx-go-cache go test ./...
```

`scripts/test-real.sh` is an opt-in real integration gate and requires Python 3. It starts a temporary Herdr server and catalog, then requires a dedicated Docker Sandboxes test app containing exactly one disposable prepared sandbox. Both names must begin with `herdr-sbx-test-`. Set `HERDR_SANDBOX_TEST_ROOT` to a new path under `/tmp`, `HERDR_SANDBOXES_SBX_APP_NAME`, `HERDR_SANDBOX_TEST_NAME`, and `HERDR_SANDBOX_TEST_SSH_CONFIG` to an existing dedicated SSH config with that app's aliases and absolute paths to its keys. The harness copies this config and isolates SSH known-hosts and runtime state. It does not create or repair the Docker test environment. Live validation is incomplete until this gate runs against an authenticated test environment.

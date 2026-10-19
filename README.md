# herdr-sbx

Connect local [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) to [Herdr](https://github.com/herdrdev/herdr).
The plugin discovers running sandboxes and adds them as machines named `sbx · <sandbox-name>`.
It installs Herdr inside sandboxes when needed and creates workspaces in their project directories.
You can also select local plugins to install inside sandboxes with their shortcuts.

## Quick start

Use a macOS or Linux host with Herdr 0.9.1 or later, `sbx` 0.45.1 or later, and OpenSSH.
Start the sandbox daemon and sign in through Docker Sandboxes before using the plugin.
Configure sandbox SSH with `sbx setup ssh`.

Install Go 1.27.1 or later and Git on the host, with `go` and `git` available on `PATH`.
Herdr downloads and builds the plugin from GitHub:

```sh
herdr plugin install lanwen/herdr-sbx
```

Review and approve the installation prompt.
Herdr enables the installed plugin.
If you already use a local checkout, follow the [switching instructions](docs/reference.md#local-checkout) first.

The auto-sync daemon follows sandbox changes in the background.
From a terminal inside local Herdr, open the sandbox manager:

```sh
herdr plugin action invoke open-status --plugin lanwen.herdr-sbx
```

On first use, the manager starts one daemon automatically.
Closing the manager leaves the daemon running.
Use the table to watch sandboxes, rename Herdr labels, and select synchronized plugins.
Use Auto-sync controls to stop it or start it again.
The plugin binary also provides `status` for cached observations and `status --json` for a detailed snapshot.
Use `daemon start` or `daemon stop` to control auto-sync, and `daemon status` or `daemon status --json` to inspect only the daemon.
For a local checkout, run these subcommands with `bin/herdr-sbx`.

## Screenshots

These screenshots use simulated data and `claude-*` sandbox names.

Sandbox table:

![Sandbox manager table with simulated claude sandboxes](docs/screenshots/sandboxes.png)

Sandbox details and recovery actions:

![Expanded sandbox details with simulated claude data](docs/screenshots/sandbox-details.png)

Herdr plugin defaults:

![Herdr plugin defaults with simulated data](docs/screenshots/plugins.png)

Compact layout:

![Compact sandbox manager with simulated claude sandboxes](docs/screenshots/compact.png)

## Upgrade

Open the sandbox manager, set Auto-sync to OFF, and wait for Stopped.
Then disable the plugin from another local Herdr terminal:

```sh
herdr plugin disable lanwen.herdr-sbx
```

Reinstall from GitHub:

```sh
herdr plugin install lanwen/herdr-sbx
```

Close and reopen the manager to load the new version, then accept the Start prompt or set Auto-sync to ON.
The upgrade keeps plugin state and managed machine entries.

## How it works

The daemon follows sandbox events and scans again every 30 seconds.
It downloads missing Herdr binaries on the host, compares their checksums, and copies them over SSH.
Stopped sandboxes keep their machine entries.
Deleted sandboxes lose only entries that the plugin manages.

The plugin reports registration failures through Herdr notifications.
It tries to resolve version differences without stopping active panes.
Guest plugins start disabled and require an explicit selection.

See the [reference](docs/reference.md) for commands, recovery, plugin selection, code navigation, and tests.
The project uses the [MIT license](LICENSE).

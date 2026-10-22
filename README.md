# herdr-sbx

Connect local [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) to [Herdr](https://github.com/herdrdev/herdr).
The plugin discovers sandboxes and adds running ones as machines named `sbx · <sandbox-name>`.
It installs Herdr inside sandboxes when needed and creates workspaces in their project directories.
You can also select local plugins to install inside sandboxes with their shortcuts.

## Quick start

Use a macOS or Linux host with Herdr 0.9.1 or later, `sbx` 0.45.1 or later, and OpenSSH.
Start the sandbox daemon and sign in through Docker Sandboxes before using the plugin.
Configure sandbox SSH with `sbx setup ssh`.

Install Go 1.27.3 or later and Git on the host, with `go` and `git` available on `PATH`.
Herdr downloads and builds the plugin from GitHub:

```sh
herdr plugin install lanwen/herdr-sbx
```

Review and approve the installation prompt.
Herdr enables the installed plugin.
If you already use a local checkout, follow the [switching instructions](docs/reference.md#local-checkout) first.

Management follows sandbox changes in the background while it is ON.
From a terminal inside local Herdr, open the sandbox manager:

```sh
herdr plugin action invoke open-status --plugin lanwen.herdr-sbx
```

To open the popup with prefix, then Ctrl+s, add this [keybinding](https://herdr.dev/docs/configuration/#custom-command-keybindings) to `~/.config/herdr/config.toml`.

```toml
[[keys.command]]
key = "prefix+ctrl+s"
type = "plugin_action"
command = "lanwen.herdr-sbx.open-status"
description = "Docker Sandboxes"
```

Run `herdr server reload-config` from a terminal inside Herdr to apply the change.
Press your configured prefix, then Ctrl+s, to open the popup.

On first use, the manager starts one daemon automatically.
Closing the manager leaves the daemon running.
Use the table to enable or disable sandboxes, rename herdr labels, and select synchronized herdr plugins.
Use the Management control to turn automatic work ON or OFF.
Disabling a sandbox keeps its herdr entry and label; after it is disabled in herdr, you can remove the entry separately.
An explicit Enable, Disable, or Rename action remains available while Management is OFF.
The plugin binary also provides `status` for cached observations and `status --json` for a detailed snapshot.
Use `daemon start` or `daemon stop` to control Management, and `daemon status` or `daemon status --json` to inspect its saved choice and daemon state.
For a local checkout, run these subcommands with `bin/herdr-sbx`.

## Screenshots

Sandbox table:

![Sandbox manager table with simulated sandboxes](docs/screenshots/sandboxes.png)

Sandbox details and recovery actions:

![Expanded sandbox details with simulated data](docs/screenshots/sandbox-details.png)

Herdr plugin defaults:

![Herdr plugin defaults with simulated data](docs/screenshots/plugins.png)

Compact layout:

![Compact sandbox manager with simulated sandboxes](docs/screenshots/compact.png)

## Upgrade

Open the sandbox manager, set Management to OFF, and wait until cancellation settles.
Then disable the plugin from another local Herdr terminal:

```sh
herdr plugin disable lanwen.herdr-sbx
```

Reinstall from GitHub:

```sh
herdr plugin install lanwen/herdr-sbx
```

Close and reopen the manager to load the new version, then accept the Start prompt or set Management to ON.
The upgrade keeps herdr plugin state and verified machine entries.

## How it works

The daemon follows sandbox events and scans again every 30 seconds.
It downloads missing Herdr binaries on the host, compares their checksums, and copies them over SSH.
Stopped sandboxes keep their machine entries.
A stopped sandbox without an entry stays Disabled quietly.
Start that sandbox independently before enabling it in the manager.
If an entry already exists, Enable enables it in Herdr even while the sandbox is stopped.
Herdr may then connect and start it; keep the entry Disabled to prevent future connections.
Management removes an entry only after confirming its sandbox was deleted, including a disabled entry; it never removes entries automatically while OFF.

The plugin reports registration failures through Herdr notifications.
It tries to resolve version differences without stopping active panes.
Guest plugins start disabled and require an explicit selection.

See the [reference](docs/reference.md) for commands, recovery, plugin selection, code navigation, and tests.
The project uses the [MIT license](LICENSE).

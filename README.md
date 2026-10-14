# herdr-sbx

Connect local [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) to [Herdr](https://github.com/herdrdev/herdr).
The plugin discovers running sandboxes and adds them as machines named `sbx · <sandbox-name>`.
It installs Herdr inside sandboxes when needed and creates workspaces in their project directories.
You can also select local plugins to install inside sandboxes with their shortcuts.

## Quick start

Use a macOS or Linux host with Herdr 0.9.1 or later, `sbx` 0.45.1 or later, and OpenSSH.
Start the sandbox daemon and sign in through Docker Sandboxes before using the plugin.
Configure sandbox SSH with `sbx setup ssh`.

Install [mise](https://mise.jdx.dev/) to use the pinned Go version and build tasks.
Run these commands on the host:

```sh
git clone https://github.com/lanwen/herdr-sbx.git
cd herdr-sbx
mise trust
mise install
mise run deps
mise run build
mise exec -- herdr plugin link . --enabled
```

The worker is the process that follows sandbox changes in the background.
From a terminal inside local Herdr, start the worker and open the status panel:

```sh
herdr plugin action invoke daemon --plugin lanwen.herdr-sbx
herdr plugin action invoke open-status --plugin lanwen.herdr-sbx
```

Starting it again has no effect while it is running.
The panel shows prerequisites, sandbox results, and worker status.

## How it works

The worker follows sandbox events and scans again every 30 seconds.
It downloads missing Herdr binaries on the host, compares their checksums, and copies them over SSH.
Stopped sandboxes keep their machine entries.
Deleted sandboxes lose only entries that the plugin manages.

The plugin reports registration failures through Herdr notifications.
It tries to resolve version differences without stopping active panes.
Guest plugins start disabled and require an explicit selection.

See the [reference](docs/reference.md) for commands, recovery, plugin selection, code navigation, and tests.
The project uses the [MIT license](LICENSE).

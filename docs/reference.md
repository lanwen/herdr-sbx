# Reference

See the [README](../README.md) for installation and requirements.
The plugin ID is `lanwen.herdr-sbx`.
Run the following commands from the checkout on the host, inside local Herdr when they need a server.

## Public sources

For external behavior and API contracts, consult the [Herdr documentation](https://herdr.dev/docs/) and [Docker Sandboxes documentation](https://docs.docker.com/ai/sandboxes/).
For regressions, upgrades, or new features, consult the [Herdr changelog](https://github.com/herdrdev/herdr/blob/master/CHANGELOG.md) and [Docker Sandboxes release notes](https://docs.docker.com/ai/sandboxes/release-notes/).
Record the installed Herdr and Docker Sandboxes versions before applying claims from these sources.
Distinguish features available in those versions from later releases and unreleased changes.

## Commands and panel

| Command | Purpose |
| --- | --- |
| `status` | Read worker status without contacting Docker or starting work |
| `check` | Inspect prerequisites without changing setup |
| `reconcile` | Prepare and register running sandboxes once |
| `daemon` | Start one background worker, or do nothing if it already runs |
| `watch` | Follow sandbox events in the foreground until Ctrl-C |
| `repair ssh` | Run interactive sandbox SSH setup |
| `adopt <sandbox-name> <machine-id>` | Take ownership of an existing matching machine |

Open the popup through the `open-status` plugin action.
Use `open-status-tab` or `open-status-split` for other layouts.
The popup requests 110 columns and 32 rows, within the terminal limits.

In the panel, enter a letter and press Enter:

| Key | Action |
| --- | --- |
| `c` | Refresh observations |
| `r` | Prepare and register running sandboxes |
| `b` | Start the background worker |
| `w` | Follow sandbox events in the foreground |
| `a` | Select an existing machine to adopt |
| `g` | Select guest plugins |
| `s` | Configure sandbox SSH |
| `t` | Select a sandbox and retry preparation |
| `n` | Approve temporary guest download access for one installation |
| `q` | Close the panel |

Prerequisite inspection observes binaries, daemon access, inventory, Docker authorization, and SSH configuration.
It does not repair them or connect to sandboxes.
Docker diagnostics can take 35 seconds, and inconclusive results remain `UNKNOWN`.
The plugin does not start the sandbox daemon, sign in, or store credentials and SSH keys.

## Worker and recovery

Herdr starts the worker when its server starts, completes a handoff, or creates a workspace.
Linking or enabling the plugin in an existing server does not trigger startup.
Use the `daemon` action to start it immediately.
The worker exits when the plugin is disabled or the final local server stays stopped.
Closing the panel does not stop it.
After a worker crash, run `daemon` again.

`RUNNING` means that the event stream is active, not that every sandbox is ready.
The status includes the last successful sync and any reconnect error.
Registration failures produce notifications, with identical warnings suppressed until the result changes or the sandbox recovers.

Host downloads select the local Herdr CLI version and compare the release checksum before SSH transfer.
Recovery can replace a confirmed guest executable with that version and request a live handoff, which replaces the server while preserving panes.
If the handoff fails, the plugin reports the error without forcing a restart.
Broken or ambiguous installations require user action.
An uncertain registration retries only after a successful catalog read finds no matching machine.

An ownership conflict means that a machine exists without a matching ownership record.
Adoption requires one exact sandbox SSH target, a running sandbox, the default remote session, and no conflicting record.
It preserves the connection and workspaces, but lets the plugin remove that entry when the sandbox is deleted.
The worker never adopts machines automatically.

New guest servers start in the resolved project directory.
Existing servers reuse a matching workspace or receive one project workspace.
The plugin records completion, so a workspace that you close stays closed.
Without project metadata, workspace setup stays unchanged.

State uses `HERDR_PLUGIN_STATE_DIR`, or `${XDG_STATE_HOME:-$HOME/.local/state}/herdr/plugins/lanwen.herdr-sbx` by default.
The directory holds ownership records, worker status, `state.json.worker.log`, and `guest-plugins.json`.
For a custom Herdr state location, provide its plugin state directory explicitly.
After a binary update, reopen the panel and restart the old worker to load it.
Relink the checkout after manifest changes.

## Guest plugins

Select plugins with `g` in the panel or the commands below.
All plugins start disabled, including newly discovered plugins.
Disabling one stops future installation and updates, but preserves its guest files and shortcuts.
The worker notices selection changes about once per second when idle and applies them to running sandboxes.

```sh
bin/herdr-sbx guest-plugins list
bin/herdr-sbx guest-plugins enable persiyanov.reviewr
bin/herdr-sbx guest-plugins disable persiyanov.reviewr
bin/herdr-sbx guest-plugins bundle example.plugin /path/to/linux-plugin
```

Reviewr and herdr-last use host downloads of matching Linux releases with checksum comparison.
Other plugins can use local files when they require no build and can run on Linux.
Otherwise, provide a bundle with `herdr-plugin.toml`, the matching plugin ID, runtime files, and binaries for the guest architecture.
Setting a bundle does not enable it.
Bundles over 128 MiB, symlinks, special files, and incompatible binaries are rejected.
Dotfiles and dot-directories are excluded.

The worker copies files over SSH and links the plugin without guest downloads or builds.
Guests need `jq`, `tar`, `sha256sum`, and `base64`, plus dependencies for each plugin.
Reviewr also needs `bash` and `git`.
Only shortcuts for the selected plugin and an absent guest prefix are copied from local Herdr configuration.
Conflicting shortcuts, different explicit prefixes, and externally managed installations require user action.
Configuration backups retain the original comments and formatting in `config.toml.herdr-sbx-<plugin-id>.bak`.
Other host configuration, environment variables, and credentials are not copied.

## Temporary download access

Normal installation needs host download access and SSH, without changes to sandbox network policy.
The optional `n` action downloads inside one selected sandbox after you review the destinations and type `allow`.
All processes in that sandbox share this temporary HTTPS GET access.
The plugin removes only rules returned as newly created by its request.
Existing rules and denies stay unchanged.

Cleanup runs before registration, including after installation failure or cancellation.
A crash can leave access active until the next panel, registration, or worker activation.
Pending cleanup blocks registration.
Changed rules and uncertain creation results require manual inspection.
Concurrent rule edits can race with removal because the policy API has no conditional delete.

## Development

`cmd/herdr-sbx` owns command definitions and argument handling.
The internal packages divide the work as follows:

| Package | Responsibility |
| --- | --- |
| `sbx` | Sandbox CLI and daemon communication |
| `herdr` | Herdr machine, workspace, plugin, and SSH operations |
| `precheck` | One file per prerequisite stage; `inspect.go` defines order |
| `registration` | Ownership, preparation, version recovery, and project workspace setup |
| `lifecycle` | Events, reconciliation, deletion, and worker lifetime |
| `guestplugins` | Selections, release recipes, bundles, and shortcut merging |
| `store` | Durable records, locks, observations, and cooldowns |
| `panel` | Display, selection, and interactive actions |
| `process`, `version` | Command execution and tool version comparison |

Run `mise run deps` before tests.
Use `mise run fmt` to format Go files and `mise run build` to rebuild the linked binary.
The test tasks use isolated home directories and Herdr state:

```sh
mise run test
mise run check       # Race tests and go vet
mise run test-panel  # Panel process with simulated commands
mise run test-worker # Two temporary Herdr servers and a simulated sandbox daemon
```

Tests live beside their implementation.
For one package, run `mise exec -- python3 scripts/test-go.py test ./internal/registration -count=1`.
Worker tests require a local Herdr binary.
These tasks do not use the normal sandbox daemon.

`mise run test-real` requires a dedicated authenticated environment with exactly one disposable sandbox.
Set `HERDR_SANDBOX_TEST_ROOT` to a new path under `/tmp`.
Set `HERDR_SANDBOXES_SBX_APP_NAME` and `HERDR_SANDBOX_TEST_NAME` to dedicated names that start with `herdr-sbx-test-`.
Set `HERDR_SANDBOX_TEST_SSH_CONFIG` to its SSH configuration, with absolute key paths.
The script isolates local Herdr and SSH state but does not create the test environment.

Use `HERDR_SANDBOX_TEST_LIFECYCLE=1` only with disposable data because it stops and deletes the named sandbox.
`HERDR_SANDBOX_TEST_EXPECT_MISSING=1` requires an initially absent guest Herdr binary.
`HERDR_SANDBOX_TEST_TEMPORARY_ACCESS=1` also requires that flag and a fixture that blocks guest installer downloads with HTTP 403.
It explicitly authorizes temporary access for that fixture.
Real sandbox, Linux host, and interactive overlay validation require their own environments and remain separate from isolated tests.

# Reference

See the [README](../README.md) for installation and requirements.
The plugin ID is `lanwen.herdr-sbx`.
The commands below are subcommands of the plugin binary, `herdr-sbx`.
For a local checkout, use `bin/herdr-sbx <command>`.
Run commands inside local Herdr when they need a server.
## Management model

For automatic-work permission and baseline rules, see [Status and Management](tui/status.md).
For connection choices, entry presence, and identity transitions, see [Sandboxes](tui/sandboxes.md).
Installed guest plugins and temporary policy rules have separate ownership protections described below.

## GitHub installation

Install mise and Git on the host, with `mise` and `git` available on `PATH`.
Herdr runs these manifest build commands in the downloaded plugin directory, in order:

```sh
mise trust --yes mise.toml
mise install --yes
mise run --yes build
```

The first command trusts only the plugin's `mise.toml` configuration.
mise installs the pinned Go version and runs the existing build task before Herdr enables the plugin.

## Local checkout

Use a checkout to develop the plugin or change its source.
Install [mise](https://mise.jdx.dev/) for the pinned Go version and build tasks.
Run these commands on the host:

```sh
git clone https://github.com/lanwen/herdr-sbx.git
cd herdr-sbx
mise trust
mise install
mise run deps
mise run link
```

For checkout upgrades, follow the README steps to turn Management OFF and disable the plugin.
Run `git pull --ff-only`, `mise run deps`, and `mise run link` in the checkout.
Close and reopen the manager, then accept the Start prompt or set Management to ON.

To switch from a checkout to GitHub installation, first turn Management OFF and disable the plugin as described in the README.
Unlink it before running `herdr plugin install lanwen/herdr-sbx`:

```sh
herdr plugin unlink lanwen.herdr-sbx
```

The unlink command removes the registration, not your checkout.

## Public sources

For external behavior and API contracts, consult the [Herdr documentation](https://herdr.dev/docs/) and [Docker Sandboxes documentation](https://docs.docker.com/ai/sandboxes/).
For regressions, upgrades, or new features, consult the [Herdr changelog](https://github.com/herdrdev/herdr/blob/master/CHANGELOG.md) and [Docker Sandboxes release notes](https://docs.docker.com/ai/sandboxes/release-notes/).
Record the installed Herdr and Docker Sandboxes versions before applying claims from these sources.
Distinguish features available in those versions from later releases and unreleased changes.

## Commands and panel

Run `herdr-sbx` to open the interactive sandbox manager.
The `open-status` plugin action opens the manager in a popup that requests 110 columns and 32 rows within terminal limits.
The `open-status-tab` and `open-status-split` actions open the same manager in a tab or split.
The manager detects its popup context from `HERDR_ENV=1`, `HERDR_PLUGIN_ID=lanwen.herdr-sbx`, `HERDR_PLUGIN_ENTRYPOINT_ID=status`, and an empty `HERDR_PANE_ID`.
Herdr gives tiled plugin panes a pane ID; popup pane commands have entrypoint context without a pane ID, as described in the [plugin environment documentation](https://herdr.dev/docs/plugins/#commands-and-environment).
Only the popup uses the [two-step quit prompt](tui-spec.md#popup-closing); closing it exits the manager process without closing a tiled pane or stopping the daemon.
`herdr-sbx status` reads saved daemon and sandbox observations without contacting Docker, starting work, or changing choices.
Its compact human summary includes Management, daemon health, sandbox choices, runtime, labels, status, and outstanding errors.
Use text and symbols alongside color, honor `NO_COLOR`, and disable terminal styling when redirected.
Human-readable CLI output removes terminal escape sequences and unsafe control characters from observed values and command errors.
Sandbox table fields stay on one line; diagnostic messages retain line breaks.
`herdr-sbx status --json` returns schema version 4 without terminal styling.
JSON preserves the original field values through JSON escaping.
It includes observation times, Management, daemon health, sandbox identity and enabled choices, native herdr state, stable entry associations, herdr plugin selections, and separate coded errors and warnings.
Missing observations remain explicitly unknown; retained labels are last known rather than fresh catalog facts.
Expose observation freshness instead of running slow active diagnostics.

`herdr-sbx daemon start` is idempotent, selects Management ON, clears a saved Stop choice, and starts the daemon after earlier cancellation settles.
`herdr-sbx daemon stop` selects Management OFF and waits for cancellation and exit.
If exit cannot be confirmed within one minute, it returns `D0001` and keeps Stop selected.
Herdr startup hooks call `herdr-sbx daemon start --automatic`, which respects a saved OFF choice.
`herdr-sbx daemon status` reports only daemon health and the saved On/Off choice.
Its `--json` form contains the same management, daemon fields, and daemon-scoped diagnostics as root JSON status, without sandbox or plugin data.

### CLI acceptance scenarios

- **S12** — Status output does not start the daemon or mutate setup; redirected and JSON output contain no terminal styling.
- **S19** — Daemon start and stop preserve the saved choice, and daemon status JSON matches the daemon subset of root status JSON without starting work.

The [shared interface specification](tui-spec.md) links to each feature.
Diagnostic presentation and recovery controls are defined in [Diagnostics](tui/diagnostics.md).

### Diagnostic codes

Codes contain a component letter and four digits.
The letters identify the daemon (D), sandbox inventory (S), herdr and SSH (H), registration (R), herdr plugins (P), and temporary access (T).

| Code | Meaning |
| --- | --- |
| `D0001` | A daemon control or stop-confirmation deadline expired. |
| `D0002` | A daemon start request failed. |
| `D0003` | A daemon stop request failed for a reason other than a deadline. |
| `D0004` | The saved daemon status could not be read. |
| `D0005` | The daemon event stream is reconnecting. |
| `D0006` | Saving a diagnostic dismissal failed. |
| `S0001` | The sandbox daemon or its inventory could not be read. |
| `S0002` | Sandbox deletion could not be verified, so the existing herdr entry remains. |
| `H0001` | Sandbox SSH setup failed. |
| `H0002` | A herdr label could not be changed. |
| `H0003` | The guest herdr executable lacks a required machine-API capability. |
| `H0004` | An explicit Fix herdr attempt failed. |
| `R0001` | Sandbox registration failed without a more specific outcome. |
| `R0002` | Changing the Enabled choice or removing a herdr entry failed. |
| `R0004` | An informational Re-sync outcome. |
| `R0005` | The sandbox-to-entry association is ambiguous. |
| `R0006` | Project workspace setup failed. |
| `R0007` | Sandbox or guest setup failed. |
| `R0008` | Herdr cannot safely create an offline entry. |
| `P0001` | Saving a herdr plugin synchronization choice failed. |
| `P0002` | A guest herdr plugin could not be synchronized. |
| `P0003` | A guest herdr plugin inspection response was malformed. |
| `P0004` | Host herdr plugin choices or defaults could not be read. |
| `P0005` | Confirmed sandbox network policy blocked a guest herdr plugin build. |
| `T0001` | Installation with temporary download access failed. |
| `T0002` | Temporary download access cleanup failed. |

`R0004` appears only in Events, while `R0005` and `S0002` are nonblocking warnings.
Other current codes and unknown codes represent errors.
A stopped sandbox without an entry is neutral and does not produce `R0008`.

## Daemon and recovery

Herdr starts the daemon when its server starts, completes a handoff, or creates a workspace.
Linking or enabling the plugin in an existing server does not trigger startup.
The [Management specification](tui/status.md) defines startup prompts, cancellation, and baseline capture.
The daemon exits when the plugin is disabled or the final local server stays stopped.
Closing the panel does not stop it.
After a daemon crash, reopen the TUI and accept the Start prompt.

`RUNNING` means that the event stream is active, not that every sandbox is ready.
The status includes the last successful sync and any reconnect error.
Host downloads select the local Herdr CLI version and compare the release checksum before SSH transfer.
Captured Herdr, sandbox CLI, and SSH command output is limited to 16 MiB per command, including stdout and stderr.
If output exceeds this limit, the plugin stops the command and reports an error without returning a partial response.
Recovery can replace a confirmed guest executable with that version and request a live handoff, which replaces the server while preserving panes.
If the handoff fails, the plugin reports the error without forcing a restart.
Broken or ambiguous installations require user action.
An uncertain registration retries only after a successful catalog read finds no matching machine.
Guest observations, connection failures, and recovery outcomes follow the [sandbox specification](tui/sandboxes.md#guest-observations-and-recovery).
Successful plugin synchronization and bounded audits follow the [plugin specification](tui/plugins.md).

New guest servers start in the resolved project directory.
Existing servers reuse a matching workspace or receive one project workspace.
For installed sbx v0.45.1 on macOS/Linux, the daemon's nonempty `workspace` is already the guest's primary working directory in both direct and clone modes.
Validate and use that inventory path for project setup instead of running `sbx exec … pwd -P`.
Clone mode can be identified separately from the daemon's `source_repo_dir` or clone label; `sbx ls --json` omits those fields.
Keep the guest directory probe for worktree overrides or conflicting or invalid workspace metadata.
Reject a non-string inventory workspace as an invalid daemon response.
Do not invent a project path for a mountless sandbox.
On hosts requiring path conversion, use the guest probe unless the conversion is verified.
Keep a saved pending guest path on retry.
The plugin records completion, so a workspace that you close stays closed.
Without project metadata, workspace setup stays unchanged.

State uses `HERDR_PLUGIN_STATE_DIR`, or `${XDG_STATE_HOME:-$HOME/.local/state}/herdr/plugins/lanwen.herdr-sbx` by default.
The directory holds stable association and enabled-choice records, daemon status, `state.json.worker.log`, and `guest-plugins.json`.
The daemon writes timestamped text events with severity and short details, including startup, reconnects, shutdown, prerequisite changes, and sandbox outcomes.
Identical prerequisite reports and sandbox outcomes are suppressed until their state or detail changes; foreground prerequisite and reconciliation tables stay unchanged.
Full command output and multiline reports are omitted from the daemon log; saved diagnostics remain separate.
The daemon retains three log files total: the active `state.json.worker.log`, the newest archive `.worker.log.1`, and the oldest archive `.worker.log.2`.
Each file has a nominal 1 MiB limit and private permissions.
Rotation checks run every 100 milliseconds, so active output can exceed the limit briefly.
Large bursts discard the oldest chunks beyond the retained history.
Rotation preserves the active file descriptor by copying and truncating the file; unsynchronized direct writes to stdout or stderr can race that final rewrite.
For a custom Herdr state location, provide its plugin state directory explicitly.
After a binary update, reopen the panel and restart the daemon to load it.
An already-running daemon keeps its earlier executable and `daemon start` alone does not replace it; use `daemon stop` followed by `daemon start`.
A mixed-version daemon can rewrite the shared status journal without fields introduced by the new binary, including diagnostic references and dismissals.
Run `mise run link` to build and relink the checkout after manifest changes.

## Guest plugins

Defaults, overrides, and visible recovery behavior are defined in [Herdr plugins](tui/plugins.md).
The daemon notices selection changes about once per second when idle.

The main herdr instance supplies each plugin's ID, version, installed root, exact manifest path, and source metadata when available.
Guest synchronization copies a source snapshot from that installed root, excluding `.git`, paths covered by its `.gitignore` rules, private state, and unsafe files.
It skips symlinks and special files without following or transferring their targets; a skipped file alone does not make the plugin Unsupported.
Source reads stay within the opened plugin directory and reject files and directories replaced after selection.
The 128 MiB source limit counts bytes actually read, including files that grow during synchronization.
It uses the snapshot digest as the guest revision instead of downloading an unpinned GitHub source archive or applying plugin-specific release recipes.
Plugins without a local source, Linux support, or safe transferable files are marked Unsupported in the Plugins tab with a reason.
An architecture mismatch found after inspecting a sandbox appears as a sync error for that sandbox.

The daemon transfers the source snapshot over SSH, runs manifest build commands that apply to Linux in their declared order inside the sandbox, then links and enables the plugin with guest herdr.
It validates the plugin manifest and copies its original bytes to the guest, because build scripts may read the manifest text.
Herdr `plugin link` does not run the build commands by itself.
An attempt is recorded before the guest build starts, so a failure or interrupted connection does not cause an automatic repeat on later reconciliation or daemon restart.
The failed source directory remains available for manual repair, and Re-sync inspects guest herdr for a matching, enabled revision before retrying the build.
For manual repair commands and failure presentation, see [Herdr plugins](tui/plugins.md).
Local or modified source cannot be reproduced by a clean GitHub install command; report its source identity and manual repair path.
Removing `.git` can prevent plugins that need Git metadata from building, in which case manual intervention is required.
Sandbox builds may need network access; a confirmed blocked policy-log entry during the attempt uses `P0005`, while ordinary timeouts or HTTP failures retain the original build error.
The plugin does not change sandbox network policy automatically.
Guest plugin inspection ignores unrelated SSH startup text and reports malformed response structure without exposing guest configuration.
Such failures use `P0002`, or `P0003` for a malformed guest response, remain in the event history, and open the affected sandbox from the Errors section.
Guest inspection applies the same ownership rules to every herdr plugin.
Canonical `<plugin-id>-<revision>` direct-child roots physically inside the private herdr-sbx guest plugin directory are managed.
Noncanonical roots and paths that resolve outside that directory are external and remain unchanged.
Guest inspection checks only the tools needed by the synchronization protocol.
Guests need `jq`, `tar`, `sha256sum`, and `base64` for synchronization.
Individual herdr plugins can have their own runtime dependencies.
Use manifest declarations, not plugin-ID-specific dependency lists.
Exclude herdr-sbx itself from guest synchronization.
Only shortcuts for the selected plugin and an absent guest prefix are copied from local Herdr configuration.
Conflicting shortcuts, different explicit prefixes, and externally managed installations require user action.
Configuration backups retain the original comments and formatting in `config.toml.herdr-sbx-<plugin-id>.bak`.
Other host configuration, environment variables, and credentials are not copied.

## Temporary download access

Host Herdr installation needs its own download access, and guest plugin builds may need sandbox network access.
Normal synchronization does not change sandbox network policy automatically.
The temporary download action is unavailable in the panel until its approval and policy flow is reworked.
All processes in that sandbox share this temporary HTTPS GET access.
The plugin removes only rules returned as newly created by its request.
Existing rules and denies stay unchanged.

Automatic cleanup runs only while Management is ON, including after installation failure or cancellation.
A crash or Management OFF can leave access active until Management resumes or an explicit eligible action handles it.
Pending cleanup blocks registration.
Cleanup records without a saved policy sandbox stay pending for manual inspection.
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
| `registration` | Machine association, enabled choices, preparation, version recovery, and project workspace setup |
| `lifecycle` | Events, reconciliation, deletion, and daemon lifetime |
| `guestplugins` | Selections, local payloads, and shortcut merging |
| `store` | Durable records, locks, observations, and cooldowns |
| `panel` | Display, selection, and interactive actions |
| `process`, `version` | Command execution and tool version comparison |

Run `mise run deps` before tests.
Use `mise run fmt` to format Go files and `mise run build` to rebuild the linked binary.
Use `mise run lint` for Go, POSIX shell, and source vocabulary checks.
Run `mise run go-lint`, `mise run shellcheck`, and `mise run source-words-check` separately when needed.
Go linting uses the explicit checks in `.golangci.yml`, including correctness, ineffective assignments, unused code, global variables, `init` functions, structured logging, and selected go-critic diagnostics.
Every Go function, including tests, must pass both gocognit and revive cognitive complexity checks with a maximum of 15.
Function and method declarations, including tests, must have at most five named input parameters, excluding the receiver.
Related required values belong in a cohesive input type; optional settings can use an options type.
The revive `argument-limit` rule counts named declaration parameters; it does not count unnamed parameters or inspect anonymous functions or function types.
Structured logging uses owned loggers, capitalized messages, typed attributes, and constant attribute keys.
Use context-aware logging when a context is available.
`mise run source-words-check` rejects `legacy` in Go, Python, shell, Ruby, and Swift source, including identifiers, comments, and strings.
The source check ignores letter case and also checks nonignored files before their first commit.
Documentation and configuration files are outside that source vocabulary rule.
Use `mise run vulncheck` to check reachable Go vulnerabilities against the Go vulnerability database.
These tasks install their pinned tools on first use and need network access for installation.
The vulnerability check also needs network access to read the database and fails when it finds reachable vulnerabilities.
Development tools are scoped to these tasks, so normal plugin installation still installs only Go.
Follow the [development workflow](development-workflow.md) for assignments, contract tables, review, and artifact acceptance.
The test tasks use isolated home directories and Herdr state:

```sh
mise run test
mise run check       # Race tests and go vet
mise run test-panel  # Panel process with simulated commands
mise run test-worker # Two temporary Herdr servers and a simulated sandbox daemon
```

Repeatable verification and artifact tasks:

```sh
mise run verify-panel  # Isolated panel tests, then build bin/herdr-sbx
mise run verify-change # Action pins, formatting, lint, vulnerabilities, isolated race tests, vet, then build
mise run action-pins-check # Read-only GitHub Action SHA validation
mise run capture-screenshots # macOS: simulated frames into docs/screenshots
mise run capture-screenshots -- /tmp/herdr-sbx-contrast --terminal-background '#E8D8C8'
python3 -m unittest discover -s scripts -p 'test_*.py'
```

GitHub CI runs `verify-change` and the workflow helpers' network-free tests.
The action-pin check uses Ruby's standard YAML parser (Psych); Ruby must be available locally and on CI.
It checks workflow and tracked action metadata, rejects mutable remote references, and permits local `./` actions.
Use literal YAML mappings and sequences for this check; aliases, anchors, explicit tags, and duplicate keys are rejected.
Screenshot rendering requires macOS and Swift; worker tests require an installed Herdr binary and run separately.
Neither verification task restarts, relinks, or reloads the live plugin.
Reopen the panel after rebuilding to load the executable; daemon updates follow the recovery instructions above.

For an assigned PR publication, prepare a body containing `![alt](path)` references to every supplied image, relative to the command's working directory:

```sh
mise run publish-pr-images -- --pr 7 --repo lanwen/herdr-sbx --body-file /tmp/body.md docs/screenshots/status.png
```

The helper preserves supplied prose, uploads through `gh --attach`, and verifies the published attachment URLs and body.
It does not generate a description or select images.
An interrupted upload may have changed the PR; inspect the actual body before retrying.

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

## Implementation boundaries

Reuse the current Bubble Tea root model, custom expanded-row renderer, and shared layout for rendering and mouse targets.
The current dependency pins are Bubble Tea v2.1.0 and Bubbles v2.2.1.
Keep external work in commands and typed result messages, with identity and generation checks for stale responses.
Update handles input and state transitions, while View only renders stored observations and choices.
Keep persistent intent, cancellation, identity checks, and operation serialization in services shared by the daemon and explicit actions.
The panel must not reconstruct these rules from display labels.

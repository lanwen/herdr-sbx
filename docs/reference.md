# Reference

See the [README](../README.md) for installation and requirements.
The plugin ID is `lanwen.herdr-sbx`.
The commands below are subcommands of the plugin binary, `herdr-sbx`.
For a local checkout, use `bin/herdr-sbx <command>`.
Run commands inside local Herdr when they need a server.

## Management model

The [interactive sandbox manager specification](tui-spec.md) defines the interface behavior.
Management ON/OFF is the global permission for automatic work; OFF also suspends cleanup and cancels safe active work.
Read-only host observation and explicit eligible actions remain available while OFF.
Eligible discovered sandboxes start with a separate Enabled choice; an explicit Disabled choice persists after its herdr entry is removed.
A stopped sandbox without an entry stays Disabled until it is running and the user enables it.
All verified Docker Sandbox entries are in scope regardless of who created them.
The plugin associates entries by stable sandbox and herdr identities, SSH target, and session rather than editable labels.
A verified external label or native-enabled change becomes the baseline when Management resumes.
Explicit Enable enables an existing herdr entry even when its sandbox is stopped, with Management ON or OFF.
Automatic reconciliation does not disable that entry merely because the sandbox is still stopped.
Guest setup waits until the sandbox is running.
Guest-plugin installation and temporary-policy ownership protections are separate and remain in force.

The plugin creates herdr entries only for running sandboxes.
Enable on a stopped sandbox without an entry leaves it Disabled without a pending choice or warning.
Its status is Stopped, and the user can enable it after starting it independently.
A manually added entry can be enabled immediately through the plugin.
Herdr may connect and start the sandbox after explicit Enable; keep the entry Disabled to prevent future herdr connections.
Disabling an entry prevents future connections but does not cancel a connection already in progress.

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
`herdr-sbx status` reads saved daemon and sandbox observations without contacting Docker or starting work.
`herdr-sbx status --json` returns schema version 4 with observation times, Management, daemon health, sandbox identity and enabled choices, native herdr state, stable entry associations, herdr plugin choices, and separate coded errors and warnings.
An unavailable observation stays unknown; a retained label is marked last known rather than presented as a fresh catalog result.
`herdr-sbx daemon start` selects Management ON and starts the daemon after any earlier cancellation settles.
`herdr-sbx daemon stop` selects Management OFF and waits for cancellation and daemon exit; an unconfirmed stop returns `D0001`.
`herdr-sbx daemon status --json` contains the same management and daemon fields as root JSON status, plus daemon-scoped diagnostics, and no sandbox or plugin data.
Herdr startup hooks call `herdr-sbx daemon start --automatic`, which respects a saved OFF choice.
Human status uses color when supported, honors `NO_COLOR`, and has no terminal styling when redirected.
JSON never contains terminal styling.

The TUI has Sandboxes, Status, and Plugins tabs in an outlined section, with the active tab leftmost.
The bottom-right indicators show Management ON or OFF immediately before the observed sbx state.
An unsettled stop shows exactly gray `Cancelling`.
The Status tab keeps the saved Management choice separate from daemon health.
Management has its own outlined subsection, focused by default; Space or Enter toggles the choice.
Its observed worker status appears in a top-right border badge, with additional details such as update age or activity muted at the bottom right.
Its outline brightens while focused, and Up/Down moves to the other available Status actions.
The Status Hint follows current Management activity and worker observations rather than retaining completed request notices or sandbox-specific guidance.
Prerequisite results use colored foreground text, with capitalized status words in the Status column.
The sbx row combines CLI version and command support, and Sandbox service combines daemon health and inventory access while retaining specific failure details.
Docker sign-in, OpenSSH, Sandbox SSH, and herdr remain separate; global Status omits per-sandbox setup.
Status polls report observations and never change the selected choice.
Management OFF prevents all automatic work, including setup, native state changes, herdr plugin synchronization, probes, retries, and cleanup.
Read-only host observations and explicit eligible actions remain available.
Turning ON first reads a fresh sandbox inventory and herdr catalog, accepts verified external label and native enabled changes as its baseline, then resumes work.
If the baseline cannot be read, mutation waits and the uncertainty remains visible.

The sandbox table has Enabled, Sandbox, Herdr label, and Status columns.
Sandbox rows have no disclosure triangle.
Enabled is a blue checkmark; Disabled and an idle missing entry use the same muted dot, with entry presence conveyed by the herdr label.
Eligible Enable and Disable update that choice immediately, with Enabling… or Disabling… in Status until the operation settles.
Older observations do not overwrite a pending choice; labels remain observed facts throughout the transition.
After the transition, the saved choice and symbol follow the verified native herdr state.
A rejected Enable returns to Disabled, and a rejected Disable returns to Enabled; report the operation error separately.
If the command returns an error but the catalog confirms the change, keep the confirmed result.
If the catalog cannot be verified, retain the last confirmed state as stale and pause automatic connection changes and guest work until a fresh observation resolves it.
Do not automatically replay a failed requested choice.
Below 72 columns, Status moves to Details and Hint; the label shortens before the sandbox name.
At 30 columns by 10 rows, tabs, filter, footer, and scrollable Hint remain reachable.
An eligible missing-entry dot requests Enable, even when an Enabled intention is already saved.
For a stopped sandbox without an entry, it leaves the sandbox Disabled quietly.
Up/Down selects a sandbox and immediately replaces its table row with an expanded preview.
Enter or Right moves focus into that preview and highlights the primary action; its outline becomes brighter.
Left/Right selects action buttons, and Up/Down moves between actions and plugin controls without leaving the preview.
Escape returns to list navigation while keeping the selected preview visible.
Left also returns to the list from the leftmost action or from plugin controls; from other action buttons it selects the previous button.
Tab and Shift+Tab cycle tabs; arrows never switch tabs.
Space changes the selected sandbox choice in list or action focus; its help text says Enable or Disable as appropriate.
The primary action shows `Enable (space)` or `Disable (space)`.
Render the action label bold and its `(space)` hotkey hint in regular weight.
Mute the primary CTA when a different action or plugin control has focus.
Enter activates the focused action, while Space on a plugin control cycles that plugin choice.
The expanded preview exposes eligible Enable, Disable, Remove from herdr, Rename, Re-sync, and Fix herdr actions.
Its colored title shows the sandbox name with runtime, then the herdr label with status.
The known herdr version occupies the top-right border.
Actions appear directly below the title with a blank line before and after.
The next line shows muted `sandbox ID → herdr ID`, followed by plugin controls.
Disable preserves the label and entry while disabling its native connection.
Remove from herdr becomes available only after native disable is confirmed and preserves the durable Disabled choice.
Rename edits the label inline for any uniquely associated entry, regardless of who created it.
Saving an unchanged label closes the editor without running a rename command or showing a transition.
During rename, show a block cursor and highlight the Rename button in sand with an Enter symbol for saving.
Left and Right continue moving the text cursor while rename is active.
Re-sync and Fix herdr are explicit actions and do not start a stopped sandbox.
Stopped sandboxes without entries stay Disabled quietly as described above.
Fresh unambiguous observations clear resolved association statuses and diagnostics.
True ambiguous matches retain a concrete explanation of the conflicting identity or target.

The Plugins tab sets defaults for herdr plugins; Details can override each sandbox with Inherit, On, or Off.
Defaults are live for inheriting enabled sandboxes while Management is ON.
Turning a herdr plugin choice Off prevents future synchronization and keeps existing guest files.
The filter opens with `/`, matches the current table fuzzily, and shows Clear only for an accepted nonempty query.
Keyboard and mouse can select rows, actions, tabs, events, and diagnostics.
Dragging selects visible plain text and requests clipboard delivery through OSC52; terminal support determines whether the copy arrives.
The standard Bubbles help component shows bold hotkeys with regular muted descriptions and faint separators, with plain output when color is disabled.
Compact and expanded help include only actions available for the current focus and selected target.
The refresh shortcut appears only when Refresh is available and no refresh is running; dismiss appears only for a selected, saved, undismissed diagnostic.
Press `?` to expand help in place without losing the tab, focus, selection, or underlying scroll position.
Press `?` or Escape to close it; scroll keys scroll help, and other advertised action shortcuts close help and execute in the preserved context.

Events appear in a collapsible outlined section above active diagnostics.
Events retain the most recent 50 records from the past hour.
Active diagnostics group causes by sandbox and code, keeping each herdr plugin cause and six-character reference separate.
The New view hides dismissed active groups; All includes them and counts occurrences.
Press `e` to reach diagnostics, Left or Right on its header to choose New or All, and `d` to dismiss the selected group.
Dismissal acknowledges a group without resolving it or hiding a later failure after full recovery.
One click selects a diagnostic; a second click on the same group opens its recovery action.
The code catalog below defines diagnostic meanings.
The [TUI specification](tui-spec.md#errors-and-concurrent-operations) defines their presentation and recovery controls.

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
Opening the TUI starts the daemon on first use.
If it is stopped on a later opening, the TUI asks whether to start it.
Use the Management controls to start or stop automatic work explicitly.
An explicit OFF choice persists and prevents automatic Herdr hooks from restarting it.
OFF cancels safe in-flight work and reports gray `Cancelling` until it settles.
ON requested during cancellation waits for the old worker, then captures a fresh inventory and catalog baseline.
Completed effects remain visible after cancellation; partial failures remain in diagnostics.
The daemon exits when the plugin is disabled or the final local server stays stopped.
Closing the panel does not stop it.
After a daemon crash, reopen the TUI and accept the Start prompt.

`RUNNING` means that the event stream is active, not that every sandbox is ready.
The status includes the last successful sync and any reconnect error.
Registration failures produce notifications, with identical warnings suppressed until the result changes or the sandbox recovers.
When a background refresh fails, status and warnings say `Could not refresh sandbox state: <actual reason>`.

Host downloads select the local Herdr CLI version and compare the release checksum before SSH transfer.
Recovery can replace a confirmed guest executable with that version and request a live handoff, which replaces the server while preserving panes.
If the handoff fails, the plugin reports the error without forcing a restart.
Broken or ambiguous installations require user action.
An uncertain registration retries only after a successful catalog read finds no matching machine.
Verified registrations are checked against the local herdr catalog without opening a new SSH connection on every catch-up.
The plugin connects only for due setup or herdr plugin work, an explicit eligible Re-sync, or an audit at most every 15 minutes, and only while the sandbox is running.
A failed fresh connection does not undo the entry or completed workspace setup.
Its diagnostic names the pending action and connection cause instead of asking for duplicate registration.
Successful herdr plugin synchronization records the effective source snapshot and shortcut choices so unchanged catch-ups do not inspect the guest again.
The plugin saves the guest server version only after a successful read-only server-status probe. It reuses that observation during guest work and bounded audits rather than probing on every TUI poll or routine catch-up. A failed version probe does not itself block registration or erase the last known version.
Hint calls an older retained observation the last known version and explains the guest version relative to the host version.
Workspace errors from herdr retain their upstream error message and code when supplied.
Herdr's machine-API forwarding discovery failure is shown as H0003, which can also mean the guest executable is missing or cannot run; it is not proof that two running server versions differ.
Other workspace failures remain R0006.
Error wording alone never triggers an automatic guest update, restart, or handoff.
If sbx refuses SSH before guest inspection because a saved host workspace path is missing, the diagnostic names the attempted herdr plugin work and the sbx path problem without suggesting sandbox removal.
The existing herdr entry stays in place, and Re-sync can be used after the path is available.

All verified Docker Sandbox machine entries are in scope regardless of creator.
Associations use the sandbox identity, herdr profile ID, SSH target, and session rather than editable labels.
An ambiguous or unrelated entry is never mutated or removed.
After a sandbox is confirmed deleted, Management can clear its retained local metadata when its herdr entry was already removed, allowing a new sandbox to reuse the name.
A verified replacement identity permits only that local cleanup; it never authorizes removal of the replacement's herdr entry.
Unconfirmed creation and uncertain absence retain their safeguards, and Management OFF suspends this cleanup too.
Native herdr state determines how an explicit toggle settles, independently of command errors or later guest setup failures.
Automatic work leaves the native connection state of an Enabled stopped sandbox unchanged and never enables it until it is running.

New guest servers start in the resolved project directory.
Existing servers reuse a matching workspace or receive one project workspace.
For installed sbx v0.45.1 on macOS/Linux, the daemon's nonempty `workspace` is already the guest's primary working directory in both direct and clone modes. Validate and use that inventory path for project setup instead of running `sbx exec … pwd -P`. Clone mode can be identified separately from the daemon's `source_repo_dir` or clone label; `sbx ls --json` omits those fields. Keep the guest directory probe for worktree overrides or conflicting or invalid workspace metadata. Reject a non-string inventory workspace as an invalid daemon response. Do not invent a project path for a mountless sandbox. On hosts requiring path conversion, use the guest probe unless the conversion is verified.
The plugin records completion, so a workspace that you close stays closed.
Without project metadata, workspace setup stays unchanged.

State uses `HERDR_PLUGIN_STATE_DIR`, or `${XDG_STATE_HOME:-$HOME/.local/state}/herdr/plugins/lanwen.herdr-sbx` by default.
The directory holds stable association and enabled-choice records, daemon status, `state.json.worker.log`, and `guest-plugins.json`.
When the daemon log exceeds 1 MiB, the daemon retains its most recent 512 KiB in the same file.
For a custom Herdr state location, provide its plugin state directory explicitly.
After a binary update, reopen the panel and restart the daemon to load it. An already-running daemon keeps its earlier executable and `daemon start` alone does not replace it; use `daemon stop` followed by `daemon start`. A mixed-version daemon can rewrite the shared status journal without fields introduced by the new binary, including diagnostic references and dismissals.
Run `mise run link` to build and relink the checkout after manifest changes.

## Guest plugins

Open Plugin defaults with `p` to toggle synchronization for each herdr plugin.
All plugins start disabled, including newly discovered plugins.
Each expanded sandbox row cycles Inherit → On → Off for each plugin with Enter, Space, or a click.
The Plugins tab toggles defaults directly with the same controls.
Inherited choices follow live defaults, while explicit overrides remain unchanged.
Changes apply automatically to running enabled sandboxes while Management is ON; stopped enabled sandboxes receive them when they next run with Management ON.
Disabling synchronization stops future installation and updates, but preserves guest files and shortcuts.
The daemon notices selection changes about once per second when idle.
The Plugins tab shows one explanation for the selected default.
On: the matching herdr plugin version will be copied to enabled sandboxes while Management is ON.
Off: the plugin will not be synchronized, and existing copies stay intact.
Unsupported plugins show a reason below their row and cannot be turned On.

The main herdr instance supplies each plugin's ID, version, installed root, exact manifest path, and source metadata when available.
Guest synchronization copies a source snapshot from that installed root, excluding `.git`, paths covered by its `.gitignore` rules, private state, and unsafe files.
It skips symlinks and special files without following or transferring their targets; a skipped file alone does not make the plugin Unsupported.
It uses the snapshot digest as the guest revision instead of downloading an unpinned GitHub source archive or applying plugin-specific release recipes.
Plugins without a local source, Linux support, or safe transferable files are marked Unsupported in the Plugins tab with a reason.
An architecture mismatch found after inspecting a sandbox appears as a sync error for that sandbox.

The daemon transfers the source snapshot over SSH, runs manifest build commands that apply to Linux in their declared order inside the sandbox, then links and enables the plugin with guest herdr.
It validates the plugin manifest and copies its original bytes to the guest, because build scripts may read the manifest text.
Herdr `plugin link` does not run the build commands by itself.
An attempt is recorded before the guest build starts, so a failure or interrupted connection does not cause an automatic repeat on later reconciliation or daemon restart.
The failed source directory remains available for manual repair, and Re-sync inspects guest herdr for a matching, enabled revision before retrying the build.
Each failed herdr plugin has its own coded diagnostic, occurrence count, recovery history, and repair guidance for that sandbox.
Show a verified manual install command in a padded callout with a blue left border only when the guest plugin was absent and the failed build is known to precede linking.
If linking may have happened, show retained-source repair guidance instead of a command that guest herdr would reject.
Place the callout before build output so long logs cannot hide the next step, and keep the full command available for direct drag selection.
Version alone does not prove a revision match; verified GitHub commit or the managed snapshot identity is also needed.
A matching externally installed plugin stays externally owned and unchanged.
For a clean GitHub source with a verified commit, the UI can show `herdr plugin install <owner>/<repo>[/subdir] --ref <commit> --yes` for manual installation inside the sandbox.
Local or modified source cannot be reproduced by that command; the UI reports its source identity and manual repair path instead.
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

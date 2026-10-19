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

Run `herdr-sbx` to open the interactive sandbox manager.
`herdr-sbx status` prints cached daemon and sandbox observations without contacting Docker or starting work.
Use `herdr-sbx status --json` for the versioned snapshot, including timestamps, labels, plugin choices, and coded errors and warnings.
Use `herdr-sbx daemon start` to start auto-sync explicitly and clear a saved Stop choice, or `herdr-sbx daemon stop` to save Stop and end it.
Stop waits for the daemon to exit before reporting success; an unconfirmed exit returns `D0001` rather than claiming the daemon stopped.
`herdr-sbx daemon status` reports only daemon health and the saved choice; `--json` returns the daemon fields from root status JSON without starting work.
Herdr startup hooks call `herdr-sbx daemon start --automatic`, which respects a saved Stop choice.
Terminal output uses color when supported and honors `NO_COLOR`.
Redirected output and JSON contain no terminal styling.
The TUI uses blue accents, filled focus highlights, styled buttons, and outlined sections.
Green, amber, and red distinguish ready, pending, and failed states alongside text and symbols.
The bottom-right indicators show a colored ON, OFF, or ERROR label for Auto-sync and a colored dot for sbx.
Append `*` to the Auto-sync label while its daemon is starting, stopping, pending, or reconnecting; keep the observed label and remove the marker when the transition settles.
Auto-sync refers to the plugin background daemon.
The footer shows shortcuts for the current controls, and `?` opens the full help.
Drag across visible text to highlight and copy it on release, without opening a separate mode.
A click activates its control on release; a drag never activates one.
The display stays still during a drag while background observations continue, then catches up after the next interaction.
Clipboard delivery uses OSC52 and depends on terminal support.
The filter shows Clear only for a nonempty accepted query, while input is not active.
Its empty `/ Filter…` prompt stays muted until the filter is focused.
The section’s top border contains Sandboxes, Status, and Plugins tabs with clickable left/right arrows.
Their order rotates so the active tab is always leftmost.
The active tab uses the arrow color; inactive tabs are muted.
Left/Right and Tab/Shift+Tab cycle the tabs, except that Left selects New and Right selects All while the diagnostics header has focus. Tab/Shift+Tab still cycle the top tabs there.
Status shows Auto-sync as `ON | OFF`, the colored observed daemon state, and prerequisite checks.
When the state is Stopped, omit the redundant daemon-not-running detail line.
The TUI loads the saved choice when it opens; later status polls report daemon state without changing that choice.
The selected choice changes immediately when toggled, while the observed state changes when the daemon reports it.
Selecting OFF shows `Running → Stopping` until the daemon exits.
If the stop request fails, OFF stays selected in red with `ERROR` beside the observed Running state, and the cause appears in the Errors section.
Press Enter on the failed choice, or click it again, to retry.
If the stop cannot be confirmed within one minute, `D0001` says that the daemon could not be reached or its stop could not be confirmed.
It shows `last at HH:MM` only while the daemon is running and its last successful sync is more than 30 seconds old.
An active refresh shows `*` even when the time is hidden.
The bottom-right Auto-sync indicator stays compact and has no timestamp.
Refresh observations is hidden in Status only when auto-sync is On, the daemon is Running, the last successful sync was within 30 seconds, and no active errors exist.
It appears when that observation is old or unknown, auto-sync is Off, the daemon is not Running, or an active error exists.
When present, place Refresh observations at the bottom of Status with a blank line above it.
If sandbox SSH is not ready, Status offers Set up SSH.
Text inputs and dialogs retain their local arrow and Tab controls.

Open the popup through the `open-status` plugin action.
Use `open-status-tab` or `open-status-split` for other layouts.
The popup requests 110 columns and 32 rows, within the terminal limits.
The old setup and management commands are replaced by actions in the TUI.
The `--automatic` daemon-start flag is for integration hooks.

The main table separates management state, runtime state, Herdr label, and readiness.
At widths below 72 columns, hide the Readiness column and keep its full explanation in Details and Hint.
Below 60 columns, show management as W for Watched, UM for Unmanaged, UW for Unwatched, D for Disabled, or UT for Untracked.
At any width, first replace a full management word with its two-cell short code when that saves a sandbox name from truncation.
Only after that checkpoint can the table shorten the sandbox name.
If more space is needed, shorten the herdr label before the sandbox name; Details keeps the full label.
Full Help explains these codes.
At phone widths, keep the sandbox name, a one-cell runtime symbol, and the herdr label visible, and shorten the table headings.
The TUI renders its main views from 30 columns and 10 rows upward.
Compact the tabs, filter, footer, and herdr plugin rows when necessary so controls stay reachable without horizontal clipping.
On small screens, long Hints and confirmations scroll in full; Page Up and Page Down move the viewport without changing the selected sandbox or action.
Keep the diagnostics New and All choices visible below a compact section title when the full title does not fit.
Show the complete herdr plugin identifier in Hint when a compact Details or Plugins row has focus.
Only a sandbox row with table focus uses the filled selection highlight. Moving into expanded controls, Events, diagnostics, or another section removes the row highlight without changing the selected sandbox.
Readiness, including ownership conflicts, has its own column.
Runtime text collapses to a colored dot when more space is needed for sandbox names or Herdr labels.
Expand a row to read its full runtime state.
Herdr labels use the sand color of the Herdr sandbox prefix.
New sandboxes are watched automatically.
Watched records the management choice, even before a herdr machine exists. A stopped watched sandbox without a registration waits until it starts; its Hint explains that the plugin will then try to add the machine.
Manually registered sandboxes remain unmanaged until you approve adoption, which preserves their existing labels.
Unwatch keeps the herdr entry and stops future management, including automatic deletion cleanup.
Stopped sandboxes are not treated as deleted. Deletion recovery checks only previously registered sandboxes missing from current inventory. If it cannot confirm deletion, it keeps that sandbox's herdr registration and reports the uncertainty for that sandbox without changing other sandbox explanations.
If reconciliation currently owns the registration state, Unwatch waits for it to finish before saving the choice.
Disable stops management and removes the owned Herdr entry without deleting the sandbox.
These choices persist across refreshes and daemon restarts.

| Key | Action |
| --- | --- |
| Up/Down | Select a row or expanded control; Down from the last one enters visible diagnostics, and Up returns to the table |
| Enter | Expand or collapse the selected sandbox |
| `w` | Watch or unwatch the selected sandbox |
| `r` | Rename its Herdr label inline |
| `/` | Filter the focused table with fuzzy matching |
| `s` | Open prerequisite status and repair actions |
| `p` | Open the plugin defaults table |
| `b` | Open Status with auto-sync controls |
| `e` | Jump to active errors or warnings |
| `c` | Refresh observations |
| Left/Right | Cycle tabs; on the diagnostics header, select New or All instead |
| Tab/Shift+Tab | Cycle tabs |
| `?` | Show contextual help |
| `q` | Close the TUI and leave the daemon running |

Mouse selection, buttons, disclosure controls, and scrolling provide equivalent navigation.
Only one sandbox row expands at a time.
Moving selection keeps it open; opening another row, toggling the expanded row, or Escape closes it.
Clicking a sandbox row selects it and toggles its details; embedded controls perform their own actions.
Expanded details show only actions applicable to that sandbox; an unmanaged registration offers Adopt instead of Watch or Unwatch.
Its details title includes the sandbox ID, ownership, and a runtime badge on the right. Show the relative observation age there only after it is more than two minutes old.
When a guest herdr server version is known, show `herdr <version>` in the lower-right Details border, using the border color.
Add `↓` when the guest server version is older than the host CLI version and `↑` when it is newer.
Omit the arrow when either version is unknown or cannot be compared, and omit the label when no guest version is known.
The panel reads saved observations and does not open SSH to render them.
The expanded row leaves its table readiness cell empty, and the first details line shows sandbox name → herdr label (readiness), followed by an outlined herdr plugin table.
Leave a blank line after that summary and indent the descriptive text before Plugins.
The unmanaged action is named Adopt and uses the primary action color.
Plugin choices update immediately when selected, while the effective value shows a loading marker until the save finishes.
The full action labels appear as horizontal buttons that wrap at narrow widths; Retry preparation is named Re-sync.
Details action confirmations and their choices appear in the Hint section, next to the action explanation.
While an action runs, show an animated spinner and its name in Hint instead of a changing Waiting, Pending, or Executing label.
Keep the action outcome in Hint; persistent failures remain in diagnostics.
Every focused Details action explains its effect in Hint, including Watch, Unwatch, Disable, Re-sync, Rename, Adopt, and Fix herdr.
Moving focus to another action replaces an old action notice with the new explanation.
The temporary download access action and its shortcut are unavailable until the feature is reworked.
Existing cleanup records, events, and diagnostics remain visible.
For a running, watched, owned sandbox with a known guest/host version difference or H0003, Fix herdr opens a confirmation in Hint.
The confirmation names the exact host version to install and explicitly says if it is older than the guest version.
Fix uses the guarded guest recovery path and requests a live server handoff; it does not stop or restart the server if handoff fails.
Re-sync retries project and herdr plugin work without changing the guest herdr binary.
Version differences alone are informational and never trigger an automatic repair or an error.
Down enters the expanded controls; Up and Down traverse plugins and actions before returning to sandbox selection.
Enter saves an inline rename and Escape cancels it.
When a click is blocked by active text input, the Hint section names the input and the action that was attempted.
Hints describe the selected sandbox or the current action and clear when navigation moves away from that context.
The selected sandbox shows ownership-conflict guidance in the colored Hint section even if another sandbox remains expanded.
An unmanaged sandbox shows that it is already a herdr machine and was added outside the plugin.
When Adopt has focus, Hint explains that the plugin will manage the machine, keep its name and connection, and remove it when the sandbox is deleted.
The Adopt hint uses the same accent color as the Adopt action.
Hints refer to this integration as the plugin; other interface text uses herdr-sbx.
Interface text uses lowercase herdr for the host and names synchronized items as herdr plugins.
Hint lines wrap at word boundaries.
When a sandbox is registered and setup is complete, its Hint says so without claiming that a fresh connection was checked.
The selected sandbox's saved status explanation, including failed and stopped states, appears in Hint instead of the expanded Details body. Special ready, ownership-conflict, and Adopt wording remains. Hint follows selection even when a different sandbox's details remain open.
Recent events appear in a collapsible outlined section above active diagnostics. Successful temporary-access cleanup is recorded as an event instead of a Hint.
Active errors and warnings share an outlined diagnostics section; transient hints use their own outlined section.
Pressing `e` focuses active diagnostics in the outlined section.
The first click on a diagnostic selects it without opening recovery; clicking that same selected diagnostic again opens its recovery action.
Clicking another diagnostic selects the new one first, while Enter opens recovery for the selected diagnostic immediately.
The section is titled Warnings, Errors, or Errors and warnings according to its active entries.
The diagnostics title switches between New and All active groups with Enter or Space; Left selects New and Right selects All when the header has focus. Counts appear as `New (N) | All (M)` and sum occurrences, not distinct errors. The selected choice is blue and the other muted. Errors, Warnings, and Events titles use regular text color only while their actionable header has focus; otherwise they match the border color. Focus does not change the outline color. New is the default; it hides dismissed groups but reports how many active occurrences are hidden. Press `d` on a group to dismiss it; there is no Dismiss button. Dismissal survives polling and reopening, and does not clear a failure. Changed details and new herdr plugin failures with the same code stay hidden until the entire sandbox-and-code group recovers; a later failure appears in New.
When visible, Down from the end of the sandbox table or its expanded controls reaches the Events header, then the diagnostics header, then visible entries; Up reverses that path. Enter or Space toggles the Events header, which starts collapsed. `e` jumps to the first visible diagnostic or its header when New is empty.
An action failure stays in the section without moving keyboard focus away from the current table or control.
If another reconciliation holds the registration lock, a rejected action has not finished. Watch and Unwatch wait for an ordinary overlap; if waiting fails, the diagnostic explains that another sync is updating registrations and the action can be retried.
Active diagnostics are grouped by sandbox and code, with one keyboard stop per group and stable sandbox-name/code ordering. The group header shows its severity-colored code without a word label, readable sandbox name, latest muted occurrence time, and compact muted `×N` for the group's summed occurrences. Each herdr plugin keeps its own detail, reference, build output, and recovery guidance inside the group. Event dates and times are muted too.
R0004 reports a re-sync attempt in Events as information; it is not an active error or warning. Any actual registration or herdr plugin failure remains under its own code.
Herdr plugin names are bold inside diagnostic groups.
Each underlying diagnostic places a muted, unique six-character lowercase alphanumeric reference immediately before its error text. The colored component code stays in the group header. The reference stays the same through repeats, dismissal, and restarts, and changes when a new failure follows recovery or its cause changes. Related history events retain the reference.
herdr notifications for failed sandbox outcomes show the sandbox name, the reference of one active diagnostic, and a short cause. They omit build output and repair commands; when several diagnostics are active, the notification points to one deterministic primary entry and directs the user to the TUI for the rest.
Guest plugin failures and recoveries remain in Events after the active error clears. Events retain at most the 50 most recent records from the past hour.
The daemon compacts its output log when it reaches 1 MiB, retaining the most recent 512 KiB while keeping the same log file.
Codes use one component letter and four digits.
The component letters are `D` for the auto-sync daemon, `S` for sandbox inventory, `H` for Herdr and SSH, `R` for registration, `P` for plugins, and `T` for temporary access.
`D0001` identifies a daemon control or stop-confirmation timeout; `D0002` and `D0003` identify start and stop failures; `D0004` and `D0005` identify status read and reconnect failures. `D0006` identifies a failed diagnostic-dismissal save in the shared status journal.
`S0001` identifies unavailable sandbox inventory. `S0002` is an amber warning when deletion of a previously registered sandbox cannot be confirmed; it includes the verification cause and keeps the herdr registration. `H0001` and `H0002` identify SSH setup and Herdr rename failures. `H0003` identifies confirmed guest herdr machine-API forwarding incompatibility; the plugin preserves the existing machine and workspaces and offers Fix herdr when safe. `H0004` identifies a failed explicit Fix herdr attempt and preserves the last confirmed guest version.
`R0001` through `R0007` identify registration, management, adoption, preparation, ownership conflict, workspace setup, and sandbox setup outcomes respectively.
`R0004` is an informational event. `R0005` and `S0002` are amber warnings because their existing herdr machines remain usable; the other current codes are errors that block work or reliable status.
Warning codes use the same amber as Unmanaged and Ownership conflict.
Status JSON schema version 3 reports warnings separately from errors while retaining internal scope IDs for diagnostics.
Event history shows the sandbox name in the TUI when known and colors each code to match its outcome.
`P0001` through `P0005` identify plugin choice, guest sync, malformed guest response, plugin discovery, and confirmed sandbox network policy failures respectively.
`T0001` and `T0002` identify temporary access installation and cleanup failures.

Prerequisite inspection observes binaries, daemon access, inventory, Docker authorization, and SSH configuration.
It does not repair them or connect to sandboxes.
Docker diagnostics can take 35 seconds, and inconclusive results remain `UNKNOWN`.
The plugin does not start the sandbox daemon, sign in, or store credentials and SSH keys.

## Daemon and recovery

Herdr starts the daemon when its server starts, completes a handoff, or creates a workspace.
Linking or enabling the plugin in an existing server does not trigger startup.
Opening the TUI starts the daemon on first use.
If it is stopped on a later opening, the TUI asks whether to start it.
Use the auto-sync controls to start or stop it explicitly.
An explicit Stop persists and prevents automatic Herdr hooks from restarting it.
The daemon exits when the plugin is disabled or the final local server stays stopped.
Closing the panel does not stop it.
After a daemon crash, reopen the TUI and accept the Start prompt.

`RUNNING` means that the event stream is active, not that every sandbox is ready.
The status includes the last successful sync and any reconnect error.
Registration failures produce notifications, with identical warnings suppressed until the result changes or the sandbox recovers.

Host downloads select the local Herdr CLI version and compare the release checksum before SSH transfer.
Recovery can replace a confirmed guest executable with that version and request a live handoff, which replaces the server while preserving panes.
If the handoff fails, the plugin reports the error without forcing a restart.
Broken or ambiguous installations require user action.
An uncertain registration retries only after a successful catalog read finds no matching machine.
Already owned registrations are verified against the local herdr catalog without opening a new SSH connection on every catch-up. The plugin connects when setup or herdr plugin work is due, on explicit Re-sync, and during an audit at most every 15 minutes. A failed fresh connection does not undo ownership or completed workspace setup. Its diagnostic names the pending action and connection cause instead of calling the saved machine unready or asking to retry registration. Successful herdr plugin synchronization records the effective source snapshot and shortcut choices so unchanged catch-ups do not inspect the guest again.
The plugin saves the guest server version only after a successful read-only server-status probe. It reuses that observation during guest work and bounded audits rather than probing on every TUI poll or routine catch-up. A failed version probe does not itself block registration or erase the last known version.
Hint calls an older retained observation the last known version and explains the guest version relative to the host version.
Workspace errors from herdr retain their upstream error message and code when supplied. Herdr's machine-API forwarding discovery failure is shown as H0003, a compatibility error that can also mean the guest executable is missing or cannot run; it is not proof that two running server versions differ. Other workspace failures remain R0006. Error wording alone never triggers an automatic guest update, restart, or handoff.
If sbx refuses SSH before guest inspection because a saved host workspace path is missing, the herdr plugin diagnostic names both the attempted sync and the sbx path problem. It shows the missing path once and omits sbx's suggestion to remove the sandbox. The owned herdr machine stays in place, and Re-sync can be used after the path is available.

An ownership conflict means that a machine exists without a matching ownership record.
Adoption requires one exact sandbox SSH target, a running sandbox, the default remote session, and no conflicting record.
It preserves the connection and workspaces, but lets the plugin remove that entry when the sandbox is deleted.
The daemon never adopts machines automatically.

New guest servers start in the resolved project directory.
Existing servers reuse a matching workspace or receive one project workspace.
For installed sbx v0.45.1 on macOS/Linux, the daemon's nonempty `workspace` is already the guest's primary working directory in both direct and clone modes. Validate and use that inventory path for project setup instead of running `sbx exec … pwd -P`. Clone mode can be identified separately from the daemon's `source_repo_dir` or clone label; `sbx ls --json` omits those fields. Keep the guest directory probe for legacy worktree overrides or conflicting or invalid workspace metadata. Reject a non-string inventory workspace as an invalid daemon response. Do not invent a project path for a mountless sandbox. On hosts requiring path conversion, use the guest probe unless the conversion is verified.
The plugin records completion, so a workspace that you close stays closed.
Without project metadata, workspace setup stays unchanged.

State uses `HERDR_PLUGIN_STATE_DIR`, or `${XDG_STATE_HOME:-$HOME/.local/state}/herdr/plugins/lanwen.herdr-sbx` by default.
The directory holds ownership records, daemon status, `state.json.worker.log`, and `guest-plugins.json`.
For a custom Herdr state location, provide its plugin state directory explicitly.
After a binary update, reopen the panel and restart the daemon to load it. An already-running daemon keeps its earlier executable and `daemon start` alone does not replace it; use `daemon stop` followed by `daemon start`. A mixed-version daemon can rewrite the shared status journal without fields introduced by the new binary, including diagnostic references and dismissals.
Run `mise run link` to build and relink the checkout after manifest changes.

## Guest plugins

Open Plugin defaults with `p` to toggle auto-sync for each plugin.
All plugins start disabled, including newly discovered plugins.
Each expanded sandbox row cycles Inherit → On → Off for each plugin with Enter, Space, or a click.
The Plugins tab toggles defaults directly with the same controls.
Inherited choices follow live defaults, while explicit overrides remain unchanged.
Changes apply to running watched sandboxes; stopped watched sandboxes receive them when they next run.
Disabling synchronization stops future installation and updates, but preserves guest files and shortcuts.
The daemon notices selection changes about once per second when idle.
The Plugins tab shows one explanation for the selected default.
On: matching plugin version will be copied to watched sandboxes.
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

Cleanup runs before registration, including after installation failure or cancellation.
A crash can leave access active until the next panel, registration, or daemon activation.
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
| `registration` | Ownership, preparation, version recovery, and project workspace setup |
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

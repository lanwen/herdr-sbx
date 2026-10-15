# Interactive sandbox manager specification

The interactive terminal interface replaces the current command menu and user-facing setup commands.
It provides sandbox management, prerequisite status, and herdr plugin configuration in one application.
This specification records the intended behavior and copy for the implemented TUI.

## Entry points and technology

Running `herdr-sbx` opens the TUI.
Existing herdr popup, tab, and split actions open the same interface.
Remove the replaced user commands, including `check`, `reconcile`, `watch`, `repair`, and `adopt`.
Expose their supported operations inside the TUI.
Keep `herdr-sbx status` and the `herdr-sbx daemon` command group as the user-facing subcommands.
The daemon group has `start`, `stop`, and `status`; `daemon status --json` returns only the daemon subset of root JSON status with the same field meanings.
The herdr plugin hooks launch background work through `herdr-sbx daemon start --automatic`, where the hidden automatic flag respects a saved Stop choice.

Use the latest stable Bubble Tea release at implementation time.
The verified baseline on October 15, 2026 is [Bubble Tea v2.1.0](https://github.com/charmbracelet/bubbletea/releases/tag/v2.1.0).
Pin the selected version in `go.mod`.
Reuse the existing operation services and daemon.

## Sandbox table

The main view contains one row per sandbox.
The table shows management state, sandbox name, sandbox runtime state, the herdr label when registered, and readiness in separate columns.
Below 72 columns, hide Readiness in the table while keeping its explanation in Details and Hint.
Below 60 columns, use W, UM, UW, D, and UT for Watched, Unmanaged, Unwatched, Disabled, and Untracked.
At wider widths, use a two-cell management code as the first width-saving checkpoint.
Only then shorten the sandbox name if it still does not fit.
After compacting management, shorten the herdr label before the sandbox name when space is still limited.
Explain the codes in full Help.
Keep sandbox names, a one-cell runtime symbol, and herdr labels visible at phone widths with compact headings.
Render the main views at 30 columns by 10 rows or larger, with tabs, filter, footer, and herdr plugin rows adapted to the available width.
If a Hint or confirmation exceeds the short screen, keep its full text and controls reachable by scrolling.
Page Up and Page Down scroll whenever a long Hint or confirmation joins the viewport, without changing the selected sandbox or action.
If the diagnostics title does not fit, put the New and All selector on a separate actionable line.
Show the complete herdr plugin identifier in Hint when a compact Details or Plugins row has focus, so names with the same prefix remain distinct.
At smaller sizes, show a short resize instruction and keep Close available.
Readiness includes ownership conflicts.
Runtime becomes a colored dot when names or labels need more space; expanded details retain its full text.
Show the herdr label for unmanaged registrations, even when it matches the sandbox name.
Runtime state and management state remain separate.
Show runtime states such as running, stopped, and pending using the observed sandbox state.
An unavailable observation is unknown, not stopped.
For an owned herdr machine, the local catalog and ownership record establish that it is registered. Do not claim it is unreachable or retry registration merely because a new SSH status check fails; a herdr client may still have an existing connection.

Management has two dimensions: whether herdr-sbx watches the sandbox, and whether it owns its herdr registration.
Present these dimensions with clear labels in the table.
The following states must remain distinguishable:

| State | Meaning |
| --- | --- |
| Watched | herdr-sbx follows lifecycle changes and manages registration and selected herdr plugin synchronization. |
| Unwatched | The user stopped management; any existing owned registration remains available. |
| Untracked | The sandbox has no saved management choice or owned registration. |
| Unmanaged | A matching herdr registration exists, but herdr-sbx does not own it. |
| Disabled | The user stopped management and removed the owned herdr registration. |

New sandboxes become watched automatically unless a matching unmanaged registration requires adoption.
Watched is a saved choice, not proof that a herdr machine already exists. A stopped sandbox can be Watched without a registration; the plugin tries to add its machine when the sandbox starts.
An explicit unwatch or disable choice persists across refreshes, daemon restarts, and TUI sessions.
When reconciliation holds the registration state, Unwatch waits for that work to finish before saving its choice, without blocking TUI navigation.
Automatic discovery must not override these choices.
Manually registered sandboxes remain unmanaged until the user approves adoption.

Use stable sandbox identity for selection and saved choices.
Show the filled highlight on the selected sandbox row only while the sandbox table has focus. Keep the selection when focus moves into expanded controls or another section, but remove the row highlight until focus returns to the table.
Refreshes must not move the selection to a different sandbox or overwrite active text input.
Do not infer deletion from a failed inventory request.
Stopped is a normal runtime state and never implies deletion. Check deletion only for a previously registered sandbox missing from current inventory. If its absence cannot be confirmed, keep its registration and show a sandbox-specific diagnostic: "Cannot confirm whether this sandbox was deleted. Its herdr registration was kept." Include the cause when available. Do not attach that explanation to other sandboxes.

## Sandbox actions

A row hotkey toggles between watched and unwatched.
The final key assignment belongs to the visual design stage.
Turning watching on prepares and registers a running sandbox, then follows future lifecycle changes.
Watching a stopped sandbox saves the choice and waits until it runs.
Unwatch stops future management and preserves the herdr entry.

Disable stops management and removes the owned herdr entry.
It does not delete the sandbox.
An unwatched owned entry can also be removed from herdr.
The disabled choice prevents automatic discovery from registering it again.
Removal failures remain visible and retryable; the interface must not claim successful removal prematurely.

Watching an unmanaged sandbox first explains adoption and asks for confirmation.
The explanation states that herdr-sbx can later remove the registration when the sandbox is deleted.
Adoption preserves the existing herdr label, connection, and workspaces.
Apply the existing adoption eligibility checks and show a reason when adoption is unavailable.
Ambiguous matches must not be selected automatically.

A rename action edits the herdr label inline in the table.
Enter saves and Escape cancels.
Renaming changes the herdr label, not the sandbox name.
A failed save preserves the input for correction or retry.
Reconciliation must preserve a user-selected label.
For unmanaged entries, require adoption before changes managed by herdr-sbx.

## Expanded sandbox row

Enter expands or collapses the selected row.
Only one row can be expanded at a time.
Expanding another row collapses the previous row.
Moving selection to another sandbox keeps the existing details open.
Toggling the expanded row or pressing Escape closes it.
Clicking a sandbox row selects it and toggles its details; embedded controls perform their own actions.
The expanded row leaves its table readiness cell empty because readiness appears in the details.
The details summary is followed by a blank line, then indented descriptive text until the outlined Plugins section.
The expanded area contains herdr plugin synchronization choices and applicable management actions.
Show only actions that apply to the sandbox's current management and readiness state.
An unmanaged registration offers Adopt in place of Watch or Unwatch.
Adopt is the primary colored action.
Its title shows Details · sandbox ID · Owned or Unowned, with the runtime badge at the top right. Show relative observation time immediately left of the badge only when it is more than two minutes old; omit `just now` and younger times.
When available, put `herdr <version>` in the lower-right Details border in the same color as that border.
Add `↓` if the guest server is older than the host CLI, or `↑` if it is newer.
Omit the arrow for equal, unknown, or unorderable versions, and omit the label when the guest version is unknown.
At narrow widths, preserve the border and main content.
Do not open SSH solely to render the detail view.
The first content line is sandbox name → herdr label (readiness).
Give the herdr plugin table its own outline.
Render all actions as full-length horizontal buttons, wrapping complete buttons when space is limited.
Name the preparation retry action Re-sync.
Up and Down traverse the expanded herdr plugin rows and action buttons without changing the selected sandbox until the user leaves the details.
Show operation progress, readiness details, and relevant recovery actions for that sandbox.
Place Details action confirmations in the Hint section. The Hint names the action and shows its confirm and cancel choices. During an active action, show an animated spinner and the action name in Hint instead of a text phase such as Waiting, Pending, or Executing. Show its result in Hint; keep coded failures in diagnostics.
When any Details action has focus, Hint explains that action instead of the general sandbox status.
Moving between actions clears stale transient feedback so the focused action has the correct explanation.
Fix herdr appears for a running, watched, owned sandbox with a known guest/host version difference or H0003.
It is separate from Re-sync and never runs automatically because versions differ.
Its confirmation names the host version it will install and warns when that version is older than the guest server.
The operation rechecks sandbox identity and ownership, uses the existing verified guest executable replacement, and requests live handoff without a stop fallback.
On success, refresh the guest version observation before reporting the new version.
On failure, keep the actual last known version and show a coded diagnostic with a retry route.
The temporary download access action and shortcut are unavailable while the feature is reworked; existing policy cleanup and its diagnostics remain active.
Unavailable actions include a reason.

For each sandbox, herdr plugin choices cycle Inherit → On → Off when activated with Enter, Space, or a click.
Default choices toggle On/Off directly without a dialog.
Show the new choice immediately after activation.
Show a loader in Effective while the save is pending, then show the effective value when it finishes.
Show both the effective value and whether it comes from a default or an override.
Provide a way to clear an override and return to Inherit.
Overrides persist independently of the sandbox runtime state.

## Plugin defaults view

Provide a separate herdr plugin table, reachable from an action below the sandbox table.
Each row identifies a herdr plugin and shows its default auto-sync value, On or Off.
Show availability or synchronization problems alongside the affected herdr plugin.
Toggling a default changes the effective value for all inheriting sandboxes.
Explicit sandbox overrides remain unchanged.
Newly discovered herdr plugins default to Off, preserving the current opt-in behavior.

Changes to herdr plugins apply immediately to running watched sandboxes.
After a successful sync, save the effective desired state for each sandbox and herdr plugin, including the source snapshot revision and copied shortcut settings. Routine catch-up compares this state locally and does not open a new guest connection or inspect the guest plugin when nothing changed. Recheck after a relevant choice, source, shortcut, sandbox identity, or lifecycle change, an explicit Re-sync, and a periodic audit no more often than every 15 minutes. A failed connection for pending work keeps the owned registration and completed workspace state; it does not trigger another machine add or repeat the same failed operation on each catch-up.
Stopped watched sandboxes receive the changes when they next run.
Turning synchronization off stops future installation and updates but preserves installed herdr plugin files and shortcuts.
Show one plain explanation for the selected herdr plugin default, reflecting its current On or Off value.
For On, state that the matching herdr plugin version will be copied to watched sandboxes.
For Off, state that the herdr plugin will not be synchronized and existing copies stay intact.
Mark herdr plugins Unsupported only when their source cannot be prepared safely or they do not support Linux, with the reason below their row.
Apply the same guest inspection and ownership rules to every selected herdr plugin.
A plugin's ID, version, installed root, and exact manifest path come from the main herdr instance.
Copy the exact source folder reported by the main herdr instance to the sandbox, excluding `.git`, paths ignored by its `.gitignore` rules, local secrets and state, and unsafe files.
Skip symlinks and special files in the plugin source, including links to files outside the source folder, without marking the whole plugin Unsupported.
Do not follow a skipped link or include its target in the guest bundle.
Preserve the manifest and source revision used by the main herdr installation; do not fetch an unpinned GitHub ZIP or substitute a newer checkout.
Do not use plugin-ID-specific download URLs, release recipes, or dependency lists.
For a plugin with Linux build commands in its manifest, run the applicable commands in the Linux sandbox before linking and enabling it in herdr.
Validate the manifest but copy its original bytes; do not reserialize it, because build scripts may read its exact text.
The build may download a prebuilt Linux binary or compile one; report which step failed and retain useful output without exposing secrets.
For a plugin without a Linux build command, require copied files that can run on the sandbox architecture.
Report an incompatible copied binary as a sandbox-specific preparation error instead of treating it as a valid Linux installation.
Identify each transferred source snapshot by its content digest, separately from any GitHub commit metadata.
Install each exact plugin revision once per sandbox, and record the attempt before starting the guest build so interruption or restart cannot silently repeat it.
After a failed attempt, do not keep rebuilding it in the background; show **Manual intervention required** with a Re-sync action.
Show one active diagnostic per failed herdr plugin and sandbox, with its own code, cause, occurrence count, and recovery history.
Keep the plugin ID visible beside the sandbox name so failures never merge into one paragraph.
Place actionable repair guidance above muted build output in a padded callout with a blue left border and clear margins.
Show a verified manual command in full at argument boundaries for direct text selection only when the guest plugin was absent and the failed build is known to precede linking.
If linking may have happened or no verified command exists, explain retained-source manual repair without inventing a command.
Keep the failed source and build directory for manual repair.
Re-sync first asks guest herdr whether the matching plugin revision is now installed, enabled, and free of blocking warnings, so a manual repair can resolve the error without another build.
Version alone does not prove an exact match; compare verified source commit or the plugin's managed source digest when available.
An externally installed match remains externally owned and is never removed or overwritten by herdr-sbx.
Only retry the transfer or build when the matching revision is still unavailable.
When herdr exposes enough source information to form a verified command for the exact revision, show that command for manual installation inside the sandbox.
Otherwise, show the required plugin ID, version, and source revision without inventing an install command.
For a clean GitHub checkout with a verified commit, the manual command uses `herdr plugin install <owner>/<repo>[/subdir] --ref <resolved-commit> --yes` inside the sandbox.
If a matching sandbox policy log entry appears during a failed guest build, label **Sandbox network access** as the cause and explain the blocked destination when known.
Do not change sandbox network permissions automatically.
An HTTP error, DNS failure, or timeout alone does not prove a sandbox policy block; keep the original build error when policy evidence is absent.
Exclude herdr-sbx itself from guest synchronization to avoid syncing this integration into its own sandboxes.
A guest installation at a canonical `<plugin-id>-<revision>` direct child of the private herdr-sbx plugin directory is managed by herdr-sbx when the resolved path stays there.
A noncanonical root is external even when it sits inside that directory or carries old ownership markers.
Paths that escape the directory through `..` or a symlink are external.
Leave external installed roots unchanged.
Guest inspection checks the tools required by the sync protocol without hardcoding a plugin ID or its runtime dependencies.
For project workspace setup on supported macOS/Linux sbx versions, validate and use the daemon inventory's nonempty primary `workspace` path for direct and clone sandboxes; clone mode does not itself imply a different absolute guest path. The daemon's `source_repo_dir` or clone label identifies clone mode, while `sbx ls --json` alone does not. Fall back to `sbx exec … pwd -P` for legacy worktree overrides, conflicting or invalid metadata, or hosts whose guest path conversion has not been verified. Reject a non-string inventory workspace. Keep a saved pending guest path on retry, and do not guess a path for mountless sandboxes.

## Status and daemon controls

Each catch-up checks the saved machine's catalog identity and ownership locally. An owned machine with completed workspace setup and no pending herdr plugin work does not need a fresh SSH status command. A fresh connection is opened only for actual pending guest work, first registration, explicit Re-sync, or an audit. A failed new connection reports that the plugin could not open it, with the cause and affected action; it does not say the saved registration failed or advise retrying registration. Preserve the existing machine, label, workspace completion, and prior successful observation. Prior success is historical, not proof of current reachability.
Cache a successfully probed running guest herdr server version per sandbox identity and observation time. Reuse probes during guest work or the bounded audit, never on TUI rendering or routine polls. Keep the cached version after a stop or failed probe; call it last known in Hint when it is no longer current. The observation time remains in status data. A probe failure alone does not block the sandbox.
Classify a confirmed guest herdr machine-API forwarding incompatibility as H0003, with guidance to use Fix herdr when safe or update the protected installation manually before Re-sync. Do not infer a running-server version mismatch solely from this error: the executable may be old, missing, or unable to run. Use H0004 for a failed explicit Fix herdr attempt while preserving the last confirmed guest version. Keep other workspace failures as R0006. Parse Herdr's structured error code and message when present, and extract a plain cause from its known textual transport wrapper without showing Rust debug syntax. Do not automatically update or restart herdr from parsed wording.
When sbx refuses the new SSH connection because the sandbox's saved host workspace path is missing, the herdr plugin diagnostic says that herdr-sbx was trying to inspect or sync the herdr plugin and that sbx blocked the connection before guest work began. Show the missing path once, without nested error prefixes or sbx's sandbox-removal suggestion. Tell the user to check or restore that path before Re-sync; keep the existing herdr machine.

Every view shows compact Auto-sync and sbx indicators at the bottom right.
Auto-sync is the user-facing name for the herdr-sbx background daemon.
Auto-sync shows a colored ON, OFF, or ERROR label without a dot; sbx shows a colored dot.
Append `*` directly to the Auto-sync label during daemon start, stop, pending, or reconnecting transitions, while preserving the observed ON, OFF, or ERROR label.
Remove the marker when the transition settles or fails; ordinary observation refresh does not add it.
Explicit state text remains visible when colors are disabled.
The interface has no application header.
It distinguishes running, starting, reconnecting, stopped, and unavailable observations.
The sbx daemon status is separate from the herdr-sbx auto-sync status.
An active daemon does not imply that every sandbox is ready.

On first use, opening the TUI starts the background daemon automatically.
Closing the TUI leaves the daemon running.
The Status tab shows `ON | OFF` with one selected choice, followed by the observed colored daemon state and prerequisites.
When it shows Stopped, omit the redundant `Daemon is not running.` detail line.
Load the saved choice when the TUI opens; status polls must never change it.
The choice changes immediately when toggled, while the observed state follows daemon reports.
Turning Off shows `Running → Stopping` until the daemon exits.
If the stop request fails, keep OFF selected in red, show `ERROR` next to the observed Running state, and show the cause in Errors.
If the daemon remains active for more than one minute, show `D0001` with a plain-language explanation.
Turning On while a stop is in progress waits for the previous daemon to exit before starting another one.
The bottom-right auto-sync indicator reflects the observed state without a timestamp.
Show `last at HH:MM` only while the daemon is running and its last successful sync is more than 30 seconds old.
Show `*` during a refresh even when the time is hidden.
Hide Refresh observations only while auto-sync is On, the daemon is Running, the last successful sync is within 30 seconds, and no active errors exist.
Show it when that observation is old or unknown, auto-sync is Off, the daemon is not Running, or an active error exists.
Place it after the Status details and prerequisite checks, separated by a blank line.
Do not repeat the last successful sync on a separate line.
Offer Set up SSH whenever the SSH prerequisite is not ready.
Starting an already running daemon must not create a second daemon.
An explicit Stop persists and prevents herdr startup from restarting the daemon automatically.
If the daemon is stopped on a later TUI opening, ask whether to start it.
Declining leaves the TUI usable for inspection and configuration.
Starting it clears the saved Stop choice.
Error codes use a component letter and four digits, such as `D0001` for a daemon stop timeout.
`P0005` identifies a guest plugin build blocked by a confirmed sandbox network policy event.

The section’s top border contains Sandboxes, Status, and Plugins tabs with clickable left/right arrows.
Their order rotates so the active tab is always leftmost.
The tabs have no background or brackets; the active tab matches the arrow color and inactive tabs are muted.
Left/Right and Tab/Shift+Tab cycle the tabs, except that Left selects New and Right selects All while the diagnostics header has focus. Tab/Shift+Tab still cycle the top tabs there.
Inputs and dialogs keep local key handling.
Up/Down navigate controls within the active tab.
The bottom-right indicators open Status.
Show prerequisites with a checkmark for success and a cross for failure, alongside text.
Use a distinct symbol and label for pending or unknown results.
Never show an inconclusive observation as a failure or success.
Include binary availability, daemon access, inventory, Docker authorization, SSH configuration, auto-sync state, and last successful synchronization.
Expose relevant repairs and retries from this view or the affected sandbox.

Inspection remains observational.
Repairs run only through an explicit action.
Do not offer temporary guest download access in the panel while its approval flow is reworked.
Continue to recover and report pending policy cleanup from prior attempts.
Slow diagnostics and repairs must not freeze navigation or rendering.
Interactive SSH setup temporarily hands the terminal to its external setup process after explicit activation.
When that process finishes or is canceled, restore the TUI and its prior focus.

## Status command

`herdr-sbx status` prints a compact, readable summary with color in supported terminals.
Include daemon health, sandbox management and runtime states, and outstanding errors.
Use text and symbols so color is never the only indication of state.
Honor `NO_COLOR` and disable terminal styling when output is redirected.

`herdr-sbx status --json` prints a richer machine-readable snapshot without terminal styling.
Include schema version 3, observation timestamps, daemon details, sandbox identities, management choices, runtime states, herdr labels, herdr plugin selections, and separate coded errors and warnings.
Represent missing observations explicitly rather than inventing healthy or stopped states.
The command does not start the daemon, repair setup, or change management choices.
Avoid slow active diagnostics in this command; expose observation freshness instead.
`herdr-sbx daemon status` prints only the auto-sync daemon's health and saved On/Off choice.
`herdr-sbx daemon status --json` returns the same daemon fields found in root status JSON, without sandbox or plugin data.
`herdr-sbx daemon start` is idempotent and clears a saved Stop choice; `herdr-sbx daemon stop` saves Stop so startup hooks do not restart it.
`daemon stop` waits until the daemon has exited before reporting success; if exit cannot be confirmed within one minute, it returns `D0001` and keeps Stop selected.

## Navigation and filtering

Support keyboard navigation and mouse selection, activation, scrolling, and focus changes.
All mouse actions also have keyboard equivalents.
Drag the left mouse button across visible text to highlight it and send the selected plain text to the terminal clipboard on release.
An ordinary click activates its original control exactly once on release; any movement to another terminal cell makes the gesture a drag and never activates a control.
Keep the displayed frame stable from press through release while background observations and operations continue.
The selection can span lines in either direction and uses displayed cells and whole graphemes, not byte offsets.
Copy visible line breaks, indentation, and interior spaces without ANSI styling or trailing screen padding.
Keep the selection highlighted until the next keyboard or pointer action; that action clears the highlight, shows the latest state, and performs its usual behavior.
Escape cancels an active drag without closing details or cancelling input; resizing cancels the gesture without copying.
Limit selection to the visible screen, and show brief clipboard feedback without obscuring the selected text.
The terminal must support OSC52 for clipboard delivery; do not claim that delivery was confirmed when support is unknown.
Use the standard Bubbles help component for contextual shortcuts and full help.
Text input captures typing without triggering global action shortcuts.

Slash opens a fuzzy filter in each table view.
Sandbox filtering matches sandbox names and herdr labels.
The herdr plugin filter matches plugin identifiers and available display names.
Escape exits filter input.
Show Clear only for a nonempty accepted filter, while input is not active.
Mute the empty `/ Filter…` prompt until filter input has focus.
Filtering changes visibility only; it does not change management or herdr plugin choices.
Show an explicit empty result when no row matches.

The interface adapts to terminal resizing and scrolls long tables.
Preserve access to navigation, auto-sync state, and errors in small terminals.
The visual design defines column priorities and the minimum useful terminal size.

## Errors and concurrent operations

Show one dedicated outlined section when active errors or warnings exist; omit it when there are none.
Title it Warnings, Errors, or Errors and warnings according to its entries, with separate counts when both appear.
Show Events above the active diagnostics section. Retain at most the 50 most recent events from the past hour; expire older events even when no new event is added. Its header shows the retained-event count; start collapsed and expand or collapse with Enter, Space, or a mouse click. Retain its expansion choice while the TUI is open.
Keep the diagnostics section visible when active groups are dismissed. Start in New (undismissed); its header switches between `New (N)` and `All (M)` (all active), where N and M sum occurrence counts rather than counting distinct entries or groups. Report the number of active occurrences hidden in New. Render the selected choice blue and the other muted. Render the Errors, Warnings, and Events section titles in regular text color only when their actionable header has focus; otherwise use the border color. The counts reflect the selected view immediately. Selection and focus do not recolor the section outline.
When New is empty but All has dismissed active entries, place its explanatory message inside the outline with a blank line above and below and a small left indent.
Group active diagnostics by sandbox scope and component code, displaying the readable sandbox name. Order groups by sandbox name, then code, with scope as the tie-break for duplicate names, so a child failure changing or recovering does not move its still-active group. Keep distinct herdr plugin causes, six-character references, repair instructions, and build output as separate details inside the group; never concatenate them into one error. One keyboard selection covers a group.
Render herdr plugin names in bold within diagnostic groups.
Dismissal acknowledges the entire sandbox-and-code group; it does not resolve any underlying failure or remove persisted records. The group stays hidden from New across polling and reopening, including repeat occurrences, changed details, and newly failing herdr plugins with the same code. It becomes New again only after every member of that group has recovered and a later failure occurs. Keep dismissal available for warnings too.
Keep keyboard focus on the current table or control after an action fails; `e` moves focus to active diagnostics.
Record R0004 re-sync outcomes as informational Events only. Do not put R0004 in active errors or warnings; show any underlying registration or herdr plugin failure with its specific code.
When visible, Down from the last sandbox row or its expanded controls enters the Events header, then the diagnostics header, then its visible entries. Up reverses this path; if the filtered table is empty, Down still reaches these sections.
Enter or Space on the diagnostics header switches New/All; Left selects New and Right selects All there. Tab/Shift+Tab still cycle the top tabs. `d` dismisses the selected group without a Dismiss button. Pressing `e` jumps to the first visible group, or its header if New is empty. Keyboard and mouse controls can reach both sections and the switch.
Render each group with its colored code alone in the header, readable sandbox name when known, latest muted occurrence time, aggregate muted `×N` when more than one occurrence, and separate child causes and recovery guidance below the current tab content; do not open a separate page. The code color conveys warning or error severity without a repeated word label.
Give every distinct underlying diagnostic a persisted, unique six-character lowercase alphanumeric reference (`[a-z0-9]{6}`). Show it muted on its detail line immediately before the first line of error text; keep the colored component code in the group header so its position stays steady. Keep a child's reference across identical repeats, polling, dismissal, and TUI restarts; a changed cause or a new failure after recovery gets a new reference. Retain it with related history events.
Keep herdr notifications for a failed sandbox short: sandbox name, one reference that points to an active diagnostic, and a one-line plain-language cause, capped at 120 displayed columns. Shorten a long sandbox name to 32 columns and truncate the cause if necessary, never the reference. Do not paste full plugin output, repair commands, or combined diagnostics into a notification. When several diagnostics are active for the same sandbox, select one primary entry deterministically, mention that others remain in the TUI, and send one notification for the sandbox outcome. If no matching diagnostic was saved, do not claim a reference exists.
Place the group's latest occurrence time after the sandbox name, matching event rows. Mute event and diagnostic dates and times. Show a compact muted `×N` after the time only when the group's aggregate occurrence count exceeds one.
Color R0005 and S0002 warnings with the same amber as Unmanaged and Ownership conflict; other current and unknown codes remain red errors.
Enter on R0005 focuses Adopt for the matching sandbox without approving it.
Enter on a selected diagnostic opens its recovery action.
The first mouse click selects a diagnostic group and leaves the user in the diagnostics section.
Only a second click on that same selected group opens its recovery action; clicking a different group selects it first.
An action failure appears in that section even if a daemon-status poll arrives before its durable record is written.
Show persistent recent events and transient hints in their own outlined sections. Successful temporary-access cleanup is an event, not a Hint; a cleanup failure remains an active diagnostic.
Show ownership-conflict guidance in the Hint section while its sandbox is selected, instead of in the details body.
Hint content follows the selected sandbox even if another sandbox's details remain open.
For an unmanaged sandbox, state that it is already a herdr machine and was added outside the plugin.
When Adopt has focus, explain that the plugin will watch the sandbox, synchronize selected herdr plugins, keep its herdr name and connection, and be able to remove the herdr machine when the sandbox is deleted.
Color the Adopt hint with the same accent as the Adopt action.
Use “plugin” for herdr-sbx in ordinary hints; elsewhere, write “herdr-sbx”.
Write “herdr” in lowercase and use “herdr plugins” when referring to plugins synchronized into sandboxes.
Wrap hint text at spaces so words stay whole; let long hints use multiple lines inside the outline.
For a registered sandbox with completed setup, Hint says: "This sandbox is registered in herdr. Setup is complete." Do not imply that a fresh connection was checked when readiness came from saved registration and sync state.
Show the selected sandbox's saved status explanation in Hint instead of its expanded Details body, including failed and stopped states. Keep the special ready, ownership-conflict, and Adopt wording; color other explanations by the observed state so the cause stays visible when details are closed. An empty explanation adds no Hint.
For a stopped Watched sandbox without a herdr registration, Hint says: "This sandbox is stopped. The plugin will try to add its herdr machine when it starts." A recovered deletion check must clear its former explanation.
Replace a hint when a newer action starts, and clear it when navigation leaves the action's context.
Retain a bounded, durable herdr plugin event history, including failures and recoveries, so a notification remains inspectable after the active error clears.
Known errors have stable codes that also appear in JSON output.
`D0006` identifies a failed diagnostic-dismissal save; the entry stays New when saving fails, and a successful retry clears this failure.
R0004 is an informational event. R0005 and S0002 are nonblocking warnings; S0002 means deletion of a previously registered sandbox could not be verified, so its herdr machine was kept. All other current codes represent blocked work or unreliable observations.
The versioned status JSON separates warnings from errors; warning-only state reports zero errors.
Warnings alone do not put Auto-sync into an Error state or offer Refresh observations.
Each error includes its scope, a readable explanation, and an available recovery action.
History entries include their code, scope, detail, time, and recovery state.
Show the sandbox name in event entries when it is known, instead of the internal scope and sandbox ID.
Color event codes according to their severity, using the same amber as Unmanaged and Ownership conflict for warnings.
Preserve useful diagnostic details for unknown errors without exposing credentials.
Repeated identical errors must not flood the interface.
Dismissal does not mark the underlying problem as resolved. All includes dismissed entries until the problem is actually cleared.

Show pending actions and their final outcome.
Prevent conflicting actions on the same sandbox while a mutation is in progress.
Coordinate saved choices with the background daemon so unwatch and disable cannot race with automatic registration.
If another reconciliation holds the registration lock, explain that sync is updating registrations and that the requested action did not finish. Do not imply it succeeded; retain a retry path. Watch and Unwatch should wait for an ordinary short overlap before reporting a failure.
Keep the interface usable when one sandbox fails or an external command is slow.

## Acceptance scenarios

1. First use starts at most one daemon; later opening with a stopped daemon asks whether to start it.
2. Closing the TUI leaves the daemon running.
3. A new sandbox becomes watched automatically; a manual herdr registration waits for approved adoption.
4. Approved adoption preserves the existing herdr label and rejects ambiguous or ineligible targets.
5. Unwatch preserves the registration; Disable removes it and prevents automatic registration on refresh or restart.
6. Inline rename survives reconciliation; cancellation leaves the previous label unchanged.
7. Expanding a second row collapses the first row.
8. A changed herdr plugin default affects inheriting sandboxes while explicit overrides retain their values.
9. Fuzzy filtering, keyboard controls, and mouse controls work in both tables.
10. Failed or slow diagnostics leave the interface responsive and show unknown or pending status accurately.
11. Known errors appear with the same stable code in the TUI and JSON status.
12. Status output does not start the daemon or mutate setup; redirected and JSON output contain no terminal styling.
13. Unwatch during reconciliation waits for its lock, then saves the choice; navigation remains responsive while it waits.
14. Active errors and warnings remain in the outlined diagnostics section; arrow keys move from the sandbox table into its entries and back, while `e` jumps directly there.
15. The Adopt hint matches its action color, wraps at whole words, and uses the product names and herdr plugin terms defined above.
16. A plugin with a Linux manifest build command transfers its exact source revision, excludes ignored files, runs the guest build once, and becomes available after herdr links it.
17. A failed guest build remains visible as Manual intervention required; Re-sync accepts a matching manually installed revision before attempting another build.
18. A build blocked by sandbox network restrictions has a distinct cause, while ambiguous failures retain their original diagnostic output.
19. Daemon start and stop preserve the saved choice, and daemon status JSON matches the daemon subset of root status JSON without starting work.
20. A click activates one control once on release; a drag beginning on the same control highlights and copies text without activating it.
21. Reversed, multiline, and Unicode drag selections copy the displayed plain text while background refreshes leave the selected frame still.
22. Escape or resize cancels an active drag without changing the focused input, dialog, or expanded sandbox.
23. Events starts collapsed above diagnostics; keyboard and mouse toggle it, and successful temporary-access cleanup appears there without a lingering Hint.
24. Dismissing an active sandbox-and-code group removes it from New across polling and reopening while All still counts and shows all occurrences; changed details and newly failing plugins in the same active group stay hidden, but full recovery followed by a new failure appears in New.
25. Diagnostic and event rows show a severity-colored code without an Error or Warning prefix, a muted date and time after the sandbox name, and a muted `×N` for repeated active occurrences.
26. If New is empty while dismissed active entries remain, the outlined section and its New/All switch remain reachable by keyboard and mouse with accurate active and hidden counts.
27. A short herdr notification contains a sandbox name and six-character reference that can be found before the error text in the exact active diagnostic; separate plugin failures have separate references and their full output remains in the TUI.
28. New/All counts appear in parentheses and sum occurrences. The selected choice is blue and the other muted; moving focus never changes the outline color. Dismiss is available through `d` without a button. When the diagnostics header has focus, Left selects New and Right selects All while Tab/Shift+Tab still cycle the top tabs. Errors, Warnings, and Events titles use regular text color only when their header has focus and border color otherwise. A selected sandbox row loses its filled highlight when table focus moves elsewhere.
29. Two plugins with the same code in one sandbox share one navigable group but retain separate references, details, and repair guidance; its aggregate `×N` and All count equal their summed occurrences.

Use isolated herdr sessions and sandbox fixtures for implementation tests.
Include input and state-transition tests for the TUI, plus daemon integration tests for saved management choices.
Check rendering, resizing, mouse behavior, and terminal restoration during visual validation.

## Visual design

The visual design covers layout, visual hierarchy, exact shortcuts, mouse behavior, expanded rows, inline rename, filtering, and error presentation.
The resulting design is recorded in [Interactive sandbox manager design](tui-design.md).

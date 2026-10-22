# Interactive sandbox manager specification

The interactive terminal interface provides sandbox management, prerequisite status, and herdr plugin configuration in one application.
This specification defines its behavior and interface text.

## Entry points and technology

Running `herdr-sbx` opens the TUI.
Herdr popup, tab, and split actions open the same interface.
Setup, synchronization, and repair actions are available inside the TUI.
Keep `herdr-sbx status` and the `herdr-sbx daemon` command group as the user-facing subcommands.
The daemon group has `start`, `stop`, and `status`; `daemon status --json` returns only the daemon subset of root JSON status with the same field meanings.
The herdr plugin hooks launch background work through `herdr-sbx daemon start --automatic`, where the hidden automatic flag respects a saved Stop choice.

The TUI uses Bubble Tea and Bubbles, with dependency versions pinned in `go.mod`.
Operation services are shared by the TUI and daemon.

## Management and sandbox state

Management is the global permission for automatic work.
Label the global control Management ON/OFF.
ON permits automatic registration, setup, selected herdr plugin synchronization, native machine enable/disable changes, and confirmed-deletion cleanup.
OFF permits no automatic work, including cleanup, retries, guest probes, and repairs.
Read-only host observations and explicit user actions remain available when Management is OFF.
Turning Management OFF does not itself disable or remove herdr machines.
An existing enabled herdr machine can still connect independently of the plugin.

Each sandbox has a durable Enabled or Disabled choice, separate from Management and entry presence.
Newly discovered sandboxes default to Enabled, except stopped sandboxes without herdr entries.
Those sandboxes remain Disabled until an eligible explicit Enable.
Preserve explicit Disabled choices across refreshes, daemon restarts, and Management OFF/ON unless a verified external change to an existing entry supersedes them on resume.
Default discovery must never reset a Disabled choice after the user removes its entry.

When Management turns ON, read the current sandbox inventory and herdr catalog before making changes.
Accept current external labels and enabled settings as the new baseline, discarding conflicting saved intentions from the previous management period.
For an existing entry, a verified external enable/disable change takes precedence over its saved choice.
For example, Management OFF, an external rename, and Management ON must preserve the external name.
Do not replay a cancelled rename or enable/disable request over that baseline.
When no entry exists, preserve a remembered Disabled choice rather than treating the sandbox as newly discovered.
Accept a verified native Disabled state on resume regardless of sandbox runtime.
If the current catalog cannot be read, defer mutations and report the uncertainty instead of applying stale intentions.
While Management is ON, reconcile entries to the choices maintained through the plugin.

The scope includes all unambiguously identified herdr entries for Docker Sandboxes, regardless of who created them.
Keep stable associations between sandbox identity and herdr machine identity, independent of editable labels.
Reject ambiguous targets, unrelated sessions, identity reuse, and label-only matches as authority for mutation or deletion.
Sandbox registration requires a verified identity association.
Installed guest plugins and temporary policy rules have separate ownership protections.

## Sandbox table

The main view contains one row per sandbox.
The main columns are Enabled, Sandbox, Herdr label, and Status, in that order.
Do not show a disclosure triangle before sandbox rows.
Render Enabled with a checkmark in the existing blue accent and Disabled with a small muted dot.
An idle sandbox without a herdr entry uses the same muted dot as Disabled, not a separate symbol.
Use the herdr label cell alone to distinguish an existing entry from an absent entry; an absent entry has no label.
An eligible Enable or Disable immediately updates the displayed checkmark or dot before external work finishes.
Show Enabling… or Disabling… in Status for the transition, including while the row is expanded.
For an eligible missing-entry Enable, show the requested checkmark immediately but keep its label blank until entry creation is observed.
Observation polls must not overwrite the pending choice or transition with older state.
After completion, settle the displayed and saved choice to the verified native herdr state.
A rejected Enable returns to Disabled; a rejected Disable returns to Enabled.
A command error does not undo a change that the catalog confirms actually succeeded.
Show errors separately; the ordinary Enable or Disable action remains available.
If the catalog cannot be verified, restore the last confirmed choice, mark the observation stale, and suspend automatic connection changes and guest work until a fresh observation resolves its actual state.
Failed persistence must not leave an unsaved optimistic choice displayed.
Enable on an existing entry requests native herdr enablement regardless of sandbox runtime.
Do not retain a failed requested choice for later automatic replay.
The Enabled control remains available when Management is OFF because explicit actions are still allowed.
There is no separate Managed column or management abbreviation legend.
Use Status for setup progress and problems preventing use through herdr.
Below 72 columns, hide Status while retaining its full explanation in Details and Hint.
Shorten the herdr label before the sandbox name when space is limited.
Keep the Enabled symbol, sandbox name, and herdr label visible at phone widths with compact headings.
Render the main views at 30 columns by 10 rows or larger, with tabs, filter, footer, and herdr plugin rows adapted to the available width.
If a Hint or confirmation exceeds the short screen, keep its full text and controls reachable by scrolling.
Page Up and Page Down scroll whenever a long Hint or confirmation joins the viewport, without changing the selected sandbox or action.
If the diagnostics title does not fit, put the New and All selector on a separate actionable line.
Show the complete herdr plugin identifier in Hint when a compact Details or Plugins row has focus, so names with the same prefix remain distinct.
At smaller sizes, show a short resize instruction and keep Close available.
Status describes setup progress, normal runtime conditions, or a problem preventing use through herdr.
Examples include Ready, Waiting for start, Stopped, Disabled in herdr, Enabling…, Disabling…, Setting up, and a specific connection or setup failure.
Explain an entry's absence or identity conflict directly in its status.
A fresh unambiguous catalog observation clears resolved association statuses and diagnostics.
Real ambiguity remains visible with its specific conflicting target or identity explained.
Stopped and Disabled are neutral conditions rather than errors.
Retain the full observed runtime state in Details.
Unknown observations and stale data must remain distinguishable from confirmed absence or a stopped sandbox.
Retain a last-known label with an observation-age explanation when the catalog is unavailable; do not clear it as though removal succeeded.
A herdr plugin synchronization failure can coexist with a usable herdr machine and must not falsely mark the whole machine unusable.
Management OFF stays in the global indicator rather than replacing every sandbox's Status.
Catalog presence and a verified identity association establish registration, not current SSH reachability.
A failed fresh SSH attempt must not trigger duplicate registration or erase completed setup.

Use stable sandbox identity for selection and saved choices.
The selected sandbox appears as an expanded preview in place of its table row.
Brighten its outline while its controls have focus; preserve its title colors in both preview and control focus.
Refreshes must not move the selection to a different sandbox or overwrite active text input.
Do not infer deletion from a failed inventory request.
Stopped is a normal runtime state and never implies deletion. Check deletion only for a previously registered sandbox missing from current inventory. If its absence cannot be confirmed, keep its registration and show a sandbox-specific diagnostic: "Cannot confirm whether this sandbox was deleted. Its herdr registration was kept." Include the cause when available. Do not attach that explanation to other sandboxes.

## Sandbox actions

A row control and a keyboard action toggle Enabled and Disabled.
For an eligible confirmed missing entry, activating the dot performs Enable or retries entry creation, even if discovery already saved an Enabled intention.
If a stopped sandbox has no entry, activation leaves it Disabled quietly.
Do not invert that hidden intention into Disabled when the user activates a missing-entry dot.
Suppress duplicate activation while entry creation is in progress.
Enable saves the enabled choice and adds an eligible herdr entry if absent, or enables the existing entry even when the sandbox is stopped.
For an existing stopped entry, call herdr's native enable operation without guest setup or a direct sandbox start.
Herdr may connect and start the sandbox as a result of that explicit action.
For a stopped sandbox without an entry, do not save pending Enabled intent or emit a warning.
Show the ordinary neutral Stopped status and keep the label blank and muted dot.
When an already enabled sandbox with an existing entry later starts, Management ON performs setup and enables the herdr connection if needed.
A stopped sandbox left Disabled because it had no entry remains Disabled when it starts until the user explicitly enables it.
With Management OFF, later automatic setup remains suspended until Management is enabled or an explicit eligible action is requested.
Disable saves the disabled choice, disables the existing herdr entry, and stops guest work where safe.
It preserves the entry and its label and does not stop or delete the sandbox.
Keep disabled sandboxes out of automatic preparation, probes, and synchronization.

Offer Remove from herdr only after the sandbox is disabled and the entry is confirmed natively disabled.
Removal deletes the herdr entry, not the sandbox, and retains the durable Disabled choice.
Do nothing further for that live disabled sandbox until it is explicitly enabled again.
Enable after removal recreates the entry when eligible.
A stopped sandbox without an entry stays Disabled until it is running and the user enables it.
Removal failures remain visible and retryable; retain the label until absence is confirmed.
Confirmed sandbox deletion permits stale-entry cleanup while Management is ON, including for disabled entries.
Management OFF suspends that cleanup too.

A rename action edits the herdr label inline in the selected card title.
Enter saves and Escape cancels.
Saving an unchanged label closes the editor without running a rename command or showing a transition.
While renaming, use a block cursor and highlight Rename in the herdr label's sand color with an Enter-to-save symbol.
Left and Right move the text cursor while editing instead of entering or leaving details.
Renaming changes the herdr label, not the sandbox name.
A failed save preserves the input for correction or retry.
Reconciliation must preserve a user-selected label.
Rename is available for any uniquely identified existing sandbox entry, regardless of Management or Enabled choice.
Without an entry there is no current herdr label to rename.

Re-sync explicitly retries setup and selected herdr plugin synchronization for a running enabled sandbox.
It remains available with Management OFF and does not enable background work.
Fix herdr remains a contextual explicit repair action with the existing version and handoff confirmation.
Neither action starts a stopped sandbox.

### Stopped-sandbox connection contract

The saved choice remembers the last settled native state for an existing entry and preserves Disabled when an entry is removed.
Only an active transition may temporarily display the requested state before it is confirmed.
Automatic plugin work must not start a stopped sandbox, open guest SSH, or enable its native herdr connection.
The plugin creates entries only for running sandboxes.
A stopped sandbox without an entry stays Disabled with neutral Stopped status, a blank label, and no warning or deferred Enable.
Do not call `machine add` or open SSH for that condition.
For an Enabled stopped sandbox with an existing entry, automatic reconciliation leaves its native connection state unchanged.
Explicit Enable and Disable change that native state immediately, with confirmation and failure reporting.
Do not undo explicit enablement merely because the next inventory observation still says Stopped.
Disable is the user's control for preventing future herdr connections; an enabled entry can connect independently of plugin setup.
When Management is OFF, do not change native connection state automatically.
Existing independently enabled herdr clients are outside the plugin's OFF control.

Disabling a herdr entry prevents future connections but does not cancel a connection already in progress.

## Expanded sandbox row

Up and Down select sandboxes and immediately open the selected sandbox's details as a preview.
Only the selected sandbox is expanded; its card replaces the table row entirely.
Enter or Right enters the preview's controls, highlights the primary action, and brightens the outline.
Escape returns to list navigation without closing the selected preview.
Left returns to list navigation from the leftmost action button or a plugin control; otherwise it selects the previous action.
Clicking a sandbox row or card header selects its preview; embedded controls perform their own actions.
Keep active Enable/Disable progress visible in the expanded title.
Place action buttons directly after the sandbox title with an empty line before and after.
Follow the actions with the muted sandbox ID → herdr ID line and the outlined Plugins section.
The expanded area contains herdr plugin synchronization choices and applicable management actions.
Show only actions that apply to the sandbox's enabled choice, entry presence, runtime, and operation state.
Offer Enable or Disable, Remove from herdr for a disabled entry, Rename for an existing entry, and eligible Re-sync or Fix herdr actions.
Its title shows the Enabled symbol and sandbox name (runtime) → herdr label (status).
Preserve the title's semantic colors rather than muting its text.
Do not show a Details heading or a separate runtime badge.
Show relative observation time near the version only when it is more than two minutes old; omit `just now` and younger times.
When available, put `herdr <version>` at the top right.
Add `↓` if the guest server is older than the host CLI, or `↑` if it is newer.
Omit the arrow for equal, unknown, or unorderable versions, and omit the label when the guest version is unknown.
At narrow widths, preserve the border and main content.
Do not open SSH solely to render the detail view.
Give the herdr plugin table its own outline.
Render all actions as full-length horizontal buttons, wrapping complete buttons when space is limited.
Append `(space)` to the primary Enable or Disable button.
Keep the action label bold and render the `(space)` hotkey hint in regular weight.
When another action or plugin control is selected, mute the primary CTA so it does not look selected.
Name the preparation retry action Re-sync.
Left and Right select buttons without switching tabs.
Inside the card, Down moves from actions to plugin controls and Up returns to actions; Escape or Left at the defined boundary returns to the list.
Enter activates the focused button.
Space invokes the primary Enable or Disable action from list or action focus; on plugin controls it cycles the plugin choice.
Contextual help shows the actual Space action, `enable` or `disable`, rather than `enable/disable`.
Tab and Shift+Tab switch tabs; arrows never do.
Show operation progress, status details, and relevant recovery actions for that sandbox.
At narrow widths, wrap the colored title into the card body above its actions when needed.
Wrapped labels and IDs retain their color and typeface styling on every continuation line.
Keep the primary action and internal controls reachable, and retain omitted version context in Hint.
Place Details action confirmations in the Hint section. The Hint names the action and shows its confirm and cancel choices. During an active action, show an animated spinner and the action name in Hint instead of a text phase such as Waiting, Pending, or Executing. Show its result in Hint; keep coded failures in diagnostics.
When any Details action has focus, Hint explains that action instead of the general sandbox status.
Moving between actions clears stale transient feedback so the focused action has the correct explanation.
Fix herdr appears for a running enabled sandbox with a verified herdr entry and a known guest/host version difference or H0003.
It is separate from Re-sync and never runs automatically because versions differ.
Its confirmation names the host version it will install and warns when that version is older than the guest server.
The operation rechecks the sandbox-to-machine identity association, uses the existing verified guest executable replacement, and requests live handoff without a stop fallback.
On success, refresh the guest version observation before reporting the new version.
On failure, keep the actual last known version and show a coded diagnostic with a retry route.
The temporary download access action and shortcut are unavailable while the feature is reworked.
Existing policy cleanup records and diagnostics remain visible; automatic cleanup obeys Management.
Unavailable actions include a reason.

For each sandbox, herdr plugin choices cycle Inherit → On → Off when activated with Enter, Space, or a click.
Default choices toggle On/Off directly without a dialog.
Show the new choice immediately after activation.
Show a loader in Effective while the save is pending, then show the effective value when it finishes.
Show both the effective value and whether it comes from a default or an override.
Provide a way to clear an override and return to Inherit.
Overrides persist independently of the sandbox runtime state.

## Plugin defaults view

Provide a separate herdr plugin table in the Plugins tab, reachable with `p` or tab navigation.
Each row identifies a herdr plugin and shows its default synchronization choice, On or Off.
Show availability or synchronization problems alongside the affected herdr plugin.
Toggling a default changes the effective value for all inheriting sandboxes.
Explicit sandbox overrides remain unchanged.
Newly discovered herdr plugins default to Off, preserving the current opt-in behavior.

When Management is ON, changes to herdr plugins apply to running enabled sandboxes.
With Management OFF, save the choices without scheduling guest work.
After a successful sync, save the effective desired state for each sandbox and herdr plugin, including the source snapshot revision and copied shortcut settings. Routine catch-up compares this state locally and does not open a new guest connection or inspect the guest plugin when nothing changed. Recheck after a relevant choice, source, shortcut, sandbox identity, or lifecycle change, an explicit Re-sync, and a periodic audit no more often than every 15 minutes. A failed connection for pending work keeps the associated registration and completed workspace state; it does not trigger another machine add or repeat the same failed operation on each catch-up.
Stopped enabled sandboxes receive the changes when they next run with Management ON.
Turning synchronization off stops future installation and updates but preserves installed herdr plugin files and shortcuts.
Show one plain explanation for the selected herdr plugin default, reflecting its current On or Off value.
For On, state that the matching herdr plugin version will be copied to enabled sandboxes while Management is ON.
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
A noncanonical root is external even when it sits inside that directory or carries ownership markers.
Paths that escape the directory through `..` or a symlink are external.
Leave external installed roots unchanged.
Guest inspection checks the tools required by the sync protocol without hardcoding a plugin ID or its runtime dependencies.
For project workspace setup on supported macOS/Linux sbx versions, validate and use the daemon inventory's nonempty primary `workspace` path for direct and clone sandboxes; clone mode does not itself imply a different absolute guest path. The daemon's `source_repo_dir` or clone label identifies clone mode, while `sbx ls --json` alone does not. Fall back to `sbx exec … pwd -P` for worktree overrides, conflicting or invalid metadata, or hosts whose guest path conversion has not been verified. Reject a non-string inventory workspace. Keep a saved pending guest path on retry, and do not guess a path for mountless sandboxes.

## Status and daemon controls

Each catch-up checks the saved machine's catalog identity and sandbox association locally.
An associated machine with completed workspace setup and no pending herdr plugin work does not need a fresh SSH status command.
Automatic guest connections require Management ON and a running enabled sandbox.
A fresh connection is opened only for actual pending guest work, first setup, explicit eligible Re-sync, or a bounded audit.
Do not create entries or open automatic guest connections for stopped sandboxes; retain existing entries and wait until the sandbox runs before guest setup.
A failed new connection reports the cause and affected action without declaring registration failed or advising duplicate registration.
Preserve the existing machine, label, workspace completion, and prior successful observation.
Prior success is historical, not proof of current reachability.
Cache a successfully probed running guest herdr server version per sandbox identity and observation time. Reuse probes during guest work or the bounded audit, never on TUI rendering or routine polls. Keep the cached version after a stop or failed probe; call it last known in Hint when it is no longer current. The observation time remains in status data. A probe failure alone does not block the sandbox.
Classify a confirmed guest herdr machine-API forwarding incompatibility as H0003, with guidance to use Fix herdr when safe or update the protected installation manually before Re-sync. Do not infer a running-server version mismatch solely from this error: the executable may be old, missing, or unable to run. Use H0004 for a failed explicit Fix herdr attempt while preserving the last confirmed guest version. Keep other workspace failures as R0006. Parse Herdr's structured error code and message when present, and extract a plain cause from its known textual transport wrapper without showing Rust debug syntax. Do not automatically update or restart herdr from parsed wording.
When sbx refuses the new SSH connection because the sandbox's saved host workspace path is missing, the herdr plugin diagnostic says that herdr-sbx was trying to inspect or sync the herdr plugin and that sbx blocked the connection before guest work began. Show the missing path once, without nested error prefixes or sbx's sandbox-removal suggestion. Tell the user to check or restore that path before Re-sync; keep the existing herdr machine.

Every view groups Management ON/OFF at the bottom right immediately before the sbx observation indicator, preserving their clickable targets.
Management is the user-facing name for the global permission to perform automatic work.
Keep daemon health distinct from that choice in the Status tab.
The interface has no application header.
Explicit state text remains visible when colors are disabled.
An active daemon does not imply that every sandbox is ready.

On first use, opening the TUI starts management automatically.
Closing the TUI leaves management running.
The Status tab has an outlined Management subsection containing `ON | OFF` and the observed worker state.
Show the worker state as a badge in the top-right border, matching the placement of the herdr version in sandbox details.
Place additional details, such as update age or activity, in muted text at the bottom-right corner.
Focus this subsection by default, using the brighter outline of focused sandbox details.
Space or Enter toggles Management while it has focus, including reversing a requested change that is still settling.
Up/Down moves between Management and the available setup or Refresh actions.
The Status Hint shows current Management activity or its observed state, never guidance for the selected sandbox.
Clear successful start/stop request notices when the request finishes and use current worker observations afterward.
Keep action errors and unconfirmed cancellation visible until resolved.
Follow Management with prerequisites under `PREREQUISITE`, `RESULT`, and `STATUS` headings.
Use colored foreground text without a filled background for results, and capitalize status words such as Running, Available, and Configured.
Combine sbx version and command support in one row, and daemon health and inventory access in a Sandbox service row.
Keep herdr, Docker sign-in, OpenSSH, and Sandbox SSH separate because their recovery actions differ.
Grouped rows preserve independent failure causes and recovery instructions without repeating checks skipped because an earlier prerequisite failed.
Do not show Sandbox setup in global Status; setup belongs to each sandbox's status.
Load the saved choice when the TUI opens; status polls must never change it.
The choice changes immediately when toggled, while observed operation state follows worker reports.
Turning OFF immediately prevents new automatic operations and requests cancellation of work already in progress.
Stop that work where safe, preserve completed effects, and show exactly `Cancelling` in gray while work settles.
Do not substitute Stopping, Pending, or an ordinary colored spinner for the cancellation state.
Do not start follow-up cleanup or retry work after OFF is requested.
Finishing a step that cannot safely be interrupted is part of cancellation and must finish before reporting settled OFF.
If cancellation or worker shutdown fails, retain the requested OFF choice, show the actual unsettled state and error, and offer a retry.
An unconfirmed stop retains the existing D0001 timeout diagnostic; never report successful quiescence prematurely.
Turning ON during cancellation waits for that work to settle, then reads the current baseline before resuming.
Do not run two management workers concurrently.

The bottom-right Management indicator stays compact and has no timestamp.
Show gray Cancelling there while a requested OFF transition remains unsettled.
Keep specific startup, reconnecting, and error details in Status rather than confusing them with the saved Management choice.
Show `last at HH:MM` in Status only while the worker is running and its last successful sync is more than 30 seconds old.
Show `*` during an observation refresh even when the time is hidden.
Hide Refresh only while Management is ON, the worker is Running, the last successful sync is within 30 seconds, and no active errors exist.
Show it when that observation is old or unknown, Management is OFF, the worker is not Running, or an active error exists.
Place it after the Status details and prerequisite checks, separated by a blank line.
Refresh must not open sandbox SSH, perform repairs, or start cleanup.
Offer Set up SSH whenever the SSH prerequisite is not ready.

An explicit OFF choice persists and prevents herdr startup hooks from restarting automatic work.
If management is stopped on a later TUI opening, ask whether to start it.
Declining leaves the TUI usable for inspection and explicit actions.
Starting clears the saved Stop choice and captures current external state before reconciliation.
Error codes use a component letter and four digits, such as `D0001` for a management stop timeout.
`P0005` identifies a guest plugin build blocked by a confirmed sandbox network policy event.

The section’s top border contains clickable Sandboxes, Status, and Plugins tabs.
Their order rotates so the active tab is always leftmost.
The tabs have no background or brackets; the active tab uses the blue accent and inactive tabs are muted.
Tab/Shift+Tab cycle the tabs.
Left selects New and Right selects All while the diagnostics header has focus.
Inputs and dialogs keep local key handling.
Up/Down navigate controls within the active tab.
The Management indicator and the sbx indicator open the Status tab.
Show prerequisites with a checkmark for success and a cross for failure, alongside text.
Use a distinct symbol and label for pending or unknown results.
Never show an inconclusive observation as a failure or success.
Include binary availability, daemon access, inventory, Docker authorization, SSH configuration, Management state, and last successful synchronization.
Expose relevant repairs and retries from this view or the affected sandbox.

Inspection remains observational.
Repairs run only through an explicit action.
Do not offer temporary guest download access in the panel while its approval flow is reworked.
Report pending policy cleanup from prior attempts, but do not automatically recover it while Management is OFF.
Slow diagnostics and repairs must not freeze navigation or rendering.
Interactive SSH setup temporarily hands the terminal to its external setup process after explicit activation.
When that process finishes or is canceled, restore the TUI and its prior focus.

## Status command

`herdr-sbx status` prints a compact, readable summary with color in supported terminals.
Include Management and daemon health, saved sandbox enabled choices, runtime states, labels, status, and outstanding errors.
Use text and symbols so color is never the only indication of state.
Honor `NO_COLOR` and disable terminal styling when output is redirected.

`herdr-sbx status --json` prints a richer machine-readable snapshot without terminal styling.
Include a schema version, observation timestamps, Management and daemon details, sandbox identities, saved enabled choices, observed native enabled state, entry associations, runtime states, herdr labels, herdr plugin selections, and separate coded errors and warnings.
Represent missing observations explicitly rather than inventing healthy or stopped states.
The command does not start the daemon, repair setup, or change management choices.
Avoid slow active diagnostics in this command; expose observation freshness instead.
`herdr-sbx daemon status` prints only the management daemon's health and saved On/Off choice.
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
Render hotkeys bold, descriptions in regular muted text, and separators faintly; keep plain output when color is disabled.
Both help views show only actions available for the current focus and selected target.
Show `c refresh` only when Refresh is available and no refresh is running.
Show `d dismiss` only for a selected, saved, undismissed diagnostic, and show diagnostic navigation only when diagnostics exist.
Pressing `?` expands help in place while preserving the tab, focus, selection, and underlying scroll position.
Pressing `?` or Escape closes expanded help; scroll keys scroll help without moving the underlying selection.
Other advertised action shortcuts close help and execute in the preserved context.
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
Preserve access to navigation, Management state, and errors in small terminals.
The visual design defines column priorities and the minimum useful terminal size.

## Errors and concurrent operations

When a background refresh fails, status and warnings show `Could not refresh sandbox state: <actual reason>`.
Include the underlying failure instead of a generic catch-up message.

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
When visible, Down from the last sandbox in list focus enters the Events header, then the diagnostics header, then its visible entries.
Up reverses this path; if the filtered table is empty, Down still reaches these sections.
Enter or Space on the diagnostics header switches New/All; Left selects New and Right selects All there. Tab/Shift+Tab still cycle the top tabs. `d` dismisses the selected group without a Dismiss button. Pressing `e` jumps to the first visible group, or its header if New is empty. Keyboard and mouse controls can reach both sections and the switch.
Render each group with its colored code alone in the header, readable sandbox name when known, latest muted occurrence time, aggregate muted `×N` when more than one occurrence, and separate child causes and recovery guidance below the current tab content; do not open a separate page. The code color conveys warning or error severity without a repeated word label.
Give every distinct underlying diagnostic a persisted, unique six-character lowercase alphanumeric reference (`[a-z0-9]{6}`). Show it muted on its detail line immediately before the first line of error text; keep the colored component code in the group header so its position stays steady. Keep a child's reference across identical repeats, polling, dismissal, and TUI restarts; a changed cause or a new failure after recovery gets a new reference. Retain it with related history events.
Keep herdr notifications for a failed sandbox short: sandbox name, one reference that points to an active diagnostic, and a one-line plain-language cause, capped at 120 displayed columns. Shorten a long sandbox name to 32 columns and truncate the cause if necessary, never the reference. Do not paste full plugin output, repair commands, or combined diagnostics into a notification. When several diagnostics are active for the same sandbox, select one primary entry deterministically, mention that others remain in the TUI, and send one notification for the sandbox outcome. If no matching diagnostic was saved, do not claim a reference exists.
Place the group's latest occurrence time after the sandbox name, matching event rows. Mute event and diagnostic dates and times. Show a compact muted `×N` after the time only when the group's aggregate occurrence count exceeds one.
Use amber for ambiguous-entry and unconfirmed-deletion warnings; other current and unknown error codes remain red.
An entry association warning opens the affected sandbox with its matching problem explained.
Enter on a selected diagnostic opens its recovery action.
The first mouse click selects a diagnostic group and leaves the user in the diagnostics section.
Only a second click on that same selected group opens its recovery action; clicking a different group selects it first.
An action failure appears in that section even if a daemon-status poll arrives before its durable record is written.
Show persistent recent events and transient hints in their own outlined sections. Successful temporary-access cleanup is an event, not a Hint; a cleanup failure remains an active diagnostic.
Show ambiguous-entry guidance in Hint while its sandbox is selected, instead of in the details body.
Hint follows the selected sandbox preview and focused control.
Explain Enable, Disable, Remove from herdr, and Rename by their effects on the selected sandbox.
Do not require users to understand registration provenance.
Use “plugin” for herdr-sbx in ordinary hints; elsewhere, write “herdr-sbx”.
Write “herdr” in lowercase and use “herdr plugins” when referring to plugins synchronized into sandboxes.
Wrap hint text at spaces so words stay whole; let long hints use multiple lines inside the outline.
For a registered sandbox with completed setup, Hint says: "This sandbox is registered in herdr. Setup is complete." Do not imply that a fresh connection was checked when status came from saved registration and sync state.
Show the selected sandbox's saved status explanation in Hint instead of its expanded Details body, including failed and stopped states. Keep the specific ready and ambiguous-entry wording; color other explanations by the observed state so the cause stays visible when details are closed. An empty explanation adds no Hint.
For a stopped enabled sandbox with an entry and a confirmed native Enabled state, Hint says: "This sandbox is stopped. Its herdr entry is saved. Setup waits until it starts and Management is ON."
If a native change fails, show the actual settled state and report the operation error separately.
If the catalog cannot be verified, explain that the displayed state is last confirmed and automatic work waits for a fresh observation.
A recovered deletion check must clear its former explanation.
Replace a hint when a newer action starts, and clear it when navigation leaves the action's context.
Retain a bounded, durable herdr plugin event history, including failures and recoveries, so a notification remains inspectable after the active error clears.
Known errors have stable codes that also appear in JSON output.
`D0006` identifies a failed diagnostic-dismissal save; the entry stays New when saving fails, and a successful retry clears this failure.
R0004 is an informational event. R0005 and S0002 are nonblocking warnings; S0002 means deletion of a previously registered sandbox could not be verified, so its herdr machine was kept. All other current codes represent blocked work or unreliable observations.
A stopped sandbox without an entry is a neutral Disabled condition and does not create an active diagnostic.
The versioned status JSON separates warnings from errors; warning-only state reports zero errors.
Warnings alone do not put Management into an Error state or offer Refresh.
Each error includes its scope, a readable explanation, and an available recovery action.
History entries include their code, scope, detail, time, and recovery state.
Show the sandbox name in event entries when it is known, instead of the internal scope and sandbox ID.
Color event codes according to their severity, using amber for warnings.
Preserve useful diagnostic details for unknown errors without exposing credentials.
Repeated identical errors must not flood the interface.
Dismissal does not mark the underlying problem as resolved. All includes dismissed entries until the problem is actually cleared.

Show pending actions and their final outcome.
Prevent conflicting actions on the same sandbox while a mutation is in progress.
Coordinate saved choices with the background daemon so Disable, removal, Management OFF, and resuming Management cannot race with automatic registration or overwrite the new baseline.
If another reconciliation holds the registration lock, explain that sync is updating registrations and that the requested action did not finish. Do not imply it succeeded; retain a retry path. Explicit entry mutations wait for an ordinary short overlap while respecting cancellation and keeping navigation responsive.
Keep the interface usable when one sandbox fails or an external command is slow.

## Acceptance scenarios

1. First use starts at most one daemon; later opening with a stopped daemon asks whether to start it.
2. Closing the TUI leaves the daemon running.
3. Fresh discovery defaults to Enabled, except stopped sandboxes without entries remain Disabled; matching manually created entries are included automatically.
4. Management OFF, an external rename or native enable/disable change, and Management ON preserve the current catalog state instead of replaying conflicting saved intentions.
5. Disable retains the entry and label; subsequent Remove from herdr removes only the entry and preserves Disabled across refresh, daemon restart, and Management OFF/ON.
6. Inline rename survives reconciliation; Escape before submission leaves the previous label unchanged, while cancellation after submission preserves the actual catalog result.
7. Up/Down immediately previews the newly selected sandbox and collapses the previous card; Enter or Right focuses its primary action, and Escape returns to list navigation without closing the preview.
8. A changed herdr plugin default affects inheriting sandboxes while explicit overrides retain their values.
9. Fuzzy filtering, keyboard controls, and mouse controls work in both tables.
10. Failed or slow diagnostics leave the interface responsive and show unknown or pending status accurately.
11. Known errors appear with the same stable code in the TUI and JSON status.
12. Status output does not start the daemon or mutate setup; redirected and JSON output contain no terminal styling.
13. Management OFF stops scheduling work immediately, cancels active work where safe, shows gray Cancelling until settled, and then performs no automatic work or cleanup.
14. Active errors and warnings remain in the outlined diagnostics section; arrow keys move from the sandbox table into its entries and back, while `e` jumps directly there.
15. Enable, Disable, Rename, and Remove from herdr hints explain their effects, wrap at whole words, and remain usable with Management OFF.
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
28. New/All counts appear in parentheses and sum occurrences. The selected choice is blue and the other muted; moving focus never changes the outline color. Dismiss is available through `d` without a button. When the diagnostics header has focus, Left selects New and Right selects All while Tab/Shift+Tab still cycle the top tabs. Errors, Warnings, and Events titles use regular text color only when their header has focus and border color otherwise. The selected sandbox card has a brighter outline only while its controls have focus.
29. Two plugins with the same code in one sandbox share one navigable group but retain separate references, details, and repair guidance; its aggregate `×N` and All count equal their summed occurrences.

30. A missing entry and a disabled entry both use the muted dot; only the herdr label indicates entry presence, and the enabled symbol uses the existing blue accent.
31. Discovery and Enable for a stopped sandbox without an entry stay Disabled with neutral Stopped status, no warning, no pending Enable, no machine add, and no SSH or sandbox start.
32. Enable after explicit removal restores an entry for a running sandbox; a stopped sandbox remains Disabled without a warning.
33. Management OFF permits read-only observation and explicit Rename, Enable, Disable, and eligible Remove actions without starting background work.
34. A sandbox deleted while Management is OFF retains its entry until Management resumes and deletion is confirmed.
35. Unknown inventory, ambiguous identity, label collisions, or name reuse never authorize entry removal; an external label change does not break identity association.
36. A plugin-sync failure or cancellation after entry creation retains the actual entry and label, and distinguishes completed setup from pending or failed work.
37. Enabling a manually added, disabled entry for a stopped sandbox enables it in herdr with Management ON or OFF; subsequent stopped observations do not disable it again.
38. Isolated lifecycle coverage verifies that automatic work does not start, connect to, or natively enable a stopped sandbox; explicit native Enable is allowed, and baseline capture accepts verified native Disabled.
39. UI, help, status output, and JSON consistently use Management, Enabled, entry presence, and Status.
40. Eligible Enable and Disable update the displayed choice immediately, retain Enabling… or Disabling… through stale polls, and settle to the actual native outcome; rejected changes revert, lost responses follow confirmed effects, and unavailable observations retain a stale last-confirmed display without automatic replay.
41. Management appears at the bottom right immediately before sbx at wide and narrow widths, with correct click targets and gray Cancelling.
42. A fresh unambiguous observation clears resolved association warnings even while Management is OFF, while true ambiguity retains a specific explanation.
43. Right enters details from the list; Left selects the preceding action or returns to the list from the leftmost action or plugin controls; Tab/Shift+Tab changes tabs.
44. Inside a card, Up/Down remains among its actions and plugin controls, with a brighter outline; Space invokes the primary Enable/Disable action and Enter invokes the focused button.
45. The selected card replaces its table row, preserves the colored title with inline runtime and status, shows the version at top right, and places spaced action buttons before muted sandbox → herdr IDs and plugin controls.
46. The primary button includes `(space)`, and contextual help names `enable` or `disable` according to the current selected sandbox.

Use isolated herdr sessions and sandbox fixtures for implementation tests.
Include input and state-transition tests for the TUI, plus daemon integration tests for Management, enabled choices, baseline capture, and cancellation.
Check rendering, resizing, mouse behavior, and terminal restoration during visual validation.

## Visual design

The visual design covers layout, visual hierarchy, exact shortcuts, mouse behavior, expanded rows, inline rename, filtering, and error presentation.
The [Interactive sandbox manager design](tui-design.md) describes the current layout and visual treatment.

# Interactive sandbox manager design

This document translates the agreed [behavior specification](tui-spec.md) into a terminal interface.
The user opens the manager to understand sandbox readiness, change management, and control plugin synchronization.
The primary path is select a sandbox, expand its row, make a change, and see the result beside that sandbox.

The current panel clears the screen, performs inspection, and waits for a letter followed by Enter.
Its selection and recovery flows replace the table with numbered prompts.
The new interface keeps the table available while observations and ordinary operations run.
The existing operation services remain responsible for sandbox and Herdr behavior.

## Visual structure

Use a full-width table and a stable footer without an application header.
Use rounded outlines to separate sections and expanded content.
Avoid enclosing every row in a box.
Use a dark surface, normal text for content, muted text for metadata, and blue accents for focus.
Selected rows and focused buttons use a filled blue background.
Use green for success, amber for pending or attention, and red for failure.
Every color has a corresponding symbol and word.

The `>` marker identifies keyboard selection even without color.
A subtle background highlights the selected sandbox row only while the sandbox table has focus. Moving focus into details, Events, or diagnostics leaves its selection intact but removes the row highlight.
Filled buttons identify controls; section tabs use text color without a background or brackets.
Use `✓ Ready`, `✗ Failed`, `… Pending`, and `? Unknown` consistently.
Do not color an unwatched, disabled, or stopped state red merely because it is inactive.
Honor `NO_COLOR` without removing selection markers, symbols, or labels.

At 110 columns and 32 rows, reserve two filter lines and two table heading lines.
At 60–71 columns, omit the table Readiness column.
At 30–59 columns, also shorten management to W, UM, UW, D, or UT and show runtime as one symbol.
At any wider width, first shorten management to a two-cell code, then shorten the sandbox name only if it still does not fit.
Use MG, SANDBOX, S, and HERDR as compact headings.
Keep the full meanings in Details, Hint, and full Help.
At 30–44 columns, compact the tabs, filter, footer, and plugin rows so the whole frame fits.
At 30 columns by 10 rows or larger, preserve keyboard and mouse access to the selected control.
Long Hints and confirmations move into the scrollable viewport on small screens; Page Up and Page Down scroll without changing selection.
When the diagnostics header cannot hold New and All, put that selector on a separate actionable row.
Show the complete herdr plugin identifier in Hint for the focused compact row.
Below that size, show a short resize instruction with Close still available.
The table receives eighteen lines, including expanded content.
The section’s top border contains Sandboxes, Status, and Plugins tabs with clickable left/right arrows.
Their order rotates so the active tab is always leftmost.
The active tab matches the arrow color, and inactive tabs are muted.
Left/Right and Tab/Shift+Tab cycle the tabs, except that Left selects New and Right selects All while the diagnostics header has focus. Tab/Shift+Tab still cycle the top tabs there.
Below the current tab, show an outlined section for active errors and warnings, an Events section, and a brief Hint section only when they have content.
The active section title reflects its contents: Errors, Warnings, or Errors and warnings.
The `e` key focuses that section on the current tab, where Enter opens recovery for the selected entry.
One click selects a diagnostic group; a second click on that same selected group opens its recovery action.
Clicking a different group changes selection without opening recovery.
Keep contextual help and compact status indicators pinned at the bottom.
Short tables leave space inside the table viewport, so controls do not move during refresh.

```text
 Last observation 14:32 · observed 2s ago
 / Filter sandboxes…                                                     6 sandboxes

   MANAGEMENT  SANDBOX           RUNTIME    HERDR LABEL              READINESS
 ─────────────────────────────────────────────────────────────────────────────────────
 > ▸ Watched   api               Running    API development          ✓ Ready
   ▸ Watched   frontend          Running    —                        … Preparing SSH
   ▸ Unwatched docs              Stopped    Documentation            — Management paused
   ▸ Unmanaged release           Running    Release desk             ! Adoption required
   ▸ Disabled  experiment        Stopped    —                        — Disabled
   ▸ Untracked new-project       Running    —                        … Awaiting discovery

                  [remaining table viewport]

 [s Status details · 1 issue]   [p Plugin defaults]

 ─ Errors · 1 active ──────────────────────────────────────────────────────────────────
 > H0001 · Host · SSH configuration is missing.
   Open Status details to set up SSH.
 ↑↓ select  Enter expand  w watch/unwatch  r rename  / filter
 Tab focus  b status  c refresh  ? help  q close
                           Auto-sync ON  sbx ●
```

Error codes in sketches are proposed identifiers, subject to one shared error catalog during implementation.
`Untracked` means that no management choice exists yet.
After automatic discovery saves Watch, the row reads `Watched` even while preparation remains pending.
If the worker is stopped, an untracked row reads `Awaiting discovery · worker stopped`.
Never present preparation as a reason to downgrade Watched to Untracked.

The management column summarizes the saved choice and ownership exception.
The runtime column contains the sandbox observation.
The Herdr label column contains the observed registration label, including unmanaged labels.
The readiness column describes progress, connection readiness, or a blocking condition.
Expanded details explicitly show registration ownership as `Owned`, `Unmanaged`, `None`, or `Unknown`.
A missing observation displays `? Unknown`, with its timestamp if a previous observation exists.

## Selection, expansion, and actions

Up and Down move between sandbox rows while the table has focus.
Enter toggles the selected row's expansion and keeps focus on its header row.
Down moves into the expanded controls.
The details border shows Details · sandbox ID · ownership and the runtime badge at the top right. Add relative observation time immediately before the badge only when it is more than two minutes old.
Place `herdr <version>` at the right end of the Details section's bottom border, in border color, when a guest server version is known.
Append `↓` when the guest server is older than the host CLI or `↑` when it is newer.
Omit the arrow for equal, unknown, or unorderable versions.
At narrow widths, keep the border and content readable.
The first line shows sandbox name → Herdr label (readiness).
The expanded table row leaves its readiness cell empty.
Leave one blank line below the name and Herdr label summary, then indent descriptive text until the outlined Plugins section.
The plugin table has its own outline, with aligned columns and selection markers.
After choosing a plugin value, show the new Choice at once and an animated loader in Effective until saving finishes.
Expanding another sandbox closes the previous expansion.
Moving selection alone keeps the expanded details open.
Escape or toggling the expanded sandbox closes its details.
Clicking a sandbox row selects it and toggles its details; embedded controls perform their own actions.
The selected sandbox header stays visible when its details scroll.

```text
 > ▾ Watched   api               Running    API development
   │ Details · abc123 · Owned                                     2m ago  Running
   │ ✓ SSH ready   ✓ Herdr ready   ✓ Registration ready
   │
   │ PLUGIN                      SYNC CHOICE       EFFECTIVE   RESULT
   │ <plugin-id>                 [Inherit ▾]       On          ✓ Synced
   │ example.tools               [Off ▾]           Off         — Existing files kept
   │
   │ [Unwatch]  [Disable · remove from herdr]  [Fix herdr]
   │ [Rename Herdr label…]
   ▸ Watched   frontend          Running    —                        … Preparing SSH
```

Show only actions that apply to the sandbox's current state.
Unmanaged rows offer Adopt instead of Watch or Unwatch.
Use the primary action color for Adopt, matching Watched.
Show Re-sync when a watched running sandbox needs preparation or plugin recovery.
Watch on a stopped sandbox reports `Watching saved · waiting for sandbox to run`.
Unwatch reports `Unwatched · Herdr entry kept`.
Disable states its effect in the button and reports removal progress before reporting success.
It never deletes the sandbox.
An unwatched owned registration has the same Disable action.

The `w` shortcut applies only when the sandbox table has focus.
For Watched it requests Unwatch, and for Unwatched, Disabled, or Untracked it requests Watch.
For Unmanaged it opens the adoption explanation.
While a conflicting operation runs, the shortcut displays the operation's progress instead of issuing another request.
After any saved Unwatch or Disable, discovery preserves that decision.

Do not report Disabled before Herdr removal succeeds.
During removal show `Unwatched` with `… Removing Herdr entry`.
After failure show `Unwatched` with `✗ Removal failed` and a Retry removal action.
Management remains stopped while removal is pending or failed.

Left/Right and Tab/Shift+Tab cycle the top tabs, wrapping at either end, except that Left selects New and Right selects All on the diagnostics header. Tab/Shift+Tab still cycle the top tabs there.
Up/Down traverse content and controls within the current tab.
Dialogs and text inputs retain local arrow and Tab handling.
Within an action group, Up and Down select a button, and Enter activates it.
Up and Down cross between the plugin table and action group, returning to sandbox selection only at the details boundary.
Within the plugin table, Up and Down select a plugin.
Enter, Space, or a click cycles Inherit → On → Off and saves the choice.
The `i` key clears the override and restores Inherit.
Plugin choices change directly without opening a selector dialog.
The selector shows the current default beside Inherit.

Changing a plugin choice reports `Saving…`, followed by its saved effective value and synchronization progress.
A failure to save retains the previous choice.
A failure to synchronize retains the saved choice and shows a retryable error.
Running watched sandboxes receive changes immediately.
Other rows show `Waiting for sandbox to run`, `Waiting for Watch`, or `Waiting for worker`, as applicable.
Off preserves existing guest files and shortcuts.

## Inline rename

The `r` shortcut begins editing the selected owned registration's Herdr label.
An explicit Rename label button in expanded details provides the mouse equivalent.
The input replaces the label cell and uses the existing label as its initial value.
The sandbox name remains visible and unchanged.

```text
 > ▸ Watched   api               Running    [API staging           ]  ✓ Ready
                                           Enter save · Esc cancel
```

Enter saves and Escape restores the original value.
During saving, keep the input visible with `Saving…` and prevent duplicate submissions.
A failed save retains the proposed text and cursor position.
Show a short validation message under the input and the coded failure in the Errors section.
Refresh must not replace text being edited.
Do not let a click elsewhere silently commit or discard an unfinished rename.
Keep input focus and display `Save with Enter or cancel with Esc`.

For an unmanaged registration, Rename opens the adoption explanation.
After successful adoption, it starts the label editor with the preserved label.
For a row without a registration, show `Rename unavailable · no Herdr entry`.

## Plugin defaults

The Plugins tab shows plugin identity, default auto-sync, and availability.
Unsupported plugins show their reason below the row.
The explanation states that defaults apply to sandboxes using Inherit.
Show only the sentence for the selected plugin's current default: On says the matching version will be copied; Off says the plugin will not be synchronized and existing copies stay intact.

```text
 Defaults apply to Inherit. Sandbox overrides keep their values.
 / Filter plugins…                                                       2 plugins

   PLUGIN                      AUTO-SYNC     AVAILABILITY
 ─────────────────────────────────────────────────────────────────────────────────────
 > <plugin-id>                 On            ✓ Installed locally
   example.tools              Off           ! Unsupported
   The plugin requires a build; no Linux sync recipe is available.

 Matching plugin version will be copied to watched sandboxes.

 ↑↓ select  Space toggle auto-sync  Enter details  / filter
 Esc back  Tab focus  b status  e diagnostics  ? help  q close
                           Auto-sync ON  sbx ●
```

Enter, Space, or a click toggles the selected default.
An unsupported plugin cannot be switched from Off to On.
An existing On choice can be switched Off, and its reason remains visible.
New plugins always enter this table with Off.

## Status, startup, and worker controls

The bottom-right footer shows plugin background activity as Auto-sync ON/OFF/ERROR and the sandbox daemon as sbx.
Auto-sync uses a colored state word, and sbx uses a colored dot with accessible state text.
During start, stop, pending, or reconnecting transitions, put `*` directly after the observed Auto-sync label.
Preserve that label during the transition, such as `ON*` while stopping and `OFF*` while starting.
Omit the footer star in settled states and during ordinary observation refreshes.
Green means running or ready, amber means pending or unknown, gray means stopped, and red means failed.
The Status tab shows `ON | OFF` with one selected choice, followed by a colored observed Running, Pending, Stopped, or Error state.
When Stopped is visible, omit the redundant `Daemon is not running.` detail.
Keep meaningful daemon details in other states.
Load the saved choice when the TUI opens; later status polls must not change it.
The choice changes immediately when toggled, while the observed state follows daemon reports.
The same observed state appears in the bottom-right footer and updates after each worker status poll.
Pressing `b` opens the same control.
Opening it never starts or stops the worker.
After the state, show `last at HH:MM` only if Auto-sync is running and the last successful sync is more than 30 seconds old.
In Status, show `*` during a refresh even if the time is hidden.
The footer shows no time.
Hide Refresh observations only when Auto-sync is selected ON, the daemon reports Running, and no active errors exist.
The last successful sync must also be known and no more than 30 seconds old.
Otherwise, show Refresh observations, including when the last successful sync is unknown or stale.
Place Refresh observations at the bottom of the Status content, after daemon details and prerequisite checks.
Leave one blank line above the action.
Do not repeat the last successful sync on another line.
Do not show a separate pending-observation sentence.

On first use, render Auto-sync OFF while requesting one background worker.
When a worker is stopped on a later opening, show the following prompt once for that opening.
Declining restores table focus and leaves inspection and configuration usable.

```text
 Auto-sync OFF

 Start the background worker?
 The worker watches sandboxes and synchronizes selected plugins.

 [Keep stopped]    [Start worker]
```

Focus Keep stopped initially.
Tab or Left/Right changes the button, Enter activates, and Escape means Keep stopped.
Turning Auto-sync On clears the saved Stop choice and reports progress.
Turning it Off shows `Running → Stopping` until the daemon exits.
If it remains active for more than one minute, show `D0001` with a plain-language explanation.
Turning On during shutdown waits for the previous daemon to exit before starting another one.
A failed stop request keeps OFF selected in red, shows `ERROR` beside the observed Running state, and displays the cause in Errors.
Herdr startup must respect the saved Stop choice.

Status details separates observations from action controls.
Opening the page can request observations but never performs a repair.
Each prerequisite has its own result, detail, and observation time.
Offer Set up SSH when SSH configuration is not ready, and hide it when SSH is ready.

```text
 Auto-sync ON  Running  last at 14:32*

   PREREQUISITE                RESULT        DETAIL
   Herdr binary                ✓ Ready       Available
   Docker sandbox CLI          ✓ Ready       Available
   Sandbox daemon              ? Unknown     Diagnostics timed out · observed 35s ago
   Sandbox inventory           … Pending     Reading inventory
 > SSH configuration           ✗ Failed      Configuration is missing
   Docker authorization        ✓ Ready       Authorized

 Selected: SSH configuration
 [Set up SSH…]

 [Back to sandboxes]
 ─ Errors · 1 active ──────────────────────────────────────────────────────────────────
 > H0001 · Host · SSH configuration is missing.
   Set up SSH, then refresh observations.
                          Auto-sync ON  sbx ●
```

Slow diagnostics update independently and show elapsed time.
Unknown results never use a failure cross.
Selecting a prerequisite reveals its supported repair or an explanation of the required manual step.
Do not offer sign-in or sandbox daemon startup as automatic repairs.
Re-sync remains available in the affected sandbox's details.
Fix herdr is a separate action for a running, watched, owned sandbox with a known version difference or H0003.

SSH setup currently needs an interactive external terminal process.
Show a short explanation, then hand the terminal to that process only after explicit activation.
Restore the TUI, prior focus, and observations when setup finishes or is canceled.
This interactive handoff is the exception to in-TUI navigation during work.
Ordinary diagnostics and background repairs must keep navigation responsive.

Temporary download access is absent from Details and the keyboard shortcuts until its approval and policy flow is reworked.
Historical policy cleanup and its diagnostics remain visible.
Require the existing typed `allow` approval before activation.
Escape cancels the approval without changing policy.
If an installation is canceled after activation, show cleanup progress before reporting cancellation complete.
Pending or failed cleanup remains visible and blocks registration.

## Adoption

Adoption uses one explanation dialog reached through Adopt or Rename on an unmanaged row.
The dialog identifies both the sandbox and the existing Herdr registration.
It does not include a label input.

```text
 Adopt release?

 Sandbox       release
 Herdr label   Release desk
 Machine       machine-123 · sandbox SSH target

 The plugin will manage this Herdr entry.
 It can remove the entry when the sandbox is deleted.
 The label, connection, and workspaces stay as they are.

 ✓ Running sandbox   ✓ Exact SSH target   ✓ Default session

 [Cancel]    [Adopt and watch]
                           Auto-sync ON  sbx ●
```

Focus Cancel initially and return to the originating row after cancellation.
While eligibility is loading, show `… Checking eligibility` and disable approval.
If eligibility fails, show the exact reason and a Refresh eligibility action.
Ambiguous registrations show their identities but have no automatic choice or enabled adoption action.
A failed adoption keeps the dialog open with Retry and Cancel.
A successful adoption preserves the label and shows Watched with any preparation progress.
When reached through Rename, successful adoption then opens the inline label editor.

## Errors, warnings, and feedback

Show Events above the outlined section for active errors and warnings.
Keep Events collapsed when the panel opens, with a disclosure arrow and the number of recent events in its title.
Retain its open or closed state while navigating tabs and receiving observations.
Hide Events only when its history is empty.
Expanding Events shows the 50 most recent events from the past hour, newest first, without changing any sandbox. Expire old entries even if no new event arrives.
Successful temporary access cleanup adds `Temporary access cleanup checked` to Events instead of leaving a Hint.

The user needs to find a notified failure, read its repair steps, and dismiss repeated reports without losing access to recovery.
Group active diagnostics by sandbox and component code.
Use the readable sandbox name in each group header and the exact sandbox identity to keep different machines separate.
Host and daemon diagnostics use their named scope in place of a sandbox.
The active section uses Errors, Warnings, or Errors and warnings according to all active groups, including dismissed groups.
Each group starts with its colored code, without an Error or Warning prefix.
Keep the component code at the same header position when focus or observations change.
Place the muted six-character reference on the next line, immediately before the first error text.
R0005 uses the same amber as Unmanaged and Ownership conflict.
Keep current codes for failed operations red, including unknown failures.
The code, cause, and section title remain readable without color.
A warning alone does not change Auto-sync to ERROR or cause Refresh observations to appear.
Put the readable sandbox name after the code, then the last occurrence date and time.
Use the latest active member's timestamp and the sum of the active members' occurrence counts in the group header.
Put each plugin ID on its own line within the group, before that plugin's reference and cause.
Render the timestamp and `×N` repeat count in muted text.
Show the repeat count only when N is greater than one.
Remove the separate Last occurred line.
Keep group order and selection stable through repeated observations and count updates.
Sort groups by readable sandbox name, then code, with scope identity as the tie-breaker.
Member first-seen times and membership changes do not affect this order, so one plugin's recovery does not move the remaining group.
Do not rotate the section on a timer or reorder it when a group receives another occurrence.
Retain each cause and plugin as a separate member with its own reference, occurrence count, output, and repair guidance.
Group membership changes presentation and dismissal, without concatenating separate plugin failures into one error paragraph.

The section title contains a `New (N) | All (M)` switch, with New selected initially.
Show R0004 re-sync outcomes as informational Events, not active diagnostics; retain specific registration or herdr plugin failures under their own codes.
New shows active groups that the user has not dismissed.
All shows every active group, including dismissed groups.
N sums recorded occurrences in the visible groups, and M sums recorded occurrences in all active groups.
Treat an active member without a positive count as one occurrence.
Polling the same saved failure does not add an occurrence.
When New hides dismissed groups, show `H active occurrences hidden`, where H equals M minus N.
At narrow widths, place this count on the first line below the title.
Use the blue accent for the selected choice and muted text for the other choice.
Use the existing `>` marker for keyboard focus.
Keep every section border in the separator color regardless of focused control, selected filter, or diagnostic severity.
Title colors must end before the following border stroke.

Dismiss acknowledges the whole sandbox/code group without resolving its causes or changing sandbox state.
Persist dismissal across observations, repeated reports, cause changes, and panel restarts while that group remains active.
A newly failing plugin with the same sandbox and code joins the dismissed group and stays hidden in New.
Confirmed recovery removes that member from the active group and its occurrence sum, while its event history remains.
Partial recovery keeps the remaining group dismissed.
Only recovery of every member ends the group's dismissal.
If the same sandbox/code fails after the whole group recovers, it starts a new undismissed group.
In All, mark dismissed groups with muted `Dismissed` text.
Dismiss is available only through the `d` hotkey, shown in contextual help when an undismissed group is selected.
Do not render a Dismiss button.
If New becomes empty while dismissed groups remain active, retain the section and its switch.
Show `No new diagnostics. Switch to All to see dismissed active entries.` with a blank line above and below and a small left indent inside the outline.
Hide the active section only when no active groups remain.

```text
 ╭─ ▸ Events · 4 recent ────────────────────────────────────╮
 ╰──────────────────────────────────────────────────────────╯

 ╭─ Errors and warnings · New (4) | All (6) ────────────────╮
 │ 2 active occurrences hidden                              │
 > R0002 · api · 2026-10-16 14:34:12 ×3
   k7m2q9 Could not remove the herdr entry.
   Management is stopped. The entry is still present.
   [Retry removal]

   R0005 · release · 2026-10-16 14:33:12
   b8v3n1 This sandbox is already a herdr machine.
   It was added outside the plugin.
 ╰──────────────────────────────────────────────────────────╯
 d dismiss                         Auto-sync ON  sbx ●
```

### Guest plugin repair

Show each failed herdr plugin as a separate detail block within its sandbox/code group.
Keep its plugin ID, unique reference, concise cause, repair callout, and build output together.
Keep `Manual intervention required` visible until a confirming inspection finds that plugin available at the required revision.
Do not concatenate failures, build output, or repair commands from different plugins into one paragraph.

```text
 ╭─ Errors · New (5) | All (5) ─────────────────────────────╮
 > P0002 · api · 2026-10-16 12:10:21 ×5
   <first-plugin-id>
   f4r8s2 Manual intervention required. Guest build failed.
   │  <repair guidance for the first plugin>

   <second-plugin-id>
   a2p9n6 Manual intervention required. Guest build failed.
   │  <repair guidance for the second plugin>
 ╰──────────────────────────────────────────────────────────╯
 d dismiss                         Auto-sync ON  sbx ●
```

Place the repair recommendation before the raw build output so the next step stays easy to find.
Use a blue vertical line on its left, two spaces of inner padding, and a blank line above and below the command.
Use the existing blue action accent for the line and instruction heading, with the command in normal high-contrast text.
The callout must state that repair happens inside the named sandbox and that Re-sync checks the result afterward.
The diagnostic code retains its severity color.
Label the following muted diagnostic text `Build output` and preserve its useful line boundaries.

```text
 > P0002 · api · 2026-10-16 12:10:21 ×3
   <plugin-id>
   f4r8s2 Manual intervention required
   Guest build step 1 failed.

   │  Run inside api:
   │
   │  herdr plugin install '<verified-repository>' \
   │    --ref '<verified-commit>' \
   │    --yes
   │
   │  Then choose Re-sync to check the installation.

   Build output
   <output from this plugin's failed build>

```

The placeholders in this sketch represent the complete verified repository and commit values shown at runtime.
Show a pinned manual installation command only when the guest plugin was absent before the attempt, the failure is positively known to have occurred before linking, and the host installation provides enough verified source information to construct it.
If linking may have occurred, or the failed stage is uncertain, omit the installation command.
Instead, keep the blue callout and direct the user to repair the retained source inside the named sandbox, showing its full path, then choose Re-sync to check the installation.
When verified source information is unavailable, show the known plugin identity and revision without inventing an installation command.
Never abbreviate a command, source revision, or argument with an ellipsis.
At 60 columns, keep the callout within the section and wrap explanatory text at spaces.
Format a long command across argument boundaries with shell continuation backslashes, preserving every argument and quote.
Do not insert a continuation inside a quoted argument or commit hash.
Long individual tokens may use the existing visual wrapping, but their full contents must remain available through scrolling and drag selection.

Retain ordinary drag selection for commands and logs, with the existing visible-text copy behavior.
Do not add a copy button, selection mode, or shortcut that is required to select this text.
Displaying or selecting a command never executes it.
Enter on a group opens recovery for its sandbox; the user explicitly activates Re-sync after any manual repair.
During Re-sync, retain each unresolved member while showing pending feedback without moving focus.
Clear only the plugin failures confirmed resolved, and keep any remaining failures and their recommendations visible.
Record each plugin's recovery separately in Events.

### Short notifications and diagnostic references

A user receiving a herdr notification needs to identify the sandbox and find its full diagnostic in the panel.
Keep the notification body to one line: sandbox name, diagnostic reference, and a short cause.
Use `herdr-sbx` as the notification title.
Do not put build output, repair commands, source paths, or a concatenation of plugin failures in the notification.

```text
 herdr-sbx
 api · f4r8s2 · herdr plugin build failed

 ╭─ Errors · New (2) | All (2) ──────────────────────────────╮
 > P0002 · api · 2026-10-16 12:10:21 ×2
   <plugin-id>
   f4r8s2 Manual intervention required
   Guest build step 1 failed.
   │  Run inside api:
   │
   │  <complete repair command>
   │
   │  Then choose Re-sync to check the installation.
 ╰──────────────────────────────────────────────────────────╯
```

Each diagnostic has a durable, unique reference matching `[a-z0-9]{6}`.
Render it muted at the beginning of its member's diagnostic text line, with one space before the error text.
For a plugin member, its plugin ID appears on the preceding line beneath the group header.
Show the concise cause before any repair callout, and keep the repair callout before raw build output.
The reference identifies this occurrence; the component code identifies its kind.
Keep the reference unchanged through repeated observations, increased repeat counts, dismissal, panel reopening, and the related recovery event.
A changed cause or recurrence after confirmed recovery receives a new member reference.
A new reference does not reset dismissal of an active sandbox/code group.
Different failed herdr plugins in the same sandbox have different references.
References must not collide among active diagnostics or retained event records.

Show one primary diagnostic in each sandbox notification, prioritizing errors over warnings and retaining a consistent choice while the same failures remain active.
The notified reference must identify an entry already saved in the diagnostic list.
Other failures remain separate detail blocks in their groups without being appended to that notification.
Continue suppressing unchanged repeated notifications.
Dismissal does not generate a new notification or a new reference.

Normalize whitespace and remove terminal control characters from notification text.
Limit the body to 120 displayed characters, truncating the sandbox name to at most 32 characters and the short cause to the remaining space, with an ellipsis where needed.
Always retain the full six-character reference and separators.
The host can further constrain its notification width, so place the reference before the cause.
At narrow panel widths, keep the component code first in the header and wrap the remaining metadata.
Keep the full reference at the start of the following diagnostic text, wrapping the cause as needed.
The list retains the full sandbox name and detailed cause; notification truncation never changes saved diagnostic content.

The notification does not change keyboard focus or start recovery.
The user opens the panel, presses `e`, and locates the same reference in New, or switches to All if the entry was dismissed.
Enter opens the existing recovery flow; Escape returns without changing the sandbox.
If the failure resolved before the panel opens, the same reference remains available in the expanded Events history while that bounded history retains it.

Acceptance: two simultaneous plugin failures produce two distinct references and separate repair guidance, while the notification contains just one short primary cause.
The reference in that notification exactly matches the muted prefix of its diagnostic and its later recovery event.
Repeated polling and dismissal preserve the reference without new notifications.
Long names, multiline build output, and narrow terminals never hide or truncate the reference in the generated notification body.
The full cause and recovery command remain readable and selectable in the panel.

### Diagnostic navigation and feedback

Pressing `e` focuses the first visible diagnostic without leaving the current tab.
If New is empty but dismissed entries remain, `e` focuses the New/All switch.
Clicking New or All selects that view and focuses the section title.
Enter or Space on the title toggles New/All; Left selects New and Right selects All when the title has focus. `n` also toggles it while a diagnostic has focus. Use blue for the selected choice and muted text for the other. Errors, Warnings, and Events titles use regular text color only while their actionable header has focus; otherwise they match the border color.
Keep the same group selected when it remains visible after a filter change.
Otherwise, select the first visible group, or the title when the chosen view is empty.

Up and Down select a group, and Enter opens recovery for its code or affected sandbox.
Plugin detail blocks do not add nested keyboard focus stops.
Press `d` to acknowledge every member of the selected undismissed group.
Clicking any detail block selects its group and opens its existing recovery path; no Dismiss button is rendered.
In New, select the next group after dismissal, or the preceding group when the dismissed one was last.
If no visible groups remain, focus the switch so All stays accessible.
In All, keep the dismissed group selected and show its updated state.
If saving dismissal fails, retain its previous state and show the failure in the diagnostics section.
Dismissal progress stays beside the affected group without blocking navigation.
For R0005, recovery opens the affected sandbox and focuses Adopt when available; adoption still requires approval.

On Sandboxes, Down from the final row enters the Events disclosure when present, then the diagnostics title, then its visible entries.
If the final row is expanded, traverse its plugin rows and actions before leaving the main section.
Up reverses this order and returns to the final action or row of the main section.
Events history is read-only and scrollable; the disclosure is its only focus stop.
Clicking its title or pressing Enter or Space toggles Events without moving focus.
Collapsing Events keeps focus on its title and removes its body from the scroll layout.
Keep expanded sandbox details open during these transitions.
If the table is empty or the filter hides every row, Down still enters the next available section.
Returning to the empty table preserves its filter.
With no events or diagnostics, keep the existing table boundary behavior.
Within diagnostics, Down at the final entry keeps the existing scrolling behavior without wrapping.
Escape returns focus to the main table without changing the filter, dismissal, or Events state.
If all active entries resolve while diagnostics has focus, return focus to the main table.
The same transitions apply to `j` and `k`; Home, End, PageUp, and PageDown retain their existing behavior.
Contextual help shows Events collapse/expand, New/All, recovery, and Dismiss controls only where available.
Details show safe diagnostic output in the scrollable section.
Recent failure and recovery events remain available after active errors clear.
Unresolved event codes use red for errors and amber for warnings, without an Error or Warning prefix.
Recovered failures show Recovered in green beside the code.
Cleared warnings show Resolved in green beside the code.
Put each event timestamp directly after the sandbox name and before any plugin ID.
Keep sandbox names neutral and timestamps muted.
Display sandbox names in active entries and events instead of the internal host-scope hash and sandbox-ID pair.
Resolve the exact sandbox identity to its current name, then use a saved event or cached observation name when the sandbox is absent.
Save the sandbox name with new events so history stays readable after deletion.
When no name is available, use `Unknown sandbox` with a short sandbox ID; never display the long host hash as its name.
Keep raw identities internally for matching and diagnostic JSON, and retain readable labels for host and daemon scopes.
At narrow widths, preserve the code, truncate the name when necessary, and wrap details.
Keep the complete timestamp, count, and New/All switch available through wrapping instead of clipping controls.
Unknown errors display `Unexpected error`, their scope, and available diagnostic details.
Do not invent a specific known code for an unclassified failure.
Successful recovery clears the active error only after a confirming result.
The Hint section holds a Details action confirmation, including its confirm and cancel choices. It names the action and keeps the active explanation in one place. While that action runs, show a compact spinner and the action name in Hint instead of phase text such as Waiting, Pending, or Executing. Show the result in Hint without stealing focus; coded failures remain in diagnostics.
Do not show a hint telling the user to open Errors when an action fails; its coded error belongs in the outlined section.
Replace a hint with the latest action's progress and clear it when the user navigates away from that action's tab or sandbox.
The Hint follows the currently selected sandbox, whether or not its details are open.
Moving selection to another sandbox changes the Hint without closing the existing expansion.
For a selected sandbox with an ownership conflict, show its guidance in the colored Hint section rather than repeating the saved status detail inside the expansion.
For an unmanaged sandbox, say that it is already a herdr machine and was added outside the plugin.
When Adopt or its approval dialog has focus, explain the management change and the retained name and connection in the same accent color as Adopt.
Suppress unrelated sandbox guidance while focus is in error or warning management.
Show ready-connection guidance in Hint instead of the details body.
Use the same placement for the selected sandbox's saved status explanation in every state, including Failed and Stopped.
Preserve the explanation's content and the existing product wording rules; do not replace a useful cause with a generic failure message.
Match the Hint color to readiness: failed, blocked, error, and manual-intervention explanations are red; stopped explanations are muted; ownership conflicts are amber; ready explanations are green.
Keep state words visible in the row and details summary so color is not required to understand the explanation.
An empty explanation produces no Hint section, and a current action's progress still takes precedence until navigation leaves that action's context.
Do not repeat the status explanation in expanded details, even while another sandbox is selected.
Details retain the name and herdr label, readiness summary, runtime badge, observation age, an active-action spinner, herdr plugin table, and applicable actions.
Selecting a row only reads its explanation; Enter or a click still toggles details, and Escape closes details without losing the selected row's Hint.
Wrap Hint text at word boundaries at narrow widths and preserve the cause and recovery guidance through scrolling.

```text
> ▸ Watched  api   Running  API development  failed
  ▾ Watched  docs  Stopped  Documentation    stopped
    ╭─ Details · docs-id · Owned ───── Stopped ╮
    │ docs → Documentation (stopped)         │
    │                                       │
    │ ╭─ Plugins ─────────────────────────╮ │
    │ …                                   │
    ╰───────────────────────────────────────╯
╭─ Hint ─────────────────────────────────────╮
│ saved machine is not ready: connection     │
│ refused. Retry when the server is available.│
╰────────────────────────────────────────────╯
```

In this example, api is selected, so its failure explanation is red in Hint while docs remains expanded.

### Diagnostic acceptance conditions

A mixed section shows colored codes without severity prefixes on its entries.
Each group keeps its code in the header and each member keeps its muted reference immediately before its error text.
Selecting another entry or receiving a repeat observation does not move the component code behind a reference.
New and All show parenthesized counts, with the selected choice blue and the other muted.
Moving focus or changing the filter does not change any border color.
An undismissed group exposes `d dismiss` in contextual help and has no Dismiss button.
The timestamp follows the sandbox name, and `×3` appears muted only after three recorded occurrences.
Events starts collapsed above diagnostics and can be expanded with keyboard or mouse.
A cleanup success appears in Events without creating a persistent Hint.
Two plugins with the same sandbox and code produce one group with two separate detail blocks and repair commands.
If their occurrence counts are three and two, the group shows muted `×5`, and contributes five to New and All.
Dismissing that group removes five from New, leaves All unchanged, and adds five to the hidden occurrence count.
Identical observations, new member references, and a panel restart keep that group dismissed.
A newly failing third plugin with the same code stays hidden in New while the dismissed group remains active.
If one plugin recovers, the remaining members stay dismissed and the counters exclude the recovered member's occurrences.
After every member recovers, a later failure with that sandbox/code appears in New.
With zero new entries and hidden active entries, both keyboard and mouse can select All and reach recovery.
Dismissal, filter changes, background updates, and partial recovery preserve a valid focus target.
At 60 columns, every switch and recovery control remains reachable, and timestamp and repeat count stay readable.

## Keyboard, mouse, and focus

All shortcuts act immediately without an extra Enter unless the shortcut opens an input or action selector.
Global letter shortcuts are suspended during text input and modal dialogs.
The standard Bubbles help component renders the footer shortcuts for the active focus context.
The full help view uses the same component.
The help view lists the complete bindings and returns to its previous focus with Escape.

| Context | Key | Result |
| --- | --- | --- |
| Active text drag | Escape | Cancel the drag without copying or changing input, details, or dialogs. |
| Browse any page | `q` or Ctrl+C | Close the TUI, leaving the worker running. |
| Browse any page | `s`, `p`, `b`, `e`, `?` | Open Status or Plugins, focus the current tab's active errors and warnings section, or open help. |
| Browse any page | `c` | Refresh observations without repairing or changing choices. |
| Focus navigation | Tab, Shift+Tab | Move forward or backward between visible focus regions. |
| Table or list | Up/Down, `j`/`k` | Select the previous or next item. |
| Events title | Enter, Space | Expand or collapse the event history. |
| Diagnostics title | Enter, Space, `n` | Toggle New/All. |
| Diagnostics title | Left, Right | Select New or All without changing the top tab. |
| Diagnostic group | `n`, `d`, Enter | Toggle New/All, dismiss the undismissed group, or open recovery. |
| Table or list | Home/End, PageUp/PageDown | Move to the edge or by one visible page. |
| Sandbox table | Enter | Expand or collapse the selected row. |
| Sandbox table | `w`, `r` | Toggle Watch or edit the Herdr label. |
| Any focused table | `/` | Focus that table's fuzzy filter. |
| Plugin defaults table | Space | Toggle default auto-sync. |
| Expanded plugin table | Space, Enter, `i` | Cycle Inherit/On/Off, or restore Inherit directly with `i`. |
| Button group | Up/Down, Enter | Select and activate a button. |
| Text input | Enter, Escape | Submit or cancel, subject to filter behavior below. |
| Detail focus | Escape | Return focus to the containing sandbox row. |
| Secondary page | Escape | Return to the previous page. |
| Sandbox table | Escape | Collapse its expansion, then clear an active filter on the next press. |

The normal focus order is table, expanded plugin table, expanded action group, bottom navigation, worker control, and the active errors and warnings section.
Skip regions that do not exist.
The footer Auto-sync control and `b` open Status, which contains one ON or OFF choice.
Dialogs contain focus until resolved and restore their opener's focus afterward.
Escape on a top-level unfiltered sandbox table does nothing.
Only an explicit close command exits the interface.

Clicking a row selects it without changing state.
Clicking its disclosure marker expands or collapses it.
Clicking an explicit button or plugin value activates that control with the same rules as the keyboard.
Clicking a management label never toggles Watch.
Wheel movement scrolls the region under the pointer without activating anything.
Mouse motion never changes keyboard selection.
Visible control bounds and mouse hit areas come from the same layout calculation.
No operation requires a double click, hover, or right click.

Users drag directly over visible text to select names, errors, warnings, hints, and manual commands.
Selection needs no toggle, shortcut, or added button.
On left-button press, save the displayed frame, coordinates, and pressed control without activating it.
Movement to another terminal cell starts a drag, even if the pointer later returns to its starting cell.
On release without a drag, activate the original control once.
On release after a drag, copy the highlighted text without activating any control.

Highlight a continuous text range in reading order, including across lines and in either drag direction.
Measure terminal cells and include complete displayed characters, including wide characters, emoji, and combining accents.
Use reverse video for selection, including when colors are disabled.
Copy plain text without ANSI styling, preserving visible line breaks, indentation, and interior spacing.
Remove screen-padding spaces at line ends.
Visible outlines and table separators remain part of the copied text.
Selection covers the visible screen without automatic scrolling at its edges.

Freeze the displayed frame, including loading indicators, from left-button press through release.
Continue background observations and operations while the user selects text.
Keep the completed selection visible until the next pointer or keyboard interaction.
That interaction clears the selection, shows the latest state, and performs its usual action.
Escape during a drag cancels only that gesture without copying, discarding input, closing a dialog, or collapsing details.
Resizing cancels the gesture without copying and redraws the current state at the new size.
Preserve the current tab, expanded details, focus, input text, and dialog when copying.
After release, the footer can briefly show `Selection sent to clipboard` without changing the layout above it.
Keep the Hint section visible because its text can also need copying.

Ctrl+C first cancels an active text input or closes a dialog, equivalent to Escape.
From a browse view, Ctrl+C closes the TUI.
Closing does not roll back saved choices or stop the worker.
If a foreground operation requires cleanup, show that progress and restore the terminal after safe cleanup completes.
Do not represent a locally canceled wait as a successfully canceled external operation.

## Fuzzy filtering and live refresh

Slash focuses the current table's filter and matches case-insensitive character sequences.
Keep the empty `/ Filter…` prompt muted until it receives focus; then use the active input style.
Sandbox matching uses sandbox names and Herdr labels.
Plugin matching uses identifiers and available display names.
Highlight matching characters without changing table alignment.
Keep normal row order rather than sorting rows on each keystroke.
Show `3 of 18 sandboxes` or the equivalent plugin count.

Enter accepts the filter and returns focus to the table.
Escape leaves filter input and keeps the current query.
Ctrl+U clears the query while the input has focus.
A Clear control appears only after a nonempty query is accepted and filter input is no longer active.
When the sandbox table has focus and no row is expanded, Escape clears its query.
On other table pages, Clear or `/` followed by Ctrl+U clears the query before Escape leaves the page.

Each table retains its own query and selection while changing pages.
If filtering hides the selected row, select the first visible match and close the hidden expansion.
If nothing matches, show `No sandboxes match “query”` with Clear filter.
Filtering never changes saved choices.

Use stable sandbox identity to retain selection, expansion, and pending operations across refresh.
Keep current row order during an interaction and append newly discovered rows.
Update values in place, without resorting on status or label changes.
After a confirmed deletion removes the selected sandbox, select the nearest surviving row and show a short removal notice.
Failed inventory retains existing rows with stale observations and an inventory error.
If a selected row disappears during rename, retain the input and report that the registration is no longer available.

## Loading, small terminals, and recovery

On initial loading, show table headings and `Loading sandboxes…` inside the viewport.
Do not briefly show an empty inventory before the request completes.
An observed empty inventory reads `No sandboxes found` and offers Refresh observations.
A failed initial request reads `Inventory unavailable` with Retry and Status details.
Partial failure preserves healthy rows and marks affected rows individually.
When observation age grows, show the last observation time and a stale marker rather than guessing current runtime.

Keep management, sandbox name, runtime, Herdr label, and readiness in separate columns at all supported widths.
Allocate name and label widths from the visible rows.
Collapse runtime text to a colored dot when either name or label needs more room.
Use distinguishable compact symbols when color is disabled.
Expanded details retain the full runtime text and stale marker.
Truncate long names and labels with an ellipsis and show their complete values in expanded details.
An active rename scrolls horizontally within its input instead of hiding the cursor.

```text
 ╭─ ← Sandboxes   Status   Plugins → ───────────╮
 / Filter…                                      6 rows
     MANAGEMENT SANDBOX R HERDR LABEL     READINESS
 > ▾ Watched    api     ● API development
   ╭─ Details · abc123 · Owned ─── 2m ago Running╮
     api → API development (ready)
     PLUGIN                 CHOICE       EFFECTIVE
     <plugin-id>            Inherit      On
     [Unwatch] [Disable · remove from Herdr] [Rename Herdr label…]
   ▸ Unmanaged  release ● Release desk    ownership…
 ╭─ Errors · 1 active ────────────────────────────╮
 │ H0001 · Host · SSH setup is required            │
 ╰────────────────────────────────────────────────╯
 ↑↓ select  Enter expand  Tab focus  ? help  q close
                          Auto-sync ON  sbx ●
```

At fewer than 24 rows, reduce spacing and the active errors and warnings section to one summary line until focused.
Keep navigation, the diagnostic summary, and the bottom help and status line fixed.
Scroll the remaining body, including expanded details, to keep the focused control visible.
Wrap action groups at control boundaries, never in the middle of a label.
Dialogs use the available body width and scroll their content before hiding actions.

The minimum useful size is 60 columns by 18 rows.
Below that size, display a compact resize message, worker state, separate error and warning counts, and `b status · e diagnostics · q close`.
Status and the active errors and warnings section remain available in a scrollable compact form.
Resizing restores the same selection, input, expansion, and query.

## Bubble Tea architecture recommendation

The current `go.mod` contains urfave/cli and go-toml, with no terminal UI framework.
Adopt [Bubble Tea v2.1.0](https://github.com/charmbracelet/bubbletea/releases/tag/v2.1.0), the verified stable baseline on October 15, 2026.
Confirm the latest stable release again when implementation starts, as required by the behavior specification.
The benefit is one event loop for keyboard, mouse, resize, and asynchronous results.
The migration cost is replacing the blocking menu and nested reader loops, plus adapting operation output into structured UI results.

Use one root model for page, focus, selected identities, expansion, filters, active input, observations, pending operations, errors, and warnings.
Small page and input components can remain inside `internal/panel`.
A custom row renderer fits the variable-height expanded row better than forcing it into a fixed-height table widget.
Use one shared layout calculation for rendered regions, scrolling, and mouse hit testing.
Avoid adding a routing framework, event bus, or general table abstraction for these three pages.

Model messages include input, resize, observation completion, operation progress, operation completion, and worker status changes.
Include target identity and request generation in asynchronous results to reject stale responses.
Update changes presentation state and returns commands for external work.
View renders the current state without reading files, probing Docker, or starting operations.
Commands call existing services outside the update loop and return typed results.
Persistent choices and conflict control belong to the services and store, so worker and UI requests obey the same rules.

The [official v2 upgrade guide](https://github.com/charmbracelet/bubbletea/blob/v2.1.0/UPGRADE_GUIDE_V2.md) specifies `charm.land/bubbletea/v2` and `View() tea.View`.
Set alternate-screen and mouse behavior on that view.
Keep `tea.MouseModeCellMotion` enabled for clicks, drag motion, and button release.
Store the pressed control, drag coordinates, and frozen frame in the panel model.
Defer click activation until release distinguishes a click from a drag.
Use `tea.SetClipboard` to send selected text through OSC52 without adding a clipboard dependency.
The [Bubble Tea clipboard API](https://pkg.go.dev/charm.land/bubbletea/v2@v2.1.0#SetClipboard) notes that some terminals do not support OSC52.
Clipboard delivery also depends on the terminal multiplexer, so feedback reports the copy request rather than confirmed delivery.
Handle key presses with `tea.KeyPressMsg` and mouse events through the v2 message types.
Do not copy v1 startup options or string-returning View examples.
Keep version-specific APIs behind the panel implementation rather than exposing them to registration or lifecycle services.

Existing inspection and service printers cannot write directly into the active renderer.
Adapt their result reporting at the panel boundary.
Use an explicit terminal handoff for interactive SSH setup, then return its result to the model.
The persistent worker remains independent of the TUI's process lifetime.
Any additional styling or input component dependency needs a concrete benefit and a compatible v2 version before adoption.

## Observable acceptance conditions

1. At 110×32, six sandbox rows, bottom navigation, worker state, and an active error or warning remain visible together.
2. At 60×18, keyboard and mouse can reach every action through scrolling without hiding the worker or diagnostic summary.
3. With color disabled, selection, readiness, management, failures, and warnings remain distinguishable by text and symbols.
4. Opening a second sandbox closes the first expansion, while refresh preserves the selected sandbox and edited label.
5. A pending watched sandbox remains Watched, and explicit Unwatch or Disable survives refresh and rediscovery.
6. Disable failure shows that management stopped and the Herdr entry remains, with an available retry.
7. Adoption starts only after approval, preserves the existing label, and refuses unknown or ambiguous eligibility.
8. Rename cancellation restores the old label, and a failed save retains the user's proposed text.
9. Plugin default and override changes show saved state separately from synchronization success, and Off never implies uninstall.
10. An Inherit choice shows its effective value, and one action restores Inherit from either override.
11. A stopped-worker prompt can be declined with Escape, while Status and plugin configuration remain usable; its Auto-sync controls reflect the observed state.
12. A 35-second diagnostic leaves navigation responsive and displays Pending before its actual result.
13. A failed inventory read preserves existing rows, their timestamps, and a visible error instead of deleting them.
14. Every mouse activation has a keyboard equivalent, and text input never triggers a global letter shortcut.
15. Repeated failures or warnings share a sandbox/code group with separate plugin details, summed occurrence counts, and dismissal that lasts until the whole group recovers.
16. Closing the TUI restores the terminal and leaves the worker running unless the user explicitly used Stop.
17. A click activates its control once on release, while a drag starting on a control copies text without activating it.
18. Forward, reverse, and multiline drags copy the highlighted plain text without splitting displayed characters or including ANSI styling.
19. Background observations and operation results do not move the selection frame, and the latest state appears on the next interaction.
20. Escape during a drag and terminal resizing cancel copying without discarding input, closing a dialog, or collapsing details.
21. Selection remains visible at narrow widths and with color disabled, and the next keyboard or pointer action works without an exit step.
22. Clipboard delivery works through the supported herdr popup, tab, and split paths when their terminal supports OSC52.
23. Down reaches active errors and warnings after the last visible sandbox and its expanded controls, and Up reverses that path without closing details.
24. An empty filtered table still connects to active errors and warnings through Down and Up without clearing the filter or selecting a hidden sandbox.
25. Status hides Refresh observations only for selected ON, observed Running, a known successful sync within 30 seconds, and no active errors.
26. R0005 alone shows an amber Warning in a Warnings section; it does not make Auto-sync show ERROR or expose Refresh by itself.
27. Errors and warnings share arrow navigation and the `e` shortcut, with codes, causes, and section titles readable when color is disabled.
28. Selecting sandbox B while A remains expanded changes Hint to B; a focused Adopt action or dialog shows its own accent-colored explanation.
29. Event codes match their outcome colors, and known sandbox scopes display names instead of hash and ID pairs.
30. Events for deleted sandboxes retain their saved names; records with no recoverable name show Unknown sandbox and a short ID.
31. Two failed herdr plugins in one sandbox produce two entries, each with its own cause, recommendation, build output, timestamps, and repeat count.
32. A verified pinned installation command appears in a padded blue-left-line callout before muted build output only when the guest plugin was initially absent and the failure is confirmed before linking; the full command has a blank line above and below it.
33. At 60 columns, prose wraps at spaces, command arguments remain complete, and drag selection works without an added button or mode.
34. Selecting a Failed or Stopped sandbox shows its saved status explanation in Hint without expanding it, with failed text red and stopped text muted.
35. A status explanation appears once, in Hint; expanding or collapsing its sandbox preserves the explanation, and moving selection updates it without closing another sandbox's details.
36. Missing status detail leaves Hint absent, and current action feedback temporarily replaces status guidance without erasing persistent diagnostics.
34. Resolving one plugin clears only its active failure and records its recovery while other plugin failures remain visible.
35. A failure after linking or at an uncertain stage shows retained-source repair guidance and Re-sync in the blue callout, without an installation command.

The behavior specification remains authoritative.
No further user decision is required for this design.
The interactive SSH handoff is an implementation constraint from the current panel, and its navigation exception is explicit above.

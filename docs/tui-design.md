# Interactive sandbox manager design

This design describes the layout and visual treatment defined by the [interface specification](tui-spec.md).

The user opens the manager to see sandbox status, enable or disable a herdr connection, and configure herdr plugin synchronization.
The main path is to select a sandbox, read its preview and Hint, enter its controls, activate an action, and inspect its result.
Management controls permission for automatic work.
Explicit actions remain available while Management is OFF.

## Frame and visual language

Keep the existing outlined sections, blue accents, filled buttons, sand-colored herdr labels, and standard Bubbles help.
Use no application header.
Put the rotating Sandboxes, Status, and Plugins tabs in the main section's top border.
The selected tab is leftmost and uses the blue accent.
Inactive tabs are muted, with no background or brackets.

The frame contains the active tab, Events, diagnostics, and Hint when those sections have content.
Events precedes diagnostics and starts collapsed.
Keep contextual help on the left and group Management immediately before sbx at the bottom right.
Both status indicators open Status without changing any setting.

Use blue for the Enabled checkmark and primary actions, sand for herdr labels, green for ready, amber for warnings, and red for blocked work.
Use gray for stopped, disabled, metadata, and the exact cancellation state `Cancelling`.
Text and symbols communicate each state when colors are disabled.
Main, Events, diagnostics, and Hint borders keep the separator color.
The sandbox card outline brightens while its controls have focus.
A section title uses normal text color only while its actionable header has focus.

The selected sandbox appears as an expanded card in place of its table row.
Keep its identity selected when focus moves elsewhere.
Use an Enter symbol to distinguish the focused button when colors are disabled.
Buttons keep complete labels and wrap as whole controls.

See the [sandbox view](screenshots/sandboxes.png) and [focused card](screenshots/sandbox-details.png).

## Sandbox columns and observed state

Use Enabled, Sandbox, Herdr label, and Status, in that order.
Show runtime alongside the sandbox name in its expanded card.
Keep the full runtime state in Details.
Below 72 columns, hide Status and retain its full explanation in Hint and Details.
Shorten the herdr label before the sandbox name.

Enabled uses the existing blue `✓` for the saved choice or a pending eligible Enable request.
Disabled and an idle confirmed missing entry both use the same small muted `·`.
Only the herdr label indicates entry presence.
An absent entry has a blank label cell, without a dash or an ownership badge.
An existing entry can be enabled even while its sandbox is stopped; failed native changes restore its actual native state and show the operation error.

An eligible missing-entry dot requests Enable or retries creation, even when discovery already saved Enabled internally.
Enable on a stopped sandbox without an entry leaves the dot and Disabled choice unchanged.
An eligible toggle immediately changes the checkmark or dot, with Enabling… or Disabling… in row Status and the expanded summary.
Keep the label blank until entry creation is confirmed, and suppress duplicate activation.
Retain the requested choice and transition through older observation polls, then show the actual outcome.
After the transition, save and display the verified native state even when the command returned an error.
If saving fails, restore the confirmed choice and report the error.
If native state cannot be verified, retain the last confirmed choice as stale and suspend automatic connection changes and guest work until a fresh observation settles it.
After the entry exists, show the checkmark and its label even when later setup fails.

Status describes usefulness through herdr, with values such as Ready, Waiting for start, Stopped, Disabled in herdr, or Setting up.
A herdr plugin failure retains the machine's completed setup and explains the affected plugin separately.
Management OFF appears in the footer instead of replacing every Status cell.
Stopped and Disabled are normal conditions.
Unknown or stale observations must not look like confirmed absence.

When the catalog cannot be read, preserve the last known label and describe its observation age in Hint.
For a failed background refresh, show `Could not refresh sandbox state: <actual reason>` in status and warnings.
Do not erase a label after failed removal or a cancelled operation unless absence is confirmed.
An ambiguous association explains the conflicting identity in Hint and prevents unsafe mutations.
An externally created matching entry needs no special category or approval.
Editable labels never identify the machine for actions.

## Selection, details, and actions

Up and Down select sandboxes and immediately open the selected sandbox's preview.
The preview replaces its table row entirely, and only the selected sandbox is expanded.
Enter or Right focuses the primary action and brightens the card outline.
Left and Right select buttons; Down moves into plugin choices and Up returns to the actions.
Escape returns to list navigation without closing the preview.
Left does the same from the leftmost action or plugin controls; from other action buttons it moves to the previous button.
A row or card-header click selects its preview, while embedded controls perform their own action.

The title is `✓ sandbox name (runtime) → herdr label (status)`, with the known herdr version at the top right.
Keep the checkmark, name, runtime, label, and status colors; do not mute the whole title.
Show relative observation age near the version only after two minutes.
For a missing entry, keep the label position empty and explain absence in Hint.
Place actions directly below the title with an empty line before and after.
Then show `sandbox ID → herdr ID` in muted text, followed by the outlined Plugins table.
Put a known `herdr <version>` in the top-right border.
Append `↓` for an older guest version or `↑` for a newer one.
Omit the version when unknown and the arrow when comparison is unavailable.
Opening Details does not probe the guest.

```text
│ ╭─ ✓ docs (Stopped) → Docs (Waiting for start) ──── herdr 0.9.0 ────╮  │
│ │                                                                  │  │
│ │  Disable (space)   Rename                                         │  │
│ │                                                                  │  │
│ │  abc123 → machine456                                              │  │
│ │ ╭─ Plugins ────────────────────────────────────────────────────╮ │  │
│ │ │ HERDR PLUGIN                 CHOICE          EFFECTIVE       │ │  │
│ │ │ example.tools                Inherit         On · default    │ │  │
│ │ ╰──────────────────────────────────────────────────────────────╯ │  │
│ ╰──────────────────────────────────────────────────────────────────╯  │
```

Choose available actions from saved Enabled intent, verified entry presence, native connection state, runtime, and the active operation.
Do not collapse these facts into a single management category.
The action rules are:

| Situation | Actions and completion |
| --- | --- |
| Running sandbox without an entry | Enable adds the entry and preserves partial results if setup fails. |
| Disabled entry | Enable enables the herdr connection, even for a stopped sandbox, or retries an incomplete enable. |
| Enabled entry | Disable saves Disabled and disables the native connection while stopping guest work safely. |
| Disabled with native disable confirmed | Remove from herdr deletes only the entry and retains Disabled. |
| Any uniquely associated entry | Rename edits the current label, including while Disabled or Management is OFF. |
| Running enabled sandbox needing setup or plugin recovery | Re-sync retries eligible setup and selected herdr plugins. |
| Running enabled sandbox with a verified entry and version mismatch or H0003 | Fix herdr opens its version and handoff confirmation. |
| Stopped sandbox without an entry | Stay Disabled quietly until it is running and the user enables it. |
| Conflicting work in progress | Keep navigation available and show that action's progress without issuing a duplicate request. |

Space toggles the selected row's Enabled control.
The checkmark or dot is its mouse target, independent of the row or card's selection target.
Enable or Disable is the primary action and shows `(space)` after its label.
Mute that primary button when another action or plugin control has focus.
Space keeps that primary meaning while action buttons have focus; Enter activates the focused button.
Help names the actual primary action as `enable` or `disable`.
Disable does not delete the sandbox or its entry.
Offer Remove from herdr only after native disable is confirmed.
A failed disable restores the actual Enabled state and leaves removal unavailable.
The ordinary Enable and Disable actions follow the settled state; do not add separate retry controls for retained intentions.
A failed remove retains its label and the Remove from herdr retry.

The plugin creates herdr entries only for running sandboxes.
For a stopped sandbox without an entry, leave the muted dot, blank label, saved Disabled choice, and neutral Stopped status.
Do not show a warning or a pending Enable intention for this condition.
Activating that unavailable Enable is a quiet no-op.
Starting the sandbox independently makes an explicit Enable eligible; it does not reset the Disabled choice.
Do not present add-enabled-then-disable as successful completion of this contract.
The UI must not claim that observing a stop event prevents a reconnect from waking a sandbox.

Action explanations appear in Hint and use the same accent as a focused primary action.
Use the following effects in the copy:

| Action | Hint |
| --- | --- |
| Enable, absent stopped entry | This sandbox is stopped. |
| Enable, existing entry | Enable this sandbox's herdr connection. Herdr may start it when connecting. |
| Disable | Disable this sandbox's herdr connection and stop its plugin work. Keep its entry, label, and existing files. |
| Remove from herdr | Remove this disabled entry from herdr. Keep the sandbox and its files. Enable adds the entry again. |
| Rename | Change this sandbox's label in herdr. Keep its sandbox name and connection. |
| Re-sync | Retry setup and selected herdr plugins in this running sandbox. Management stays unchanged. |
| Fix herdr | Show the known guest version, target host version, and live handoff behavior before confirmation. |

Fix herdr confirmation names the exact target version and warns about a downgrade.
Put its confirm and cancel controls in Hint, with Cancel initially focused.
A failed handoff does not force a restart.
Report the new version only after a fresh successful observation.
The temporary download access action remains unavailable.
Existing policy cleanup records remain visible, and automatic cleanup obeys Management.

While an explicit action runs, Hint shows an animated spinner and its action name.
Keep the completed result in that action's context and durable failures in diagnostics.
When focus moves elsewhere, replace transient feedback with the current control's explanation.
If a submitted operation is cancelled, display its actual completed effects and unfinished work.
Cancelling a local wait is not proof that an external operation stopped.

## Inline rename and herdr plugin choices

Press `r` or activate Rename to edit the herdr label in the selected card title.
Enter submits and Escape cancels an unsubmitted edit.
The input scrolls horizontally to keep its cursor visible.
Use a block cursor during rename and highlight Rename in sand with an Enter-to-save symbol.
Left and Right keep their text-editing meaning until the edit ends.
A failed save retains the proposed text and cursor for correction.
Observations do not overwrite active input.
If navigation is blocked by an edit, Hint says `Renaming <sandbox>: save with Enter or cancel with Esc before leaving this input.`
Cancellation after submission preserves the actual observed label, rather than claiming that the previous label was restored.

In Details, Down moves from action buttons to herdr plugin rows, and Up returns to actions.
Enter, Space, or a click cycles Inherit → On → Off.
The `i` key returns a focused override to Inherit.
Show Choice immediately, with a loader in Effective until persistence completes.
Then show the effective On or Off and whether it comes from a default or override.
A save failure restores the previous saved choice and shows its diagnostic.
A guest sync failure keeps the successfully saved choice.

Plugins shows one table of herdr plugin identifiers, default On/Off choices, and availability.
Enter, Space, or a click toggles a default directly.
New herdr plugins default to Off.
Mark unsupported sources Unsupported and show the actual reason below the row.
An unsupported Off choice cannot become On, but an existing On choice can become Off.
No custom bundle configuration is present.

For the selected On default, show `The matching herdr plugin version will be copied to enabled sandboxes while Management is ON.`
For Off, show `The herdr plugin will not be synchronized. Existing copies stay intact.`
While Management is OFF, changes save choices without scheduling guest work.
A stopped enabled sandbox waits until it runs with Management ON.
At narrow widths, stack Choice and Effective below the identifier and show the full focused identifier in Hint.

## Management, cancellation, and Status

The footer shows the requested Management ON or OFF choice at the bottom right immediately before sbx.
Daemon health is a separate observation in Status.
Polling must not alter the requested choice.
Turning OFF immediately selects OFF and stops scheduling automatic work.
Show exactly `Cancelling` in gray until active work settles.
Do not replace that state with Stopping, Pending, a spinner, or a successful OFF indication alone.

```text
                                        Management OFF Cancelling sbx ●
```

Turning ON during cancellation immediately selects ON but keeps gray Cancelling until the previous work settles.
Hint explains that Management will read current sandbox and herdr state before resuming.
Do not imply that a second worker is already running.
After cancellation settles, ON reads the fresh baseline before automatic mutations.
External labels and verified native enable changes become that baseline.
Remembered Disabled survives when its entry is absent.
A verified native Disabled state becomes Disabled regardless of sandbox runtime.

If shutdown or cancellation fails, keep the requested choice and the actual unsettled state visible.
Show the specific diagnostic, including D0001 for an unconfirmed stop, and offer Retry.
OFF must not silently schedule cleanup or retry work.
Completed entry creation or rename remains visible after cancellation.
An unsafe-to-interrupt step keeps Cancelling visible until it settles.

Status has an outlined Management subsection showing `ON | OFF`, with one selected choice and the observed daemon state beside it.
Focus Management by default and brighten its outline like focused sandbox details.
Space and Enter toggle it; Up/Down visits the other available Status actions.
The Hint describes current Management activity and worker state, replacing successful request notices when they finish and excluding sandbox-specific guidance.
Keep startup, reconnecting, failed cancellation, and other daemon details here.
Show `last at HH:MM` only while running and only when the known successful sync is older than 30 seconds.
Show `*` during an observation refresh, even when that time is hidden.
The footer has no timestamp or observation-refresh star.

See the [Status view](screenshots/status.png).

Prerequisite results use colored foreground text without filled backgrounds.
Capitalize status words while preserving command, path, and error text.
Combine sbx version and capability checks, and combine daemon health and inventory access as Sandbox service.
Preserve independent failure causes and recovery actions within grouped rows; avoid repeating derivative skipped-check explanations.
Keep Docker sign-in and SSH checks distinct because they need different repairs.
Omit Sandbox setup from global Status; each sandbox shows its own setup state.

Refresh is read-only host inspection.
It never opens sandbox SSH, repairs setup, or runs cleanup.
Hide it only when Management is ON, the daemon is Running, sync is known within 30 seconds, and no active error exists.
Otherwise, put it after prerequisite details with one blank line above it.
Warnings alone do not count as errors for this rule.
Offer Set up SSH when SSH is not ready.
Its explicit terminal handoff restores the TUI and prior focus when complete or cancelled.

On first use, request one daemon and show the requested ON choice with its observed startup state.
On later opening with management stopped, ask `Start Management?` and explain automatic setup, registration, and selected herdr plugin sync.
Focus Keep OFF initially, with Start Management as the other choice.
Escape means Keep OFF.
Declining leaves observation and explicit actions available.
Closing the TUI leaves the daemon and saved Management choice unchanged.

## Keyboard, mouse, and filtering

Use the standard Bubbles help component for contextual shortcuts and full Help.
Use bold hotkeys, regular muted descriptions, and faint separators; render plain text when color is disabled.
Both views list only actions available for the current focus and selected target.
Show `c refresh` only when Refresh is available and idle, and `d dismiss` only for a selected, saved, undismissed diagnostic.
Expand help in place with `?`, preserving the tab, focus, selection, and underlying scroll position.
Close it with `?` or Escape; scroll keys scroll help, while other advertised shortcuts close help and execute in the preserved context.
See the [expanded help view](screenshots/help.png).
Text input captures typing before global shortcuts.
The primary controls are:

| Key | Action |
| --- | --- |
| Up/Down | Preview adjacent sandboxes, or move within entered controls; Events and diagnostics retain their own navigation. |
| Enter | Enter the preview on its primary action, or activate the focused control. |
| Space | Enable or disable from list/action focus; cycle a plugin choice or toggle a focused section header. |
| `r` | Rename a uniquely associated existing entry inline. |
| `i` | Restore Inherit on a focused herdr plugin override. |
| `/` | Focus fuzzy filtering for the current table. |
| Left/Right | Right enters a preview; Left selects the preceding action or leaves from the first action or plugin controls; diagnostics uses New/All. |
| Tab/Shift+Tab | Cycle tabs, with input and confirmation controls handled locally. |
| `s` or `b` | Open Status without changing Management. |
| `p` | Open Plugins. |
| `e` | Focus the first visible diagnostic group, or its header when New is empty. |
| `d` | Dismiss the selected diagnostic group. |
| `c` | Request read-only observations. |
| Page Up/Page Down | Scroll long content or Hint without changing the selected sandbox or action. |
| Escape | Leave input, cancel confirmation, or return from card controls to list navigation. |
| `?` | Expand or close contextual help in place. |
| `q` | Close the TUI. |

Sandbox filtering matches names and herdr labels.
Herdr plugin filtering matches identifiers and display names.
Mute an empty `/ Filter…` prompt until focused.
Enter accepts a filter, Escape leaves its input, and Ctrl+U clears the input.
Show Clear only for a nonempty accepted query while input is inactive.
Keep each table's query and stable identity selection across tabs and observations.
An empty result says that no rows match and leaves Clear and diagnostics reachable.
Filtering changes visibility only.

Mouse actions activate once on release.
A drag starting on a control selects visible text without activating the control.
Freeze the displayed frame during selection while background state continues to update.
On release, send plain selected text through OSC52 and keep the highlight until the next interaction.
That interaction clears the highlight, renders current state, and performs its usual action.
Escape or resize cancels a drag without closing Details or discarding input.
Preserve Unicode characters, indentation, and visible line breaks while omitting ANSI styling and trailing screen padding.
Report clipboard delivery as a request when terminal support is unknown.

## Diagnostics, events, and recovery

Hide diagnostics only when no active groups remain.
Title the section Errors, Warnings, or Errors and warnings according to its active entries.
Use New and All with occurrence counts in parentheses.
The selected choice is blue and the other is muted.
Move the selector to a separate actionable row when the title is too wide.
Show the number of active occurrences hidden from New.

Group by stable sandbox scope and component code, ordered by readable sandbox name, code, and scope.
Each group has one navigation selection and retains separate herdr plugin causes.
Render the colored code, sandbox name, latest muted time, and muted `×N` when repeated.
Put each muted six-character reference on the next line before its cause.
Keep herdr plugin identifiers bold, with separate output and recovery instructions.
Stable references and group order do not change during polling.

```text
╭─ Errors · New (3) | All (5) ───────────────────────────────────────────╮
│ 2 active occurrences hidden                                           │
│ > P0002 · claude-api · 2026-10-20 12:10:21 ×3                          │
│   example.tools                                                       │
│   f4r8s2 Manual intervention required. Guest build failed.             │
│                                                                       │
│   │  Repair the retained source inside claude-api.                     │
│   │  Then choose Re-sync to check the installation.                    │
│                                                                       │
│   Build output                                                        │
│   <this herdr plugin's output>                                        │
╰───────────────────────────────────────────────────────────────────────╯
```

Repair callouts have a blue line at the left, two spaces of padding, and blank lines around commands.
Only show a verified exact-revision install command when the guest plugin was absent and failure is confirmed before linking.
Otherwise, explain repair of the retained source and show its full path.
Keep commands complete at argument boundaries and allow text selection.
Unknown build failures retain their output without claiming a network policy block.
Confirmed network-policy failures use their distinct code and cause.

One click selects a group, and a second click on that selected group opens recovery.
Enter also opens recovery.
The `d` shortcut dismisses the whole group without a button.
Dismissal persists through repeats, changed causes, new plugin members, and reopening until the whole group recovers.
All retains active dismissed entries and sums their occurrences.
In All, mark dismissed groups with muted `Dismissed · still active` text.
A save failure leaves the group New and shows D0006.
When New is empty, pad `No new diagnostics. Switch to All to see dismissed active entries.` with blank lines and a small indent.
After dismissal in New, select the next visible group, or the preceding group when the dismissed group was last.
If New has no visible groups, focus its New/All header.
In All, keep the dismissed group selected.
When every active diagnostic recovers, return diagnostic focus to the current tab's main content.

Down from the final sandbox in list focus enters Events, then the diagnostics header and visible groups.
Up reverses the route, including when the table has no matching rows.
Left selects New and Right selects All on the diagnostics header.
Tab still changes top tabs there.
Action failures preserve navigation focus and appear in diagnostics immediately, even before persistence catches up.

Events starts collapsed, retains expansion while open, and keeps at most 50 events from the last hour.
Expire old events even without new activity.
Use readable sandbox names, severity-colored codes, and muted times.
Save the sandbox name with each new event so its history stays readable after deletion.
Use the current sandbox name when available, then the saved event name.
Without a known name, show `Unknown sandbox` and a short sandbox ID instead of the internal host-scope hash.
Keep readable host and daemon scope labels.
Color resolved event codes green and retain their recovery details in the event text.
R0004 re-sync outcomes and successful temporary-access cleanup belong to Events.
R0005 and S0002 are amber warnings.
Unconfirmed deletion means that the entry was retained, not that a normally stopped sandbox disappeared.

Choose one saved active diagnostic for each sandbox notification, with errors before warnings and a stable order among equal severities.
Remove terminal control sequences and normalize whitespace before truncating notification text.
Keep the diagnostic reference intact and suppress unchanged repeated notifications.
Notifications do not move keyboard focus or start recovery.

Hint follows the selected sandbox preview and focused control.
For a focused action, its explanation takes precedence over the row status.
For a stopped Enabled sandbox with an entry and confirmed native Enabled state, use this Hint:
`This sandbox is stopped. Its herdr entry is saved. Setup waits until it starts and Management is ON.`
If a native change fails, show the actual settled state and report the operation error separately.
If the catalog cannot be verified, explain that the displayed state is last confirmed and automatic work waits for a fresh observation.
For completed setup, use `This sandbox is registered in herdr. Setup is complete.`
Do not imply a fresh connection check from cached state.
Wrap prose at spaces and preserve full text through scrolling.
Use `plugin` for herdr-sbx in ordinary hints and `herdr plugins` for guest synchronization.

## Narrow layouts, loading, and partial failure

Support all main views from 30 columns by 10 rows.
Keep the current tab and a compact footer pinned, with the body scrolling between them.
At phone widths, use E, Sandbox, and herdr as headings.
Keep the Enabled symbol, sandbox name, and label visible without a management legend.
Compact the tab gaps before shortening visible names.
Keep complete tab names available through navigation and Help.

See the [compact view](screenshots/compact.png).

If `Management OFF Cancelling` and sbx cannot fit together, put gray Cancelling on the line immediately above the footer.
Keep the requested Management choice and sbx grouped at the bottom right, in that order.
Reduce contextual help to its essential shortcuts and keep full Help available.
Never abbreviate Cancelling or replace it with only a spinner.

Long Hints and confirmations join the scrollable body instead of consuming every row above the footer.
Page Up and Page Down keep the full text and controls reachable without changing action selection.
Reveal a focused control after keyboard navigation without resetting an intentional page scroll on ordinary refresh.
Wrap complete action buttons and stack compact herdr plugin values.
Keep full identifiers in Hint so similar prefixes remain distinguishable.
Below 30×10, show a short resize instruction and keep Close available.

Initial loading retains the table headings and shows an animated loading indicator.
Do not flash an empty inventory before observation completes.
A confirmed empty result says `No sandboxes found`.
A failed first observation says `Inventory unavailable` and offers read-only refresh through Status.
Later failures retain known rows, labels, selection, and stale explanations.
An omitted sandbox is not confirmed deleted.
Deletion cleanup requires confirmed identity and Management ON, including for disabled entries.
OFF leaves such entries visible until cleanup becomes eligible.

## Implementation boundaries and acceptance

Reuse the current Bubble Tea root model, custom expanded-row renderer, and shared layout for rendering and mouse targets.
The current dependency pins are Bubble Tea v2.1.0 and Bubbles v2.2.1.
Keep external work in commands and typed result messages, with identity and generation checks for stale responses.
Update handles input and state transitions, while View only renders stored observations and choices.
Keep persistent intent, cancellation, identity checks, and operation serialization in services shared by the daemon and explicit actions.
The panel must not reconstruct these rules from display labels.

Visual validation uses simulated names and isolated fixtures, never normal live sandboxes.
The specification's acceptance scenarios remain the behavior checklist.
The following checks cover their visible outcomes:

1. At 110×32, columns align, the selected sandbox has a preview with a brighter outline when controls have focus, and Management sits immediately before sbx at the bottom right.
2. At 30×10, every tab, Enabled control, action, confirmation, Hint, and diagnostic remains reachable without horizontal clipping.
3. A missing-entry dot invokes Enable twice only when the first attempt finishes, and it never silently toggles hidden Enabled intent off.
4. Enable on a stopped sandbox without an entry stays Disabled with neutral Stopped status, without a warning or a pending enable.
5. Disabled keeps its label, confirmed native disable reveals Remove from herdr, and failed removal retains its label and retry.
6. A removed Disabled entry remains a dot with a blank label after observations and Management OFF/ON.
7. OFF selects immediately, gray Cancelling remains until settled, and failed cancellation preserves the requested choice and diagnostic.
8. ON during cancellation waits visibly, then reads a baseline without overlapping workers or replaying a cancelled intention.
9. Rename and eligible explicit actions work with Management OFF and leave the global choice unchanged.
10. Partial setup failure preserves confirmed entry creation and displays the specific failed step, with separate herdr plugin details.
11. Stale catalog or inventory observations preserve labels and distinguish uncertainty from absence and Stopped.
12. Keyboard, click, and drag preserve their separate actions, including first-click diagnostic selection and second-click recovery.
13. Hint follows the current row or action, with word wrapping and no stale feedback after navigation.
14. Color-disabled rendering retains symbols, state words, focus, counts, cancellation, and actionable error codes.
15. The panel, Help, text status, and JSON consistently use Management, Enabled, entry presence, and Status.
16. Eligible toggles change immediately and show Enabling… or Disabling… through delayed work and intervening polls, including expanded rows.
17. Up/Down previews adjacent sandboxes immediately; Enter or Right focuses the primary action with a brighter outline, and Escape returns to list navigation.
18. Left/Right selects buttons, with Left returning to the list at the first action or plugin controls; Up/Down stays among card controls, and arrows never change tabs.
19. The selected card replaces its row, preserving colored name/runtime/label/status, a top-right herdr version, spaced actions, and muted sandbox → herdr IDs.

Automatic work does not start a stopped sandbox or open guest SSH to it.
Explicitly enabling an existing herdr entry may let Herdr connect and start the sandbox.
Disabling prevents future connections but does not cancel a connection already in progress.

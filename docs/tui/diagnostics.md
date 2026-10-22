# Diagnostics and events

See the [shared interface rules](../tui-spec.md) for navigation, help, styling, and responsive behavior.

When a background refresh fails, status and warnings show `Could not refresh sandbox state: <actual reason>`.
Include the underlying failure instead of a generic catch-up message.

Show one dedicated outlined section when active errors or warnings exist; omit it when there are none.
Title it Warnings, Errors, or Errors and warnings according to its entries, with separate counts when both appear.
Show Events above the active diagnostics section.
Retain at most the 50 most recent events from the past hour; expire older events even when no new event is added.
Its header shows the retained-event count; start collapsed and expand or collapse with Enter, Space, or a mouse click.
Retain its expansion choice while the TUI is open.
Keep the diagnostics section visible when active groups are dismissed.
Start in New (undismissed); its header switches between `New (N)` and `All (M)` (all active), where N and M sum occurrence counts rather than counting distinct entries or groups.
Report the number of active occurrences hidden in New.
Render the selected choice blue and the other muted.
Render the Errors, Warnings, and Events section titles in regular text color only when their actionable header has focus; otherwise use the border color.
The counts reflect the selected view immediately.
Selection and focus do not recolor the section outline.
When New is empty but All has dismissed active entries, place its explanatory message inside the outline with a blank line above and below and a small left indent.
Group active diagnostics by sandbox scope and component code, displaying the readable sandbox name.
Order groups by sandbox name, then code, with scope as the tie-break for duplicate names, so a child failure changing or recovering does not move its still-active group.
Keep distinct herdr plugin causes, six-character references, repair instructions, and build output as separate details inside the group; never concatenate them into one error.
One keyboard selection covers a group.
Render herdr plugin names in bold within diagnostic groups.
Dismissal acknowledges the entire sandbox-and-code group; it does not resolve any underlying failure or remove persisted records.
The group stays hidden from New across polling and reopening, including repeat occurrences, changed details, and newly failing herdr plugins with the same code.
It becomes New again only after every member of that group has recovered and a later failure occurs.
Keep dismissal available for warnings too.
Keep keyboard focus on the current table or control after an action fails; `e` moves focus to active diagnostics.
Record R0004 re-sync outcomes as informational Events only.
Do not put R0004 in active errors or warnings; show any underlying registration or herdr plugin failure with its specific code.
When visible, Down from the last sandbox in list focus enters the Events header, then the diagnostics header, then its visible entries.
Up reverses this path; if the filtered table is empty, Down still reaches these sections.
Enter or Space on the diagnostics header switches New/All; Left selects New and Right selects All there.
Tab/Shift+Tab still cycle the top tabs. `d` dismisses the selected group without a Dismiss button.
Pressing `e` jumps to the first visible group, or its header if New is empty.
Keyboard and mouse controls can reach both sections and the switch.
Render each group with its colored code alone in the header, readable sandbox name when known, latest muted occurrence time, aggregate muted `×N` when more than one occurrence, and separate child causes and recovery guidance below the current tab content; do not open a separate page.
The code color conveys warning or error severity without a repeated word label.
Give every distinct underlying diagnostic a persisted, unique six-character lowercase alphanumeric reference (`[a-z0-9]{6}`).
Show it muted on its detail line immediately before the first line of error text; keep the colored component code in the group header so its position stays steady.
Keep a child's reference across identical repeats, polling, dismissal, and TUI restarts; a changed cause or a new failure after recovery gets a new reference.
Retain it with related history events.
Keep herdr notifications for a failed sandbox short: sandbox name, one reference that points to an active diagnostic, and a one-line plain-language cause, capped at 120 displayed columns.
Shorten a long sandbox name to 32 columns and truncate the cause if necessary, never the reference.
Do not paste full plugin output, repair commands, or combined diagnostics into a notification.
When several diagnostics are active for the same sandbox, select one primary entry deterministically, mention that others remain in the TUI, and send one notification for the sandbox outcome.
If no matching diagnostic was saved, do not claim a reference exists.
Place the group's latest occurrence time after the sandbox name, matching event rows.
Mute event and diagnostic dates and times.
Show a compact muted `×N` after the time only when the group's aggregate occurrence count exceeds one.
Use amber for ambiguous-entry and unconfirmed-deletion warnings; other current and unknown error codes remain red.
An entry association warning opens the affected sandbox with its matching problem explained.
Enter on a selected diagnostic opens its recovery action.
The first mouse click selects a diagnostic group and leaves the user in the diagnostics section.
Only a second click on that same selected group opens its recovery action; clicking a different group selects it first.
An action failure appears in that section even if a daemon-status poll arrives before its durable record is written.
Show persistent recent events and transient hints in their own outlined sections.
Successful temporary-access cleanup is an event, not a Hint; a cleanup failure remains an active diagnostic.

Retain a bounded, durable herdr plugin event history, including failures and recoveries, so a notification remains inspectable after the active error clears.
Known errors have stable codes that also appear in JSON output.
`D0006` identifies a failed diagnostic-dismissal save; the entry stays New when saving fails, and a successful retry clears this failure.
R0004 is an informational event.
R0005 and S0002 are nonblocking warnings; S0002 means deletion of a previously registered sandbox could not be verified, so its herdr machine was kept.
All other current codes represent blocked work or unreliable observations.
A stopped sandbox without an entry is a neutral Disabled condition and does not create an active diagnostic.
The versioned status JSON separates warnings from errors; warning-only state reports zero errors.
Each error includes its scope, a readable explanation, and an available recovery action.
History entries include their code, scope, detail, time, and recovery state.
Show the sandbox name in event entries when it is known, instead of the internal scope and sandbox ID.
Color event codes according to their severity, using amber for warnings.
Preserve useful diagnostic details for unknown errors without exposing credentials.
Repeated identical errors must not flood the interface.
Dismissal does not mark the underlying problem as resolved.
All includes dismissed entries until the problem is actually cleared.

## Repair callouts

Repair callouts have a blue line at the left, two spaces of padding, and blank lines around commands.
Keep commands complete at argument boundaries and available for text selection.
The [plugin recovery rules](plugins.md) determine whether to offer a verified install command or retained-source repair.

## Dismissal focus and event identity

All retains active dismissed entries and sums their occurrences.
In All, mark dismissed groups with muted `Dismissed · still active` text.
A save failure leaves the group New and shows D0006.
When New is empty, pad `No new diagnostics.
Switch to All to see dismissed active entries.` with blank lines and a small indent.
After dismissal in New, select the next visible group, or the preceding group when the dismissed group was last.
If New has no visible groups, focus its New/All header.
In All, keep the dismissed group selected.
When every active diagnostic recovers, return diagnostic focus to the current tab's main content.

Down from the final sandbox in list focus enters Events, then the diagnostics header and visible groups.
Up reverses the route, including when the table has no matching rows.

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


## Acceptance scenarios

- **S10** — Failed or slow diagnostics leave the interface responsive and show unknown or pending status accurately.
- **S11** — Known errors appear with the same stable code in the TUI and JSON status.
- **S14** — Active errors and warnings remain in the outlined diagnostics section; arrow keys move from the sandbox table into its entries and back, while `e` jumps directly there.
- **S23** — Events starts collapsed above diagnostics; keyboard and mouse toggle it, and successful temporary-access cleanup appears there without a lingering Hint.
- **S24** — Dismissing an active sandbox-and-code group removes it from New across polling and reopening while All still counts and shows all occurrences; changed details and newly failing plugins in the same active group stay hidden, but full recovery followed by a new failure appears in New.
- **S25** — Diagnostic and event rows show a severity-colored code without an Error or Warning prefix, a muted date and time after the sandbox name, and a muted `×N` for repeated active occurrences.
- **S26** — If New is empty while dismissed active entries remain, the outlined section and its New/All switch remain reachable by keyboard and mouse with accurate active and hidden counts.
- **S27** — A short herdr notification contains a sandbox name and six-character reference that can be found before the error text in the exact active diagnostic; separate plugin failures have separate references and their full output remains in the TUI.
- **S28** — New/All counts appear in parentheses and sum occurrences. The selected choice is blue and the other muted; moving focus never changes the outline color. Dismiss is available through `d` without a button. When the diagnostics header has focus, Left selects New and Right selects All while Tab/Shift+Tab still cycle the top tabs. Errors, Warnings, and Events titles use regular text color only when their header has focus and border color otherwise. The selected sandbox card has a brighter outline only while its controls have focus.
- **S29** — Two plugins with the same code in one sandbox share one navigable group but retain separate references, details, and repair guidance; its aggregate `×N` and All count equal their summed occurrences.

Diagnostic meanings are defined in the [code catalog](../reference.md#diagnostic-codes).

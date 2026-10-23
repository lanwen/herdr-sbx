# Interactive sandbox manager specification

This document owns shared interaction rules.
Feature documents own their behavior, transition tables, and acceptance scenarios:

| Feature | Specification |
| --- | --- |
| Sandbox state, identity, actions, and expanded rows | [Sandboxes](tui/sandboxes.md) |
| Management permission, worker state, prerequisites, and Refresh | [Status and Management](tui/status.md) |
| Defaults, per-sandbox choices, synchronization, and recovery | [Herdr plugins](tui/plugins.md) |
| Events, active diagnostics, dismissal, and notifications | [Diagnostics](tui/diagnostics.md) |

The [reference](reference.md) owns CLI and external integration contracts.
The [visual companion](tui-design.md) contains examples of these requirements.

## Vocabulary

Management is permission for automatic plugin work.
Enabled is a sandbox's saved connection choice; only a pending eligible action temporarily shows a requested state before verification.
Entry presence is an observed herdr catalog fact, conveyed by the label.
Runtime describes whether the sandbox is running or stopped, independently of its entry and connection choice.
Worker state describes observed daemon activity, independently of the requested Management choice.
Unknown means an observation is unavailable; stale means a retained observation is no longer confirmed current.

## Entry points

Running `herdr-sbx` opens the TUI.
Herdr popup, tab, and split actions open the same interface.
Setup, synchronization, and repair actions are available inside the TUI.

The TUI uses Bubble Tea and Bubbles, with dependency versions pinned in `go.mod`.
Operation services are shared by the TUI and daemon.

## Frame and styling

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


The bottom-right Management indicator stays compact and has no timestamp or observation-refresh star.
Show gray Cancelling there while a requested OFF transition remains unsettled.
Keep specific startup, reconnecting, and error details in Status rather than confusing them with the saved Management choice.

The section’s top border contains clickable Sandboxes, Status, and Plugins tabs.
Their order rotates so the active tab is always leftmost.
The tabs have no background or brackets; the active tab uses the blue accent and inactive tabs are muted.
Tab/Shift+Tab cycle the tabs.
Left selects New and Right selects All while the diagnostics header has focus.
Inputs and dialogs keep local key handling.
Up/Down navigate controls within the active tab.
The Management indicator and the sbx indicator open the Status tab.

## Keyboard reference

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
| Escape | Leave input, cancel confirmation, or return from card controls to list navigation; at the popup's Sandboxes list with no filter, request closing. |
| `?` | Expand or close contextual help in place. |
| `q` or Ctrl+C | Close the TUI; popup closing requires a second quit key within five seconds. Inputs and confirmations keep their local handling. |

### Popup closing

Popup detection follows the [Herdr environment contract](reference.md#commands-and-panel).
Tab, split, and ordinary terminal invocations keep their existing closing behavior.
The popup uses this confirmation sequence:

| Starting state | Action | Immediate feedback | External effect | Settlement and failure |
| --- | --- | --- | --- | --- |
| Popup with no quit prompt; no input or confirmation owns the key | Press `q`, Ctrl+C, or Escape at the Sandboxes list with no active filter | Show `Press esc, q or ctrl+c again to quit` followed by muted `...5s` in the framed Hint area, replacing its displayed contents temporarily | None | Arm closing for five seconds; preserve notice and context data and keep the quit Hint outside the scrolling body at compact sizes. |
| Quit prompt active | Each second passes before the deadline | Count down the muted suffix through `...4s`, `...3s`, `...2s`, and `...1s` | None | Keep the main phrase in the information color and wrap the suffix with it at compact widths; never display zero or negative seconds. |
| Quit prompt active, before its deadline | Press any of Escape, `q`, or Ctrl+C where it requests closing | Remove the prompt; close, or show the existing finishing-work notice | Exit only the manager process after pending work settles | The daemon continues running. |
| Quit prompt active | Press any other key | Restore normal Hint content and perform the key's normal action | Only the key's normal effect | A later quit key starts a new five-second window. |
| Quit prompt active | Five seconds pass | Restore normal Hint content | None | A later quit key starts over, even if the timeout message was delayed. Old timeout messages cannot cancel a newer prompt. |
| Input, confirmation, help, card controls, another section or tab, active filter, or text drag owns Escape | Press Escape | Perform the existing cancel, back, clear-filter, or drag-cancel action | None | Do not arm closing. |

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
The feature specifications define column priorities; shared responsive rules below define the minimum terminal size.

## Filtering and responsive layout

Keep the current tab and compact footer pinned, with the body scrolling between them.
At phone widths, use E, Sandbox, and herdr as headings.
Compact tab gaps before shortening visible names, and keep complete tab names available through navigation and Help.
If Management, Cancelling, and sbx cannot fit together, put gray Cancelling on the line immediately above the footer.
Keep the requested Management choice immediately before sbx at the bottom right.
Never abbreviate Cancelling or replace it with only a spinner.

Enter accepts a filter, Escape leaves its input, and Ctrl+U clears the input.
Show Clear only for a nonempty accepted query while input is inactive.
Keep each table's query and stable identity selection across tabs and observations.
An empty result says that no rows match and leaves Clear and diagnostics reachable.
Filtering changes visibility only.

Render the main views at 30 columns by 10 rows or larger, with tabs, filter, footer, and herdr plugin rows adapted to the available width.
If a Hint or confirmation exceeds the short screen, keep its full text and controls reachable by scrolling.
Page Up and Page Down scroll whenever a long Hint or confirmation joins the viewport, without changing the selected sandbox or action.
If the diagnostics title does not fit, put the New and All selector on a separate actionable line.
Show the complete herdr plugin identifier in Hint when a compact Details or Plugins row has focus, so names with the same prefix remain distinct.
At smaller sizes, show a short resize instruction and keep Close available.

Keep the requested Management choice and sbx grouped at the bottom right, in that order.
Reduce contextual help to its essential shortcuts and keep full Help available.
Never abbreviate Cancelling or replace it with only a spinner.

Long Hints and confirmations join the scrollable body instead of consuming every row above the footer.
Page Up and Page Down keep the full text and controls reachable without changing action selection.
Reveal a focused control after keyboard navigation without resetting an intentional page scroll on ordinary refresh.
Wrap complete action buttons and stack compact herdr plugin values.
Keep full identifiers in Hint so similar prefixes remain distinguishable.
Below 30×10, show a short resize instruction and keep Close available.

## Hints and concurrent operations

Hint follows the selected sandbox preview and focused control.
Explain Enable, Disable, Remove from herdr, and Rename by their effects on the selected sandbox.
Do not require users to understand registration provenance.
Use “plugin” for herdr-sbx in ordinary hints; elsewhere, write “herdr-sbx”.
Write “herdr” in lowercase and use “herdr plugins” when referring to plugins synchronized into sandboxes.
Wrap hint text at spaces so words stay whole; let long hints use multiple lines inside the outline.

Replace a hint when a newer action starts, and clear it when navigation leaves the action's context.

Show pending actions and their final outcome.
Prevent conflicting actions on the same sandbox while a mutation is in progress.
Coordinate saved choices with the background daemon so Disable, removal, Management OFF, and resuming Management cannot race with automatic registration or overwrite the new baseline.
If another reconciliation holds the registration lock, explain that sync is updating registrations and that the requested action did not finish.
Do not imply it succeeded; retain a retry path.
Explicit entry mutations wait for an ordinary short overlap while respecting cancellation and keeping navigation responsive.
Keep the interface usable when one sandbox fails or an external command is slow.

## Acceptance scenarios

- **S09** — Fuzzy filtering, keyboard controls, and mouse controls work in both tables.
- **S20** — A click activates one control once on release; a drag beginning on the same control highlights and copies text without activating it.
- **S21** — Reversed, multiline, and Unicode drag selections copy the displayed plain text while background refreshes leave the selected frame still.
- **S22** — Escape or resize cancels an active drag without changing the focused input, dialog, or expanded sandbox.
- **S39** — UI, help, status output, and JSON consistently use Management, Enabled, entry presence, and Status.
Use isolated herdr sessions and sandbox fixtures for implementation tests.
Include input and state-transition tests for the TUI, plus daemon integration tests for Management, enabled choices, baseline capture, and cancellation.
Check rendering, resizing, mouse behavior, and terminal restoration during visual validation.

Acceptance IDs S01–S46 identify behavior scenarios across documents; V01–V19 identify visual checks.
CLI acceptance is in the [reference](reference.md#commands-and-panel).
## Visual acceptance

- **V01** — At 110×32, columns align, the selected sandbox has a preview with a brighter outline when controls have focus, and Management sits immediately before sbx at the bottom right.
- **V02** — At 30×10, every tab, Enabled control, action, confirmation, Hint, and diagnostic remains reachable without horizontal clipping.
- **V03** — A missing-entry dot invokes Enable twice only when the first attempt finishes, and it never silently toggles hidden Enabled intent off.
- **V04** — Enable on a stopped sandbox without an entry stays Disabled with neutral Stopped status, without a warning or a pending enable.
- **V05** — Disabled keeps its label, confirmed native disable reveals Remove from herdr, and failed removal retains its label and retry.
- **V06** — A removed Disabled entry remains a dot with a blank label after observations and Management OFF/ON.
- **V07** — OFF selects immediately, gray Cancelling remains until settled, and failed cancellation preserves the requested choice and diagnostic.
- **V08** — ON during cancellation waits visibly, then reads a baseline without overlapping workers or replaying a cancelled intention.
- **V09** — Rename and eligible explicit actions work with Management OFF and leave the global choice unchanged.
- **V10** — Partial setup failure preserves confirmed entry creation and displays the specific failed step, with separate herdr plugin details.
- **V11** — Stale catalog or inventory observations preserve labels and distinguish uncertainty from absence and Stopped.
- **V12** — Keyboard, click, and drag preserve their separate actions, including first-click diagnostic selection and second-click recovery.
- **V13** — Hint follows the current row or action, with word wrapping and no stale feedback after navigation.
- **V14** — Color-disabled rendering retains symbols, state words, focus, counts, cancellation, and actionable error codes.
- **V15** — The panel, Help, text status, and JSON consistently use Management, Enabled, entry presence, and Status.
- **V16** — Eligible toggles change immediately and show Enabling… or Disabling… through delayed work and intervening polls, including expanded rows.
- **V17** — Up/Down previews adjacent sandboxes immediately; Enter or Right focuses the primary action with a brighter outline, and Escape returns to list navigation.
- **V18** — Left/Right selects buttons, with Left returning to the list at the first action or plugin controls; Up/Down stays among card controls, and arrows never change tabs.
- **V19** — The selected card replaces its row, preserving colored name/runtime/label/status, a top-right herdr version, spaced actions, and muted sandbox → herdr IDs.

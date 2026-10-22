# Status and Management

See the [shared interface rules](../tui-spec.md) for navigation, help, styling, and responsive behavior.

## Permission and baseline

Management is the global permission for automatic work.
Label the global control Management ON/OFF.
ON permits automatic registration, setup, selected herdr plugin synchronization, native machine enable/disable changes, and confirmed-deletion cleanup.
OFF permits no automatic work, including cleanup, retries, guest probes, and repairs.
Read-only host observations and explicit user actions remain available when Management is OFF.
Turning Management OFF does not itself disable or remove herdr machines.
An existing enabled herdr machine can still connect independently of the plugin.

When Management turns ON, read the current sandbox inventory and herdr catalog before making changes.
Accept current external labels and enabled settings as the new baseline, discarding conflicting saved intentions from the previous management period.
For an existing entry, a verified external enable/disable change takes precedence over its saved choice.
For example, Management OFF, an external rename, and Management ON must preserve the external name.
Do not replay a cancelled rename or enable/disable request over that baseline.
When no entry exists, preserve a remembered Disabled choice rather than treating the sandbox as newly discovered.
Accept a verified native Disabled state on resume regardless of sandbox runtime.
If the current catalog cannot be read, defer mutations and report the uncertainty instead of applying stale intentions.
While Management is ON, reconcile entries to the choices maintained through the plugin.

## Management transitions

| Starting state | Action | Immediate feedback | External effect | Settlement and failure |
| --- | --- | --- | --- | --- |
| ON | OFF | OFF selected; gray Cancelling while work settles | Save OFF, prevent all new automatic work including cleanup, cancel at safe boundaries | Confirm observed Stopped only after worker exit is verified; OFF is selected immediately and completed effects remain. No native disable or removal implied. |
| Cancelling | ON | ON requested; gray Cancelling remains | Wait for previous worker, then read fresh baseline | Launch replacement only after confirmed stop; its automatic mutations wait for a verified baseline. Never overlap workers. |
| OFF | ON | ON requested with observed startup feedback | Read inventory/catalog; accept external labels/native settings and preserve Disabled for absent entries | Resume mutation only after verified baseline; unavailable observations defer work visibly. |
| Stop unconfirmed | OFF or queued ON | Requested choice plus unsettled state and error | Continue cancellation; no replacement worker | Retain D0001 for unconfirmed stop and offer retry; never claim quiescence. |
| OFF | Host observation or eligible explicit action | Normal read or action feedback | Host reads or only the requested action | Keep OFF; no background work. |

Running establishes an active worker event stream, not readiness of every sandbox.

## Controls and cancellation

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

Warnings alone do not put Management into an Error state or offer Refresh.

## Startup prompt

On first use, request one daemon and show the requested ON choice with its observed startup state.
On later opening with management stopped, ask `Start Management?` and explain automatic setup, registration, and selected herdr plugin sync.
Focus Keep OFF initially, with Start Management as the other choice.
Escape means Keep OFF.
Declining leaves observation and explicit actions available.

## Acceptance scenarios

- **S01** — First use starts at most one daemon; later opening with a stopped daemon asks whether to start it.
- **S02** — Closing the TUI leaves the daemon running.
- **S04** — Management OFF, an external rename or native enable/disable change, and Management ON preserve the current catalog state instead of replaying conflicting saved intentions.
- **S13** — Management OFF stops scheduling work immediately, cancels active work where safe, shows gray Cancelling until settled, and then performs no automatic work or cleanup.
- **S33** — Management OFF permits read-only observation and explicit Rename, Enable, Disable, and eligible Remove actions without starting background work.
- **S41** — Management appears at the bottom right immediately before sbx at wide and narrow widths, with correct click targets and gray Cancelling.

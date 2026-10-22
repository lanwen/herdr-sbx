# Sandboxes

See the [shared interface rules](../tui-spec.md) for navigation, help, styling, and responsive behavior.

## State and identity

Each sandbox has a durable Enabled or Disabled choice, separate from Management and entry presence.
Newly discovered sandboxes default to Enabled, except stopped sandboxes without herdr entries.
Those sandboxes remain Disabled until an eligible explicit Enable.
Preserve explicit Disabled choices across refreshes, daemon restarts, and Management OFF/ON unless a verified external change to an existing entry supersedes them on resume.
Default discovery must never reset a Disabled choice after the user removes its entry.

The scope includes all unambiguously identified herdr entries for Docker Sandboxes, regardless of who created them.
Keep stable associations between sandbox identity and herdr machine identity, independent of editable labels.
Reject ambiguous targets, unrelated sessions, identity reuse, and label-only matches as authority for mutation or deletion.
Sandbox registration requires a verified identity association.
Installed guest plugins and temporary policy rules have separate ownership protections.

## Action transitions

These rows define native effects and settlement with Management ON or OFF.
Mutations require verified sandbox identity, plus a verified sandbox-to-entry association when an entry exists.
Unknown or transitional runtime is not Stopped: an unavailable running-state check prevents entry creation and reports uncertainty.

| Starting state | Action | Immediate feedback | External effect | Settlement and failure |
| --- | --- | --- | --- | --- |
| Running; no entry | Enable | Blue checkmark and Enabling…; blank label | Verify running identity, prepare guest, and add entry | Keep verified entry and native choice even if later setup fails; report setup failure separately. |
| Stopped; no entry | Enable | Muted dot and Stopped; no transition or warning | No add, SSH, or start | Disabled with blank label; a later start still requires explicit Enable. |
| Existing entry; Running | Enable | Blue checkmark and Enabling…; label retained | Native enable if needed, then verify catalog | Follow native state; this action itself does not perform guest setup. Automatic setup requires Management ON. |
| Existing entry; Stopped | Enable | Blue checkmark and Enabling… | Native enable; no plugin guest setup or direct start | Follow native state; Herdr may connect and start the sandbox. Stopped observations do not undo Enabled. |
| Existing entry; Running or Stopped | Disable | Muted dot and Disabling…; label retained | Native disable and safe cancellation of guest work | Follow native state; preserve entry and files. A Herdr connection already in progress is not cancelled. |
| Disabled; confirmed natively disabled | Remove from herdr | Removal progress; label retained | Remove verified entry and reread catalog | Confirmed absence clears label and preserves Disabled and live sandbox completion metadata. Retained entry or unreadable verification reports failure or uncertainty. |
| Native disable unconfirmed, or choice Enabled | Remove from herdr | Unavailable | None | Disable and verify first. |
| No entry | Remove from herdr | Unavailable | None | Preserve the existing choice. The missing-entry dot offers Enable. |

### Native outcome verification

| Observation after Enable or Disable | Settlement |
| --- | --- |
| Fresh catalog confirms requested flag, even after command error or lost response | Save and display verified choice; the native toggle succeeded and reports no toggle error. |
| Fresh catalog confirms opposite flag, even after successful command response | Restore actual choice and report failure; no automatic replay. |
| Fresh catalog unexpectedly confirms entry absent after attempted creation or native mutation | Settle Disabled, clear label, and report absence; do not infer authority for duplicate add. |
| Catalog unavailable, native flag missing, or identity ambiguous | Retain last confirmed choice as stale, expose uncertainty, and pause automatic native changes and guest work until verified. |
| Saving the choice fails | Restore saved choice and report persistence failure; no unsaved optimistic display. |
| Older operation completes after a newer accepted choice | Preserve newer choice and transition; stale completion cannot overwrite it. |

Remove and Rename also follow verified results after command errors.
Confirmed removal or a confirmed changed label counts as success; unavailable verification remains uncertain.
Guest setup errors are separate from a successful native mutation.

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
Enable on an existing entry requests native herdr enablement for running or stopped sandboxes.
Do not retain a failed requested choice for later automatic replay.
The Enabled control remains available when Management is OFF because explicit actions are still allowed.
There is no separate Managed column or management abbreviation legend.
Use Status for setup progress and problems preventing use through herdr.
Below 72 columns, hide Status while retaining its full explanation in Details and Hint.
Shorten the herdr label before the sandbox name when space is limited.
Keep the Enabled symbol, sandbox name, and herdr label visible at phone widths with compact headings.

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
Stopped is a normal runtime state and never implies deletion.
Check deletion only for a previously registered sandbox missing from current inventory.
If its absence cannot be confirmed, keep its registration and show a sandbox-specific diagnostic: "Cannot confirm whether this sandbox was deleted.
Its herdr registration was kept." Include the cause when available.
Do not attach that explanation to other sandboxes.

## Sandbox actions

A row control and a keyboard action toggle Enabled and Disabled.
The checkmark or dot has its own mouse target, independent of row or card selection.
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
Place Details action confirmations in the Hint section.
The Hint names the action and shows its confirm and cancel choices.
During an active action, show an animated spinner and the action name in Hint instead of a text phase such as Waiting, Pending, or Executing.
Show its result in Hint; keep coded failures in diagnostics.
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

## Guest observations and recovery

Each catch-up checks the saved machine's catalog identity and sandbox association locally.
An associated machine with completed workspace setup and no pending herdr plugin work does not need a fresh SSH status command.
Automatic guest connections require Management ON and a running enabled sandbox.
A fresh connection is opened only for actual pending guest work, first setup, explicit eligible Re-sync, or a bounded audit.
Do not create entries or open automatic guest connections for stopped sandboxes; retain existing entries and wait until the sandbox runs before guest setup.
A failed new connection reports the cause and affected action without declaring registration failed or advising duplicate registration.
Preserve the existing machine, label, workspace completion, and prior successful observation.
Prior success is historical, not proof of current reachability.
Cache a successfully probed running guest herdr server version per sandbox identity and observation time.
Reuse probes during guest work or the bounded audit, never on TUI rendering or routine polls.
Keep the cached version after a stop or failed probe; call it last known in Hint when it is no longer current.
The observation time remains in status data.
A probe failure alone does not block the sandbox.
Classify a confirmed guest herdr machine-API forwarding incompatibility as H0003, with guidance to use Fix herdr when safe or update the protected installation manually before Re-sync.
Do not infer a running-server version mismatch solely from this error: the executable may be old, missing, or unable to run.
Use H0004 for a failed explicit Fix herdr attempt while preserving the last confirmed guest version.
Keep other workspace failures as R0006.
Parse Herdr's structured error code and message when present, and extract a plain cause from its known textual transport wrapper without showing Rust debug syntax.
Do not automatically update or restart herdr from parsed wording.
When sbx refuses the new SSH connection because the sandbox's saved host workspace path is missing, the herdr plugin diagnostic says that herdr-sbx was trying to inspect or sync the herdr plugin and that sbx blocked the connection before guest work began.
Show the missing path once, without nested error prefixes or sbx's sandbox-removal suggestion.
Tell the user to check or restore that path before Re-sync; keep the existing herdr machine.

For project workspace paths and guest preparation, see the [operational reference](../reference.md#daemon-and-recovery).
Pending temporary-policy cleanup blocks registration.

## Action hints and rename input

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

Enter submits and Escape cancels an unsubmitted edit.
The input scrolls horizontally to keep its cursor visible.
Use a block cursor during rename and highlight Rename in sand with an Enter-to-save symbol.
Left and Right keep their text-editing meaning until the edit ends.
A failed save retains the proposed text and cursor for correction.
Observations do not overwrite active input.
If navigation is blocked by an edit, Hint says `Renaming <sandbox>: save with Enter or cancel with Esc before leaving this input.`
Cancellation after submission preserves the actual observed label, rather than claiming that the previous label was restored.

Show ambiguous-entry guidance in Hint while its sandbox is selected, instead of in the details body.

For a registered sandbox with completed setup, Hint says: "This sandbox is registered in herdr.
Setup is complete." Do not imply that a fresh connection was checked when status came from saved registration and sync state.
Show the selected sandbox's saved status explanation in Hint instead of its expanded Details body, including failed and stopped states.
Keep the specific ready and ambiguous-entry wording; color other explanations by the observed state so the cause stays visible when details are closed.
An empty explanation adds no Hint.
For a stopped enabled sandbox with an entry and a confirmed native Enabled state, Hint says: "This sandbox is stopped.
Its herdr entry is saved.
Setup waits until it starts and Management is ON."
If a native change fails, show the actual settled state and report the operation error separately.
If the catalog cannot be verified, explain that the displayed state is last confirmed and automatic work waits for a fresh observation.
A recovered deletion check must clear its former explanation.

If a submitted operation is cancelled, display its actual completed effects and unfinished work.
Cancelling a local wait is not proof that an external operation stopped.


## Inventory loading

Initial loading retains the table headings and shows an animated loading indicator.
Do not flash an empty inventory before observation completes.
A confirmed empty result says `No sandboxes found`.
A failed first observation says `Inventory unavailable` and offers read-only refresh through Status.
Later failures retain known rows, labels, selection, and stale explanations.
An omitted sandbox is not confirmed deleted.
Deletion cleanup requires confirmed identity and Management ON, including for disabled entries.
OFF leaves such entries visible until cleanup becomes eligible.


## Deletion and name reuse

After a sandbox is confirmed deleted, Management can clear its retained local metadata when its herdr entry was already removed, allowing a new sandbox to reuse the name.
A verified replacement identity permits only that local cleanup; it never authorizes removal of the replacement's herdr entry.
Unconfirmed creation and uncertain absence retain their safeguards, and Management OFF suspends this cleanup too.
Native herdr state determines how an explicit toggle settles, independently of command errors or later guest setup failures.

| Sequence or trigger | Required result |
| --- | --- |
| Disable → Remove entry → refresh or restart while sandbox still exists | Keep Disabled and do not automatically recreate its entry. |
| Removed-entry sandbox confirmed deleted with Management ON | Clear retained local metadata without a machine command or guest-file deletion. |
| Name reused by a verified different sandbox identity | Clear only dead, nonpending metadata with no tracked native profile; preserve replacement and its herdr entry. |
| Creation unresolved, absence unknown, or old native profile still tracked during name reuse | Retain specific uncertainty; name reuse never authorizes native deletion. |
| Management OFF | Suspend automatic local and native cleanup until Management resumes. |

Verify this sequence through deletion events and periodic recovery.

## Acceptance scenarios

- **S03** — Fresh discovery defaults to Enabled, except stopped sandboxes without entries remain Disabled; matching manually created entries are included automatically.
- **S05** — Disable retains the entry and label; subsequent Remove from herdr removes only the entry and preserves Disabled across refresh, daemon restart, and Management OFF/ON.
- **S06** — Inline rename survives reconciliation; Escape before submission leaves the previous label unchanged, while cancellation after submission preserves the actual catalog result.
- **S07** — Up/Down immediately previews the newly selected sandbox and collapses the previous card; Enter or Right focuses its primary action, and Escape returns to list navigation without closing the preview.
- **S15** — Enable, Disable, Rename, and Remove from herdr hints explain their effects, wrap at whole words, and remain usable with Management OFF.
- **S30** — A missing entry and a disabled entry both use the muted dot; only the herdr label indicates entry presence, and the enabled symbol uses the existing blue accent.
- **S31** — Discovery and Enable for a stopped sandbox without an entry stay Disabled with neutral Stopped status, no warning, no pending Enable, no machine add, and no SSH or sandbox start.
- **S32** — Enable after explicit removal restores an entry for a running sandbox; a stopped sandbox remains Disabled without a warning.
- **S34** — A sandbox deleted while Management is OFF retains its entry until Management resumes and deletion is confirmed.
- **S35** — Unknown inventory, ambiguous identity, label collisions, or name reuse never authorize entry removal; an external label change does not break identity association.
- **S36** — A plugin-sync failure or cancellation after entry creation retains the actual entry and label, and distinguishes completed setup from pending or failed work.
- **S37** — Enabling a manually added, disabled entry for a stopped sandbox enables it in herdr with Management ON or OFF; subsequent stopped observations do not disable it again.
- **S38** — Isolated lifecycle coverage verifies that automatic work does not start, connect to, or natively enable a stopped sandbox; explicit native Enable is allowed, and baseline capture accepts verified native Disabled.
- **S40** — Eligible Enable and Disable update the displayed choice immediately, retain Enabling… or Disabling… through stale polls, and settle to the actual native outcome; rejected changes revert, lost responses follow confirmed effects, and unavailable observations retain a stale last-confirmed display without automatic replay.
- **S42** — A fresh unambiguous observation clears resolved association warnings even while Management is OFF, while true ambiguity retains a specific explanation.
- **S43** — Right enters details from the list; Left selects the preceding action or returns to the list from the leftmost action or plugin controls; Tab/Shift+Tab changes tabs.
- **S44** — Inside a card, Up/Down remains among its actions and plugin controls, with a brighter outline; Space invokes the primary Enable/Disable action and Enter invokes the focused button.
- **S45** — The selected card replaces its table row, preserves the colored title with inline runtime and status, shows the version at top right, and places spaced action buttons before muted sandbox → herdr IDs and plugin controls.
- **S46** — The primary button includes `(space)`, and contextual help names `enable` or `disable` according to the current selected sandbox.

# Herdr plugins

See the [shared interface rules](../tui-spec.md) for navigation, help, styling, and responsive behavior.

## Sandbox overrides

For each sandbox, herdr plugin choices cycle Inherit → On → Off when activated with Enter, Space, or a click.
Default choices toggle On/Off directly without a dialog.
Show the new choice immediately after activation.
Show a loader in Effective while the save is pending, then show the effective value when it finishes.
Show both the effective value and whether it comes from a default or an override.
Provide a way to clear an override and return to Inherit.
Overrides persist independently of the sandbox runtime state.

Enter, Space, or a click cycles Inherit → On → Off.
The `i` key returns a focused override to Inherit.
Show Choice immediately, with a loader in Effective until persistence completes.
Then show the effective On or Off and whether it comes from a default or override.
A save failure restores the previous saved choice and shows its diagnostic.
A guest sync failure keeps the successfully saved choice.

## Plugin defaults view

Provide a separate herdr plugin table in the Plugins tab, reachable with `p` or tab navigation.
Each row identifies a herdr plugin and shows its default synchronization choice, On or Off.
Show availability or synchronization problems alongside the affected herdr plugin.
Toggling a default changes the effective value for all inheriting sandboxes.
Explicit sandbox overrides remain unchanged.
Newly discovered herdr plugins default to Off, preserving the current opt-in behavior.

When Management is ON, changes to herdr plugins apply to running enabled sandboxes.
With Management OFF, save the choices without scheduling guest work.
After a successful sync, save the effective desired state for each sandbox and herdr plugin, including the source snapshot revision and copied shortcut settings.
Routine catch-up compares this state locally and does not open a new guest connection or inspect the guest plugin when nothing changed.
Recheck after a relevant choice, source, shortcut, sandbox identity, or lifecycle change, an explicit Re-sync, and a periodic audit no more often than every 15 minutes.
A failed connection for pending work keeps the associated registration and completed workspace state; it does not trigger another machine add or repeat the same failed operation on each catch-up.
Stopped enabled sandboxes receive the changes when they next run with Management ON.
Turning synchronization off stops future installation and updates but preserves installed herdr plugin files and shortcuts.
Show one plain explanation for the selected herdr plugin default, reflecting its current On or Off value.
For On, state that the matching herdr plugin version will be copied to enabled sandboxes while Management is ON.
For Off, state that the herdr plugin will not be synchronized and existing copies stay intact.
Mark herdr plugins Unsupported only when their source cannot be prepared safely or they do not support Linux, with the reason below their row.
The [guest plugin contract](../reference.md#guest-plugins) defines source preparation, transfer, manifest builds, revision identity, and ownership.
Report the failed build step and retain useful output without exposing secrets.
After a failed attempt, do not keep rebuilding it in the background; show **Manual intervention required** with a Re-sync action.
Show one active diagnostic per failed herdr plugin and sandbox, with its own code, cause, occurrence count, and recovery history.
Keep the plugin ID visible beside the sandbox name so failures never merge into one paragraph.
Place actionable repair guidance above muted build output using the [diagnostic callout style](diagnostics.md#repair-callouts).
Show a verified manual command in full at argument boundaries for direct text selection only when the guest plugin was absent and the failed build is known to precede linking.
If linking may have happened or no verified command exists, explain retained-source manual repair and show its full path without inventing a command.
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
## Choice failures and unsupported sources

Enter, Space, or a click toggles a default directly.
New herdr plugins default to Off.
Mark unsupported sources Unsupported and show the actual reason below the row.
An unsupported Off choice cannot become On, but an existing On choice can become Off.
No custom bundle configuration is present.

For the selected On default, show `The matching herdr plugin version will be copied to enabled sandboxes while Management is ON.`
For Off, show `The herdr plugin will not be synchronized.
Existing copies stay intact.`
While Management is OFF, changes save choices without scheduling guest work.
A stopped enabled sandbox waits until it runs with Management ON.
At narrow widths, stack Choice and Effective below the identifier and show the full focused identifier in Hint.

## Acceptance scenarios

- **S08** — A changed herdr plugin default affects inheriting sandboxes while explicit overrides retain their values.
- **S16** — A plugin with a Linux manifest build command transfers its exact source revision, excludes ignored files, runs the guest build once, and becomes available after herdr links it.
- **S17** — A failed guest build remains visible as Manual intervention required; Re-sync accepts a matching manually installed revision before attempting another build.
- **S18** — A build blocked by sandbox network restrictions has a distinct cause, while ambiguous failures retain their original diagnostic output.

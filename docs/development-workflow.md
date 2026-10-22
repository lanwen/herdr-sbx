# Development workflow

Use the [reference](reference.md#development) for package navigation, commands, and isolation requirements.
The [TUI specification](tui-spec.md) owns interface behavior; the reference owns CLI and external contracts.

## Assign an outcome

Give each focused agent a compact assignment with these fields:

| Field | Required content |
| --- | --- |
| Outcome | The observable result or concrete question to resolve. |
| Reference | Authoritative feature section or transition rows, plus relevant code entry points. |
| Ownership | Exact files or module to edit; identify concurrent editors. Review tasks are read-only. |
| Acceptance | Cases that must work, including relevant failures and lifecycle sequences. |
| Evidence | Focused tests, contract sources, rendered frames, or other observations needed to demonstrate completion. |

Assign each file to one editor at a time.
Parallelize independent reads, then pass resolved findings to the implementer before dependent edits.
Reuse an agent while its context remains relevant; give a fresh reviewer a focused brief and the change to review.
An implementer returns changed files, observed test results, unresolved findings, and the next required action.

## Resolve behavior before implementation

For changes crossing Herdr or sandbox boundaries, the Herdr expert first resolves the required external operations and their outcomes.
Distinguish documented guarantees, observed behavior in an installed version, and unresolved assumptions.
The main agent records the agreed behavior in the owning feature specification using this table shape:

| Starting state | Action | Immediate feedback | External effect | Settlement and failure |
| --- | --- | --- | --- | --- |
| Relevant observed and saved facts | User action or automatic trigger | Choice and transition shown | Operation and preconditions | Verified result, rejection, and unavailable observation |

Cover present and absent entries, running and stopped sandboxes, and Management ON and OFF where relevant.
Include command errors, confirmed effects despite a lost response, unavailable verification, and persistence failure where they affect the change.
Resolve an unknown contract before implementing dependent behavior; continue independent work while the main agent resolves it.
Link assignments to the authoritative rows instead of copying a second behavior specification into each message.

After implementation, independently review affected lifecycle sequences as well as individual operations.
For association changes, include Disable → Remove entry → delete sandbox → reuse name, and both event-driven and periodic recovery.
The main agent resolves contradictions between requirements, code, and documents before acceptance.

## Review and verify

Domain review checks external effects and lifecycle outcomes against the agreed contract.
UI review checks changed interactions and complete rendered frames against the feature specification.
Standards review checks the actual change and affected callers against the coding standards and existing module boundaries.
Assign read-only review explicitly, including when an implementer role performs it.
Reviewers return concrete defects with triggers, evidence, and missing behavior tests; they also state when no defects were found and what remains unverified.
The assigned editor makes corrections.

Use the smallest review scope that covers the change.
A visual correction usually needs implementation, rendered-output review, and one verification handoff; request domain review when external behavior changes.
The main agent resolves findings and checks integration without repeating each specialist's investigation.

Give legwork stable changes and the required verification scope after edits finish.
Legwork records the revision and working-tree changes being verified, runs the repository tasks through successful stages, and reports failures with their stage and relevant output.
If the assigned source changes during verification, identify the affected results and rerun only the checks invalidated by those changes.
Routine retries are limited to diagnosed environment failures with unchanged inputs and safely repeatable effects.
Check the actual outcome of an interrupted publication before deciding whether another attempt is needed.
Product failures and changed screenshot meaning return to the responsible editor; they do not authorize changed test expectations.

UI acceptance includes a successful build of `bin/herdr-sbx` after the final source edit.
Report the executable path and whether the panel must be reopened to load it.
Rebuilding does not replace a running panel or daemon; keep live reload, restart, and relink separate from isolated verification.
Commit only the exact files assigned by the main agent after review and required checks pass.

## Visual artifacts

Use the shared fixture in `internal/panel/screenshots_test.go` for screenshot data.
Keep realistic sandbox UUIDs, herdr IDs, public project names with `claude-` or `codex-` prefixes, and `sbx · ` labels.
Use independent neutral identifiers for herdr plugins.
Long labels and IDs must exercise wrapping at compact widths.

Before capture, agree on the changed views, fixture values, terminal sizes, and output paths.
Inspect complete frames after rendering changes, including compact details and Status, both focus states where affected, and a contrasting terminal background to expose missing surface colors.
Preserve ANSI regression assertions for foreground, background, and text attributes; generating a PNG alone does not establish visual correctness.
Capture and publication are separate tasks.
Publish only reviewed images with `gh --attach`, preserve the approved description, and verify the resulting attachment links.

## Final acceptance

The main agent confirms that requirements, verified external outcomes, rendered UI, built executable, authoritative documents, and requested PR artifacts agree.
Report material limitations and checks that were not run.

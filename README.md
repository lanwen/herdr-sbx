# lanwen/herdr-sbx

The display name is `lanwen/herdr-sbx` and the plugin ID is `lanwen.herdr-sbx` (Herdr IDs do not allow slashes).

If replacing the former `herdr.docker-sandboxes` link, unlink that ID before linking this checkout again. The new ID uses fresh plugin state. Existing Herdr machine profiles remain in the catalog; remove obsolete sandbox profiles before registering them again, because the new plugin will not adopt untracked machines.

This Herdr plugin checks local Docker Sandboxes prerequisites, installs missing guest Herdr, and keeps owned machine entries aligned with sandbox lifecycle changes.

Build the plugin with:

```sh
mise install
mise run deps
mise run build
herdr plugin link .
```

Mise pins the Go version and provides development tasks. Herdr's plugin build still uses `go build` directly, so linking needs Go on `PATH` but does not require mise.

Open **lanwen/herdr-sbx status** with `herdr plugin action invoke open-status --plugin lanwen.herdr-sbx`. The default action opens a 110-column by 32-row popup (subject to Herdr’s terminal-size limits). Tab and split actions remain available as `open-status-tab` and `open-status-split`. The panel's **Check again** command only observes local binaries, daemon status, sandbox inventory, Docker sign-in through the Authentication result of `sbx diagnose --json`, and the effective SSH configuration. Diagnostics may take up to 35 seconds; unavailable or inconclusive results remain UNKNOWN. An unrelated diagnostic failure does not invalidate a successful authentication check. Diagnostics are not uploaded. It does not start the daemon, sign in, configure SSH, connect to a sandbox, or install software.

The panel offers **Start daemon**, **Sign in to Docker**, and **Set up sandbox SSH** as separate, interactive terminal actions. Each runs only after you choose it, then refreshes the status panel. The plugin does not store Docker credentials or SSH keys.

The registration action is explicit. It checks a running sandbox through its managed SSH alias. If guest Herdr is missing, it downloads the Linux binary on the host using the official release manifest, verifies its SHA256 checksum, streams it over SSH, and then verifies compatibility before `herdr machine add`. Existing incompatible or broken guest installations are reported and left untouched. It records its owned profile before and after the catalog change.

For sandboxes that report a project workspace, registration resolves the actual working directory inside the guest and creates one Herdr project workspace without changing focus or terminal defaults. A matching existing workspace is reused. This also applies to previously registered plugin-owned machines. Completion is saved, so closing the workspace later does not cause it to reappear. Missing project metadata leaves workspace setup unchanged; failed or uncertain setup is shown without losing machine ownership.

Machine labels use `sbx · <sandbox-name>`. Reconciliation updates previously owned machine labels when their sandboxes are running; manual or modified profiles remain protected by ownership checks. To give these labels a sand color in agent rows, merge this rule into your Herdr configuration:

```toml
[ui.sidebar.agents]
rows = [
  ["state_icon", { token = "machine", rules = [{ starts_with = "sbx · ", fg = "#D8C3A5" }] }, "workspace", "tab"],
  ["agent"],
]
```

This styles the machine token in agent rows; Herdr's machine selector uses its own theme colors.

Normal installation uses the guest architecture, needs no guest download access or restart, and never adds policy allowances. Host downloads and SSH transfer must succeed. Press **t** to select a sandbox and retry a failed installation.

The optional guest-download strategy remains available through **n**, Enter in the panel: **Temporarily allow downloads and install Herdr**. Choose a running sandbox by number and type `allow` after reviewing its ID and destinations. This one attempt permits HTTPS GET requests for the installer, manifest, and release downloads. Every process in that sandbox shares the temporary access. The plugin removes only rule IDs returned as newly created by its request; existing rules and denies remain untouched. Background installation and ordinary retry never add policy allowances.

Cleanup runs after success, failure, or cancellation and before machine registration. A crash can leave temporary access active until the next panel opening, registration, or worker activation. Pending cleanup is shown in the panel and blocks new registration. Before removing an owned rule, cleanup verifies its scope, decision, HTTP targets and methods still match the created request. A changed rule needs manual inspection. The policy API has no conditional deletion, so an edit racing between verification and removal cannot be prevented. If a create response was lost, the plugin cannot prove ownership of a matching rule: inspect the reported sandbox ID and destination manually. It never removes a rule merely because its destination matches. Installation has a single internal strategy entry point; future installation approaches can replace it without changing discovery or registration.


Use **Watch lanwen/herdr-sbx lifecycle** to open a foreground watcher. It registers a started, prepared sandbox after rechecking its stable identity. Stopped sandbox registrations remain available for ordinary SSH reconnects. A deletion removes only the matching profile that the plugin recorded as its own. Press Ctrl-C to stop the watcher.

Press **b**, Enter in the panel to start the background worker, or run `herdr plugin action invoke start-worker --plugin lanwen.herdr-sbx` from a Herdr terminal.

**Start lanwen/herdr-sbx worker** starts one detached lifecycle worker. It reconnects after a sandbox daemon interruption, tolerates a short local server handoff, and exits when the plugin is disabled or the final local server remains stopped. Closing the panel does not stop it. If the worker process crashes, use this action again; Herdr does not supervise its restart.

The panel shows **Worker: STARTING / RUNNING / RECONNECTING / STOPPED** and the last successful lifecycle sync or catch-up time. Press **c**, Enter or reopen the panel to refresh it. Running means the event stream is active; individual sandbox readiness is shown separately. Reconnecting includes the latest failure reason. The worker lock detects exits even if the process could not save its final status. An older active worker without status reporting shows **UNKNOWN** until restarted. Foreground watchers do not change the background worker indicator.

The status panel separates the last sandbox observation from connection and installation results. Old results are marked stale. **Retry Herdr preparation** lets you choose a sandbox from a numbered list and retries it without waiting for the automatic retry cooldown; **Check again** remains observational.

Run isolated checks with:

```sh
mise run test
mise run check       # race tests and go vet
mise run test-panel  # fake sbx, Herdr and SSH commands in a PTY
mise run test-worker # two temporary Herdr servers and a fake sandbox daemon
```

Run `mise run deps` first. The Go and panel test tasks reuse downloaded modules with network fetching disabled. The Go test wrapper gives each run its own HOME, XDG directories and Herdr session/state, clearing inherited Herdr connection settings. Worker tests need a local Herdr binary and create their own temporary sessions. Neither task uses the normal sandbox daemon. `mise run test-real` is separate and requires the disposable authenticated fixture described below; `mise run check` never invokes it.

For a focused test, use `mise exec -- python3 scripts/test-go.py test ./internal/registration -run TestLabelMigration -count=1`. Set `GOCACHE` to a writable cache directory if the system default is unavailable. `mise run fmt` formats the source; `mise run build` updates the binary used by the linked plugin.

`scripts/test-real.sh` is an opt-in real integration gate and requires Python 3. It starts a temporary Herdr server and catalog, then requires a dedicated Docker Sandboxes test app containing exactly one disposable prepared sandbox. Both names must begin with `herdr-sbx-test-`. Set `HERDR_SANDBOX_TEST_ROOT` to a new path under `/tmp`, `HERDR_SANDBOXES_SBX_APP_NAME`, `HERDR_SANDBOX_TEST_NAME`, and `HERDR_SANDBOX_TEST_SSH_CONFIG` to an existing dedicated SSH config with that app's aliases and absolute paths to its keys. The harness copies this config and isolates SSH known-hosts and runtime state. It does not create or repair the Docker test environment. Live validation is incomplete until this gate runs against an authenticated test environment.

Set `HERDR_SANDBOX_TEST_LIFECYCLE=1` only for a disposable test sandbox. While the same isolated test server is alive, the gate records its UUID and profile ID, stops it, proves the profile remains, reconnects through that exact profile, rechecks the UUID, removes the named sandbox, and waits for the worker to remove that owned profile. Set `HERDR_SANDBOX_TEST_EXPECT_MISSING=1` to require that guest Herdr is absent before the first reconciliation and test installation followed by reuse. The harness does not create that guest fixture or test same-name recreation; those require dedicated environment inputs.

`scripts/test-worker-lifetime.sh` exercises two isolated local Herdr servers with a test-owned fake sandbox daemon. Set `HERDR_WORKER_TEST_ROOT` to a new path under `/tmp`; it never starts or repairs Docker Sandboxes.

`GO_BIN="$(go env GOROOT)/bin/go" python3 scripts/test-panel-pty.py` exercises the actual panel process in its own terminal with simulated prerequisite commands. It does not test Herdr's overlay or focus handling.

The plugin declares macOS and Linux hosts, but the checked local evidence is limited to the isolated macOS process and worker gates. Real sandbox, Linux, guest architecture, terminal overlay, focus, and visual checks require their dedicated environments and remain unverified.

Temporary policy tests use isolated Unix-socket peers and cover registration after cleanup, blocked downloads, pre-existing and concurrent rules, cancellation, uncertain creation, cleanup recovery, and sandbox identity changes. Authenticated live policy behavior remains unverified; these supplemental tests do not satisfy the real sandbox release gate.

For the isolated real harness, `HERDR_SANDBOX_TEST_TEMPORARY_ACCESS=1` explicitly authorizes the panel's temporary-access action for the named disposable test sandbox only. It requires `HERDR_SANDBOX_TEST_EXPECT_MISSING=1` and a prepared fixture that returns HTTP 403 for installer downloads. The gate verifies the guest download is blocked, drives the explicit confirmation flow, checks cleanup and policy preservation, and reuses the resulting machine. Ordinary registration uses host transfer; its gate checks that policy remains unchanged. Do not use the normal daemon or a working sandbox as this fixture. This opt-in gate has not been run.

## Code navigation

Start with the package that owns the behavior you want to change:

| Change | Location |
| --- | --- |
| Commands, arguments, help and exit handling | `cmd/herdr-sandboxes` |
| sbx CLI, daemon inventory/events and policy protocol | `internal/sbx` |
| Herdr machine, workspace, session and plugin operations | `internal/herdr` |
| Process execution, deadlines and environment handling | `internal/process` |
| Numeric tool-version parsing and compatibility comparison | `internal/version` |
| Passive prerequisite stages and their order | `internal/precheck` |
| Registration, guest preparation, labels and initial workspace | `internal/registration` |
| State journals, file locks, cooldowns and observations | `internal/store` |
| Event reconciliation, deletion and worker lifetime | `internal/lifecycle` |
| Panel rendering, sandbox selection and explicit setup actions | `internal/panel` |

Commands call the panel, checks, registration or lifecycle packages. Those packages use the sbx and Herdr clients for external communication and the store for durable state. The two clients do not depend on each other. Each prerequisite stage has its own file under `internal/precheck`; `inspect.go` shows the check order.

Tests live beside the code they exercise. Transport tests use fake command execution or local HTTP peers; workflow tests cover ownership and interruption recovery. Process and shell tests exercise installation and cancellation in temporary directories. The scripts under `scripts/` cover the panel, worker lifetime and the separately gated real sandbox fixture.

### Worker status from the terminal

Run `./bin/herdr-sandboxes status` from this checkout to print the worker state, last successful sync and connection detail. It does not contact Docker, start a worker or check prerequisites. A stopped worker is a valid result (exit 0); unreadable state returns an error (exit 1).

The command uses `HERDR_PLUGIN_STATE_DIR` when provided, otherwise `${XDG_STATE_HOME:-$HOME/.local/state}/herdr/plugins/lanwen.herdr-sbx`. For a custom Herdr state location, pass its plugin state directory explicitly. The same command is exposed as `herdr plugin action invoke status --plugin lanwen.herdr-sbx` after re-linking the updated manifest.

For a new guest server, registration resolves the sandbox project directory and starts Herdr there before adding the machine. It verifies the initial workspace instead of creating a second one. Existing running sessions keep their workspaces. An interrupted startup retains its intended directory for retry; a guest that cannot honor it reports a setup error rather than silently creating another workspace.

To verify this with an isolated local Herdr server: `SBX_TEST_HERDR_BINARY="$(command -v herdr)" mise exec -- python3 scripts/test-go.py test ./internal/herdr -run TestProjectBootstrapRealServer -v -count=1`. The test uses temporary configuration and state, never the normal session.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestStatusReportsUnderlyingFailureWithoutResyncOutcomeAsError(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, store.NotePreparationFailure("sandbox", "guest build failed", time.Minute))
	require.NoError(t, store.RecordError(store.CodePreparation, "sandbox", "Re-sync did not finish"))
	require.NoError(t, store.RecordError(store.CodeRegistration, "sandbox", "guest build failed"))
	report, err := readStatusReport()
	require.NoError(t, err)
	require.Len(t, report.Errors, 1)
	require.False(t, report.Errors[0].Code != store.CodeRegistration)
	require.Len(t, report.Warnings, 0)
}

func TestJSONStatusIsReadOnlyWithNoToolsOrState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", root)
	t.Setenv("PATH", t.TempDir())
	cmd := commands()
	var out bytes.Buffer
	cmd.Writer = &out
	require.NoError(t, cmd.Run(context.Background(), []string{"herdr-sbx", "status", "--json"}))
	var report statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	require.EqualValues(t, 4, report.SchemaVersion)
	require.EqualValues(t, "stopped", report.Daemon.State)
	require.NotNil(t, report.Sandboxes)
	require.NotNil(t, report.Errors)
	require.NotNil(t, report.Warnings)

	_, err := os.Stat(root)
	require.True(t, os.IsNotExist(err), "status created state: %v", err)

	require.NotContains(t, out.String(), "\x1b", "JSON contains styling")
}

func TestStatusReportsSavedChoicesLabelsAndErrors(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	require.NoError(t, store.ObserveSandbox("scope", sbx.Sandbox{ID: "id", Name: "demo", Status: "stopped"}))
	require.NoError(t, store.SetEnabled("scope", "id", store.Disabled))
	require.NoError(t, store.UpdateStatusStore(func(saved *store.StatusStore) {
		row := saved.Sandboxes[store.StatusKey("scope", "id")]
		native := false
		row.Label, row.ProfileID, row.EntryPresent, row.NativeEnabled = "My workspace", "machine", true, &native
		saved.Sandboxes[store.StatusKey("scope", "id")] = row
	}))
	require.NoError(t, store.RecordError(store.CodeRename, "scope/id", "Rename failed"))
	report, err := readStatusReport()
	require.NoError(t, err)
	require.Len(t, report.Sandboxes, 1)
	require.False(t, report.Sandboxes[0].Enabled != store.Disabled)
	require.EqualValues(t, "My workspace", report.Sandboxes[0].Label)
	require.Len(t, report.Errors, 1)
	require.False(t, report.Errors[0].Code != store.CodeRename)
	var out bytes.Buffer
	require.NoError(t, printStatus(&out, false))
	require.Contains(t, out.String(), "My workspace")
	require.Contains(t, out.String(), store.CodeRename)
	require.NotContains(t, out.String(), "\x1b")
}

func TestStatusPreservesMissingEntryIntentAndUnknownNativeState(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	require.NoError(t, store.ObserveSandbox("scope", sbx.Sandbox{ID: "id", Name: "demo", Status: "stopped"}))
	require.NoError(t, store.SetEnabled("scope", "id", store.Disabled))
	report, err := readStatusReport()
	require.NoError(t, err)
	row := report.Sandboxes[0]
	require.False(t, row.EntryPresent)
	require.EqualValues(t, "", row.Label)
	require.Nil(t, row.NativeEnabled)
	require.False(t, row.Enabled != store.Disabled)
	var out bytes.Buffer
	require.NoError(t, printStatus(&out, false))
	require.Contains(t, out.String(), "unknown")
	require.Contains(t, out.String(), "Disabled")
}

func TestDaemonStatusIsReadOnlyAndExactRootSubset(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", root)
	t.Setenv("PATH", t.TempDir())
	run := func(args ...string) map[string]json.RawMessage {
		t.Helper()
		cmd := commands()
		var out bytes.Buffer
		cmd.Writer = &out
		require.NoError(t, cmd.Run(context.Background(), append([]string{"herdr-sbx"}, args...)))
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(out.Bytes(), &fields))
		return fields
	}
	daemon := run("daemon", "status", "--json")
	require.Len(t, daemon, 6, "unexpected daemon fields: %v", daemon)

	_, err := os.Stat(root)
	require.True(t, os.IsNotExist(err), "daemon status created state: %v", err)

	require.NoError(t, store.SetWorkerControl(true))
	require.NoError(t, store.RecordError(store.CodeDaemonStop, "daemon", "Stop failed"))
	require.NoError(t, store.RecordError(store.CodeRename, "scope/id", "Rename failed"))
	require.NoError(t, store.RecordError(store.CodeOwnershipConflict, "scope/id", "Manual machine remains usable"))
	daemon = run("daemon", "status", "--json")
	full := run("status", "--json")
	for key, value := range daemon {
		if key == "read_at" || key == "errors" || key == "warnings" {
			continue
		}
		require.True(t, bytes.Equal(value, full[key]), "%s differs: daemon=%s root=%s", key, value, full[key])
	}
	var daemonErrors, fullErrors []store.ErrorRecord
	require.NoError(t, json.Unmarshal(daemon["errors"], &daemonErrors))
	require.NoError(t, json.Unmarshal(full["errors"], &fullErrors))
	require.Len(t, daemonErrors, 1)
	require.False(t, daemonErrors[0].Code != store.CodeDaemonStop)
	require.Len(t, fullErrors, 2)
	var daemonWarnings, fullWarnings []store.ErrorRecord
	require.NoError(t, json.Unmarshal(daemon["warnings"], &daemonWarnings))
	require.NoError(t, json.Unmarshal(full["warnings"], &fullWarnings))
	require.NotNil(t, daemonWarnings)
	require.Len(t, daemonWarnings, 0)
	require.Len(t, fullWarnings, 1)
	require.False(t, fullWarnings[0].Code != store.CodeOwnershipConflict)
	// Unrelated plugin metadata cannot prevent observing or diagnosing the daemon.
	require.NoError(t, os.WriteFile(filepath.Join(root, "guest-plugins.json"), []byte("invalid JSON"), 0600))
	run("daemon", "status", "--json")
	var plain bytes.Buffer
	require.NoError(t, printDaemonStatus(&plain, false))
	require.Contains(t, plain.String(), "Daemon: STOPPED")
	require.NotContains(t, plain.String(), "Cached sandbox")
	require.NotContains(t, plain.String(), "\x1b")
}

func TestStatusSeparatesWarningsWithoutChangingSavedIssues(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	for _, code := range []string{store.CodeOwnershipConflict, store.CodeGuestPluginSync, "X9999"} {
		require.NoError(t, store.RecordError(code, "scope/id", "Needs attention"))
	}
	report, err := readStatusReport()
	require.NoError(t, err)
	require.EqualValues(t, 4, report.SchemaVersion)
	require.Len(t, report.Errors, 2)
	require.Len(t, report.Warnings, 1)
	require.False(t, report.Warnings[0].Code != store.CodeOwnershipConflict)
	var jsonOut bytes.Buffer
	require.NoError(t, printStatus(&jsonOut, true))
	var decoded statusReport
	require.NoError(t, json.Unmarshal(jsonOut.Bytes(), &decoded))
	require.Len(t, decoded.Warnings, 1)
	require.Len(t, decoded.Errors, 2)
	var out bytes.Buffer
	printStatusIssues(&out, report.daemonStatusReport, func(value, tone string) string { return tone + ":" + value })
	for _, want := range []string{"Errors: 2 · Warnings: 1", "33:R0005", "31:P0002", "31:X9999"} {
		require.Contains(t, out.String(), want, "missing %q in output: %s", want, out.String())
	}
	saved, err := store.ReadStatusStore()
	require.NoError(t, err)
	require.Len(t, saved.Errors, 3)
}

func TestStatusKeepsEnabledIntentSeparateFromNativeHoldAndDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", root)
	t.Setenv("PATH", t.TempDir())
	require.NoError(t, store.SetEnabled("scope", "id", store.Enabled))
	require.NoError(t, store.SetWorkerControl(true))
	observed := time.Now().Add(-time.Hour).UTC()
	require.NoError(t, store.UpdateStatusStore(func(saved *store.StatusStore) {
		native := false
		saved.Sandboxes[store.StatusKey("scope", "id")] = store.SandboxStatus{
			Scope: "scope", SandboxID: "id", Name: "claude", Label: "saved label", ProfileID: "stable-machine",
			EntryPresent: true, NativeEnabled: &native, Observed: "stopped", ObservedAt: observed,
			Connection: "Waiting for start", Stale: true,
		}
	}))
	path, err := store.StatePath()
	require.NoError(t, err)
	before, err := os.ReadFile(path + ".status")
	require.NoError(t, err)
	report, err := readStatusReport()
	require.NoError(t, err)
	row := report.Sandboxes[0]
	require.False(t, report.Management.Enabled)
	require.False(t, row.Enabled != store.Enabled)
	require.True(t, row.EntryPresent)
	require.NotNil(t, row.NativeEnabled)
	require.False(t, *row.NativeEnabled)
	require.EqualValues(t, "stable-machine", row.ProfileID)
	require.True(t, row.ObservedAt.Equal(observed))
	var out bytes.Buffer
	require.NoError(t, printStatus(&out, false))
	require.Contains(t, out.String(), "Management: OFF")
	require.Contains(t, out.String(), "saved label (last known)")
	require.Contains(t, out.String(), "(stale)")
	after, err := os.ReadFile(path + ".status")
	require.NoError(t, err)
	require.Equal(t, after, before)
}

func TestDaemonSummaryShowsSavedChoiceDuringCancellation(t *testing.T) {
	report := daemonReport(store.WorkerStatus{State: "cancelling", Detail: "Cancelling"}, store.StatusStore{WorkerControl: &store.WorkerControl{Initialized: true, Stopped: true}})
	var out bytes.Buffer
	printDaemonSummary(&out, report, func(value, tone string) string { return tone + ":" + value })
	for _, want := range []string{"Management: 2:OFF", "Daemon: 2:Cancelling"} {
		require.Contains(t, out.String(), want, "missing %q: %s", want, out.String())
	}
}

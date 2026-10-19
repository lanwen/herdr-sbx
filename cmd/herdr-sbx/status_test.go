package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/lanwen/herdr-sbx/internal/store"
)

func TestStatusReportsUnderlyingFailureWithoutResyncOutcomeAsError(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := store.NotePreparationFailure("sandbox", "guest build failed", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordError(store.CodePreparation, "sandbox", "Re-sync did not finish"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordError(store.CodeRegistration, "sandbox", "guest build failed"); err != nil {
		t.Fatal(err)
	}
	report, err := readStatusReport()
	if err != nil || len(report.Errors) != 1 || report.Errors[0].Code != store.CodeRegistration || len(report.Warnings) != 0 {
		t.Fatalf("re-sync outcome replaced actual failure: %+v, %v", report, err)
	}
}

func TestJSONStatusIsReadOnlyWithNoToolsOrState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", root)
	t.Setenv("PATH", t.TempDir())
	cmd := commands()
	var out bytes.Buffer
	cmd.Writer = &out
	if err := cmd.Run(context.Background(), []string{"herdr-sbx", "status", "--json"}); err != nil {
		t.Fatal(err)
	}
	var report statusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != 3 || report.Worker.State != "stopped" || report.Sandboxes == nil || report.Errors == nil || report.Warnings == nil {
		t.Fatalf("invalid report: %+v", report)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("status created state: %v", err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("JSON contains styling")
	}
}

func TestStatusReportsSavedChoicesLabelsAndErrors(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	if err := store.ObserveSandbox("scope", sbx.Sandbox{ID: "id", Name: "demo", Status: "stopped"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetManagement("scope", "id", store.Unwatched); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {{SandboxID: "id", Label: "My workspace", ProfileID: "machine"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordError(store.CodeRename, "scope/id", "Rename failed"); err != nil {
		t.Fatal(err)
	}
	report, err := readStatusReport()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sandboxes) != 1 || report.Sandboxes[0].Management != store.Unwatched || report.Sandboxes[0].Label != "My workspace" {
		t.Fatalf("rows: %+v", report.Sandboxes)
	}
	if len(report.Errors) != 1 || report.Errors[0].Code != store.CodeRename {
		t.Fatalf("errors: %+v", report.Errors)
	}
	var out bytes.Buffer
	if err := printStatus(&out, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "My workspace") || !strings.Contains(out.String(), store.CodeRename) || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("output: %s", out.String())
	}
}

func TestStatusDoesNotConfirmUnresolvedOwnershipOrRemoval(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := store.ObserveSandbox("scope", sbx.Sandbox{ID: "id", Name: "demo", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(store.State{Version: 1, Scopes: map[string][]store.Tracked{"scope": {{SandboxID: "id", Label: "pending", Pending: true}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetManagement("scope", "id", store.Disabled); err != nil {
		t.Fatal(err)
	}
	report, err := readStatusReport()
	if err != nil {
		t.Fatal(err)
	}
	row := report.Sandboxes[0]
	if row.Owned || row.Label != "" || row.Management != store.Unwatched || row.DesiredManagement != store.Disabled {
		t.Fatalf("unconfirmed state: %+v", row)
	}
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
		if err := cmd.Run(context.Background(), append([]string{"herdr-sbx"}, args...)); err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
			t.Fatal(err)
		}
		return fields
	}
	daemon := run("daemon", "status", "--json")
	if len(daemon) != 6 {
		t.Fatalf("unexpected daemon fields: %v", daemon)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("daemon status created state: %v", err)
	}
	if err := store.SetWorkerControl(true); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordError(store.CodeDaemonStop, "daemon", "Stop failed"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordError(store.CodeRename, "scope/id", "Rename failed"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordError(store.CodeOwnershipConflict, "scope/id", "Manual machine remains usable"); err != nil {
		t.Fatal(err)
	}
	daemon = run("daemon", "status", "--json")
	full := run("status", "--json")
	for key, value := range daemon {
		if key == "read_at" || key == "errors" || key == "warnings" {
			continue
		}
		if !bytes.Equal(value, full[key]) {
			t.Fatalf("%s differs: daemon=%s root=%s", key, value, full[key])
		}
	}
	var daemonErrors, fullErrors []store.ErrorRecord
	if err := json.Unmarshal(daemon["errors"], &daemonErrors); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(full["errors"], &fullErrors); err != nil {
		t.Fatal(err)
	}
	if len(daemonErrors) != 1 || daemonErrors[0].Code != store.CodeDaemonStop || len(fullErrors) != 2 {
		t.Fatalf("daemon=%+v root=%+v", daemonErrors, fullErrors)
	}
	var daemonWarnings, fullWarnings []store.ErrorRecord
	if err := json.Unmarshal(daemon["warnings"], &daemonWarnings); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(full["warnings"], &fullWarnings); err != nil {
		t.Fatal(err)
	}
	if daemonWarnings == nil || len(daemonWarnings) != 0 || len(fullWarnings) != 1 || fullWarnings[0].Code != store.CodeOwnershipConflict {
		t.Fatalf("daemon warnings=%+v root warnings=%+v", daemonWarnings, fullWarnings)
	}
	// Unrelated plugin metadata cannot prevent observing or diagnosing the daemon.
	if err := os.WriteFile(filepath.Join(root, "guest-plugins.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	run("daemon", "status", "--json")
	var plain bytes.Buffer
	if err := printDaemonStatus(&plain, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain.String(), "Daemon: STOPPED") || strings.Contains(plain.String(), "Cached sandbox") || strings.Contains(plain.String(), "\x1b") {
		t.Fatalf("daemon output: %s", plain.String())
	}
}

func TestStatusSeparatesWarningsWithoutChangingSavedIssues(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	for _, code := range []string{store.CodeOwnershipConflict, store.CodeGuestPluginSync, "X9999"} {
		if err := store.RecordError(code, "scope/id", "Needs attention"); err != nil {
			t.Fatal(err)
		}
	}
	report, err := readStatusReport()
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != 3 || len(report.Errors) != 2 || len(report.Warnings) != 1 || report.Warnings[0].Code != store.CodeOwnershipConflict {
		t.Fatalf("incorrect severity split: %+v", report)
	}
	var jsonOut bytes.Buffer
	if err := printStatus(&jsonOut, true); err != nil {
		t.Fatal(err)
	}
	var decoded statusReport
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Warnings) != 1 || len(decoded.Errors) != 2 {
		t.Fatalf("JSON severity split: %s", jsonOut.String())
	}
	var out bytes.Buffer
	printStatusIssues(&out, report.daemonStatusReport, func(value, tone string) string { return tone + ":" + value })
	for _, want := range []string{"Errors: 2 · Warnings: 1", "33:R0005", "31:P0002", "31:X9999"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output: %s", want, out.String())
		}
	}
	saved, err := store.ReadStatusStore()
	if err != nil || len(saved.Errors) != 3 {
		t.Fatalf("saved issue identity changed: %+v, %v", saved.Errors, err)
	}
}

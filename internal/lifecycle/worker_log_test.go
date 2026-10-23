package lifecycle

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/precheck"
	"github.com/lanwen/herdr-sbx/internal/registration"
	"github.com/lanwen/herdr-sbx/internal/store"
	"github.com/stretchr/testify/require"
)

func TestWorkerLogRotationKeepsInheritedOutputAndRecentArchives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json.worker.log")
	log, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)
	defer log.Close()
	original, err := log.Stat()
	require.NoError(t, err)
	var history []byte
	for _, marker := range []string{"first", "second", "third", "fourth"} {
		payload := strings.Repeat(marker, workerLogLimit/len(marker)+1)
		command := exec.Command("sh", "-c", "cat; printf stderr-tail >&2")
		command.Stdin = strings.NewReader(payload)
		command.Stdout, command.Stderr = log, log
		require.NoError(t, command.Run())
		history = append(history, []byte(payload+"stderr-tail")...)
		require.NoError(t, rotateWorkerLog(path))
		var retained []byte
		for _, suffix := range []string{".2", ".1", ""} {
			content, err := os.ReadFile(path + suffix)
			if suffix == ".2" && len(history) < 2*workerLogLimit {
				require.ErrorIs(t, err, os.ErrNotExist)
				continue
			}
			require.NoError(t, err)
			require.LessOrEqual(t, len(content), workerLogLimit)
			info, err := os.Stat(path + suffix)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			if suffix == "" {
				require.True(t, os.SameFile(original, info), "active inode changed")
			}
			retained = append(retained, content...)
		}
		require.Equal(t, history[len(history)-len(retained):], retained)
		require.True(t, bytes.HasSuffix(retained, []byte("stderr-tail")))
	}
}

func TestWorkerLogRotationBoundsLargeBursts(t *testing.T) {
	for _, size := range []int{workerLogLimit, workerLogLimit + 1, 2 * workerLogLimit, 4*workerLogLimit + 23} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "worker.log")
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i % 251)
			}
			require.NoError(t, os.WriteFile(path, data, 0600))
			require.NoError(t, rotateWorkerLog(path))
			active, err := os.ReadFile(path)
			require.NoError(t, err)
			if size <= workerLogLimit {
				require.Equal(t, data, active)
				return
			}
			require.Len(t, active, (size-1)%workerLogLimit+1)
			retained := []byte{}
			for _, suffix := range []string{".2", ".1"} {
				archived, err := os.ReadFile(path + suffix)
				if suffix == ".2" && size <= 2*workerLogLimit {
					require.ErrorIs(t, err, os.ErrNotExist)
					continue
				}
				require.NoError(t, err)
				require.Len(t, archived, workerLogLimit)
				retained = append(retained, archived...)
			}
			retained = append(retained, active...)
			require.Equal(t, data[len(data)-len(retained):], retained)
		})
	}
}

func TestWorkerLogRotationMissingSmallAndFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.log")
	require.NoError(t, rotateWorkerLog(path))
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, os.WriteFile(path, []byte("recent output"), 0600))
	require.NoError(t, rotateWorkerLog(path))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "recent output", string(data))
	badPath := path + ".bad"
	require.NoError(t, os.Symlink(badPath, badPath))
	require.ErrorIs(t, rotateWorkerLog(badPath), syscall.ELOOP)

	data = bytes.Repeat([]byte("x"), 2*workerLogLimit+1)
	require.NoError(t, os.WriteFile(path, data, 0600))
	require.NoError(t, os.Mkdir(path+".2", 0700))
	require.NoError(t, os.WriteFile(path+".2/block", nil, 0600))
	require.Error(t, rotateWorkerLog(path))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, after, "archive failure truncated active data")
	temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".worker-log-*"))
	require.NoError(t, err)
	require.Empty(t, temporary)
}

func TestWorkerChecksLogTransitionsWithoutRepeatingReports(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))
	checks := &workerChecks{logger: logger}
	result := precheck.Result{Checks: []precheck.Check{
		{Name: "Herdr", Level: precheck.Ready, Detail: "available"},
		{Name: "Sandbox daemon", Level: precheck.Blocked, Detail: "not running"},
	}}
	checks.print(io.Discard, result)
	initial := out.String()
	require.Contains(t, initial, "time=")
	require.Contains(t, initial, "level=WARN")
	require.Contains(t, initial, `check="Sandbox daemon"`)
	require.Contains(t, initial, `detail="not running"`)
	for range 100 {
		checks.print(io.Discard, result)
	}
	require.Equal(t, initial, out.String())
	result.Checks[1].Level, result.Checks[1].Detail = precheck.Ready, "running"
	checks.print(io.Discard, result)
	recovery := strings.TrimPrefix(out.String(), initial)
	require.Contains(t, recovery, "level=INFO")
	require.Contains(t, recovery, "state=ready")
	require.Contains(t, recovery, "detail=running")
	require.NotContains(t, recovery, "Herdr")
	t.Logf("initial blocked-check report: %d bytes; 100 unchanged reports: 0 additional bytes; recovery event: %d bytes", len(initial), len(recovery))
}

func TestWorkerOutputLogsBoundedEvents(t *testing.T) {
	var out bytes.Buffer
	writer := &workerOutput{logger: slog.New(slog.NewTextHandler(&out, nil))}
	message := "Guest setup failed: unavailable\n" + strings.Repeat("command response\n", 1000)
	n, err := writer.Write([]byte(message))
	require.NoError(t, err)
	require.Equal(t, len(message), n)
	require.Contains(t, out.String(), "time=")
	require.Contains(t, out.String(), "Guest setup failed: unavailable")
	require.Contains(t, out.String(), "see diagnostics")
	require.NotContains(t, out.String(), "command response")
	require.Equal(t, 1, strings.Count(out.String(), "\n"))
}

func TestWorkerResultsLogChangesAndRecoveryByIdentity(t *testing.T) {
	var out bytes.Buffer
	results := &workerResults{logger: slog.New(slog.NewTextHandler(&out, nil))}
	healthy := registration.ResultObservation{SandboxID: "one", Name: "demo", Status: "ready", Detail: "registered"}
	failed := registration.ResultObservation{SandboxID: "two", Name: "other", Status: "setup required", Detail: "SSH unavailable", DiagnosticCode: store.CodeSandboxSetup}
	results.print(io.Discard, healthy)
	results.print(io.Discard, failed)
	initial := out.String()
	require.Contains(t, initial, "level=WARN")
	require.Contains(t, initial, "sandbox_id=two")
	require.Contains(t, initial, "SSH unavailable")
	for range 100 {
		results.print(io.Discard, healthy)
		results.print(io.Discard, failed)
	}
	require.Equal(t, initial, out.String(), "alternating scan rows grew the log")
	failed.Status, failed.Detail, failed.DiagnosticCode = "ready", "registered", ""
	results.print(io.Discard, failed)
	recovery := strings.TrimPrefix(out.String(), initial)
	require.Contains(t, recovery, "level=INFO")
	require.Contains(t, recovery, "sandbox_id=two")
	require.Contains(t, recovery, "state=ready")
}

func TestWorkerLogRotationRejectsUnsafePathsAndSecuresExistingLogs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.log")
	require.NoError(t, os.WriteFile(path, []byte("small"), 0644))
	require.NoError(t, os.Chmod(path, 0644))
	require.NoError(t, rotateWorkerLog(path))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	target := filepath.Join(dir, "unrelated")
	require.NoError(t, os.WriteFile(target, []byte("unchanged"), 0644))
	require.NoError(t, os.Chmod(target, 0644))
	symlink := path + ".unsafe"
	require.NoError(t, os.Symlink(target, symlink))
	require.Error(t, rotateWorkerLog(symlink))
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("x"), workerLogLimit+1), 0600))
	require.NoError(t, os.Symlink(target, path+".1"))
	require.Error(t, rotateWorkerLog(path))
	info, err = os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0644), info.Mode().Perm())
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "unchanged", string(data))
}

func TestWorkerLogTailRejectsExternalShrink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.log")
	require.NoError(t, os.WriteFile(path, []byte("externally truncated"), 0600))
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	defer file.Close()
	require.ErrorContains(t, retainWorkerLogTail(file, workerLogLimit), "shrank during rotation")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "externally truncated", string(data))
}

func TestWorkerLogConcurrentProcessRotation(t *testing.T) {
	if path := os.Getenv("HERDR_TEST_ROTATE_LOG"); path != "" {
		require.NoError(t, rotateWorkerLog(path))
		return
	}
	path := filepath.Join(t.TempDir(), "worker.log")
	data := make([]byte, 5*workerLogLimit+17)
	for i := range data {
		data[i] = byte(i % 251)
	}
	require.NoError(t, os.WriteFile(path, data, 0600))
	commands := make([]*exec.Cmd, 4)
	for i := range commands {
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestWorkerLogConcurrentProcessRotation$")
		commands[i].Env = append(os.Environ(), "HERDR_TEST_ROTATE_LOG="+path)
		require.NoError(t, commands[i].Start())
	}
	for _, command := range commands {
		require.NoError(t, command.Wait())
	}
	var retained []byte
	for _, suffix := range []string{".2", ".1", ""} {
		part, err := os.ReadFile(path + suffix)
		require.NoError(t, err)
		retained = append(retained, part...)
	}
	require.Equal(t, data[len(data)-2*workerLogLimit-17:], retained)
}

func TestWorkerLogRejectsFIFOsWithoutWaitingForPeers(t *testing.T) {
	for _, operation := range []string{"startup", "active rotation", "archive rotation"} {
		t.Run(operation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "worker.log")
			fifo := path
			if operation == "archive rotation" {
				require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("x"), workerLogLimit+1), 0600))
				fifo += ".1"
			}
			require.NoError(t, syscall.Mkfifo(fifo, 0600))
			done := make(chan error, 1)
			go func() {
				if operation == "startup" {
					file, err := openWorkerLog(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
					if file != nil {
						_ = file.Close()
					}
					done <- err
				} else {
					done <- rotateWorkerLog(path)
				}
			}()
			select {
			case err := <-done:
				require.Error(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("log validation waited for a FIFO peer")
			}
		})
	}
}

package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkerHealthUsesLockAndPreservesLastSync(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	status, err := ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "stopped", status.State)
	unlock, err := LockWorker()
	require.NoError(t, err)
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	status, err = ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "unknown", status.State)
	require.NoError(t, RecordWorkerStatus("running", "connected", true))
	status, err = ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "running", status.State)
	require.False(t, status.LastSync.IsZero())
	require.NoError(t, RecordWorkerStatus("reconnecting", "stream lost", false))
	retry, err := ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "reconnecting", retry.State)
	require.True(t, retry.LastSync.Equal(status.LastSync))
	require.NoError(t, RecordWorkerStatus("", "", true))
	retry, err = ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "reconnecting", retry.State)
	require.EqualValues(t, "stream lost", retry.Detail)
	// Simulate exit without a final status write. The remaining saved state must
	// never make an unlocked worker look alive.
	unlock()
	unlock = nil
	status, err = ReadWorkerStatus()
	require.NoError(t, err)
	require.EqualValues(t, "stopped", status.State)
	require.False(t, status.LastSync.IsZero())
}

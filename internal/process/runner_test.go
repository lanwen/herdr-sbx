package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunnerCapturedOutputLimit(t *testing.T) {
	for _, size := range []int{maxOutputBytes - 1, maxOutputBytes, maxOutputBytes + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			output, err := (Runner{}).CombinedOutput(context.Background(), outputHelper("bytes", size))
			if size > maxOutputBytes {
				require.ErrorIs(t, err, errOutputLimit)
				require.EqualValues(t, "", output)
				return
			}
			require.NoError(t, err)
			require.Len(t, output, size)
			require.EqualValues(t, "", strings.Trim(output, "x"))
		})
	}
}

func TestRunnerMergesOutput(t *testing.T) {
	output, err := (Runner{}).CombinedOutput(context.Background(), outputHelper("merged", 0))
	require.NoError(t, err)
	require.EqualValues(t, "stdout\nstderr\n", output)
}

func TestRunnerLimitsCombinedStreams(t *testing.T) {
	output, err := (Runner{}).CombinedOutput(context.Background(), outputHelper("combined-limit", 0))
	require.ErrorIs(t, err, errOutputLimit)
	require.EqualValues(t, "", output)
}

func TestRunnerStopsContinuingProducer(t *testing.T) {
	started := time.Now()
	command := outputHelper("continuous", 0)
	command.Timeout = 5 * time.Second
	output, err := (Runner{}).CombinedOutput(context.Background(), command)
	require.ErrorIs(t, err, errOutputLimit)
	require.EqualValues(t, "", output)
	require.False(t, time.Since(started) > 3*time.Second)
}

func TestRunnerCancellationAndTimeout(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		timer := time.AfterFunc(100*time.Millisecond, cancel)
		defer timer.Stop()
		_, err := (Runner{}).CombinedOutput(ctx, outputHelper("sleep", 0))
		require.ErrorIs(t, err, context.Canceled, "cancelled command = %v", err)
	})
	t.Run("cancelled before start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := (Runner{}).CombinedOutput(ctx, outputHelper("sleep", 0))
		require.ErrorIs(t, err, context.Canceled, "cancelled command = %v", err)
	})
	t.Run("timeout", func(t *testing.T) {
		command := outputHelper("sleep", 0)
		command.Timeout = 100 * time.Millisecond
		_, err := (Runner{}).CombinedOutput(context.Background(), command)
		require.Error(t, err)
		require.EqualValues(t, "timed out", err.Error())
	})
}

func TestRunnerBoundsInheritedPipeWait(t *testing.T) {
	started := time.Now()
	_, err := (Runner{}).CombinedOutput(context.Background(), outputHelper("inherited", 0))
	require.ErrorIs(t, err, exec.ErrWaitDelay)
	require.False(t, time.Since(started) > 2500*time.Millisecond)
}

func outputHelper(mode string, size int) Command {
	return Command{
		Name: os.Args[0], Args: []string{"-test.run=^TestRunnerOutputHelper$"},
		Env: map[string]string{"HERDR_SBX_OUTPUT_HELPER": mode, "HERDR_SBX_OUTPUT_SIZE": strconv.Itoa(size)},
	}
}

func TestRunnerOutputHelper(t *testing.T) {
	mode := os.Getenv("HERDR_SBX_OUTPUT_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "bytes":
		size, _ := strconv.Atoi(os.Getenv("HERDR_SBX_OUTPUT_SIZE"))
		_, _ = os.Stdout.WriteString(strings.Repeat("x", size))
	case "merged":
		fmt.Fprintln(os.Stdout, "stdout")
		fmt.Fprintln(os.Stderr, "stderr")
	case "combined-limit":
		_, _ = os.Stdout.WriteString(strings.Repeat("x", maxOutputBytes/2))
		_, _ = os.Stderr.WriteString(strings.Repeat("x", maxOutputBytes/2+1))
	case "continuous":
		chunk := strings.Repeat("x", 32<<10)
		for {
			if _, err := os.Stdout.WriteString(chunk); err != nil {
				os.Exit(0)
			}
		}
	case "sleep":
		time.Sleep(3 * time.Second)
	case "inherited":
		child := exec.Command(os.Args[0], "-test.run=^TestRunnerOutputHelper$")
		child.Env = append(os.Environ(), "HERDR_SBX_OUTPUT_HELPER=sleep")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

func TestRunnerStreamsStdinToRealProcess(t *testing.T) {
	payload := "verified binary bytes\x00\x01\xff\n"
	output, err := (Runner{}).CombinedOutput(context.Background(), Command{
		Name: "/bin/cat", Stdin: strings.NewReader(payload), Timeout: time.Second,
	})
	require.NoError(t, err)
	require.False(t, output != payload)
}

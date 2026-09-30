package process

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunnerStreamsStdinToRealProcess(t *testing.T) {
	payload := "verified binary bytes\x00\x01\xff\n"
	output, err := (Runner{}).CombinedOutput(context.Background(), Command{
		Name: "/bin/cat", Stdin: strings.NewReader(payload), Timeout: time.Second,
	})
	if err != nil || output != payload {
		t.Fatalf("stdin roundtrip = %q, %v", output, err)
	}
}

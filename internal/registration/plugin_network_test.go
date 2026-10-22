package registration

import (
	"testing"
	"time"

	"github.com/lanwen/herdr-sbx/internal/sbx"
	"github.com/stretchr/testify/require"
)

func TestConfirmedNetworkBlockRequiresCorrelatedChange(t *testing.T) {
	started := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	old := sbx.NetworkPolicyLog{SandboxID: "id", Host: "downloads.example.org", Reason: "default-deny", Count: 2, LastSeen: started.Add(-time.Minute)}
	newer := old
	newer.Count = 3
	newer.LastSeen = started.Add(time.Second)
	{
		cause := confirmedNetworkBlock([]sbx.NetworkPolicyLog{old}, []sbx.NetworkPolicyLog{newer}, "id", started, "curl: downloads.example.org: connection denied")
		require.Contains(t, cause, "Sandbox network access: policy blocked downloads.example.org", "correlated block not found: %q", cause)
	}
	for _, test := range []struct {
		name    string
		entry   sbx.NetworkPolicyLog
		failure string
	}{
		{"no new request", old, "curl: downloads.example.org: connection denied"},
		{"different sandbox", sbx.NetworkPolicyLog{SandboxID: "other", Host: newer.Host, Count: 3, LastSeen: newer.LastSeen}, "curl: downloads.example.org: connection denied"},
		{"unrelated destination", newer, "build command returned 403"},
		{"old timestamp", sbx.NetworkPolicyLog{SandboxID: "id", Host: newer.Host, Count: 3, LastSeen: started.Add(-2 * time.Second)}, "curl: downloads.example.org: connection denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			{
				cause := confirmedNetworkBlock([]sbx.NetworkPolicyLog{old}, []sbx.NetworkPolicyLog{test.entry}, "id", started, test.failure)
				require.Equal(t, "", cause, "uncertain block classified: %q", cause)
			}
		})
	}
	otherProxy := old
	otherProxy.Count = 1
	otherProxy.LastSeen = started.Add(time.Second)
	{
		cause := confirmedNetworkBlock([]sbx.NetworkPolicyLog{old, otherProxy}, []sbx.NetworkPolicyLog{otherProxy, old}, "id", started, "curl: downloads.example.org: connection denied")
		require.Equal(t, "", cause, "unchanged duplicate aggregates classified: %q", cause)
	}
}

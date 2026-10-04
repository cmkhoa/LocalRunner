package metrics

import (
	"testing"
	"time"
)

func TestCollectorAggregatesSuccessAndFailure(t *testing.T) {
	collector := New()
	collector.Record(Request{Succeeded: true, QueueWait: 10 * time.Millisecond, Latency: 40 * time.Millisecond, GeneratedTokens: 8, GenerationDuration: 200 * time.Millisecond})
	collector.Record(Request{ErrorCategory: "runner_unavailable", QueueWait: 2 * time.Millisecond, Latency: 3 * time.Millisecond})

	snapshot := collector.Snapshot()
	if got, want := snapshot.Requests.Total, uint64(2); got != want {
		t.Errorf("Total = %d, want %d", got, want)
	}
	if got, want := snapshot.Requests.Succeeded, uint64(1); got != want {
		t.Errorf("Succeeded = %d, want %d", got, want)
	}
	if got, want := snapshot.Requests.Failed, uint64(1); got != want {
		t.Errorf("Failed = %d, want %d", got, want)
	}
	if got, want := snapshot.Requests.ByError["runner_unavailable"], uint64(1); got != want {
		t.Errorf("ByError = %d, want %d", got, want)
	}
	if got, want := snapshot.Generation.Tokens, uint64(8); got != want {
		t.Errorf("Tokens = %d, want %d", got, want)
	}
	if got, want := snapshot.Generation.TokensPerSecond, 40.0; got != want {
		t.Errorf("TokensPerSecond = %f, want %f", got, want)
	}
}

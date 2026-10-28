package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestResponseMetricsUsesPredictedTokensWhenUsageIsAbsent(t *testing.T) {
	response := json.RawMessage(`{"timings":{"predicted_n":5,"predicted_ms":104.516,"predicted_per_second":38.27}}`)
	tokens, duration, tokensPerSecond := responseMetrics(response)
	if got, want := tokens, 5; got != want {
		t.Errorf("tokens = %d, want %d", got, want)
	}
	if got, want := duration, time.Duration(104.516*float64(time.Millisecond)); got != want {
		t.Errorf("duration = %s, want %s", got, want)
	}
	if got, want := tokensPerSecond, 38.27; got != want {
		t.Errorf("tokensPerSecond = %f, want %f", got, want)
	}
}

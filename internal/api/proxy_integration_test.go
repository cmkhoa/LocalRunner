package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"localrunner/internal/runner"
)

type mockRunner struct {
	endpoint string
	status   runner.Status
	profiles []runner.Profile
}

func (m mockRunner) Status() runner.Status {
	if m.status.State != "" {
		return m.status
	}
	return runner.Status{State: runner.Ready, Healthy: true}
}
func (m mockRunner) Register(string) (runner.Status, error)        { return m.Status(), nil }
func (m mockRunner) Profiles() []runner.Profile                    { return m.profiles }
func (m mockRunner) ActivateProfile(string) (runner.Status, error) { return m.Status(), nil }
func (m mockRunner) Start(context.Context) (runner.Status, error)  { return m.Status(), nil }
func (m mockRunner) Stop(context.Context) (runner.Status, error) {
	return runner.Status{State: runner.Stopped}, nil
}
func (m mockRunner) ChatEndpoint() (string, error) {
	if m.endpoint == "" {
		return "", runner.ErrUnavailable
	}
	return m.endpoint, nil
}

func TestChatCompletionProxiesMockRunner(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream request = %s %s", r.Method, r.URL.Path)
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if got, want := request.Messages[0].Content, "hello"; got != want {
			t.Errorf("message = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"world"}}],"usage":{"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	handler := newHandler(mockRunner{endpoint: upstream.URL + "/v1/chat/completions"}, slog.Default(), DefaultConfig())
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}]}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"world"`)) {
		t.Errorf("response = %s, want proxied completion", response.Body.String())
	}
}

func TestStreamingClientCancellationCancelsMockRunner(t *testing.T) {
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	defer upstream.Close()

	service := httptest.NewServer(newHandler(mockRunner{endpoint: upstream.URL + "/v1/chat/completions"}, slog.Default(), DefaultConfig()))
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.URL+"/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(response.Body)
	if !scanner.Scan() {
		t.Fatalf("stream ended before first event: %v", scanner.Err())
	}
	if got, want := scanner.Text(), `data: {"choices":[{"delta":{"content":"hello"}}]}`; got != want {
		t.Errorf("first event = %q, want %q", got, want)
	}
	cancel()
	_ = response.Body.Close()

	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream stream was not canceled after client disconnect")
	}
}

func TestActiveLimitsUseSelectedProfile(t *testing.T) {
	defaults := DefaultConfig()
	defaults.DefaultMaxTokens = 16
	defaults.QueueCapacity = 2
	handler := &Handler{runner: mockRunner{
		status:   runner.Status{State: runner.Ready, Healthy: true, ActiveProfile: "small"},
		profiles: []runner.Profile{{Name: "small", RequestTimeout: "3s", MaxGeneratedTokens: 8, QueueCapacity: 1}},
	}, config: defaults}

	limits := handler.activeLimits()
	if got, want := limits.RequestTimeout.String(), "3s"; got != want {
		t.Errorf("RequestTimeout = %q, want %q", got, want)
	}
	if got, want := limits.MaxGeneratedTokens, 8; got != want {
		t.Errorf("MaxGeneratedTokens = %d, want %d", got, want)
	}
	if got, want := limits.DefaultMaxTokens, 8; got != want {
		t.Errorf("DefaultMaxTokens = %d, want %d", got, want)
	}
	if got, want := limits.QueueCapacity, 1; got != want {
		t.Errorf("QueueCapacity = %d, want %d", got, want)
	}
}

var _ runnerService = mockRunner{}

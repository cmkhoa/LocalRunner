package api_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"localrunner/internal/api"
	"localrunner/internal/runner"
)

func TestRunnerStatusStartsStopped(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	handler := api.NewHandler(runner.NewManager(runner.DefaultConfig(), logger), logger)
	request := httptest.NewRequest(http.MethodGet, "/v1/runners/status", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "{\"state\":\"stopped\",\"healthy\":false,\"restart_count\":0}\n"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
}

func TestChatRejectsStreaming(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	handler := api.NewHandler(runner.NewManager(runner.DefaultConfig(), logger), logger)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"stream":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if got, want := response.Body.String(), "{\"error\":{\"code\":\"invalid_request\",\"message\":\"streaming is not supported\"}}\n"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(data []byte) (int, error) {
	w.t.Log(string(data))
	return len(data), nil
}

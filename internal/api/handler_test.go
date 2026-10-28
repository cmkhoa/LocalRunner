package api_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestStreamingChatRequiresReadyRunner(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	handler := api.NewHandler(runner.NewManager(runner.DefaultConfig(), logger), logger)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"stream":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if got, want := payload.Error.Code, "runner_unavailable"; got != want {
		t.Errorf("error code = %q, want %q", got, want)
	}
	if got, want := payload.Error.Message, "runner is unavailable: stopped"; got != want {
		t.Errorf("error message = %q, want %q", got, want)
	}
	if payload.Error.RequestID == "" || payload.Error.RequestID != response.Header().Get("X-Request-ID") {
		t.Errorf("request ID = %q, header = %q", payload.Error.RequestID, response.Header().Get("X-Request-ID"))
	}
}

func TestListModelsUsesProfileNamesInsteadOfPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-model.gguf")
	if err := os.WriteFile(path, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	manager := runner.NewManager(runner.DefaultConfig(), logger)
	if err := manager.AddProfile(runner.Profile{Name: "local-qwen", ModelPath: path}); err != nil {
		t.Fatal(err)
	}
	handler := api.NewHandler(manager, logger)
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if got, want := response.Body.String(), "{\"data\":[{\"created\":0,\"id\":\"local-qwen\",\"object\":\"model\",\"owned_by\":\"localrunner\"}],\"object\":\"list\"}\n"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
	if bytes.Contains(response.Body.Bytes(), []byte(path)) {
		t.Error("model response leaked an absolute filesystem path")
	}
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(data []byte) (int, error) {
	w.t.Log(string(data))
	return len(data), nil
}

// Package api provides LocalRunner's localhost HTTP interface.
package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"localrunner/internal/metrics"
	"localrunner/internal/queue"
	"localrunner/internal/runner"
)

// Config defines the API's startup-time resource limits. These controls are
// process-wide because the request queue is allocated when the handler starts.
type Config struct {
	MaxRequestBytes    int64
	MaxInputBytes      int
	MaxGeneratedTokens int
	DefaultMaxTokens   int
	QueueCapacity      int
	RequestTimeout     time.Duration
}

// DefaultConfig returns conservative limits for one local llama.cpp runner.
func DefaultConfig() Config {
	return Config{
		MaxRequestBytes:    1 << 20,
		MaxInputBytes:      32 << 10,
		MaxGeneratedTokens: 512,
		DefaultMaxTokens:   256,
		QueueCapacity:      16,
		RequestTimeout:     2 * time.Minute,
	}
}

// Validate reports configuration combinations that would make request
// handling ambiguous or unsafe.
func (c Config) Validate() error {
	if c.MaxRequestBytes <= 0 {
		return errors.New("max request bytes must be greater than zero")
	}
	if c.MaxInputBytes <= 0 {
		return errors.New("max input bytes must be greater than zero")
	}
	if c.MaxGeneratedTokens <= 0 {
		return errors.New("max generated tokens must be greater than zero")
	}
	if c.DefaultMaxTokens <= 0 || c.DefaultMaxTokens > c.MaxGeneratedTokens {
		return errors.New("default max tokens must be greater than zero and no more than max generated tokens")
	}
	if c.QueueCapacity <= 0 {
		return errors.New("queue capacity must be greater than zero")
	}
	if c.RequestTimeout <= 0 {
		return errors.New("request timeout must be greater than zero")
	}
	return nil
}

// Handler owns routing for the public API.
type Handler struct {
	runner  runnerService
	logger  *slog.Logger
	queue   *queue.Queue[chatResult]
	client  *http.Client
	metrics *metrics.Collector
	config  Config
}

// runnerService is the lifecycle surface the HTTP API needs. Keeping it small
// lets API integration tests use a controlled local upstream without changing
// production runner behavior.
type runnerService interface {
	Status() runner.Status
	Register(string) (runner.Status, error)
	Profiles() []runner.Profile
	ActivateProfile(string) (runner.Status, error)
	Start(context.Context) (runner.Status, error)
	Stop(context.Context) (runner.Status, error)
	ChatEndpoint() (string, error)
}

type requestIDKey struct{}

// NewHandler constructs the v1 API handler.
func NewHandler(manager *runner.Manager, logger *slog.Logger) http.Handler {
	return NewHandlerWithConfig(manager, logger, DefaultConfig())
}

// NewHandlerWithConfig constructs a handler with validated startup-time API
// limits. Callers loading configuration from the environment should validate it
// before this point.
func NewHandlerWithConfig(manager *runner.Manager, logger *slog.Logger, config Config) http.Handler {
	return newHandler(manager, logger, config)
}

func newHandler(manager runnerService, logger *slog.Logger, config Config) http.Handler {
	if err := config.Validate(); err != nil {
		panic(fmt.Sprintf("invalid API config: %v", err))
	}
	physicalQueueCapacity := config.QueueCapacity
	for _, profile := range manager.Profiles() {
		if profile.QueueCapacity > physicalQueueCapacity {
			physicalQueueCapacity = profile.QueueCapacity
		}
	}
	h := &Handler{runner: manager, logger: logger, queue: queue.New[chatResult](physicalQueueCapacity), client: &http.Client{Timeout: config.RequestTimeout}, metrics: metrics.New(), config: config}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/runners/status", h.runnerStatus)
	mux.HandleFunc("POST /v1/models/register", h.registerModel)
	mux.HandleFunc("GET /v1/models/profiles", h.listProfiles)
	mux.HandleFunc("GET /v1/models", h.listModels)
	mux.HandleFunc("POST /v1/models/activate", h.activateProfile)
	mux.HandleFunc("POST /v1/runners/start", h.startRunner)
	mux.HandleFunc("POST /v1/runners/stop", h.stopRunner)
	mux.HandleFunc("POST /v1/chat/completions", h.chatCompletion)
	mux.HandleFunc("GET /metrics", h.metricsSnapshot)
	return h.withJSON(mux)
}

func (h *Handler) runnerStatus(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, h.runner.Status())
}

func (h *Handler) metricsSnapshot(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, h.metrics.Snapshot())
}

func (h *Handler) registerModel(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
	}
	if err := h.decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	status, err := h.runner.Register(request.Path)
	if err != nil {
		h.writeRunnerError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, status)
}

func (h *Handler) listProfiles(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]any{"profiles": h.runner.Profiles()})
}

// listModels exposes the small OpenAI-compatible model-list subset. Profile
// names are stable client-facing IDs; absolute model paths stay private.
func (h *Handler) listModels(w http.ResponseWriter, r *http.Request) {
	status := h.runner.Status()
	seen := make(map[string]struct{})
	models := make([]map[string]any, 0, len(h.runner.Profiles())+1)
	add := func(id string) {
		if id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		models = append(models, map[string]any{"id": id, "object": "model", "created": 0, "owned_by": "localrunner"})
	}
	if status.ActiveProfile != "" {
		add(status.ActiveProfile)
	} else {
		add(status.ActiveModel)
	}
	for _, profile := range h.runner.Profiles() {
		add(profile.Name)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": models})
}

func (h *Handler) activateProfile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if err := h.decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	status, err := h.runner.ActivateProfile(request.Name)
	if err != nil {
		if strings.Contains(err.Error(), "was not found") {
			h.writeError(w, http.StatusNotFound, "profile_not_found", err.Error())
			return
		}
		h.writeRunnerError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, status)
}

func (h *Handler) startRunner(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	status, err := h.runner.Start(ctx)
	if err != nil {
		h.writeRunnerError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, status)
}

func (h *Handler) stopRunner(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	status, err := h.runner.Stop(ctx)
	if err != nil {
		h.writeRunnerError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, status)
}

type chatRequest struct {
	Model              string         `json:"model,omitempty"`
	Messages           []chatMessage  `json:"messages"`
	MaxTokens          int            `json:"max_tokens,omitempty"`
	Temperature        *float64       `json:"temperature,omitempty"`
	Stream             bool           `json:"stream,omitempty"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResult struct {
	response           json.RawMessage
	generatedTokens    int
	generationDuration time.Duration
	tokensPerSecond    float64
	streamStarted      bool
}

type streamStartedError struct{ err error }

func (e *streamStartedError) Error() string { return e.err.Error() }
func (e *streamStartedError) Unwrap() error { return e.err }

func (h *Handler) chatCompletion(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestID := requestIDFromRequest(r)
	limits := h.activeLimits()
	var request chatRequest
	if err := h.decodeJSON(w, r, &request); err != nil {
		h.metrics.Record(metrics.Request{ErrorCategory: "invalid_request", Latency: time.Since(started)})
		h.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := request.validate(limits.MaxInputBytes, limits.MaxGeneratedTokens); err != nil {
		h.metrics.Record(metrics.Request{ErrorCategory: "invalid_request", Latency: time.Since(started)})
		h.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if request.Stream {
		h.streamChatCompletion(w, r, request, started, requestID)
		return
	}
	if request.MaxTokens == 0 {
		request.MaxTokens = limits.DefaultMaxTokens
	}
	request.Stream = false

	ctx, cancel := context.WithTimeout(r.Context(), limits.RequestTimeout)
	defer cancel()
	result, queueStats, err := h.queue.SubmitWithLimit(ctx, limits.QueueCapacity, func(ctx context.Context) (chatResult, error) {
		response, err := h.proxyChat(ctx, request)
		return chatResult{response: response}, err
	})
	if err != nil {
		h.metrics.Record(metrics.Request{ErrorCategory: chatErrorCategory(err), QueueWait: queueStats.Wait, Latency: time.Since(started)})
		h.writeChatError(w, requestID, err)
		return
	}
	generatedTokens, generationDuration, tokensPerSecond := responseMetrics(result.response)
	h.metrics.Record(metrics.Request{Succeeded: true, QueueWait: queueStats.Wait, Latency: time.Since(started), GeneratedTokens: generatedTokens, GenerationDuration: generationDuration, TokensPerSecond: tokensPerSecond})
	h.logger.Info("chat completion completed", "request_id", requestID, "queue_wait_ms", queueStats.Wait.Milliseconds(), "latency_ms", time.Since(started).Milliseconds(), "generated_tokens", generatedTokens, "tokens_per_second", tokensPerSecond)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.response)
}

// streamChatCompletion proxies llama.cpp's OpenAI-style server-sent events.
// The queue worker owns the ResponseWriter while this handler waits, so the
// active runner remains protected for the entire stream duration.
func (h *Handler) streamChatCompletion(w http.ResponseWriter, r *http.Request, request chatRequest, started time.Time, requestID string) {
	limits := h.activeLimits()
	if request.MaxTokens == 0 {
		request.MaxTokens = limits.DefaultMaxTokens
	}
	if _, err := h.runner.ChatEndpoint(); err != nil {
		h.metrics.Record(metrics.Request{ErrorCategory: chatErrorCategory(err), Latency: time.Since(started)})
		h.writeChatError(w, requestID, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), limits.RequestTimeout)
	defer cancel()
	var streamStarted atomic.Bool
	result, queueStats, err := h.queue.SubmitWithLimit(ctx, limits.QueueCapacity, func(ctx context.Context) (chatResult, error) {
		return h.proxyStream(ctx, request, w, &streamStarted)
	})
	if err != nil {
		h.metrics.Record(metrics.Request{ErrorCategory: chatErrorCategory(err), QueueWait: queueStats.Wait, Latency: time.Since(started)})
		var streamed *streamStartedError
		if streamStarted.Load() || errors.As(err, &streamed) {
			h.logger.Warn("streamed chat completion interrupted", "request_id", requestID, "error", err)
			return
		}
		h.writeChatError(w, requestID, err)
		return
	}
	h.metrics.Record(metrics.Request{Succeeded: true, QueueWait: queueStats.Wait, Latency: time.Since(started), GeneratedTokens: result.generatedTokens, GenerationDuration: result.generationDuration, TokensPerSecond: result.tokensPerSecond})
	h.logger.Info("streamed chat completion completed", "request_id", requestID, "queue_wait_ms", queueStats.Wait.Milliseconds(), "latency_ms", time.Since(started).Milliseconds(), "generated_tokens", result.generatedTokens, "tokens_per_second", result.tokensPerSecond)
}

// activeLimits overlays the selected profile's safe runtime controls on the
// process defaults. Profiles can only be changed while the runner is stopped
// or unhealthy, so each request sees one stable active profile.
func (h *Handler) activeLimits() Config {
	limits := h.config
	activeProfile := h.runner.Status().ActiveProfile
	if activeProfile == "" {
		return limits
	}
	for _, profile := range h.runner.Profiles() {
		if profile.Name != activeProfile {
			continue
		}
		if profile.MaxGeneratedTokens > 0 {
			limits.MaxGeneratedTokens = profile.MaxGeneratedTokens
		}
		if profile.DefaultMaxTokens > 0 {
			limits.DefaultMaxTokens = profile.DefaultMaxTokens
		}
		if profile.QueueCapacity > 0 {
			limits.QueueCapacity = profile.QueueCapacity
		}
		if profile.RequestTimeout != "" {
			limits.RequestTimeout, _ = time.ParseDuration(profile.RequestTimeout)
		}
		if limits.DefaultMaxTokens > limits.MaxGeneratedTokens {
			limits.DefaultMaxTokens = limits.MaxGeneratedTokens
		}
		return limits
	}
	return limits
}

func chatErrorCategory(err error) string {
	switch {
	case errors.Is(err, queue.ErrFull):
		return "queue_full"
	case errors.Is(err, runner.ErrUnavailable):
		return "runner_unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "upstream_failure"
	}
}

func responseMetrics(response json.RawMessage) (int, time.Duration, float64) {
	var payload struct {
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Timings struct {
			PredictedMS        float64 `json:"predicted_ms"`
			PredictedPerSecond float64 `json:"predicted_per_second"`
			PredictedN         int     `json:"predicted_n"`
		} `json:"timings"`
	}
	if err := json.Unmarshal(response, &payload); err != nil {
		return 0, 0, 0
	}
	tokens := payload.Usage.CompletionTokens
	if tokens == 0 {
		tokens = payload.Timings.PredictedN
	}
	return tokens, time.Duration(payload.Timings.PredictedMS * float64(time.Millisecond)), payload.Timings.PredictedPerSecond
}

func (request chatRequest) validate(maxInputBytes, maxGeneratedTokens int) error {
	if len(request.Messages) == 0 {
		return errors.New("messages is required")
	}
	if len(request.Messages) > 64 {
		return errors.New("messages may contain at most 64 entries")
	}
	inputBytes := 0
	for _, message := range request.Messages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" {
			return errors.New("message role must be system, user, or assistant")
		}
		if strings.TrimSpace(message.Content) == "" {
			return errors.New("message content is required")
		}
		inputBytes += len(message.Content)
	}
	if inputBytes > maxInputBytes {
		return fmt.Errorf("message content exceeds %d bytes", maxInputBytes)
	}
	if request.MaxTokens < 0 || request.MaxTokens > maxGeneratedTokens {
		return fmt.Errorf("max_tokens must be between 0 and %d", maxGeneratedTokens)
	}
	if request.Temperature != nil && (*request.Temperature < 0 || *request.Temperature > 2) {
		return errors.New("temperature must be between 0 and 2")
	}
	return nil
}

func (h *Handler) proxyChat(ctx context.Context, chat chatRequest) (json.RawMessage, error) {
	endpoint, err := h.runner.ChatEndpoint()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(chat)
	if err != nil {
		return nil, fmt.Errorf("encode upstream request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read upstream response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upstream failure: HTTP %d", response.StatusCode)
	}
	if !json.Valid(responseBody) {
		return nil, errors.New("upstream returned invalid JSON")
	}
	return json.RawMessage(responseBody), nil
}

func (h *Handler) proxyStream(ctx context.Context, chat chatRequest, writer http.ResponseWriter, streamStarted *atomic.Bool) (chatResult, error) {
	var result chatResult
	endpoint, err := h.runner.ChatEndpoint()
	if err != nil {
		return result, err
	}
	body, err := json.Marshal(chat)
	if err != nil {
		return result, fmt.Errorf("encode upstream request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("build upstream request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	response, err := h.client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return result, context.DeadlineExceeded
		}
		return result, fmt.Errorf("upstream request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result, fmt.Errorf("upstream failure: HTTP %d", response.StatusCode)
	}

	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return result, errors.New("response writer does not support streaming")
	}
	writer.WriteHeader(http.StatusOK)
	result.streamStarted = true
	streamStarted.Store(true)
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))); json.Valid(data) {
			tokens, duration, tokensPerSecond := responseMetrics(json.RawMessage(data))
			if tokens > 0 {
				result.generatedTokens = tokens
			}
			if duration > 0 {
				result.generationDuration = duration
			}
			if tokensPerSecond > 0 {
				result.tokensPerSecond = tokensPerSecond
			}
		}
		if _, err := writer.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			return result, &streamStartedError{err: fmt.Errorf("write stream: %w", err)}
		}
		flusher.Flush()
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return result, &streamStartedError{err: context.DeadlineExceeded}
		}
		return result, &streamStartedError{err: fmt.Errorf("read upstream stream: %w", err)}
	}
	return result, nil
}

func (h *Handler) withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID)))
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		h.logger.Error("writing JSON response", "error", err)
	}
}

func (h *Handler) writeRunnerError(w http.ResponseWriter, err error) {
	code, status := "runner_failure", http.StatusInternalServerError
	switch {
	case errors.Is(err, runner.ErrNoModel):
		code, status = "no_model", http.StatusConflict
	case errors.Is(err, runner.ErrInvalidState):
		code, status = "invalid_runner_state", http.StatusConflict
	case errors.Is(err, runner.ErrUnavailable):
		code, status = "runner_unavailable", http.StatusServiceUnavailable
	default:
		var pathError *os.PathError
		if errors.As(err, &pathError) || containsAny(err.Error(), "model path", "model file") {
			code, status = "invalid_model", http.StatusBadRequest
		}
	}
	h.writeError(w, status, code, err.Error())
}

func (h *Handler) writeChatError(w http.ResponseWriter, requestID string, err error) {
	switch {
	case errors.Is(err, queue.ErrFull):
		h.writeError(w, http.StatusTooManyRequests, "queue_full", "runner request queue is full")
	case errors.Is(err, runner.ErrUnavailable):
		h.writeError(w, http.StatusServiceUnavailable, "runner_unavailable", err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		h.writeError(w, http.StatusGatewayTimeout, "timeout", "request timed out")
	default:
		h.logger.Warn("chat completion failed", "request_id", requestID, "error", err)
		h.writeError(w, http.StatusBadGateway, "upstream_failure", "runner request failed")
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	h.writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID")}})
}

func newRequestID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", bytes)
}

func requestIDFromRequest(request *http.Request) string {
	if requestID, ok := request.Context().Value(requestIDKey{}).(string); ok {
		return requestID
	}
	return ""
}

func (h *Handler) decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, h.config.MaxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func containsAny(value string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}

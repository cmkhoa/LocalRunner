// Package runner manages the lifecycle of a llama.cpp server process.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type State string

const (
	Stopped   State = "stopped"
	Starting  State = "starting"
	Ready     State = "ready"
	Unhealthy State = "unhealthy"
	Stopping  State = "stopping"
)

var (
	ErrNoModel      = errors.New("no model is registered")
	ErrInvalidState = errors.New("runner operation is not valid in the current state")
	ErrUnavailable  = errors.New("runner is unavailable")
)

type Config struct {
	Executable       string
	Address          string
	ContextSize      int
	GPULayers        int
	ReadinessTimeout time.Duration
	ShutdownTimeout  time.Duration
	AutoRestart      bool
	AutoRestartDelay time.Duration
	MaxAutoRestarts  int
}

// Profile provides a reusable, explicitly named local-model configuration.
// It intentionally exposes only safe runtime settings, never raw arguments.
type Profile struct {
	Name        string `json:"name"`
	ModelPath   string `json:"model_path"`
	ContextSize int    `json:"context_size,omitempty"`
	GPULayers   int    `json:"gpu_layers,omitempty"`
}

func DefaultConfig() Config {
	return Config{Executable: "llama-server", Address: "127.0.0.1:8081", ContextSize: 2048, GPULayers: 99, ReadinessTimeout: 60 * time.Second, ShutdownTimeout: 10 * time.Second, AutoRestartDelay: 2 * time.Second, MaxAutoRestarts: 3}
}

type Status struct {
	State               State  `json:"state"`
	PID                 *int   `json:"pid,omitempty"`
	ActiveModel         string `json:"active_model,omitempty"`
	ActiveProfile       string `json:"active_profile,omitempty"`
	Healthy             bool   `json:"healthy"`
	RestartCount        uint64 `json:"restart_count"`
	AutoRestartAttempts uint32 `json:"auto_restart_attempts,omitempty"`
}

// Manager owns one registered model and one child process at a time.
type Manager struct {
	mu                  sync.RWMutex
	config              Config
	baseConfig          Config
	logger              *slog.Logger
	client              *http.Client
	status              Status
	modelPath           string
	profiles            map[string]Profile
	process             *exec.Cmd
	processDone         chan struct{}
	hasStarted          bool
	autoRestartAttempts uint32
}

func NewManager(config Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Manager{config: config, baseConfig: config, logger: logger, client: &http.Client{Timeout: 2 * time.Second}, profiles: make(map[string]Profile), status: Status{State: Stopped}}
}

// Register validates and stores the one active GGUF model path.
func (m *Manager) Register(path string) (Status, error) {
	absPath, err := validateModelPath(path)
	if err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != Stopped && m.status.State != Unhealthy {
		return Status{}, fmt.Errorf("%w: stop the runner before registering a model", ErrInvalidState)
	}
	m.modelPath = absPath
	m.config = m.baseConfig
	m.status.ActiveModel = filepath.Base(absPath)
	m.status.ActiveProfile = ""
	return m.status, nil
}

// AddProfile validates and stores a named profile for later activation.
func (m *Manager) AddProfile(profile Profile) error {
	profile, err := m.validateProfile(profile)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.profiles[profile.Name]; exists {
		return fmt.Errorf("profile %q is already defined", profile.Name)
	}
	m.profiles[profile.Name] = profile
	return nil
}

// Profiles returns configured names and safe runtime settings, never prompts.
func (m *Manager) Profiles() []Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	profiles := make([]Profile, 0, len(m.profiles))
	for _, profile := range m.profiles {
		profiles = append(profiles, profile)
	}
	slices.SortFunc(profiles, func(left, right Profile) int { return strings.Compare(left.Name, right.Name) })
	return profiles
}

// ActivateProfile selects a configured model and settings while stopped.
func (m *Manager) ActivateProfile(name string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != Stopped && m.status.State != Unhealthy {
		return Status{}, fmt.Errorf("%w: stop the runner before activating a profile", ErrInvalidState)
	}
	profile, exists := m.profiles[name]
	if !exists {
		return Status{}, fmt.Errorf("profile %q was not found", name)
	}
	m.modelPath = profile.ModelPath
	m.config = m.baseConfig
	if profile.ContextSize > 0 {
		m.config.ContextSize = profile.ContextSize
	}
	if profile.GPULayers > 0 {
		m.config.GPULayers = profile.GPULayers
	}
	m.status.ActiveModel = filepath.Base(profile.ModelPath)
	m.status.ActiveProfile = profile.Name
	return m.status, nil
}

// Start launches llama-server and returns only after its health endpoint is up.
func (m *Manager) Start(ctx context.Context) (Status, error) {
	m.mu.Lock()
	if m.status.State != Stopped && m.status.State != Unhealthy {
		state := m.status.State
		m.mu.Unlock()
		return Status{}, fmt.Errorf("%w: %s", ErrInvalidState, state)
	}
	if m.modelPath == "" {
		m.mu.Unlock()
		return Status{}, ErrNoModel
	}
	if err := validateRunnerConfig(m.config); err != nil {
		m.mu.Unlock()
		return Status{}, err
	}
	executable, err := exec.LookPath(m.config.Executable)
	if err != nil {
		m.mu.Unlock()
		return Status{}, fmt.Errorf("llama-server executable: %w", err)
	}
	host, port, _ := net.SplitHostPort(m.config.Address)
	cmd := exec.Command(executable, "--model", m.modelPath, "--host", host, "--port", port, "--ctx-size", strconv.Itoa(m.config.ContextSize), "--gpu-layers", strconv.Itoa(m.config.GPULayers))
	cmd.Stdout = logWriter{logger: m.logger, stream: "stdout"}
	cmd.Stderr = logWriter{logger: m.logger, stream: "stderr"}
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return Status{}, fmt.Errorf("start llama-server: %w", err)
	}
	pid := cmd.Process.Pid
	m.process, m.processDone = cmd, make(chan struct{})
	m.status.State, m.status.PID, m.status.Healthy = Starting, &pid, false
	done := m.processDone
	m.mu.Unlock()
	go m.watch(cmd, done)
	readinessCtx, cancel := context.WithTimeout(ctx, m.config.ReadinessTimeout)
	defer cancel()
	if err := m.waitUntilHealthy(readinessCtx); err != nil {
		m.stopProcess(cmd, done)
		m.mu.Lock()
		if m.process == cmd {
			m.status.State, m.status.Healthy = Unhealthy, false
		}
		m.mu.Unlock()
		return Status{}, fmt.Errorf("runner readiness check: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.process != cmd || m.status.State != Starting {
		return Status{}, fmt.Errorf("%w: runner stopped while starting", ErrInvalidState)
	}
	m.status.State, m.status.Healthy = Ready, true
	if m.hasStarted {
		m.status.RestartCount++
	}
	m.hasStarted = true
	return m.status, nil
}

// Stop asks the child process to terminate and escalates after a bounded timeout.
func (m *Manager) Stop(ctx context.Context) (Status, error) {
	m.mu.Lock()
	if m.status.State == Stopped {
		m.mu.Unlock()
		return Status{}, fmt.Errorf("%w: already stopped", ErrInvalidState)
	}
	cmd, done := m.process, m.processDone
	m.status.State, m.status.Healthy = Stopping, false
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return Status{}, fmt.Errorf("%w: process is unavailable", ErrInvalidState)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		m.logger.Warn("graceful runner shutdown signal failed", "error", err)
	}
	timeout := m.config.ShutdownTimeout
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		m.logger.Warn("runner shutdown timed out; killing process")
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return Status{}, fmt.Errorf("kill runner process: %w", err)
		}
		<-done
	case <-ctx.Done():
		return Status{}, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status, nil
}

func (m *Manager) Status() Status { m.mu.RLock(); defer m.mu.RUnlock(); return m.status }

// ChatEndpoint returns the local upstream URL only while the managed process is
// ready. API handlers must use this instead of reading runner configuration.
func (m *Manager) ChatEndpoint() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.status.State != Ready || !m.status.Healthy {
		return "", fmt.Errorf("%w: %s", ErrUnavailable, m.status.State)
	}
	return "http://" + m.config.Address + "/v1/chat/completions", nil
}

func (m *Manager) waitUntilHealthy(ctx context.Context) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+m.config.Address+"/health", nil)
		if err == nil {
			response, requestErr := m.client.Do(request)
			if requestErr == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) watch(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	var restartDelay time.Duration
	shouldRestart := false
	m.mu.Lock()
	if m.process == cmd {
		m.process, m.processDone, m.status.PID, m.status.Healthy = nil, nil, nil, false
		if m.status.State == Stopping {
			m.status.State = Stopped
		} else {
			m.status.State = Unhealthy
			if err != nil {
				m.logger.Warn("runner process exited", "error", err)
			}
			if m.config.AutoRestart && m.modelPath != "" && m.autoRestartAttempts < uint32(m.config.MaxAutoRestarts) {
				m.autoRestartAttempts++
				m.status.AutoRestartAttempts = m.autoRestartAttempts
				restartDelay, shouldRestart = m.config.AutoRestartDelay, true
				m.logger.Warn("scheduling automatic runner restart", "attempt", m.autoRestartAttempts, "delay_ms", restartDelay.Milliseconds())
			}
		}
	}
	close(done)
	m.mu.Unlock()
	if shouldRestart {
		go m.restartAfter(restartDelay)
	}
}

func (m *Manager) restartAfter(delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C
	m.mu.RLock()
	eligible := m.status.State == Unhealthy && m.process == nil
	readinessTimeout := m.config.ReadinessTimeout
	m.mu.RUnlock()
	if !eligible {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), readinessTimeout)
	defer cancel()
	if _, err := m.Start(ctx); err != nil {
		m.logger.Warn("automatic runner restart failed", "error", err)
	}
}

func (m *Manager) stopProcess(cmd *exec.Cmd, done chan struct{}) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	<-done
}

func validateModelPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("model path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve model path: %w", err)
	}
	if strings.ToLower(filepath.Ext(absPath)) != ".gguf" {
		return "", errors.New("model path must end in .gguf")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("model path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("model path must be a regular file")
	}
	if info.Size() == 0 {
		return "", errors.New("model file must not be empty")
	}
	return absPath, nil
}

func (m *Manager) validateProfile(profile Profile) (Profile, error) {
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" {
		return Profile{}, errors.New("profile name is required")
	}
	if strings.ContainsAny(profile.Name, "/\\") {
		return Profile{}, errors.New("profile name must not contain path separators")
	}
	modelPath, err := validateModelPath(profile.ModelPath)
	if err != nil {
		return Profile{}, err
	}
	profile.ModelPath = modelPath
	if profile.ContextSize < 0 || profile.GPULayers < 0 {
		return Profile{}, errors.New("profile runtime settings must not be negative")
	}
	return profile, nil
}

func validateRunnerConfig(config Config) error {
	if strings.TrimSpace(config.Executable) == "" {
		return errors.New("llama-server executable is required")
	}
	host, port, err := net.SplitHostPort(config.Address)
	if err != nil || (host != "127.0.0.1" && host != "localhost") || port == "" {
		return errors.New("runner address must be a localhost host:port")
	}
	if config.ContextSize <= 0 || config.GPULayers < 0 || config.ReadinessTimeout <= 0 || config.ShutdownTimeout <= 0 || config.AutoRestartDelay <= 0 || config.MaxAutoRestarts < 0 {
		return errors.New("runner configuration has invalid limits")
	}
	return nil
}

type logWriter struct {
	logger *slog.Logger
	stream string
}

func (w logWriter) Write(data []byte) (int, error) {
	if message := strings.TrimSpace(string(data)); message != "" {
		w.logger.Debug("llama-server output", "stream", w.stream, "message", message)
	}
	return len(data), nil
}

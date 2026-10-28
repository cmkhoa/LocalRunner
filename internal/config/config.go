// Package config reads LocalRunner's process-level configuration.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"localrunner/internal/api"
	"localrunner/internal/runner"
)

const defaultListenAddress = "127.0.0.1:8080"

// Config contains settings needed before a model runner can be started.
type Config struct {
	ListenAddress string
	Runner        runner.Config
	API           api.Config
	Profiles      []runner.Profile
}

// FromEnv loads safe defaults for the local HTTP API.
func FromEnv() (Config, error) {
	listenAddress := os.Getenv("LOCALRUNNER_LISTEN_ADDRESS")
	if listenAddress == "" {
		listenAddress = defaultListenAddress
	}

	host, _, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return Config{}, fmt.Errorf("LOCALRUNNER_LISTEN_ADDRESS must be host:port: %w", err)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return Config{}, fmt.Errorf("LOCALRUNNER_LISTEN_ADDRESS must bind to localhost")
	}

	runnerConfig := runner.DefaultConfig()
	apiConfig := api.DefaultConfig()
	runnerConfig.Executable = envOrDefault("LOCALRUNNER_LLAMA_SERVER_PATH", runnerConfig.Executable)
	runnerConfig.Address = envOrDefault("LOCALRUNNER_RUNNER_ADDRESS", runnerConfig.Address)
	if err := validateLoopbackAddress("LOCALRUNNER_RUNNER_ADDRESS", runnerConfig.Address); err != nil {
		return Config{}, err
	}
	if runnerConfig.ContextSize, err = positiveIntEnv("LOCALRUNNER_CONTEXT_SIZE", runnerConfig.ContextSize); err != nil {
		return Config{}, err
	}
	if runnerConfig.GPULayers, err = nonNegativeIntEnv("LOCALRUNNER_GPU_LAYERS", runnerConfig.GPULayers); err != nil {
		return Config{}, err
	}
	if runnerConfig.ReadinessTimeout, err = durationEnv("LOCALRUNNER_READINESS_TIMEOUT", runnerConfig.ReadinessTimeout); err != nil {
		return Config{}, err
	}
	if runnerConfig.ShutdownTimeout, err = durationEnv("LOCALRUNNER_SHUTDOWN_TIMEOUT", runnerConfig.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if runnerConfig.AutoRestart, err = boolEnv("LOCALRUNNER_AUTO_RESTART", runnerConfig.AutoRestart); err != nil {
		return Config{}, err
	}
	if runnerConfig.AutoRestartDelay, err = durationEnv("LOCALRUNNER_AUTO_RESTART_DELAY", runnerConfig.AutoRestartDelay); err != nil {
		return Config{}, err
	}
	if runnerConfig.MaxAutoRestarts, err = nonNegativeIntEnv("LOCALRUNNER_MAX_AUTO_RESTARTS", runnerConfig.MaxAutoRestarts); err != nil {
		return Config{}, err
	}
	if apiConfig.MaxRequestBytes, err = positiveInt64Env("LOCALRUNNER_MAX_REQUEST_BYTES", apiConfig.MaxRequestBytes); err != nil {
		return Config{}, err
	}
	if apiConfig.MaxInputBytes, err = positiveIntEnv("LOCALRUNNER_MAX_INPUT_BYTES", apiConfig.MaxInputBytes); err != nil {
		return Config{}, err
	}
	if apiConfig.MaxGeneratedTokens, err = positiveIntEnv("LOCALRUNNER_MAX_GENERATED_TOKENS", apiConfig.MaxGeneratedTokens); err != nil {
		return Config{}, err
	}
	if apiConfig.DefaultMaxTokens, err = positiveIntEnv("LOCALRUNNER_DEFAULT_MAX_TOKENS", apiConfig.DefaultMaxTokens); err != nil {
		return Config{}, err
	}
	if apiConfig.QueueCapacity, err = positiveIntEnv("LOCALRUNNER_QUEUE_CAPACITY", apiConfig.QueueCapacity); err != nil {
		return Config{}, err
	}
	if apiConfig.RequestTimeout, err = durationEnv("LOCALRUNNER_REQUEST_TIMEOUT", apiConfig.RequestTimeout); err != nil {
		return Config{}, err
	}
	if err := apiConfig.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid API limits: %w", err)
	}

	profiles, err := profilesFromEnv()
	if err != nil {
		return Config{}, err
	}
	return Config{ListenAddress: listenAddress, Runner: runnerConfig, API: apiConfig, Profiles: profiles}, nil
}

func profilesFromEnv() ([]runner.Profile, error) {
	path := os.Getenv("LOCALRUNNER_PROFILES_PATH")
	if path == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read LOCALRUNNER_PROFILES_PATH: %w", err)
	}
	var document struct {
		Profiles map[string]runner.Profile `json:"profiles"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse LOCALRUNNER_PROFILES_PATH: %w", err)
	}
	profiles := make([]runner.Profile, 0, len(document.Profiles))
	for name, profile := range document.Profiles {
		profile.Name = name
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func validateLoopbackAddress(key, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return fmt.Errorf("%s must be host:port", key)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("%s must bind to localhost", key)
	}
	return nil
}

func positiveIntEnv(key string, fallback int) (int, error) {
	value, err := nonNegativeIntEnv(key, fallback)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}
	return value, nil
}

func nonNegativeIntEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return parsed, nil
}

func positiveInt64Env(key string, fallback int64) (int64, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", key)
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return parsed, nil
}

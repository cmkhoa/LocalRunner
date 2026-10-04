// Package config reads LocalRunner's process-level configuration.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"localrunner/internal/runner"
)

const defaultListenAddress = "127.0.0.1:8080"

// Config contains settings needed before a model runner can be started.
type Config struct {
	ListenAddress string
	Runner        runner.Config
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

	return Config{ListenAddress: listenAddress, Runner: runnerConfig}, nil
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

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromEnvDefaultsToLoopback(t *testing.T) {
	t.Setenv("LOCALRUNNER_LISTEN_ADDRESS", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if got, want := cfg.ListenAddress, "127.0.0.1:8080"; got != want {
		t.Errorf("ListenAddress = %q, want %q", got, want)
	}
}

func TestFromEnvRejectsNonLoopbackHost(t *testing.T) {
	t.Setenv("LOCALRUNNER_LISTEN_ADDRESS", "0.0.0.0:8080")

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() error = nil, want error")
	}
}

func TestFromEnvLoadsProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	contents := []byte(`{"profiles":{"small":{"model_path":"/tmp/model.gguf","context_size":1024,"gpu_layers":4}}}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALRUNNER_PROFILES_PATH", path)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if len(cfg.Profiles) != 1 {
		t.Fatalf("Profiles length = %d, want 1", len(cfg.Profiles))
	}
	if got, want := cfg.Profiles[0].Name, "small"; got != want {
		t.Errorf("profile name = %q, want %q", got, want)
	}
}

func TestFromEnvLoadsProfileRuntimeLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	contents := []byte(`{"profiles":{"small":{"model_path":"/tmp/model.gguf","readiness_timeout":"30s","request_timeout":"5s","max_generated_tokens":32,"default_max_tokens":16,"queue_capacity":2}}}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALRUNNER_PROFILES_PATH", path)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	profile := cfg.Profiles[0]
	if got, want := profile.RequestTimeout, "5s"; got != want {
		t.Errorf("RequestTimeout = %q, want %q", got, want)
	}
	if got, want := profile.QueueCapacity, 2; got != want {
		t.Errorf("QueueCapacity = %d, want %d", got, want)
	}
}

func TestFromEnvLoadsAutoRestartSettings(t *testing.T) {
	t.Setenv("LOCALRUNNER_AUTO_RESTART", "true")
	t.Setenv("LOCALRUNNER_AUTO_RESTART_DELAY", "3s")
	t.Setenv("LOCALRUNNER_MAX_AUTO_RESTARTS", "2")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if !cfg.Runner.AutoRestart {
		t.Error("AutoRestart = false, want true")
	}
	if got, want := cfg.Runner.AutoRestartDelay.String(), "3s"; got != want {
		t.Errorf("AutoRestartDelay = %q, want %q", got, want)
	}
	if got, want := cfg.Runner.MaxAutoRestarts, 2; got != want {
		t.Errorf("MaxAutoRestarts = %d, want %d", got, want)
	}
}

func TestFromEnvLoadsAPILimits(t *testing.T) {
	t.Setenv("LOCALRUNNER_MAX_REQUEST_BYTES", "2048")
	t.Setenv("LOCALRUNNER_MAX_INPUT_BYTES", "1024")
	t.Setenv("LOCALRUNNER_MAX_GENERATED_TOKENS", "32")
	t.Setenv("LOCALRUNNER_DEFAULT_MAX_TOKENS", "16")
	t.Setenv("LOCALRUNNER_QUEUE_CAPACITY", "3")
	t.Setenv("LOCALRUNNER_REQUEST_TIMEOUT", "5s")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if got, want := cfg.API.MaxRequestBytes, int64(2048); got != want {
		t.Errorf("MaxRequestBytes = %d, want %d", got, want)
	}
	if got, want := cfg.API.MaxGeneratedTokens, 32; got != want {
		t.Errorf("MaxGeneratedTokens = %d, want %d", got, want)
	}
	if got, want := cfg.API.DefaultMaxTokens, 16; got != want {
		t.Errorf("DefaultMaxTokens = %d, want %d", got, want)
	}
	if got, want := cfg.API.QueueCapacity, 3; got != want {
		t.Errorf("QueueCapacity = %d, want %d", got, want)
	}
	if got, want := cfg.API.RequestTimeout.String(), "5s"; got != want {
		t.Errorf("RequestTimeout = %q, want %q", got, want)
	}
}

func TestFromEnvRejectsDefaultMaxTokensAboveMaximum(t *testing.T) {
	t.Setenv("LOCALRUNNER_MAX_GENERATED_TOKENS", "16")
	t.Setenv("LOCALRUNNER_DEFAULT_MAX_TOKENS", "17")

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() error = nil, want error")
	}
}

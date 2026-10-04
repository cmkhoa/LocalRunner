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

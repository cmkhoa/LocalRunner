package runner

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterAcceptsNonEmptyGGUFFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test-model.gguf")
	if err := os.WriteFile(path, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(DefaultConfig(), slog.Default())
	status, err := manager.Register(path)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got, want := status.ActiveModel, "test-model.gguf"; got != want {
		t.Errorf("ActiveModel = %q, want %q", got, want)
	}
	if got, want := status.State, Stopped; got != want {
		t.Errorf("State = %q, want %q", got, want)
	}
}

func TestRegisterRejectsNonGGUFPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.txt")
	if err := os.WriteFile(path, []byte("not a model"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewManager(DefaultConfig(), slog.Default()).Register(path); err == nil {
		t.Fatal("Register() error = nil, want an error")
	}
}

func TestStartRequiresRegisteredModel(t *testing.T) {
	if _, err := NewManager(DefaultConfig(), slog.Default()).Start(t.Context()); err != ErrNoModel {
		t.Fatalf("Start() error = %v, want %v", err, ErrNoModel)
	}
}

func TestActivateProfileSelectsValidatedModelAndSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile-model.gguf")
	if err := os.WriteFile(path, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(DefaultConfig(), slog.Default())
	if err := manager.AddProfile(Profile{Name: "small", ModelPath: path, ContextSize: 1024, GPULayers: 4, ReadinessTimeout: "3s"}); err != nil {
		t.Fatalf("AddProfile() error = %v", err)
	}
	status, err := manager.ActivateProfile("small")
	if err != nil {
		t.Fatalf("ActivateProfile() error = %v", err)
	}
	if got, want := status.ActiveModel, "profile-model.gguf"; got != want {
		t.Errorf("ActiveModel = %q, want %q", got, want)
	}
	if got, want := status.ActiveProfile, "small"; got != want {
		t.Errorf("ActiveProfile = %q, want %q", got, want)
	}
	if got, want := manager.config.ReadinessTimeout.String(), "3s"; got != want {
		t.Errorf("ReadinessTimeout = %q, want %q", got, want)
	}
	profiles := manager.Profiles()
	if len(profiles) != 1 || profiles[0].Name != "small" {
		t.Errorf("Profiles() = %#v, want small", profiles)
	}
}

func TestDiagnosticIsBoundedAndSafeForStatus(t *testing.T) {
	value := diagnostic("runner failed\n" + strings.Repeat("x", 300))
	if strings.ContainsAny(value, "\n\r\t") {
		t.Errorf("diagnostic contains a control character: %q", value)
	}
	if got, want := value[:13], "runner failed"; got != want {
		t.Errorf("diagnostic prefix = %q, want %q", got, want)
	}
	if !strings.HasSuffix(value, "…") {
		t.Errorf("diagnostic = %q, want truncated suffix", value)
	}
	if len(value) > 256 {
		t.Errorf("diagnostic length = %d, want at most 256", len(value))
	}
}

package runner

import (
	"log/slog"
	"os"
	"path/filepath"
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

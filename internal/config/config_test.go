package config

import "testing"

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

package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Clear any env vars that might be set.
	for _, key := range []string{"ABLETON_OSC_HOST", "ABLETON_OSC_PORT", "ABLETON_OSC_CLIENT_PORT", "ABLETON_OSC_TIMEOUT_MS"} {
		t.Setenv(key, "")
	}

	cfg := Load()

	if cfg.AbletonHost != "127.0.0.1" {
		t.Errorf("AbletonHost = %q, want %q", cfg.AbletonHost, "127.0.0.1")
	}
	if cfg.AbletonPort != 11000 {
		t.Errorf("AbletonPort = %d, want %d", cfg.AbletonPort, 11000)
	}
	if cfg.AbletonClientPort != 11001 {
		t.Errorf("AbletonClientPort = %d, want %d", cfg.AbletonClientPort, 11001)
	}
	if cfg.Timeout != 500*time.Millisecond {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, 500*time.Millisecond)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("ABLETON_OSC_HOST", "192.168.1.100")
	t.Setenv("ABLETON_OSC_PORT", "12000")
	t.Setenv("ABLETON_OSC_CLIENT_PORT", "12001")
	t.Setenv("ABLETON_OSC_TIMEOUT_MS", "1000")

	cfg := Load()

	if cfg.AbletonHost != "192.168.1.100" {
		t.Errorf("AbletonHost = %q, want %q", cfg.AbletonHost, "192.168.1.100")
	}
	if cfg.AbletonPort != 12000 {
		t.Errorf("AbletonPort = %d, want %d", cfg.AbletonPort, 12000)
	}
	if cfg.AbletonClientPort != 12001 {
		t.Errorf("AbletonClientPort = %d, want %d", cfg.AbletonClientPort, 12001)
	}
	if cfg.Timeout != 1000*time.Millisecond {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, 1000*time.Millisecond)
	}
}

func TestLoadInvalidEnvFallsBackToDefaults(t *testing.T) {
	t.Setenv("ABLETON_OSC_PORT", "not-a-number")
	t.Setenv("ABLETON_OSC_TIMEOUT_MS", "-100")

	cfg := Load()

	if cfg.AbletonPort != 11000 {
		t.Errorf("AbletonPort = %d, want %d (default)", cfg.AbletonPort, 11000)
	}
	if cfg.Timeout != 500*time.Millisecond {
		t.Errorf("Timeout = %v, want %v (default)", cfg.Timeout, 500*time.Millisecond)
	}
}

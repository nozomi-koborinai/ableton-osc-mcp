package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAbletonHost       = "127.0.0.1"
	defaultAbletonPort       = 11000
	defaultAbletonClientPort = 11001
	defaultTimeoutMs         = 500
)

// Config holds the application configuration.
type Config struct {
	AbletonHost       string
	AbletonPort       int
	AbletonClientPort int
	Timeout           time.Duration
	TasteProfilePath  string
	SplicePath        string // optional; empty means auto-detect common Splice folders
}

// Load reads configuration from environment variables with defaults.
func Load() Config {
	host := strings.TrimSpace(os.Getenv("ABLETON_OSC_HOST"))
	if host == "" {
		host = defaultAbletonHost
	}
	return Config{
		AbletonHost:       host,
		AbletonPort:       envInt("ABLETON_OSC_PORT", defaultAbletonPort),
		AbletonClientPort: envInt("ABLETON_OSC_CLIENT_PORT", defaultAbletonClientPort),
		Timeout:           envDurationMs("ABLETON_OSC_TIMEOUT_MS", defaultTimeoutMs),
		TasteProfilePath:  envString("ABLETON_OSC_TASTE_PROFILE_PATH", defaultTasteProfilePath()),
		SplicePath:        strings.TrimSpace(os.Getenv("ABLETON_OSC_SPLICE_PATH")),
	}
}

func defaultTasteProfilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ableton-osc-mcp", "taste-profile.json")
}

func envString(key string, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func envDurationMs(key string, defMs int) time.Duration {
	ms := envInt(key, defMs)
	if ms <= 0 {
		ms = defMs
	}
	return time.Duration(ms) * time.Millisecond
}

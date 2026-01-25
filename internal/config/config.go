package config

import (
	"os"
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
	}
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

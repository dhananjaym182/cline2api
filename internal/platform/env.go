package platform

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvInt reads a positive integer environment variable, falling back to def
// when the value is missing, invalid, or not positive.
func EnvInt(key string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// EnvDurationFromMS reads a positive duration in milliseconds, falling back to
// def when the value is missing, invalid, or not positive.
func EnvDurationFromMS(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return time.Duration(n) * time.Millisecond
}

package typesafe

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// Environment names and defaults used by explicit configuration loading.
const (
	APIKeyEnv             = "TYPESAFE_API_KEY" //nolint:gosec // G101: environment variable name, not an embedded credential.
	BaseURLEnv            = "TYPESAFE_BASE_URL"
	DefaultModelEnv       = "TYPESAFE_DEFAULT_MODEL"
	LogLevelEnv           = "TYPESAFE_LOG_LEVEL"
	DefaultBaseURL        = "https://api.typesafe.ai"
	DefaultModel          = "jev-latest"
	DefaultAttemptTimeout = 10 * time.Second
	DefaultCallTimeout    = 30 * time.Second
	// LogLevelOff disables SDK diagnostics, including body logging.
	LogLevelOff slog.Level = 127
)

// LoadEnv fills omitted config fields from TYPESAFE_* environment variables.
// Explicit nonempty strings and LogLevel take precedence; blank environment values
// and unknown log levels are ignored. It never changes process-wide logging.
// Supported levels are debug, info, warn, warning, error and off. A supplied
// logger is still required to enable diagnostics. NewClient validates the result.
func LoadEnv(config Config) Config {
	for _, item := range []struct {
		value *string
		name  string
	}{{value: &config.APIKey, name: APIKeyEnv}, {value: &config.BaseURL, name: BaseURLEnv}, {value: &config.Model, name: DefaultModelEnv}} {
		if *item.value == "" {
			*item.value = strings.TrimSpace(os.Getenv(item.name))
		}
	}

	if config.LogLevel == nil {
		level, ok := map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "warning": slog.LevelWarn, "error": slog.LevelError, "off": LogLevelOff}[strings.ToLower(strings.TrimSpace(os.Getenv(LogLevelEnv)))]
		if ok {
			config.LogLevel = &level
		}
	} else {
		level := *config.LogLevel

		config.LogLevel = &level
	}

	return config
}

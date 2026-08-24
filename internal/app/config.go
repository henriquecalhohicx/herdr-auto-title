package app

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"herdr-auto-title/internal/debounce"
	"herdr-auto-title/internal/resolver"
)

// Environment variables Auto Title reads. V1 has no configuration file.
const (
	EnvDebug     = "HERDR_AUTO_TITLE_DEBUG"
	EnvDebounce  = "HERDR_AUTO_TITLE_DEBOUNCE_MS"
	EnvMaxLength = "HERDR_AUTO_TITLE_MAX_LENGTH"
)

// Config is Auto Title's runtime configuration.
type Config struct {
	Debug     bool
	Debounce  time.Duration
	MaxWait   time.Duration
	MaxLength int
}

// LoadConfig reads configuration from the environment. Unusable values are
// reported as warnings and the default is kept, so a typo never stops the
// plugin from running.
func LoadConfig() (Config, []string) {
	cfg := Config{
		Debounce:  debounce.DefaultDelay,
		MaxLength: resolver.DefaultMaxLength,
	}
	var warnings []string

	if raw := os.Getenv(EnvDebug); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s=%q is not a boolean, ignoring", EnvDebug, raw))
		} else {
			cfg.Debug = value
		}
	}

	if raw := os.Getenv(EnvDebounce); raw != "" {
		ms, err := strconv.Atoi(raw)
		switch {
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("%s=%q is not a number, using %s", EnvDebounce, raw, cfg.Debounce))
		case ms <= 0:
			warnings = append(warnings, fmt.Sprintf("%s=%d must be positive, using %s", EnvDebounce, ms, cfg.Debounce))
		default:
			cfg.Debounce = time.Duration(ms) * time.Millisecond
		}
	}

	if raw := os.Getenv(EnvMaxLength); raw != "" {
		length, err := strconv.Atoi(raw)
		switch {
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("%s=%q is not a number, using %d", EnvMaxLength, raw, cfg.MaxLength))
		case length <= 0:
			warnings = append(warnings, fmt.Sprintf("%s=%d must be positive, using %d", EnvMaxLength, length, cfg.MaxLength))
		default:
			cfg.MaxLength = length
		}
	}

	// The cap follows the window, so raising the window calms a noisy pane
	// instead of being overridden.
	cfg.MaxWait = cfg.Debounce * debounce.MaxWaitFactor

	return cfg, warnings
}

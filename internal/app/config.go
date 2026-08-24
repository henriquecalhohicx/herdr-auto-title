package app

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"herdr-auto-title/internal/resolver"
)

// Environment variables Auto Title reads. V1 has no configuration file.
const (
	EnvDebug     = "HERDR_AUTO_TITLE_DEBUG"
	EnvPoll      = "HERDR_AUTO_TITLE_POLL_MS"
	EnvMaxLength = "HERDR_AUTO_TITLE_MAX_LENGTH"
)

// DefaultPoll is how often the session is read.
//
// A snapshot of a six-pane session measured 0.47 ms and 6 KB, so two polls a
// second cost about a thousandth of a core. Half a second is short enough that
// a rename lands while the user is still looking at the tab they changed.
const DefaultPoll = 500 * time.Millisecond

// Config is Auto Title's runtime configuration.
type Config struct {
	Debug     bool
	Poll      time.Duration
	MaxLength int
}

// LoadConfig reads configuration from the environment. Unusable values are
// reported as warnings and the default is kept, so a typo never stops the
// plugin from running.
func LoadConfig() (Config, []string) {
	cfg := Config{
		Poll:      DefaultPoll,
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

	if raw := os.Getenv(EnvPoll); raw != "" {
		ms, err := strconv.Atoi(raw)
		switch {
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("%s=%q is not a number, using %s", EnvPoll, raw, cfg.Poll))
		case ms <= 0:
			warnings = append(warnings, fmt.Sprintf("%s=%d must be positive, using %s", EnvPoll, ms, cfg.Poll))
		default:
			cfg.Poll = time.Duration(ms) * time.Millisecond
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

	return cfg, warnings
}

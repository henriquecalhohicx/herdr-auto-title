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
	EnvSweep     = "HERDR_AUTO_TITLE_SWEEP_MS"
)

// DefaultSweep is how often the session is swept for changes the event stream
// has not delivered yet.
//
// Subscribing makes Herdr replay a backlog before the live events, and live
// events queue behind it — measured at thirteen seconds on a five-pane session,
// and the drain grows by about ten seconds for every additional active pane.
// Without a sweep, a tab opened during that window keeps its number until the
// replay finishes.
const DefaultSweep = 2 * time.Second

// Config is Auto Title's runtime configuration.
type Config struct {
	Debug     bool
	Debounce  time.Duration
	MaxWait   time.Duration
	MaxLength int
	// Sweep is the interval between session sweeps. Zero disables them and
	// leaves Auto Title relying on the event stream alone.
	Sweep time.Duration
}

// LoadConfig reads configuration from the environment. Unusable values are
// reported as warnings and the default is kept, so a typo never stops the
// plugin from running.
func LoadConfig() (Config, []string) {
	cfg := Config{
		Debounce:  debounce.DefaultDelay,
		MaxLength: resolver.DefaultMaxLength,
		Sweep:     DefaultSweep,
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

	if raw := os.Getenv(EnvSweep); raw != "" {
		ms, err := strconv.Atoi(raw)
		switch {
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("%s=%q is not a number, using %s", EnvSweep, raw, cfg.Sweep))
		case ms < 0:
			warnings = append(warnings, fmt.Sprintf("%s=%d cannot be negative, using %s", EnvSweep, ms, cfg.Sweep))
		default:
			// Zero is meaningful: it turns sweeping off.
			cfg.Sweep = time.Duration(ms) * time.Millisecond
		}
	}

	// The cap follows the window, so raising the window calms a noisy pane
	// instead of being overridden.
	cfg.MaxWait = cfg.Debounce * debounce.MaxWaitFactor

	return cfg, warnings
}

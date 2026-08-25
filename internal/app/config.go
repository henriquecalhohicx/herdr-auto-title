package app

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/kryptamine/herdr-auto-title/internal/resolver"
	"github.com/kryptamine/herdr-auto-title/internal/state"
)

// Environment variables Auto Title reads. V1 has no configuration file.
const (
	EnvDebug     = "HERDR_AUTO_TITLE_DEBUG"
	EnvPoll      = "HERDR_AUTO_TITLE_POLL_MS"
	EnvMaxLength = "HERDR_AUTO_TITLE_MAX_LENGTH"
	EnvManual    = "HERDR_AUTO_TITLE_MANUAL_FILE"
)

// DefaultPoll is how often the session is read. A six-pane snapshot measured
// 0.47 ms and 6 KB, so twice a second costs about a thousandth of a core, and
// a rename lands while the user is still looking at the tab.
const DefaultPoll = 500 * time.Millisecond

// Config is Auto Title's runtime configuration.
type Config struct {
	Debug bool
	Poll  time.Duration
	// MaxLength is measured in columns of the tab bar rather than in
	// characters: a CJK character or an emoji takes two.
	MaxLength int
	// ManualPath is where tabs the user renamed by hand are remembered across
	// restarts. Empty keeps them in memory only.
	ManualPath string
}

// LoadConfig reads configuration from the environment. Unusable values are
// reported as warnings and the default is kept, so a typo never stops the
// plugin from running.
func LoadConfig() (Config, []string) {
	cfg := Config{
		Poll:       DefaultPoll,
		MaxLength:  resolver.DefaultMaxLength,
		ManualPath: state.DefaultManualPath(),
	}
	var warnings []string

	cfg.Debug = fromEnv(&warnings, EnvDebug, cfg.Debug, boolean)
	cfg.Poll = fromEnv(&warnings, EnvPoll, cfg.Poll, milliseconds)
	cfg.MaxLength = fromEnv(&warnings, EnvMaxLength, cfg.MaxLength, count)
	// A path needs neither parsing nor checking, so it does not go through
	// fromEnv: any string the user set is the path they meant.
	if raw := os.Getenv(EnvManual); raw != "" {
		cfg.ManualPath = raw
	}

	return cfg, warnings
}

// fromEnv returns what the environment says name is, or fallback when it says
// nothing usable. Nothing here fails: a typo must not stop the plugin starting,
// which is why it warns instead, and is the only place a warning is worded.
func fromEnv[T any](warnings *[]string, name string, fallback T, convert converter[T]) T {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := convert(raw)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s=%q %s, using %v", name, raw, err, fallback))
		return fallback
	}
	return value
}

// converter turns the raw text of a variable into a value, or says what is
// wrong with it.
type converter[T any] func(raw string) (T, error)

// The reasons a variable is rejected. Each reads as the middle of the warning
// it lands in: `HERDR_AUTO_TITLE_POLL_MS="0" must be positive, using 500ms`.
var (
	errNotBoolean  = errors.New("is not a boolean")
	errNotNumber   = errors.New("is not a number")
	errNotPositive = errors.New("must be positive")
)

func boolean(raw string) (bool, error) {
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errNotBoolean
	}
	return value, nil
}

// count reads a number that must be positive. Zero would stop the plugin doing
// anything: a poll interval of zero spins, and a title of no length is no title.
func count(raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errNotNumber
	}
	if value <= 0 {
		return 0, errNotPositive
	}
	return value, nil
}

// milliseconds reads a duration written as a count of them, which is easier to
// pass through a shell than "500ms".
func milliseconds(raw string) (time.Duration, error) {
	value, err := count(raw)
	if err != nil {
		return 0, err
	}
	return time.Duration(value) * time.Millisecond, nil
}

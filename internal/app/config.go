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

// DefaultPoll is how often the session is read.
//
// A snapshot of a six-pane session measured 0.47 ms and 6 KB, so two polls a
// second cost about a thousandth of a core. Half a second is short enough that
// a rename lands while the user is still looking at the tab they changed.
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

	cfg.Debug = read(&warnings, EnvDebug, cfg.Debug, boolean)
	cfg.Poll = read(&warnings, EnvPoll, cfg.Poll, milliseconds)
	cfg.MaxLength = read(&warnings, EnvMaxLength, cfg.MaxLength, number(positive))
	cfg.ManualPath = read(&warnings, EnvManual, cfg.ManualPath, text)

	return cfg, warnings
}

// read returns what the environment says name is, or fallback when it says
// nothing usable.
//
// Nothing here fails. A variable that cannot be used is reported and the
// default is kept, so a typo in a shell profile never stops the plugin from
// starting — which is why this appends a warning rather than returning an
// error, and why it is the only place a warning is worded.
func read[T any](warnings *[]string, name string, fallback T, convert converter[T]) T {
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

// validator says what is wrong with a number, or nothing when it is usable.
// It is separate from parsing because the two limits differ only in this.
type validator func(value int) error

// The reasons a variable is rejected. Each reads as the middle of the warning
// it lands in: `HERDR_AUTO_TITLE_POLL_MS="0" must be positive, using 500ms`.
var (
	errNotBoolean  = errors.New("is not a boolean")
	errNotNumber   = errors.New("is not a number")
	errNotPositive = errors.New("must be positive")
)

// positive rejects zero, for the settings where it would stop the plugin doing
// anything: a poll interval of zero spins, and a title of no length is no title.
func positive(value int) error {
	if value <= 0 {
		return errNotPositive
	}
	return nil
}

// text accepts whatever it is given: any string is a usable path.
func text(raw string) (string, error) { return raw, nil }

func boolean(raw string) (bool, error) {
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errNotBoolean
	}
	return value, nil
}

// number reads a count and holds it to valid.
func number(valid validator) converter[int] {
	return func(raw string) (int, error) {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, errNotNumber
		}
		return value, valid(value)
	}
}

// milliseconds reads a duration written as a count of them, which is easier to
// pass through a shell than "500ms".
func milliseconds(raw string) (time.Duration, error) {
	value, err := number(positive)(raw)
	if err != nil {
		return 0, err
	}
	return time.Duration(value) * time.Millisecond, nil
}

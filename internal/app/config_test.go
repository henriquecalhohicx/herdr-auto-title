package app

import (
	"testing"
	"time"

	"herdr-auto-title/internal/resolver"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv(EnvDebug, "")
	t.Setenv(EnvPoll, "")
	t.Setenv(EnvMaxLength, "")

	cfg, warnings := LoadConfig()
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if cfg.Debug {
		t.Error("debug is on by default")
	}
	if cfg.Poll != DefaultPoll {
		t.Errorf("poll = %s, want %s", cfg.Poll, DefaultPoll)
	}
	if cfg.MaxLength != resolver.DefaultMaxLength {
		t.Errorf("max length = %d, want %d", cfg.MaxLength, resolver.DefaultMaxLength)
	}
}

func TestLoadConfigFromEnvironment(t *testing.T) {
	t.Setenv(EnvDebug, "true")
	t.Setenv(EnvPoll, "250")
	t.Setenv(EnvMaxLength, "32")

	cfg, warnings := LoadConfig()
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if !cfg.Debug {
		t.Error("debug is off despite being enabled")
	}
	if cfg.Poll != 250*time.Millisecond {
		t.Errorf("poll = %s, want 250ms", cfg.Poll)
	}
	if cfg.MaxLength != 32 {
		t.Errorf("max length = %d, want 32", cfg.MaxLength)
	}
	// Raising the window must raise the cap with it.
}

func TestLoadConfigKeepsDefaultsOnBadValues(t *testing.T) {
	t.Setenv(EnvDebug, "yes please")
	t.Setenv(EnvPoll, "-5")
	t.Setenv(EnvMaxLength, "plenty")

	cfg, warnings := LoadConfig()
	if len(warnings) != 3 {
		t.Errorf("warnings = %v, want one per bad value", warnings)
	}
	if cfg.Debug {
		t.Error("debug was enabled by an unparseable value")
	}
	if cfg.Poll != DefaultPoll {
		t.Errorf("poll = %s, want the default %s", cfg.Poll, DefaultPoll)
	}
	if cfg.MaxLength != resolver.DefaultMaxLength {
		t.Errorf("max length = %d, want the default %d", cfg.MaxLength, resolver.DefaultMaxLength)
	}
}

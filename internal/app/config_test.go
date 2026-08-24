package app

import (
	"testing"
	"time"

	"herdr-auto-title/internal/debounce"
	"herdr-auto-title/internal/resolver"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv(EnvDebug, "")
	t.Setenv(EnvDebounce, "")
	t.Setenv(EnvMaxLength, "")

	cfg, warnings := LoadConfig()
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if cfg.Debug {
		t.Error("debug is on by default")
	}
	if cfg.Debounce != debounce.DefaultDelay {
		t.Errorf("debounce = %s, want %s", cfg.Debounce, debounce.DefaultDelay)
	}
	if cfg.MaxLength != resolver.DefaultMaxLength {
		t.Errorf("max length = %d, want %d", cfg.MaxLength, resolver.DefaultMaxLength)
	}
	if cfg.MaxWait != debounce.DefaultMaxWait {
		t.Errorf("max wait = %s, want %s", cfg.MaxWait, debounce.DefaultMaxWait)
	}
}

func TestLoadConfigFromEnvironment(t *testing.T) {
	t.Setenv(EnvDebug, "true")
	t.Setenv(EnvDebounce, "500")
	t.Setenv(EnvMaxLength, "32")

	cfg, warnings := LoadConfig()
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if !cfg.Debug {
		t.Error("debug is off despite being enabled")
	}
	if cfg.Debounce != 500*time.Millisecond {
		t.Errorf("debounce = %s, want 500ms", cfg.Debounce)
	}
	if cfg.MaxLength != 32 {
		t.Errorf("max length = %d, want 32", cfg.MaxLength)
	}
}

func TestLoadConfigKeepsDefaultsOnBadValues(t *testing.T) {
	t.Setenv(EnvDebug, "yes please")
	t.Setenv(EnvDebounce, "-5")
	t.Setenv(EnvMaxLength, "plenty")

	cfg, warnings := LoadConfig()
	if len(warnings) != 3 {
		t.Errorf("warnings = %v, want one per bad value", warnings)
	}
	if cfg.Debug {
		t.Error("debug was enabled by an unparseable value")
	}
	if cfg.Debounce != debounce.DefaultDelay {
		t.Errorf("debounce = %s, want the default %s", cfg.Debounce, debounce.DefaultDelay)
	}
	if cfg.MaxLength != resolver.DefaultMaxLength {
		t.Errorf("max length = %d, want the default %d", cfg.MaxLength, resolver.DefaultMaxLength)
	}
}

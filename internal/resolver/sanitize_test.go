package resolver

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{"plain value is kept", "dashboard", 64, "dashboard"},
		{"ansi colour codes are stripped", "\x1b[31mdashboard\x1b[0m", 64, "dashboard"},
		{"osc title sequence is stripped", "\x1b]0;dashboard\x07api", 64, "api"},
		{"newlines become spaces", "fix oauth\nredirect", 64, "fix oauth redirect"},
		{"carriage returns and tabs collapse", "a\r\n\tb", 64, "a b"},
		{"repeated whitespace collapses", "dashboard    tests", 64, "dashboard tests"},
		{"repeated separators collapse", "dashboard ·  · tests", 64, "dashboard · tests"},
		{"separator spacing is normalized", "dashboard·tests", 64, "dashboard · tests"},
		{"leading and trailing separators are trimmed", "· dashboard ·", 64, "dashboard"},
		{"surrounding whitespace is trimmed", "   dashboard   ", 64, "dashboard"},
		{"control characters are removed", "dash\x00board\x07", 64, "dashboard"},
		{"empty input yields empty output", "", 64, ""},
		{"whitespace only yields empty output", "   \n\t ", 64, ""},
		{"escape only yields empty output", "\x1b[0m", 64, ""},
		{"value at the limit is kept whole", "abcdefghij", 10, "abcdefghij"},
		{"value over the limit is truncated", "abcdefghijk", 10, "abcdefghij"},
		{"truncation leaves no dangling separator", "abcdefg · hij", 9, "abcdefg"},
		// Non-ASCII on purpose: a title is truncated by runes, and cutting
		// these two-byte characters by byte count would corrupt the result.
		{"truncation counts runes not bytes", "проектная-работа", 8, "проектна"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Sanitize(tc.in, tc.maxLen); got != tc.want {
				t.Errorf("Sanitize(%q, %d) = %q, want %q", tc.in, tc.maxLen, got, tc.want)
			}
		})
	}
}

func TestSanitizeIsIdempotent(t *testing.T) {
	in := "\x1b[31mdashboard\x1b[0m ·  · tests\n"
	once := Sanitize(in, 64)
	if twice := Sanitize(once, 64); twice != once {
		t.Errorf("Sanitize is not idempotent: %q then %q", once, twice)
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name  string
		parts Parts
		want  string
	}{
		{"context and activity are joined", Parts{Context: "dashboard", Activity: "Tests"}, "dashboard · Tests"},
		{"context alone carries no separator", Parts{Context: "dashboard"}, "dashboard"},
		{"activity alone carries no separator", Parts{Activity: "Tests"}, "Tests"},
		{"nothing yields nothing", Parts{}, ""},
		{"untrusted parts are sanitized", Parts{Context: "\x1b[31mdash\nboard", Activity: "Tests "}, "dash board · Tests"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Format(tc.parts, 64); got != tc.want {
				t.Errorf("Format(%+v) = %q, want %q", tc.parts, got, tc.want)
			}
		})
	}
}

func TestFormatTruncatesAssembledTitle(t *testing.T) {
	got := Format(Parts{Context: "dashboard", Activity: "OAuth scopes"}, 12)
	if got != "dashboard" {
		t.Errorf("Format truncated to %q, want %q", got, "dashboard")
	}
}

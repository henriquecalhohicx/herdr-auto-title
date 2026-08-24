package resolver

import "testing"

func TestIsGeneric(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"shell name", "zsh", true},
		{"shell name in another case", "ZSH", true},
		{"multi-word program name", "Claude Code", true},
		{"multi-word program name in another case", "claude code", true},
		{"runtime name", "node", true},
		{"surrounded by whitespace", "  bash  ", true},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"home directory", "~", true},
		{"abbreviated path", "~/W/herdr-auto-title", true},
		{"absolute path", "/Users/dev/work/dashboard", true},
		{"work described in words", "Fix OAuth redirect", false},
		{"file name", "auth.ts", false},
		{"host name", "prod-01", false},
		// Observed live: editor titles carry the path and change on every
		// buffer switch, which renamed a tab five times in six seconds.
		{"editor title carrying a home path", "main.go (~/work/dashboard) - Nvim", true},
		{"editor title carrying a uri", "- (oil:///home/dev/work/dashboard) - Nvim", true},
		{"absolute path inside a sentence", "editing /etc/hosts", true},
		{"relative path inside a sentence", "Fix bug in src/auth.ts", false},
		{"word that merely contains a shell name", "bashful", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsGeneric(tc.value); got != tc.want {
				t.Errorf("IsGeneric(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

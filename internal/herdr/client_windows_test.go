//go:build windows

package herdr

import "testing"

func TestPipeNamePrependsThePrefix(t *testing.T) {
	got := pipeName(`C:\Users\me\AppData\Roaming\herdr\herdr.sock`)
	want := `\\.\pipe\C:\Users\me\AppData\Roaming\herdr\herdr.sock`

	if got != want {
		t.Errorf("pipeName = %q, want %q", got, want)
	}
}

func TestPipeNameLeavesAnExistingPipePathUntouched(t *testing.T) {
	for _, path := range []string{`\\.\pipe\herdr`, `\\?\pipe\herdr`} {
		if got := pipeName(path); got != path {
			t.Errorf("pipeName(%q) = %q, want unchanged", path, got)
		}
	}
}

package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~", home},
		{"~/Documents", filepath.Join(home, "Documents")},
		{"~/a/b/c", filepath.Join(home, "a", "b", "c")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
		{"~user/foo", "~user/foo"}, // not our user, leave as-is
	}

	for _, tt := range tests {
		got := expandTilde(tt.input)
		if got != tt.want {
			t.Errorf("expandTilde(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

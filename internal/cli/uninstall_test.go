package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestPromptYesNo(t *testing.T) {
	for _, tc := range []struct {
		reply string
		want  bool
	}{
		{"y\n", true},
		{"YES\n", true},
		{"  yes  \n", true},
		{"\n", false},
		{"n\n", false},
		{"maybe\n", false},
		{"", false},
	} {
		var out bytes.Buffer
		got, err := promptYesNo(strings.NewReader(tc.reply), &out)
		if err != nil {
			t.Fatalf("promptYesNo(%q): %v", tc.reply, err)
		}
		if got != tc.want {
			t.Errorf("promptYesNo(%q) = %v, want %v", tc.reply, got, tc.want)
		}
		if !strings.Contains(out.String(), "[y/N]") {
			t.Errorf("prompt %q does not show the [y/N] default", out.String())
		}
	}
}

func TestUninstallCommandHasYesFlag(t *testing.T) {
	cmd := NewUninstallCommand()
	if cmd.Use != "uninstall" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	flag := cmd.Flags().Lookup("yes")
	if flag == nil || flag.Shorthand != "y" {
		t.Fatalf("missing --yes/-y flag: %+v", flag)
	}
}

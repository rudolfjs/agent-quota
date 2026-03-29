package cli_test

import (
	"testing"

	"github.com/schnetlerr/agent-quota/internal/cli"
)

func TestResolveOutputMode(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		pretty   bool
		jsonFlag bool
		isTTY    bool
		want     cli.OutputMode
	}{
		{
			name:     "TTY no provider no flags -> pretty",
			provider: "",
			pretty:   false,
			jsonFlag: false,
			isTTY:    true,
			want:     cli.OutputPretty,
		},
		{
			name:     "provider set no flags -> json",
			provider: "claude",
			pretty:   false,
			jsonFlag: false,
			isTTY:    true,
			want:     cli.OutputJSON,
		},
		{
			name:     "json flag true -> json regardless of TTY",
			provider: "",
			pretty:   false,
			jsonFlag: true,
			isTTY:    true,
			want:     cli.OutputJSON,
		},
		{
			name:     "json flag true non-TTY -> json",
			provider: "",
			pretty:   false,
			jsonFlag: true,
			isTTY:    false,
			want:     cli.OutputJSON,
		},
		{
			name:     "pretty flag true -> pretty regardless of TTY",
			provider: "",
			pretty:   true,
			jsonFlag: false,
			isTTY:    false,
			want:     cli.OutputPretty,
		},
		{
			name:     "pretty flag true with provider -> pretty wins",
			provider: "claude",
			pretty:   true,
			jsonFlag: false,
			isTTY:    false,
			want:     cli.OutputPretty,
		},
		{
			name:     "non-TTY no flags no provider -> json fallback",
			provider: "",
			pretty:   false,
			jsonFlag: false,
			isTTY:    false,
			want:     cli.OutputJSON,
		},
		{
			name:     "provider set non-TTY no flags -> json",
			provider: "openai",
			pretty:   false,
			jsonFlag: false,
			isTTY:    false,
			want:     cli.OutputJSON,
		},
		{
			name:     "both pretty and json -> json takes precedence",
			provider: "",
			pretty:   true,
			jsonFlag: true,
			isTTY:    true,
			want:     cli.OutputJSON,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cli.ResolveOutputMode(tc.provider, tc.pretty, tc.jsonFlag, tc.isTTY)
			if got != tc.want {
				t.Errorf("ResolveOutputMode(%q, %v, %v, %v) = %v, want %v",
					tc.provider, tc.pretty, tc.jsonFlag, tc.isTTY, got, tc.want)
			}
		})
	}
}

func TestOutputMode_String(t *testing.T) {
	tests := []struct {
		mode cli.OutputMode
		want string
	}{
		{cli.OutputAuto, "auto"},
		{cli.OutputPretty, "pretty"},
		{cli.OutputJSON, "json"},
		{cli.OutputText, "text"},
	}

	for _, tc := range tests {
		if got := tc.mode.String(); got != tc.want {
			t.Errorf("OutputMode(%d).String() = %q, want %q", tc.mode, got, tc.want)
		}
	}
}

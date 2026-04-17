package selfupdate

import "testing"

func TestDecideUpdate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		in           PolicyInput
		wantUpdate   bool
		reasonSubstr string
	}{
		{
			name:         "newer stable release",
			in:           PolicyInput{Current: "v0.2.2", Latest: "v0.3.0"},
			wantUpdate:   true,
			reasonSubstr: "newer release available",
		},
		{
			name:         "already latest",
			in:           PolicyInput{Current: "v0.3.0", Latest: "v0.3.0"},
			wantUpdate:   false,
			reasonSubstr: "already on the latest",
		},
		{
			name:         "already latest with force reinstalls",
			in:           PolicyInput{Current: "v0.3.0", Latest: "v0.3.0", Force: true},
			wantUpdate:   true,
			reasonSubstr: "--force",
		},
		{
			name:         "newer local version refuses downgrade",
			in:           PolicyInput{Current: "v1.2.3", Latest: "v1.2.0"},
			wantUpdate:   false,
			reasonSubstr: "refusing to downgrade",
		},
		{
			name:         "prerelease skipped without opt-in",
			in:           PolicyInput{Current: "v0.3.0", Latest: "v0.4.0-rc.1"},
			wantUpdate:   false,
			reasonSubstr: "pass --pre",
		},
		{
			name:         "prerelease installed with --pre",
			in:           PolicyInput{Current: "v0.3.0", Latest: "v0.4.0-rc.1", AllowPrerelease: true},
			wantUpdate:   true,
			reasonSubstr: "newer release available",
		},
		{
			name:         "dev build always updates to stable",
			in:           PolicyInput{Current: "dev", Latest: "v0.3.0"},
			wantUpdate:   true,
			reasonSubstr: "development snapshot",
		},
		{
			name:         "empty current treated as dev",
			in:           PolicyInput{Current: "", Latest: "v0.3.0"},
			wantUpdate:   true,
			reasonSubstr: "development snapshot",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decideUpdate(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ShouldUpdate != tc.wantUpdate {
				t.Fatalf("ShouldUpdate = %v, want %v (reason: %q)", got.ShouldUpdate, tc.wantUpdate, got.Reason)
			}
			if tc.reasonSubstr != "" && !contains(got.Reason, tc.reasonSubstr) {
				t.Fatalf("Reason = %q, want substring %q", got.Reason, tc.reasonSubstr)
			}
		})
	}
}

func TestDecideUpdate_invalidVersionErrors(t *testing.T) {
	t.Parallel()
	_, err := decideUpdate(PolicyInput{Current: "v0.2.2", Latest: "garbage"})
	if err == nil {
		t.Fatal("expected error from unparseable Latest version")
	}
}

func contains(s, substr string) bool {
	if substr == "" {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

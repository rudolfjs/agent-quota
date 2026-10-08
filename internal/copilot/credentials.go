package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/tidwall/jsonc"

	"github.com/rudolfjs/agent-quota/internal/credential"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
)

// Copilot CLI uses keytar's generic password entry. The host (including its
// scheme) and login come from config.json, not from the macOS username.
func copilotKeychain(host, login string) credential.Store {
	return credential.NewKeychain("copilot-cli", host+":"+login, "GitHub Copilot CLI")
}

// Current CLI preferences live in settings.json beside the authentication state.
// Only use the legacy config.json preference when settings.json is absent.
func readPlaintextPreference(ctx context.Context, configPath string, legacy bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, apierrors.NewConfigError("Copilot settings read cancelled", err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return legacy, nil
	}
	if err != nil {
		return false, apierrors.NewConfigError("failed to read Copilot settings file", err)
	}
	var settings struct {
		StoreTokenPlaintext bool `json:"storeTokenPlaintext"`
	}
	// Copilot permits comments and trailing commas in user-editable settings.
	if err := json.Unmarshal(jsonc.ToJSON(data), &settings); err != nil {
		return false, apierrors.NewConfigError("failed to parse Copilot settings file", errors.New("invalid Copilot settings JSON"))
	}
	return settings.StoreTokenPlaintext, nil
}

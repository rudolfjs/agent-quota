package copilot

import "github.com/rudolfjs/agent-quota/internal/credential"

// Copilot CLI uses keytar's generic password entry. The host (including its
// scheme) and login come from config.json, not from the macOS username.
func copilotKeychain(host, login string) credential.Store {
	return credential.NewKeychain("copilot-cli", host+":"+login, "GitHub Copilot CLI")
}

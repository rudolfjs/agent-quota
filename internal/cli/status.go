package cli

import (
	"context"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/schnetlerr/agent-quota/internal/config"
	"github.com/schnetlerr/agent-quota/internal/output"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

// NewStatusCommand returns the "status" subcommand which prints a JSON
// summary of all available providers — suitable for scripts and agents.
func NewStatusCommand(reg *provider.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print JSON quota status for all configured providers",
		Long:  "Fetches quota data from all configured providers and prints JSON to stdout.\nIdeal for scripting: agent-quota status | jq '.[] | select(.provider==\"claude\")'",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			providers, err := config.SelectProviders(reg, "", cfg)
			if err != nil {
				return err
			}
			if len(providers) == 0 {
				return nil // nothing to report; exit 0
			}
			results := fetchAll(cmd.Context(), providers)
			return output.WriteJSON(cmd.OutOrStdout(), results)
		},
	}
}

// fetchAll fetches quota from all providers concurrently and returns results
// in the same order as the input slice. Errors are captured as error-status results.
func fetchAll(ctx context.Context, providers []provider.Provider) []provider.QuotaResult {
	type indexed struct {
		i   int
		res provider.QuotaResult
	}

	ch := make(chan indexed, len(providers))
	for i, p := range providers {
		go func(i int, p provider.Provider) {
			if !p.Available() {
				ch <- indexed{i, provider.QuotaResult{
					Provider:  p.Name(),
					Status:    "unavailable",
					FetchedAt: time.Now(),
				}}
				return
			}

			res, err := p.FetchQuota(ctx)
			if err != nil {
				slog.Debug("provider fetch failed", "provider", p.Name(), "error", err)
				res = provider.QuotaResult{
					Provider:  p.Name(),
					Status:    "error",
					FetchedAt: time.Now(),
				}
			}
			ch <- indexed{i, res}
		}(i, p)
	}

	results := make([]provider.QuotaResult, len(providers))
	for range providers {
		r := <-ch
		results[r.i] = r.res
	}
	return results
}

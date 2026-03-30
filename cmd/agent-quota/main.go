// Command agent-quota fetches AI provider usage/quota data.
// Pretty TUI for humans, headless JSON for scripts/agents.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/schnetlerr/agent-quota/internal/claude"
	"github.com/schnetlerr/agent-quota/internal/cli"
	"github.com/schnetlerr/agent-quota/internal/config"
	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/gemini"
	"github.com/schnetlerr/agent-quota/internal/openai"
	"github.com/schnetlerr/agent-quota/internal/output"
	"github.com/schnetlerr/agent-quota/internal/provider"
	"github.com/schnetlerr/agent-quota/internal/tui"
	"github.com/schnetlerr/agent-quota/internal/version"
)

func main() {
	ctx := context.Background()

	// Build registry and register providers.
	registry := provider.NewRegistry()
	registry.Register(claude.New())
	registry.Register(openai.New())
	registry.Register(gemini.New())

	// Root command flags.
	var (
		providerFlag   string
		modelFlag      string
		jsonFlag       bool
		prettyFlag     bool
		debug          bool
		refreshMinutes int
	)

	rootCmd := &cobra.Command{
		Use:   "agent-quota",
		Short: "Fetch AI provider usage and quota data",
		Long:  "CLI tool that fetches AI provider usage/quota data.\nPretty TUI for humans, headless JSON for scripts/agents.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if debug {
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
			}

			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			providers, err := config.SelectProviders(registry, providerFlag, cfg)
			if err != nil {
				return err
			}

			isTTY := isTerminal(os.Stdout)
			mode := cli.ResolveOutputMode(providerFlag, prettyFlag, jsonFlag, isTTY)

			slog.Debug("resolved output mode",
				slog.String("mode", mode.String()),
				slog.String("provider", providerFlag),
				slog.Bool("json", jsonFlag),
				slog.Bool("pretty", prettyFlag),
				slog.Bool("tty", isTTY),
			)

			if mode == cli.OutputPretty {
				settings, err := config.LoadSettingsDefault()
				if err != nil {
					return err
				}
				providers = config.ApplyProviderOrder(providers, settings.ProviderOrder)

				refreshInterval, err := resolveTUIRefreshInterval(cfg, settings, refreshMinutes, cmd.Flags().Changed("refresh-minutes"))
				if err != nil {
					return err
				}
				return runTUI(cmd.Context(), providers, refreshInterval, settings)
			}

			results, err := fetchResults(cmd.Context(), providers)
			if err != nil {
				return err
			}
			results = filterByModel(results, modelFlag)

			switch mode {
			case cli.OutputJSON:
				return output.WriteJSON(os.Stdout, results)
			case cli.OutputText:
				return output.WriteText(os.Stdout, results, time.Now())
			default:
				return output.WriteJSON(os.Stdout, results)
			}
		},
	}

	rootCmd.Flags().StringVarP(&providerFlag, "provider", "p", "", "specific provider to query (e.g. claude, openai, gemini)")
	rootCmd.Flags().StringVarP(&modelFlag, "model", "m", "", "filter output to a specific model window (e.g. gemini-3-flash-preview)")
	rootCmd.Flags().BoolVar(&jsonFlag, "json", false, "force JSON output")
	rootCmd.Flags().BoolVar(&prettyFlag, "pretty", false, "force TUI output")
	rootCmd.Flags().IntVar(&refreshMinutes, "refresh-minutes", 0, "override TUI auto-refresh interval in minutes")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "enable debug logging to stderr")

	rootCmd.AddCommand(cli.NewStatusCommand(registry))

	if err := fang.Execute(ctx, rootCmd, fang.WithVersion(version.String())); err != nil {
		os.Exit(1)
	}
}

// filterByModel filters each QuotaResult's Windows to those whose Name matches
// model exactly. A blank model returns results unchanged. Non-matching results
// keep their other fields intact but have an empty (non-nil) Windows slice.
func filterByModel(results []provider.QuotaResult, model string) []provider.QuotaResult {
	if model == "" {
		return results
	}
	out := make([]provider.QuotaResult, len(results))
	for i, r := range results {
		filtered := make([]provider.Window, 0)
		for _, w := range r.Windows {
			if w.Name == model {
				filtered = append(filtered, w)
			}
		}
		r.Windows = filtered
		out[i] = r
	}
	return out
}

// fetchResults queries the selected provider(s) and collects results.
func fetchResults(ctx context.Context, providers []provider.Provider) ([]provider.QuotaResult, error) {
	if len(providers) == 0 {
		return nil, apierrors.NewConfigError("no providers are configured on this machine", fmt.Errorf("no providers selected"))
	}

	results := make([]provider.QuotaResult, 0, len(providers))
	for _, p := range providers {
		if !p.Available() {
			results = append(results, provider.QuotaResult{
				Provider:  p.Name(),
				Status:    "unavailable",
				FetchedAt: time.Now(),
			})
			continue
		}

		result, err := p.FetchQuota(ctx)
		if err != nil {
			slog.Debug("provider fetch failed", slog.String("provider", p.Name()), "error", err)
			results = append(results, provider.QuotaResult{
				Provider:  p.Name(),
				Status:    "error",
				FetchedAt: time.Now(),
			})
			continue
		}
		results = append(results, result)
	}
	return results, nil
}

func resolveTUIRefreshInterval(cfg config.Config, settings config.Settings, overrideMinutes int, overrideSet bool) (time.Duration, error) {
	if overrideSet {
		if overrideMinutes <= 0 {
			return 0, apierrors.NewConfigError("TUI refresh interval must be greater than 0 minutes", fmt.Errorf("invalid refresh-minutes value: %d", overrideMinutes))
		}
		return time.Duration(overrideMinutes) * time.Minute, nil
	}
	if settings.TUI.RefreshMinutes > 0 {
		return time.Duration(settings.TUI.RefreshMinutes) * time.Minute, nil
	}
	return cfg.TUIRefreshInterval(), nil
}

// runTUI launches the interactive Bubbletea v2 TUI.
func runTUI(_ context.Context, providers []provider.Provider, refreshInterval time.Duration, settings config.Settings) error {
	if len(providers) == 0 {
		return apierrors.NewConfigError("no providers are configured on this machine", fmt.Errorf("no providers selected"))
	}
	settingsPath, err := config.DefaultSettingsPath()
	if err != nil {
		settingsPath = ""
	}
	m := tui.New(providers,
		tui.WithRefreshInterval(refreshInterval),
		tui.WithSettings(settings, func(settings config.Settings) error {
			if settingsPath == "" {
				return apierrors.NewConfigError("failed to persist agent-quota settings", fmt.Errorf("settings path is unavailable"))
			}
			return config.SaveSettings(settingsPath, settings)
		}),
	)
	p := tea.NewProgram(m)
	_, err = p.Run()
	return err
}

// isTerminal reports whether f is connected to a terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

package cli

import (
	"github.com/spf13/cobra"

	"github.com/rudolfjs/agent-quota/internal/selfupdate"
	"github.com/rudolfjs/agent-quota/internal/version"
)

// NewSelfUpdateCommand returns the "self-update" subcommand, which
// downloads the latest release from GitHub and replaces the running
// binary in place.
func NewSelfUpdateCommand() *cobra.Command {
	var (
		force bool
		pre   bool
		check bool
	)
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update agent-quota to the latest GitHub release",
		Long: `Downloads the latest release archive from GitHub, verifies its sha256
checksum, and atomically swaps the running binary in place.

Flags:
  --check   report the update decision without installing
  --force   reinstall even if the current version matches the latest release
  --pre     consider prerelease tags (default: stable only)

Supported release targets: linux/amd64, darwin/amd64, and darwin/arm64.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := selfupdate.Run(cmd.Context(), selfupdate.Options{
				CurrentVersion:  extractVersionCore(version.String()),
				AllowPrerelease: pre,
				Force:           force,
				CheckOnly:       check,
				Out:             cmd.OutOrStdout(),
			})
			return err
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "reinstall even if already on the latest release")
	cmd.Flags().BoolVar(&pre, "pre", false, "include prerelease tags when resolving the latest version")
	cmd.Flags().BoolVar(&check, "check", false, "report the update decision without downloading or installing")
	return cmd
}

// extractVersionCore strips the optional " (commit)" suffix that
// version.String() appends for versioned builds, leaving just the
// bare semver that the selfupdate policy can parse.
func extractVersionCore(full string) string {
	for i := range len(full) {
		if full[i] == ' ' {
			return full[:i]
		}
	}
	return full
}

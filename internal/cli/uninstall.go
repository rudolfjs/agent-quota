package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rudolfjs/agent-quota/internal/config"
	"github.com/rudolfjs/agent-quota/internal/selfupdate"
)

// NewUninstallCommand returns the "uninstall" subcommand, which removes the
// binary, the aq shortcut and agent-quota's config directory.
func NewUninstallCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove agent-quota, the aq shortcut, and its config",
		Long: `Removes the agent-quota binary, the aq shortcut beside it, and
agent-quota's own config and cache directory. Lists what will be removed and
asks for confirmation first.

Provider credentials (Claude, Codex, and Copilot files and Keychain entries)
belong to those CLIs and are never touched.

Flags:
  --yes   skip the confirmation prompt

Supports Linux x86_64 and macOS Intel/Apple Silicon.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dirs, err := config.Dirs()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			_, err = selfupdate.Uninstall(cmd.Context(), selfupdate.UninstallOptions{
				ConfigDirs: dirs,
				Yes:        yes,
				Confirm:    func() (bool, error) { return confirmFromTTY(out) },
				Out:        out,
			})
			return err
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

// confirmFromTTY reads the answer from the terminal, as install.sh does, so
// the prompt works even when stdin is redirected.
func confirmFromTTY(out io.Writer) (bool, error) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false, err
	}
	defer func() { _ = tty.Close() }()
	return promptYesNo(tty, out)
}

// promptYesNo defaults to no: only "y" or "yes" proceeds.
func promptYesNo(in io.Reader, out io.Writer) (bool, error) {
	_, _ = fmt.Fprint(out, "Proceed with uninstall? [y/N] ")
	reply, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(reply)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

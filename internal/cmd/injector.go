package cmd

import (
	"github.com/spf13/cobra"

	"github.com/denniswebb/ghostwire/internal/logging"
)

// InjectorCmd represents the ghostwire injector subcommand.
var InjectorCmd = &cobra.Command{
	Use:   "injector",
	Short: "Run mutating admission webhook server",
	RunE: func(cmd *cobra.Command, args []string) error {
		if logger := logging.GetLogger(); logger != nil {
			logger.Info("injector command not yet implemented")
		}
		return nil
	},
}

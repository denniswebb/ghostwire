package cmd

import (
	"github.com/spf13/cobra"

	"github.com/denniswebb/ghostwire/internal/logging"
)

// WatcherCmd represents the ghostwire watcher subcommand.
var WatcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Poll pod labels and toggle iptables jump",
	RunE: func(cmd *cobra.Command, args []string) error {
		if logger := logging.GetLogger(); logger != nil {
			logger.Info("watcher command not yet implemented")
		}
		return nil
	},
}

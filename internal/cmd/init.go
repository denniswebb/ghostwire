package cmd

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/denniswebb/ghostwire/internal/discovery"
	"github.com/denniswebb/ghostwire/internal/logging"
)

// InitCmd represents the ghostwire init subcommand.
var InitCmd = &cobra.Command{
	Use:   "init",
	Short: "Discover services and build DNAT rules",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		logger := logging.GetLogger()
		if logger == nil {
			logger = slog.Default()
		}

		namespace := viper.GetString("namespace")
		if namespace == "" {
			namespace = os.Getenv("POD_NAMESPACE")
		}
		if namespace == "" {
			namespace = "default"
		}

		previewPattern := viper.GetString("svc-preview-pattern")
		if previewPattern == "" {
			previewPattern = "{{name}}-preview"
		}

		activeSuffix := viper.GetString("active-suffix")
		if activeSuffix == "" {
			activeSuffix = "-active"
		}

		previewSuffix := viper.GetString("preview-suffix")
		if previewSuffix == "" {
			previewSuffix = "-preview"
		}

		clientset, err := discovery.NewInClusterClient()
		if err != nil {
			logger.Error("failed to create kubernetes client", slog.String("error", err.Error()))
			return err
		}

		discoveryCfg := discovery.Config{
			Clientset:      clientset,
			Namespace:      namespace,
			PreviewPattern: previewPattern,
			ActiveSuffix:   activeSuffix,
			PreviewSuffix:  previewSuffix,
		}

		mappings, err := discovery.Discover(ctx, discoveryCfg, logger)
		if err != nil {
			logger.Error("service discovery failed", slog.String("error", err.Error()))
			return err
		}

		logger.Info(
			"service discovery complete",
			slog.Int("mappings", len(mappings)),
			slog.String("namespace", namespace),
		)

		// TODO(ghostwire/iptables): Build DNAT rules from discovered mappings.
		return nil
	},
}

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/denniswebb/ghostwire/internal/k8s"
	"github.com/denniswebb/ghostwire/internal/logging"
)

// WatcherCmd represents the ghostwire watcher subcommand.
var WatcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Poll pod labels and toggle iptables jump",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := logging.GetLogger()
		if logger == nil {
			logger = slog.Default()
		}

		podName := os.Getenv("POD_NAME")
		if podName == "" {
			return fmt.Errorf("environment variable POD_NAME is required")
		}
		podNamespace := os.Getenv("POD_NAMESPACE")
		if podNamespace == "" {
			return fmt.Errorf("environment variable POD_NAMESPACE is required")
		}

		labelKey := viper.GetString("role-label-key")
		activeValue := viper.GetString("role-active")
		previewValue := viper.GetString("role-preview")

		pollIntervalRaw := viper.GetString("poll-interval")
		pollInterval, err := time.ParseDuration(pollIntervalRaw)
		if err != nil {
			return fmt.Errorf("parse poll interval %q: %w", pollIntervalRaw, err)
		}

		clientset, err := k8s.NewInClusterClient()
		if err != nil {
			return fmt.Errorf("create kubernetes client: %w", err)
		}

		labelReader := k8s.NewPodLabelReader(clientset, podNamespace, podName)
		pollLogger := logger.With(
			slog.String("component", "watcher"),
			slog.String("pod_name", podName),
			slog.String("namespace", podNamespace),
			slog.String("label_key", labelKey),
		)

		poller, err := k8s.NewPoller(k8s.PollerConfig{
			LabelReader:  labelReader,
			LabelKey:     labelKey,
			ActiveValue:  activeValue,
			PreviewValue: previewValue,
			PollInterval: pollInterval,
			Logger:       pollLogger,
		})
		if err != nil {
			return fmt.Errorf("create poller: %w", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sigCh)

		done := make(chan struct{})
		go func() {
			defer close(done)
			poller.Run(ctx)
		}()

		pollLogger.Info("watcher started",
			slog.String("poll_interval", pollInterval.String()),
			slog.String("active_value", activeValue),
			slog.String("preview_value", previewValue),
		)

		select {
		case sig := <-sigCh:
			pollLogger.Info("shutdown signal received", slog.String("signal", sig.String()))
		case <-ctx.Done():
		}

		cancel()
		<-done

		pollLogger.Info("watcher shutdown complete")

		return nil
	},
}

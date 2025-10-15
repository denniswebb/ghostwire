package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/denniswebb/ghostwire/internal/iptables"
	"github.com/denniswebb/ghostwire/internal/k8s"
	"github.com/denniswebb/ghostwire/internal/logging"
	"github.com/denniswebb/ghostwire/internal/metrics"
)

const (
	httpListenAddr           = ":8081"
	metricErrorLabelRead     = "label_read"
	metricErrorLabelIptables = "iptables"
	metricErrorChainVerify   = "chain_verify"
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

		natChain := strings.TrimSpace(viper.GetString("nat-chain"))
		if natChain == "" {
			natChain = "CANARY_DNAT"
		}
		jumpHook := strings.TrimSpace(viper.GetString("jump-hook"))
		if jumpHook == "" {
			jumpHook = "OUTPUT"
		}
		ipv6Enabled := viper.GetBool("ipv6")
		dnatMapPath := viper.GetString("iptables-dnat-map")

		pollLogger := logger.With(
			slog.String("component", "watcher"),
			slog.String("pod_name", podName),
			slog.String("namespace", podNamespace),
			slog.String("label_key", labelKey),
			slog.String("nat_chain", natChain),
			slog.String("jump_hook", jumpHook),
			slog.Bool("ipv6_enabled", ipv6Enabled),
			slog.String("http_addr", httpListenAddr),
		)

		clientset, err := k8s.NewInClusterClient()
		if err != nil {
			return fmt.Errorf("create kubernetes client: %w", err)
		}

		metricsCollector := metrics.NewMetrics()
		metricsCollector.SetJumpActive(false)
		healthChecker := metrics.NewHealthChecker()

		dnatCount, err := metrics.CountDNATMappings(dnatMapPath)
		if err != nil {
			pollLogger.Warn("failed to count dnat mappings",
				slog.String("dnat_map_path", dnatMapPath),
				slog.Any("error", err),
			)
		} else {
			metricsCollector.SetDNATRuleCount(dnatCount)
		}

		executor := iptables.NewExecutor()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		chainExists, err := executor.ChainExists(ctx, "nat", natChain)
		if err != nil {
			metricsCollector.IncrementError(metricErrorChainVerify)
			pollLogger.Error("failed to verify dnat chain", slog.Any("error", err))
		} else if !chainExists {
			metricsCollector.IncrementError(metricErrorChainVerify)
			pollLogger.Warn("dnat chain missing")
		} else {
			healthChecker.SetChainVerified()
			pollLogger.Info("dnat chain verified")
		}

		labelReader := k8s.NewPodLabelReader(clientset, podNamespace, podName)
		wrappedReader := &metricsLabelReader{
			delegate: labelReader,
			metrics:  metricsCollector,
			health:   healthChecker,
		}

		jm := &jumpManager{
			executor:     executor,
			table:        "nat",
			hook:         jumpHook,
			chain:        natChain,
			ipv6:         ipv6Enabled,
			activeValue:  activeValue,
			previewValue: previewValue,
			metrics:      metricsCollector,
			logger:       pollLogger,
		}

		poller, err := k8s.NewPoller(k8s.PollerConfig{
			LabelReader:       wrappedReader,
			LabelKey:          labelKey,
			ActiveValue:       activeValue,
			PreviewValue:      previewValue,
			PollInterval:      pollInterval,
			Logger:            pollLogger,
			TransitionHandler: jm,
		})
		if err != nil {
			return fmt.Errorf("create poller: %w", err)
		}

		srv := &http.Server{
			Addr:              httpListenAddr,
			Handler:           buildWatcherMux(metricsCollector, healthChecker),
			ReadHeaderTimeout: 5 * time.Second,
		}

		serverErrCh := make(chan error, 1)
		go func() {
			defer close(serverErrCh)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErrCh <- err
			}
		}()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sigCh)

		pollDone := make(chan struct{})
		go func() {
			defer close(pollDone)
			poller.Run(ctx)
		}()

		pollLogger.Info("watcher started",
			slog.String("poll_interval", pollInterval.String()),
			slog.String("active_value", activeValue),
			slog.String("preview_value", previewValue),
		)

		var serverErr error
		select {
		case sig := <-sigCh:
			pollLogger.Info("shutdown signal received", slog.String("signal", sig.String()))
		case err, ok := <-serverErrCh:
			if ok && err != nil {
				serverErr = err
				pollLogger.Error("http server encountered error", slog.Any("error", err))
			}
		case <-ctx.Done():
		}

		cancel()
		<-pollDone

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			pollLogger.Error("http server shutdown failed", slog.Any("error", err))
		}
		shutdownCancel()

		if serverErr == nil {
			if err, ok := <-serverErrCh; ok && err != nil {
				pollLogger.Error("http server encountered error", slog.Any("error", err))
			}
		}

		pollLogger.Info("watcher shutdown complete")
		return nil
	},
}

func buildWatcherMux(metricsCollector *metrics.Metrics, healthChecker *metrics.HealthChecker) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metricsCollector.Handler())
	mux.Handle("/healthz", healthChecker.Handler())
	return mux
}

type jumpManager struct {
	executor     iptables.Executor
	table        string
	hook         string
	chain        string
	ipv6         bool
	activeValue  string
	previewValue string
	metrics      *metrics.Metrics
	logger       *slog.Logger
}

func (j *jumpManager) OnTransition(ctx context.Context, previous string, current string) error {
	switch current {
	case j.previewValue:
		j.logger.Info("activating dnat jump", slog.String("previous_role", previous), slog.String("current_role", current))
		if err := iptables.AddJump(ctx, j.executor, j.table, j.hook, j.chain, j.ipv6, j.logger); err != nil {
			j.metrics.IncrementError(metricErrorLabelIptables)
			return fmt.Errorf("add jump: %w", err)
		}
		j.metrics.SetJumpActive(true)
	case j.activeValue:
		j.logger.Info("deactivating dnat jump", slog.String("previous_role", previous), slog.String("current_role", current))
		if err := iptables.RemoveJump(ctx, j.executor, j.table, j.hook, j.chain, j.ipv6, j.logger); err != nil {
			j.metrics.IncrementError(metricErrorLabelIptables)
			return fmt.Errorf("remove jump: %w", err)
		}
		j.metrics.SetJumpActive(false)
	default:
		j.logger.Debug("ignoring transition", slog.String("previous_role", previous), slog.String("current_role", current))
	}
	return nil
}

type metricsLabelReader struct {
	delegate k8s.LabelReader
	metrics  *metrics.Metrics
	health   *metrics.HealthChecker
}

func (m *metricsLabelReader) GetLabel(ctx context.Context, labelKey string) (string, error) {
	value, err := m.delegate.GetLabel(ctx, labelKey)
	if err != nil {
		m.metrics.IncrementError(metricErrorLabelRead)
		return "", err
	}
	if m.health != nil {
		m.health.SetLabelsRead()
	}
	return value, nil
}

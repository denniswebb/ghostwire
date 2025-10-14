package iptables

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/denniswebb/ghostwire/internal/discovery"
)

// AddDNATRules builds DNAT rules for each discovered service mapping.
func AddDNATRules(ctx context.Context, executor Executor, table string, chain string, mappings []discovery.ServiceMapping, ipv6 bool, logger *slog.Logger) error {
	for _, mapping := range mappings {
		if err := ctx.Err(); err != nil {
			return err
		}

		protocol := strings.ToLower(string(mapping.Protocol))
		ruleArgs := []string{"-t", table, "-A", chain, "-d", mapping.ActiveClusterIP, "-p", protocol, "--dport", fmt.Sprintf("%d", mapping.Port), "-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", mapping.PreviewClusterIP, mapping.Port)}
		isIPv6 := strings.Contains(mapping.ActiveClusterIP, ":") || strings.Contains(mapping.PreviewClusterIP, ":")

		if !isIPv6 {
			logger.Info("adding dnat rule", slog.String("service", mapping.ServiceName), slog.Int("port", int(mapping.Port)), slog.String("protocol", protocol), slog.String("active_ip", mapping.ActiveClusterIP), slog.String("preview_ip", mapping.PreviewClusterIP), slog.Bool("ipv6", false))
			if err := executor.Run(ctx, ipv4Binary, ruleArgs...); err != nil {
				return fmt.Errorf("add dnat rule for %s: %w", mapping.ServiceName, err)
			}
			continue
		}

		if !ipv6 {
			logger.Warn("skipping ipv6 dnat rule without ipv6 support", slog.String("service", mapping.ServiceName), slog.String("active_ip", mapping.ActiveClusterIP), slog.String("preview_ip", mapping.PreviewClusterIP))
			continue
		}

		logger.Info("adding dnat rule", slog.String("service", mapping.ServiceName), slog.Int("port", int(mapping.Port)), slog.String("protocol", protocol), slog.String("active_ip", mapping.ActiveClusterIP), slog.String("preview_ip", mapping.PreviewClusterIP), slog.Bool("ipv6", true))
		if err := executor.Run(ctx, ipv6Binary, ruleArgs...); err != nil {
			return fmt.Errorf("add ipv6 dnat rule for %s: %w", mapping.ServiceName, err)
		}
	}

	return nil
}

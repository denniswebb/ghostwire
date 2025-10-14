package iptables

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/denniswebb/ghostwire/internal/discovery"
)

func isIPv6(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() == nil
}

// AddDNATRules builds DNAT rules for each discovered service mapping.
func AddDNATRules(ctx context.Context, executor Executor, table string, chain string, mappings []discovery.ServiceMapping, ipv6 bool, logger *slog.Logger) (int, error) {
	added := 0
	for _, mapping := range mappings {
		if err := ctx.Err(); err != nil {
			return added, err
		}

		if mapping.ActiveClusterIP == "" || mapping.PreviewClusterIP == "" || mapping.Port == 0 {
			logger.Warn("skipping dnat rule due to missing IP/port",
				slog.String("service", mapping.ServiceName),
				slog.String("active_ip", mapping.ActiveClusterIP),
				slog.String("preview_ip", mapping.PreviewClusterIP),
				slog.Int("port", int(mapping.Port)))
			continue
		}

		protocol := strings.ToLower(string(mapping.Protocol))
		ruleArgs := []string{"-w", "5", "-t", table, "-A", chain, "-d", mapping.ActiveClusterIP, "-p", protocol, "--dport", fmt.Sprintf("%d", mapping.Port), "-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", mapping.PreviewClusterIP, mapping.Port)}

		isActiveV6 := isIPv6(mapping.ActiveClusterIP)
		isPreviewV6 := isIPv6(mapping.PreviewClusterIP)

		if isActiveV6 != isPreviewV6 {
			logger.Warn("skipping dnat rule due to mixed IP families", slog.String("service", mapping.ServiceName), slog.String("active_ip", mapping.ActiveClusterIP), slog.String("preview_ip", mapping.PreviewClusterIP))
			continue
		}

		useIPv6 := isActiveV6
		bin := ipv4Binary
		if useIPv6 {
			if !ipv6 {
				logger.Warn("skipping ipv6 dnat rule without ipv6 support", slog.String("service", mapping.ServiceName), slog.String("active_ip", mapping.ActiveClusterIP), slog.String("preview_ip", mapping.PreviewClusterIP))
				continue
			}
			bin = ipv6Binary
		}

		logger.Info("adding dnat rule", slog.String("service", mapping.ServiceName), slog.Int("port", int(mapping.Port)), slog.String("protocol", protocol), slog.String("active_ip", mapping.ActiveClusterIP), slog.String("preview_ip", mapping.PreviewClusterIP), slog.Bool("ipv6", useIPv6))
		if err := executor.Run(ctx, bin, ruleArgs...); err != nil {
			return added, fmt.Errorf("add dnat rule for %s: %w", mapping.ServiceName, err)
		}
		added++
	}

	return added, nil
}

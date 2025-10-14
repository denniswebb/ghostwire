package iptables

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// AddExclusions injects RETURN rules for CIDRs that should bypass DNAT handling.
func AddExclusions(ctx context.Context, executor Executor, table string, chain string, cidrs []string, ipv6 bool, logger *slog.Logger) error {
	for _, raw := range cidrs {
		if err := ctx.Err(); err != nil {
			return err
		}

		cidr := strings.TrimSpace(raw)
		if cidr == "" {
			continue
		}

		isIPv6 := strings.Contains(cidr, ":")
		if !isIPv6 {
			logger.Info("adding exclusion", slog.String("cidr", cidr), slog.String("table", table), slog.String("chain", chain), slog.Bool("ipv6", false))
			if err := executor.Run(ctx, ipv4Binary, "-t", table, "-A", chain, "-d", cidr, "-j", "RETURN"); err != nil {
				return fmt.Errorf("add exclusion for %s: %w", cidr, err)
			}
			continue
		}

		if !ipv6 {
			logger.Warn("skipping ipv6 exclusion without ipv6 support", slog.String("cidr", cidr), slog.String("table", table), slog.String("chain", chain))
			continue
		}

		logger.Info("adding exclusion", slog.String("cidr", cidr), slog.String("table", table), slog.String("chain", chain), slog.Bool("ipv6", true))
		if err := executor.Run(ctx, ipv6Binary, "-t", table, "-A", chain, "-d", cidr, "-j", "RETURN"); err != nil {
			return fmt.Errorf("add ipv6 exclusion for %s: %w", cidr, err)
		}
	}

	return nil
}

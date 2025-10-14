package iptables

import (
	"context"
	"fmt"
	"log/slog"
)

const (
	ipv4Binary = "iptables"
	ipv6Binary = "ip6tables"
)

// EnsureChain verifies the DNAT chain exists and is empty for both IPv4 and IPv6.
func EnsureChain(ctx context.Context, executor Executor, table string, chain string, ipv6 bool, logger *slog.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	exists, err := executor.ChainExists(ctx, table, chain)
	if err != nil {
		return fmt.Errorf("determine chain existence: %w", err)
	}

	if exists {
		logger.Info("flushing existing chain", slog.String("table", table), slog.String("chain", chain), slog.Bool("ipv6", false))
		if err := executor.Run(ctx, ipv4Binary, "-w", "5", "-t", table, "-F", chain); err != nil {
			return fmt.Errorf("flush chain %s: %w", chain, err)
		}
	} else {
		logger.Info("creating chain", slog.String("table", table), slog.String("chain", chain), slog.Bool("ipv6", false))
		if err := executor.Run(ctx, ipv4Binary, "-w", "5", "-t", table, "-N", chain); err != nil {
			return fmt.Errorf("create chain %s: %w", chain, err)
		}
	}

	if !ipv6 {
		return nil
	}

	if err := ensureIPv6Chain(ctx, executor, table, chain, logger); err != nil {
		logger.Warn("ip6tables chain preparation failed", slog.String("table", table), slog.String("chain", chain), slog.Any("error", err))
	}

	return nil
}

func ensureIPv6Chain(ctx context.Context, executor Executor, table string, chain string, logger *slog.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	exists, err := executor.ChainExists6(ctx, table, chain)
	if err != nil {
		return fmt.Errorf("determine ipv6 chain existence: %w", err)
	}

	if exists {
		logger.Info("flushing existing chain", slog.String("table", table), slog.String("chain", chain), slog.Bool("ipv6", true))
		return executor.Run(ctx, ipv6Binary, "-w", "5", "-t", table, "-F", chain)
	}

	logger.Info("creating chain", slog.String("table", table), slog.String("chain", chain), slog.Bool("ipv6", true))
	return executor.Run(ctx, ipv6Binary, "-w", "5", "-t", table, "-N", chain)
}

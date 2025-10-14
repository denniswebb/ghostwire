package iptables

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/denniswebb/ghostwire/internal/discovery"
)

// Setup orchestrates chain preparation, exclusion insertion, DNAT rules, and audit output.
func Setup(ctx context.Context, cfg Config, mappings []discovery.ServiceMapping, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	executor := NewExecutor()

	if strings.TrimSpace(cfg.ChainName) == "" {
		return fmt.Errorf("nat chain name cannot be empty; set GW_NAT_CHAIN or use default CANARY_DNAT")
	}

	if err := EnsureChain(ctx, executor, "nat", cfg.ChainName, cfg.IPv6, logger); err != nil {
		return fmt.Errorf("prepare chain %s: %w", cfg.ChainName, err)
	}

	if err := AddExclusions(ctx, executor, "nat", cfg.ChainName, cfg.ExcludeCIDRs, cfg.IPv6, logger); err != nil {
		return fmt.Errorf("add exclusions: %w", err)
	}

	if err := AddDNATRules(ctx, executor, "nat", cfg.ChainName, mappings, cfg.IPv6, logger); err != nil {
		return fmt.Errorf("add dnat rules: %w", err)
	}

	if cfg.DnatMapPath != "" {
		if err := WriteDNATMap(cfg.DnatMapPath, mappings, logger); err != nil {
			return fmt.Errorf("write dnat map: %w", err)
		}
	}

	exclusionCount := 0
	for _, cidr := range cfg.ExcludeCIDRs {
		if strings.TrimSpace(cidr) != "" {
			exclusionCount++
		}
	}

	logger.Info(
		"dnat chain configured but NOT activated - watcher will add jump rule when role=preview",
		slog.String("chain_name", cfg.ChainName),
		slog.Int("exclusions", exclusionCount),
		slog.Int("dnat_rules", len(mappings)),
		slog.Bool("ipv6_enabled", cfg.IPv6),
		slog.String("dnat_map_path", cfg.DnatMapPath),
	)

	return nil
}

package iptables

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// JumpExists determines whether a jump from the provided hook to the target chain exists in the IPv4 table.
func JumpExists(ctx context.Context, executor Executor, table string, hook string, chain string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	exists, err := jumpExistsWithBinary(ctx, executor, ipv4Binary, table, hook, chain)
	if err != nil {
		return false, fmt.Errorf("check jump existence: %w", err)
	}

	return exists, nil
}

// AddJump inserts a jump rule at the top of the specified hook, ensuring idempotent behavior.
func AddJump(ctx context.Context, executor Executor, table string, hook string, chain string, ipv6 bool, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	exists, err := JumpExists(ctx, executor, table, hook, chain)
	if err != nil {
		return fmt.Errorf("determine jump existence: %w", err)
	}

	if exists {
		logger.Debug("jump rule already present",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Bool("ipv6", false),
		)
		return nil
	}

	logger.Info("adding jump rule",
		slog.String("table", table),
		slog.String("hook", hook),
		slog.String("chain", chain),
		slog.Bool("ipv6", false),
	)
	if err := executor.Run(ctx, ipv4Binary, "-w", iptablesWaitSeconds, "-t", table, "-I", hook, "1", "-j", chain); err != nil {
		return fmt.Errorf("add ipv4 jump: %w", err)
	}

	if !ipv6 {
		return nil
	}

	ipv6Exists, err := jumpExistsWithBinary(ctx, executor, ipv6Binary, table, hook, chain)
	if err != nil {
		logger.Warn("failed to verify ipv6 jump existence before add",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Bool("ipv6", true),
			slog.Any("error", err),
		)
	} else if ipv6Exists {
		logger.Debug("ipv6 jump rule already present",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Bool("ipv6", true),
		)
		return nil
	}

	logger.Info("adding ipv6 jump rule",
		slog.String("table", table),
		slog.String("hook", hook),
		slog.String("chain", chain),
		slog.Bool("ipv6", true),
	)
	if err := executor.Run(ctx, ipv6Binary, "-w", iptablesWaitSeconds, "-t", table, "-I", hook, "1", "-j", chain); err != nil {
		logger.Warn("failed to add ipv6 jump rule",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Any("error", err),
		)
	}

	return nil
}

// RemoveJump deletes the jump rule from the specified hook, ignoring missing rules.
func RemoveJump(ctx context.Context, executor Executor, table string, hook string, chain string, ipv6 bool, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	existsV4, err := JumpExists(ctx, executor, table, hook, chain)
	if err != nil {
		return fmt.Errorf("determine v4 jump existence: %w", err)
	}

	if existsV4 {
		logger.Info("removing jump rule",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Bool("ipv6", false),
		)
		if err := executor.Run(ctx, ipv4Binary, "-w", iptablesWaitSeconds, "-t", table, "-D", hook, "-j", chain); err != nil {
			return fmt.Errorf("remove ipv4 jump: %w", err)
		}
	} else {
		logger.Debug("ipv4 jump absent; continuing to ipv6",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Bool("ipv6", false),
		)
	}

	if !ipv6 {
		return nil
	}

	ipv6Exists, err := jumpExistsWithBinary(ctx, executor, ipv6Binary, table, hook, chain)
	if err != nil {
		logger.Warn("failed to verify ipv6 jump existence before remove",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Bool("ipv6", true),
			slog.Any("error", err),
		)
		return nil
	}

	if !ipv6Exists {
		logger.Debug("ipv6 jump rule absent; nothing to remove",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Bool("ipv6", true),
		)
		return nil
	}

	logger.Info("removing ipv6 jump rule",
		slog.String("table", table),
		slog.String("hook", hook),
		slog.String("chain", chain),
		slog.Bool("ipv6", true),
	)
	if err := executor.Run(ctx, ipv6Binary, "-w", iptablesWaitSeconds, "-t", table, "-D", hook, "-j", chain); err != nil {
		logger.Warn("failed to remove ipv6 jump rule",
			slog.String("table", table),
			slog.String("hook", hook),
			slog.String("chain", chain),
			slog.Any("error", err),
		)
	}

	return nil
}

func jumpExistsWithBinary(ctx context.Context, executor Executor, binary string, table string, hook string, chain string) (bool, error) {
	if err := executor.Run(ctx, binary, "-w", iptablesWaitSeconds, "-t", table, "-C", hook, "-j", chain); err != nil {
		var cmdErr *CommandError
		if errors.As(err, &cmdErr) {
			var exitErr interface{ ExitCode() int }
			if errors.As(cmdErr.Err, &exitErr) && exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		return false, err
	}

	return true, nil
}

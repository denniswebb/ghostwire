package iptables

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Executor abstracts command execution for iptables interactions.
type Executor interface {
	Run(ctx context.Context, command string, args ...string) error
	ChainExists(ctx context.Context, table string, chain string) (bool, error)
	ChainExists6(ctx context.Context, table string, chain string) (bool, error)
}

// CommandError captures detailed failure information from command execution.
type CommandError struct {
	Command string
	Args    []string
	Output  string
	Err     error
}

// Error implements the error interface.
func (e *CommandError) Error() string {
	joined := strings.Join(e.Args, " ")
	if e.Output != "" {
		return fmt.Sprintf("command %s %s failed: %v: %s", e.Command, joined, e.Err, strings.TrimSpace(e.Output))
	}
	return fmt.Sprintf("command %s %s failed: %v", e.Command, joined, e.Err)
}

// Unwrap exposes the underlying error for errors.Is / errors.As checks.
func (e *CommandError) Unwrap() error {
	return e.Err
}

// RealExecutor executes commands on the host system.
type RealExecutor struct{}

// NewExecutor constructs a RealExecutor instance.
func NewExecutor() Executor {
	return &RealExecutor{}
}

// Run executes the provided command and returns detailed errors when it fails.
func (r *RealExecutor) Run(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &CommandError{
			Command: command,
			Args:    append([]string(nil), args...),
			Output:  string(output),
			Err:     err,
		}
	}
	return nil
}

func chainExists(ctx context.Context, binary string, table string, chain string) (bool, error) {
	cmd := exec.CommandContext(ctx, binary, "-w", "5", "-t", table, "-L", chain)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, &CommandError{
			Command: binary,
			Args:    []string{"-w", "5", "-t", table, "-L", chain},
			Output:  string(output),
			Err:     err,
		}
	}

	return false, fmt.Errorf("checking chain existence: %w", err)
}

// ChainExists determines whether the requested IPv4 chain is present in the specified table.
func (r *RealExecutor) ChainExists(ctx context.Context, table string, chain string) (bool, error) {
	return chainExists(ctx, ipv4Binary, table, chain)
}

// ChainExists6 determines whether the requested IPv6 chain is present in the specified table.
func (r *RealExecutor) ChainExists6(ctx context.Context, table string, chain string) (bool, error) {
	return chainExists(ctx, ipv6Binary, table, chain)
}

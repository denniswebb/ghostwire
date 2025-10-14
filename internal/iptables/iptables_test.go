package iptables

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/denniswebb/ghostwire/internal/discovery"
)

type execCall struct {
	command string
	args    []string
}

type recordingExecutor struct {
	calls []execCall
}

func (r *recordingExecutor) Run(_ context.Context, command string, args ...string) error {
	r.calls = append(r.calls, execCall{
		command: command,
		args:    append([]string(nil), args...),
	})
	return nil
}

func (r *recordingExecutor) ChainExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestIsIPv6(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "ipv4", input: "10.0.0.1", want: false},
		{name: "ipv6", input: "fd00::1", want: true},
		{name: "invalid", input: "not-an-ip", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isIPv6(tc.input); got != tc.want {
				t.Fatalf("isIPv6(%q) = %t, want %t", tc.input, got, tc.want)
			}
		})
	}
}

func TestAddDNATRulesIPFamilyHandling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := discardLogger()
	table := "nat"
	chain := "CANARY_DNAT"

	t.Run("ipv4", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}
		mappings := []discovery.ServiceMapping{
			{
				ServiceName:      "svc",
				Port:             80,
				Protocol:         corev1.ProtocolTCP,
				ActiveClusterIP:  "10.0.0.1",
				PreviewClusterIP: "10.0.0.2",
			},
		}

		if err := AddDNATRules(ctx, exec, table, chain, mappings, false, logger); err != nil {
			t.Fatalf("AddDNATRules returned error: %v", err)
		}

		if len(exec.calls) != 1 {
			t.Fatalf("expected 1 command, got %d", len(exec.calls))
		}

		call := exec.calls[0]
		wantArgs := []string{"-w", "5", "-t", table, "-A", chain, "-d", "10.0.0.1", "-p", "tcp", "--dport", "80", "-j", "DNAT", "--to-destination", "10.0.0.2:80"}
		if call.command != ipv4Binary {
			t.Fatalf("expected command %q, got %q", ipv4Binary, call.command)
		}
		if !equalSlices(call.args, wantArgs) {
			t.Fatalf("expected args %v, got %v", wantArgs, call.args)
		}
	})

	t.Run("ipv6", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}
		mappings := []discovery.ServiceMapping{
			{
				ServiceName:      "svc6",
				Port:             443,
				Protocol:         corev1.ProtocolTCP,
				ActiveClusterIP:  "fd00::1",
				PreviewClusterIP: "fd00::2",
			},
		}

		if err := AddDNATRules(ctx, exec, table, chain, mappings, true, logger); err != nil {
			t.Fatalf("AddDNATRules returned error: %v", err)
		}

		if len(exec.calls) != 1 {
			t.Fatalf("expected 1 command, got %d", len(exec.calls))
		}

		call := exec.calls[0]
		wantArgs := []string{"-w", "5", "-t", table, "-A", chain, "-d", "fd00::1", "-p", "tcp", "--dport", "443", "-j", "DNAT", "--to-destination", "fd00::2:443"}
		if call.command != ipv6Binary {
			t.Fatalf("expected command %q, got %q", ipv6Binary, call.command)
		}
		if !equalSlices(call.args, wantArgs) {
			t.Fatalf("expected args %v, got %v", wantArgs, call.args)
		}
	})

	t.Run("mixed families skip", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}
		mappings := []discovery.ServiceMapping{
			{
				ServiceName:      "mixed",
				Port:             53,
				Protocol:         corev1.ProtocolUDP,
				ActiveClusterIP:  "10.0.0.1",
				PreviewClusterIP: "fd00::1",
			},
		}

		if err := AddDNATRules(ctx, exec, table, chain, mappings, true, logger); err != nil {
			t.Fatalf("AddDNATRules returned error: %v", err)
		}

		if len(exec.calls) != 0 {
			t.Fatalf("expected no commands due to skip, got %d", len(exec.calls))
		}
	})

	t.Run("ipv6 disabled skip", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}
		mappings := []discovery.ServiceMapping{
			{
				ServiceName:      "svc6",
				Port:             8080,
				Protocol:         corev1.ProtocolTCP,
				ActiveClusterIP:  "fd00::1",
				PreviewClusterIP: "fd00::2",
			},
		}

		if err := AddDNATRules(ctx, exec, table, chain, mappings, false, logger); err != nil {
			t.Fatalf("AddDNATRules returned error: %v", err)
		}

		if len(exec.calls) != 0 {
			t.Fatalf("expected no commands when ipv6 disabled, got %d", len(exec.calls))
		}
	})
}

func TestAddExclusions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := discardLogger()
	table := "nat"
	chain := "CANARY_DNAT"
	cidrs := []string{"10.0.0.0/24", "fd00::/64"}

	t.Run("ipv6 enabled", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}

		if err := AddExclusions(ctx, exec, table, chain, cidrs, true, logger); err != nil {
			t.Fatalf("AddExclusions returned error: %v", err)
		}

		if len(exec.calls) != 2 {
			t.Fatalf("expected 2 commands, got %d", len(exec.calls))
		}

		ipv4Call := exec.calls[0]
		ipv6Call := exec.calls[1]

		if ipv4Call.command != ipv4Binary {
			t.Fatalf("expected ipv4 command %q, got %q", ipv4Binary, ipv4Call.command)
		}
		wantIPv4Args := []string{"-w", "5", "-t", table, "-A", chain, "-d", "10.0.0.0/24", "-j", "RETURN"}
		if !equalSlices(ipv4Call.args, wantIPv4Args) {
			t.Fatalf("expected ipv4 args %v, got %v", wantIPv4Args, ipv4Call.args)
		}

		if ipv6Call.command != ipv6Binary {
			t.Fatalf("expected ipv6 command %q, got %q", ipv6Binary, ipv6Call.command)
		}
		wantIPv6Args := []string{"-w", "5", "-t", table, "-A", chain, "-d", "fd00::/64", "-j", "RETURN"}
		if !equalSlices(ipv6Call.args, wantIPv6Args) {
			t.Fatalf("expected ipv6 args %v, got %v", wantIPv6Args, ipv6Call.args)
		}
	})

	t.Run("ipv6 disabled skips v6", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}

		if err := AddExclusions(ctx, exec, table, chain, cidrs, false, logger); err != nil {
			t.Fatalf("AddExclusions returned error: %v", err)
		}

		if len(exec.calls) != 1 {
			t.Fatalf("expected 1 command, got %d", len(exec.calls))
		}
		if exec.calls[0].command != ipv4Binary {
			t.Fatalf("expected ipv4 command when ipv6 disabled, got %q", exec.calls[0].command)
		}
	})
}

func TestSetupRejectsEmptyChainName(t *testing.T) {
	t.Parallel()

	err := Setup(context.Background(), Config{
		ChainName: " ",
	}, nil, discardLogger())
	if err == nil {
		t.Fatal("expected error for empty chain name, got nil")
	}
	if !strings.Contains(err.Error(), "nat chain name cannot be empty") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestChainExistsAddsWaitFlag(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "iptables_args.txt")

	scriptContent := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$*\" > %s\nexit 1\n", logPath)
	if err := os.WriteFile(filepath.Join(tempDir, "iptables"), []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to write stub iptables: %v", err)
	}

	originalPath := os.Getenv("PATH")
	if originalPath != "" {
		t.Setenv("PATH", fmt.Sprintf("%s:%s", tempDir, originalPath))
	} else {
		t.Setenv("PATH", tempDir)
	}

	exec := &RealExecutor{}
	exists, err := exec.ChainExists(context.Background(), "nat", "CANARY_DNAT")
	if err != nil {
		t.Fatalf("ChainExists returned error: %v", err)
	}
	if exists {
		t.Fatal("expected chain to be absent")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read args log: %v", err)
	}

	got := strings.TrimSpace(string(data))
	want := "-w 5 -t nat -L CANARY_DNAT"
	if got != want {
		t.Fatalf("expected iptables args %q, got %q", want, got)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

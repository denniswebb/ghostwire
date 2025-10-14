package iptables

import (
	"bytes"
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
	calls            []execCall
	chainExists      bool
	chainExistsErr   error
	chainExists6     bool
	chainExists6Err  error
	runErrors        map[string]error
	chainExistsHits  int
	chainExists6Hits int
}

func (r *recordingExecutor) Run(_ context.Context, command string, args ...string) error {
	call := execCall{
		command: command,
		args:    append([]string(nil), args...),
	}
	r.calls = append(r.calls, call)

	if r.runErrors != nil {
		key := command + " " + strings.Join(args, " ")
		if err, ok := r.runErrors[key]; ok {
			return err
		}
	}

	return nil
}

func (r *recordingExecutor) ChainExists(context.Context, string, string) (bool, error) {
	r.chainExistsHits++
	if r.chainExistsErr != nil {
		return false, r.chainExistsErr
	}
	return r.chainExists, nil
}

func (r *recordingExecutor) ChainExists6(context.Context, string, string) (bool, error) {
	r.chainExists6Hits++
	if r.chainExists6Err != nil {
		return false, r.chainExists6Err
	}
	return r.chainExists6, nil
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

		added, err := AddDNATRules(ctx, exec, table, chain, mappings, false, logger)
		if err != nil {
			t.Fatalf("AddDNATRules returned error: %v", err)
		}
		if added != 1 {
			t.Fatalf("expected 1 rule added, got %d", added)
		}

		if len(exec.calls) != 1 {
			t.Fatalf("expected 1 command, got %d", len(exec.calls))
		}

		call := exec.calls[0]
		wantArgs := []string{"-w", iptablesWaitSeconds, "-t", table, "-A", chain, "-d", "10.0.0.1", "-p", "tcp", "--dport", "80", "-j", "DNAT", "--to-destination", "10.0.0.2:80"}
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

		added, err := AddDNATRules(ctx, exec, table, chain, mappings, true, logger)
		if err != nil {
			t.Fatalf("AddDNATRules returned error: %v", err)
		}
		if added != 1 {
			t.Fatalf("expected 1 rule added, got %d", added)
		}

		if len(exec.calls) != 1 {
			t.Fatalf("expected 1 command, got %d", len(exec.calls))
		}

		call := exec.calls[0]
		wantArgs := []string{"-w", iptablesWaitSeconds, "-t", table, "-A", chain, "-d", "fd00::1", "-p", "tcp", "--dport", "443", "-j", "DNAT", "--to-destination", "fd00::2:443"}
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

		added, err := AddDNATRules(ctx, exec, table, chain, mappings, true, logger)
		if err != nil {
			t.Fatalf("AddDNATRules returned error: %v", err)
		}
		if added != 0 {
			t.Fatalf("expected 0 rules added due to skip, got %d", added)
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

		added, err := AddDNATRules(ctx, exec, table, chain, mappings, false, logger)
		if err != nil {
			t.Fatalf("AddDNATRules returned error: %v", err)
		}
		if added != 0 {
			t.Fatalf("expected 0 rules added when ipv6 disabled, got %d", added)
		}

		if len(exec.calls) != 0 {
			t.Fatalf("expected no commands when ipv6 disabled, got %d", len(exec.calls))
		}
	})
}

func TestEnsureChain(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := discardLogger()
	table := "nat"
	chain := "CANARY_DNAT"

	t.Run("creates chain when missing", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{chainExists: false}
		if err := EnsureChain(ctx, exec, table, chain, false, logger); err != nil {
			t.Fatalf("EnsureChain returned error: %v", err)
		}
		if exec.chainExistsHits != 1 {
			t.Fatalf("ChainExists called %d times, want 1", exec.chainExistsHits)
		}
		if len(exec.calls) != 1 {
			t.Fatalf("expected 1 command, got %d", len(exec.calls))
		}
		call := exec.calls[0]
		wantArgs := []string{"-w", iptablesWaitSeconds, "-t", table, "-N", chain}
		if call.command != ipv4Binary || !equalSlices(call.args, wantArgs) {
			t.Fatalf("unexpected command %+v", call)
		}
	})

	t.Run("flushes chain when present", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{chainExists: true}
		if err := EnsureChain(ctx, exec, table, chain, false, logger); err != nil {
			t.Fatalf("EnsureChain returned error: %v", err)
		}
		if exec.chainExistsHits != 1 {
			t.Fatalf("ChainExists called %d times, want 1", exec.chainExistsHits)
		}
		if len(exec.calls) != 1 {
			t.Fatalf("expected 1 command, got %d", len(exec.calls))
		}
		call := exec.calls[0]
		wantArgs := []string{"-w", iptablesWaitSeconds, "-t", table, "-F", chain}
		if call.command != ipv4Binary || !equalSlices(call.args, wantArgs) {
			t.Fatalf("unexpected command %+v", call)
		}
	})

	t.Run("creates ipv6 chain when enabled", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{chainExists: false, chainExists6: false}
		if err := EnsureChain(ctx, exec, table, chain, true, logger); err != nil {
			t.Fatalf("EnsureChain returned error: %v", err)
		}
		if exec.chainExistsHits != 1 || exec.chainExists6Hits != 1 {
			t.Fatalf("unexpected chain check counts ipv4=%d ipv6=%d", exec.chainExistsHits, exec.chainExists6Hits)
		}
		if len(exec.calls) != 2 {
			t.Fatalf("expected 2 commands, got %d", len(exec.calls))
		}
		first := exec.calls[0]
		if first.command != ipv4Binary || !equalSlices(first.args, []string{"-w", iptablesWaitSeconds, "-t", table, "-N", chain}) {
			t.Fatalf("unexpected ipv4 command %+v", first)
		}
		second := exec.calls[1]
		if second.command != ipv6Binary || !equalSlices(second.args, []string{"-w", iptablesWaitSeconds, "-t", table, "-N", chain}) {
			t.Fatalf("unexpected ipv6 command %+v", second)
		}
	})

	t.Run("ipv6 failures are logged but tolerated", func(t *testing.T) {
		t.Parallel()
		ResetIPv6ChainFailuresForTest()
		exec := &recordingExecutor{chainExists: false, chainExists6Err: fmt.Errorf("boom")}
		buf := &bytes.Buffer{}
		logger := slog.New(slog.NewTextHandler(buf, nil))

		if err := EnsureChain(ctx, exec, table, chain, true, logger); err != nil {
			t.Fatalf("EnsureChain returned error: %v", err)
		}

		if !strings.Contains(buf.String(), "ip6tables chain preparation failed") {
			t.Fatalf("expected warning about ipv6 failure, got %q", buf.String())
		}

		if got := IPv6ChainFailures(); got != 1 {
			t.Fatalf("expected IPv6 failure counter to be 1, got %d", got)
		}
	})

	t.Run("chain exists error propagates", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{chainExistsErr: fmt.Errorf("lookup failed")}
		if err := EnsureChain(ctx, exec, table, chain, false, logger); err == nil {
			t.Fatalf("expected error from EnsureChain")
		}
	})

	t.Run("run error propagates", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{
			runErrors: map[string]error{
				fmt.Sprintf("%s -w 5 -t %s -N %s", ipv4Binary, table, chain): fmt.Errorf("create failed"),
			},
		}
		if err := EnsureChain(ctx, exec, table, chain, false, logger); err == nil {
			t.Fatalf("expected error from EnsureChain")
		}
	})
}

func TestAddExclusionsScenarios(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("empty cidr list produces no commands", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}
		if err := AddExclusions(ctx, exec, "nat", "CHAIN", nil, false, discardLogger()); err != nil {
			t.Fatalf("AddExclusions returned error: %v", err)
		}
		if len(exec.calls) != 0 {
			t.Fatalf("expected no commands, got %d", len(exec.calls))
		}
	})

	t.Run("mixed ipv4 ipv6 with ipv6 disabled skips ipv6 cidrs", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}
		buf := &bytes.Buffer{}
		logger := slog.New(slog.NewTextHandler(buf, nil))

		cidrs := []string{"169.254.169.254/32", "fd00::/64"}
		if err := AddExclusions(ctx, exec, "nat", "CHAIN", cidrs, false, logger); err != nil {
			t.Fatalf("AddExclusions returned error: %v", err)
		}

		if len(exec.calls) != 1 {
			t.Fatalf("expected 1 command for ipv4 exclusion, got %d", len(exec.calls))
		}
		call := exec.calls[0]
		wantArgs := []string{"-w", iptablesWaitSeconds, "-t", "nat", "-A", "CHAIN", "-d", "169.254.169.254/32", "-j", "RETURN"}
		if call.command != ipv4Binary || !equalSlices(call.args, wantArgs) {
			t.Fatalf("unexpected command %+v", call)
		}

		if !strings.Contains(buf.String(), "skipping ipv6 exclusion without ipv6 support") {
			t.Fatalf("expected warning about skipping ipv6 exclusion, got %q", buf.String())
		}
	})
}

func TestWriteDNATMap(t *testing.T) {
	t.Parallel()

	logger := discardLogger()

	t.Run("writes expected contents and permissions", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "dnat.map")

		mappings := []discovery.ServiceMapping{
			{
				ServiceName:      "orders",
				Port:             80,
				Protocol:         corev1.ProtocolTCP,
				ActiveClusterIP:  "10.0.0.10",
				PreviewClusterIP: "10.0.1.10",
			},
			{
				ServiceName:      "payment",
				Port:             443,
				Protocol:         corev1.ProtocolTCP,
				ActiveClusterIP:  "10.0.0.20",
				PreviewClusterIP: "10.0.1.20",
			},
		}

		if err := WriteDNATMap(path, mappings, logger); err != nil {
			t.Fatalf("WriteDNATMap returned error: %v", err)
		}

		// #nosec G304 -- temp dir path is fully controlled by test, no external input.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		expected := "# DNAT mappings generated by ghostwire-init\n# Format: service:port/protocol active_ip -> preview_ip\norders:80/TCP 10.0.0.10 -> 10.0.1.10\npayment:443/TCP 10.0.0.20 -> 10.0.1.20\n"
		if string(data) != expected {
			t.Fatalf("unexpected map contents:\n%s\nwant:\n%s", data, expected)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("file perm = %v, want 0644", info.Mode().Perm())
		}
	})

	t.Run("handles empty mappings", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "dnat-empty.map")

		if err := WriteDNATMap(path, nil, logger); err != nil {
			t.Fatalf("WriteDNATMap returned error: %v", err)
		}

		// #nosec G304 -- temp dir path is fully controlled by test, no external input.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		expected := "# DNAT mappings generated by ghostwire-init\n# Format: service:port/protocol active_ip -> preview_ip\n"
		if string(data) != expected {
			t.Fatalf("unexpected map contents %q", data)
		}
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "missing", "dnat.map")
		if err := WriteDNATMap(path, nil, logger); err == nil {
			t.Fatalf("expected error for invalid path")
		}
	})

	t.Run("path traversal rejected", func(t *testing.T) {
		t.Parallel()
		if err := WriteDNATMap("../dnat.map", nil, logger); err == nil {
			t.Fatalf("expected error for traversal path")
		}
	})
}

func TestAddDNATRulesSCTP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := discardLogger()
	table := "nat"
	chain := "CANARY_DNAT"

	exec := &recordingExecutor{}
	mappings := []discovery.ServiceMapping{
		{
			ServiceName:      "sctp-service",
			Port:             5000,
			Protocol:         corev1.ProtocolSCTP,
			ActiveClusterIP:  "10.0.0.30",
			PreviewClusterIP: "10.0.1.30",
		},
	}

	added, err := AddDNATRules(ctx, exec, table, chain, mappings, false, logger)
	if err != nil {
		t.Fatalf("AddDNATRules returned error: %v", err)
	}
	if added != 1 {
		t.Fatalf("expected 1 rule added, got %d", added)
	}

	if len(exec.calls) != 1 {
		t.Fatalf("expected 1 command, got %d", len(exec.calls))
	}

	call := exec.calls[0]
	wantArgs := []string{"-w", iptablesWaitSeconds, "-t", table, "-A", chain, "-d", "10.0.0.30", "-p", "sctp", "--dport", "5000", "-j", "DNAT", "--to-destination", "10.0.1.30:5000"}
	if call.command != ipv4Binary || !equalSlices(call.args, wantArgs) {
		t.Fatalf("unexpected command %+v", call)
	}
}

func withExecutorFactory(exec Executor) func() {
	previous := executorFactory
	executorFactory = func() Executor { return exec }
	return func() {
		executorFactory = previous
	}
}

func TestSetup(t *testing.T) {
	ctx := context.Background()
	logger := discardLogger()

	makeMappings := func() []discovery.ServiceMapping {
		return []discovery.ServiceMapping{
			{
				ServiceName:      "orders",
				Port:             80,
				Protocol:         corev1.ProtocolTCP,
				ActiveClusterIP:  "10.0.0.10",
				PreviewClusterIP: "10.0.1.10",
			},
			{
				ServiceName:      "payment",
				Port:             443,
				Protocol:         corev1.ProtocolTCP,
				ActiveClusterIP:  "10.0.0.20",
				PreviewClusterIP: "10.0.1.20",
			},
		}
	}

	t.Run("successful setup writes map and executes commands", func(t *testing.T) {
		exec := &recordingExecutor{}
		restore := withExecutorFactory(exec)
		t.Cleanup(restore)

		dir := t.TempDir()
		mapPath := filepath.Join(dir, "dnat.map")

		cfg := Config{
			ChainName:    "CANARY_DNAT",
			ExcludeCIDRs: []string{"169.254.169.254/32"},
			IPv6:         false,
			DnatMapPath:  mapPath,
		}

		if err := Setup(ctx, cfg, makeMappings(), logger); err != nil {
			t.Fatalf("Setup returned error: %v", err)
		}

		if len(exec.calls) != 1+1+2 { // ensure chain + exclusion + two dnat rules
			t.Fatalf("expected 4 commands, got %d", len(exec.calls))
		}

		// #nosec G304 -- temp dir path is fully controlled by test, no external input.
		data, err := os.ReadFile(mapPath)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if !strings.Contains(string(data), "orders:80/TCP") || !strings.Contains(string(data), "payment:443/TCP") {
			t.Fatalf("dnat map missing expected entries: %s", data)
		}
	})

	t.Run("empty mappings succeed with no dnat commands", func(t *testing.T) {
		exec := &recordingExecutor{}
		restore := withExecutorFactory(exec)
		t.Cleanup(restore)

		cfg := Config{
			ChainName:    "CANARY_DNAT",
			ExcludeCIDRs: []string{"169.254.169.254/32"},
			IPv6:         false,
		}

		if err := Setup(ctx, cfg, nil, logger); err != nil {
			t.Fatalf("Setup returned error: %v", err)
		}

		dnatCommands := 0
		for _, call := range exec.calls {
			if call.command != ipv4Binary {
				continue
			}
			for _, arg := range call.args {
				if arg == "--to-destination" {
					dnatCommands++
					break
				}
			}
		}
		if dnatCommands != 0 {
			t.Fatalf("expected no dnat commands, got %d", dnatCommands)
		}
	})

	t.Run("empty chain name falls back to default", func(t *testing.T) {
		exec := &recordingExecutor{}
		restore := withExecutorFactory(exec)
		t.Cleanup(restore)

		if err := Setup(ctx, Config{ChainName: "   "}, nil, logger); err != nil {
			t.Fatalf("expected default chain for empty name, got error: %v", err)
		}

		if len(exec.calls) == 0 {
			t.Fatalf("expected at least one command to be issued")
		}

		call := exec.calls[0]
		wantArgs := []string{"-w", iptablesWaitSeconds, "-t", "nat", "-N", defaultChainName}
		if call.command != ipv4Binary || !equalSlices(call.args, wantArgs) {
			t.Fatalf("unexpected command for default chain: %+v", call)
		}
	})

	t.Run("context cancellation handled", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()
		if err := Setup(cancelCtx, Config{ChainName: "CANARY_DNAT"}, nil, logger); err == nil {
			t.Fatalf("expected context cancellation error")
		}
	})

	t.Run("ensure chain error propagates", func(t *testing.T) {
		exec := &recordingExecutor{chainExistsErr: fmt.Errorf("boom")}
		restore := withExecutorFactory(exec)
		t.Cleanup(restore)

		if err := Setup(ctx, Config{ChainName: "CANARY_DNAT"}, nil, logger); err == nil {
			t.Fatalf("expected error from ensure chain")
		}
	})

	t.Run("exclusion error propagates", func(t *testing.T) {
		exec := &recordingExecutor{
			runErrors: map[string]error{
				fmt.Sprintf("%s -w %s -t %s -A %s -d %s -j RETURN", ipv4Binary, iptablesWaitSeconds, "nat", "CANARY_DNAT", "169.254.169.254/32"): fmt.Errorf("exclude failed"),
			},
		}
		restore := withExecutorFactory(exec)
		t.Cleanup(restore)

		cfg := Config{
			ChainName:    "CANARY_DNAT",
			ExcludeCIDRs: []string{"169.254.169.254/32"},
		}

		if err := Setup(ctx, cfg, makeMappings(), logger); err == nil {
			t.Fatalf("expected error from exclusions")
		}
	})

	t.Run("dnat rule error propagates", func(t *testing.T) {
		exec := &recordingExecutor{
			runErrors: map[string]error{
				fmt.Sprintf("%s -w %s -t %s -A %s -d %s -p %s --dport %d -j DNAT --to-destination %s:%d", ipv4Binary, iptablesWaitSeconds, "nat", "CANARY_DNAT", "10.0.0.10", "tcp", 80, "10.0.1.10", 80): fmt.Errorf("dnat failed"),
			},
		}
		restore := withExecutorFactory(exec)
		t.Cleanup(restore)

		cfg := Config{
			ChainName:    "CANARY_DNAT",
			ExcludeCIDRs: []string{"169.254.169.254/32"},
		}

		if err := Setup(ctx, cfg, makeMappings(), logger); err == nil {
			t.Fatalf("expected error from dnat rules")
		}
	})

	t.Run("dnat map error propagates", func(t *testing.T) {
		exec := &recordingExecutor{}
		restore := withExecutorFactory(exec)
		t.Cleanup(restore)

		cfg := Config{
			ChainName:    "CANARY_DNAT",
			ExcludeCIDRs: []string{"169.254.169.254/32"},
			DnatMapPath:  filepath.Join(t.TempDir(), "missing", "dnat.map"),
		}

		if err := Setup(ctx, cfg, makeMappings(), logger); err == nil {
			t.Fatalf("expected error from dnat map write")
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
		wantIPv4Args := []string{"-w", iptablesWaitSeconds, "-t", table, "-A", chain, "-d", "10.0.0.0/24", "-j", "RETURN"}
		if !equalSlices(ipv4Call.args, wantIPv4Args) {
			t.Fatalf("expected ipv4 args %v, got %v", wantIPv4Args, ipv4Call.args)
		}

		if ipv6Call.command != ipv6Binary {
			t.Fatalf("expected ipv6 command %q, got %q", ipv6Binary, ipv6Call.command)
		}
		wantIPv6Args := []string{"-w", iptablesWaitSeconds, "-t", table, "-A", chain, "-d", "fd00::/64", "-j", "RETURN"}
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

	t.Run("invalid cidr returns error", func(t *testing.T) {
		t.Parallel()
		exec := &recordingExecutor{}
		err := AddExclusions(ctx, exec, table, chain, []string{"bad-cidr"}, false, logger)
		if err == nil {
			t.Fatalf("expected error for invalid cidr")
		}
		if len(exec.calls) != 0 {
			t.Fatalf("expected no commands when cidr invalid, got %d", len(exec.calls))
		}
	})
}

func TestChainExistsAddsWaitFlag(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "iptables_args.txt")

	scriptPath := filepath.Join(tempDir, "iptables")
	scriptContent := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$*\" > %s\nexit 1\n", logPath)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o600); err != nil {
		t.Fatalf("failed to write stub iptables: %v", err)
	}
	// #nosec G302 - executable permissions are required so the stub can run in this test.
	if err := os.Chmod(scriptPath, 0o700); err != nil {
		t.Fatalf("failed to chmod stub iptables: %v", err)
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

	// #nosec G304 - logPath is generated within the test temp directory.
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

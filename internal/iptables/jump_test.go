package iptables

import (
	"context"
	"strings"
	"testing"
)

type fakeExitError struct {
	code int
}

func (f fakeExitError) Error() string { return "exit" }

func (f fakeExitError) ExitCode() int { return f.code }

type fakeExecutor struct {
	responses map[string]error
	calls     []execCall
}

func (f *fakeExecutor) Run(ctx context.Context, command string, args ...string) error {
	call := execCall{command: command, args: append([]string(nil), args...)}
	f.calls = append(f.calls, call)

	if f.responses != nil {
		if err, ok := f.responses[runKey(command, args)]; ok {
			return err
		}
	}

	return nil
}

func (f *fakeExecutor) ChainExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (f *fakeExecutor) ChainExists6(context.Context, string, string) (bool, error) {
	return false, nil
}

func runKey(command string, args []string) string {
	return command + " " + strings.Join(args, " ")
}

func TestAddJumpInsertsRuleWhenMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	exec := &fakeExecutor{
		responses: map[string]error{
			runKey(ipv4Binary, []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"}): &CommandError{
				Command: ipv4Binary,
				Args:    []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"},
				Err:     fakeExitError{code: 1},
			},
		},
	}

	if err := AddJump(ctx, exec, "nat", "OUTPUT", "CANARY_DNAT", false, discardLogger()); err != nil {
		t.Fatalf("AddJump returned error: %v", err)
	}

	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(exec.calls))
	}

	got := exec.calls[1]
	if got.command != ipv4Binary || runKey(got.command, got.args) != runKey(ipv4Binary, []string{"-w", iptablesWaitSeconds, "-t", "nat", "-I", "OUTPUT", "1", "-j", "CANARY_DNAT"}) {
		t.Fatalf("unexpected insert command: %#v", got)
	}
}

func TestAddJumpSkipsWhenPresent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	exec := &fakeExecutor{}

	if err := AddJump(ctx, exec, "nat", "OUTPUT", "CANARY_DNAT", false, discardLogger()); err != nil {
		t.Fatalf("AddJump returned error: %v", err)
	}

	if len(exec.calls) != 1 {
		t.Fatalf("expected only the check command, got %d calls", len(exec.calls))
	}
	if runKey(exec.calls[0].command, exec.calls[0].args) != runKey(ipv4Binary, []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"}) {
		t.Fatalf("unexpected check command: %#v", exec.calls[0])
	}
}

func TestAddJumpAddsIPv6WhenEnabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	exec := &fakeExecutor{
		responses: map[string]error{
			runKey(ipv4Binary, []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"}): &CommandError{
				Command: ipv4Binary,
				Args:    []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"},
				Err:     fakeExitError{code: 1},
			},
			runKey(ipv6Binary, []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"}): &CommandError{
				Command: ipv6Binary,
				Args:    []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"},
				Err:     fakeExitError{code: 1},
			},
		},
	}

	if err := AddJump(ctx, exec, "nat", "OUTPUT", "CANARY_DNAT", true, discardLogger()); err != nil {
		t.Fatalf("AddJump returned error: %v", err)
	}

	if len(exec.calls) != 4 {
		t.Fatalf("expected 4 commands with ipv6 enabled, got %d", len(exec.calls))
	}
	if runKey(exec.calls[3].command, exec.calls[3].args) != runKey(ipv6Binary, []string{"-w", iptablesWaitSeconds, "-t", "nat", "-I", "OUTPUT", "1", "-j", "CANARY_DNAT"}) {
		t.Fatalf("unexpected ipv6 insert command: %#v", exec.calls[3])
	}
}

func TestRemoveJumpRemovesRuleWhenPresent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	exec := &fakeExecutor{}

	if err := RemoveJump(ctx, exec, "nat", "OUTPUT", "CANARY_DNAT", false, discardLogger()); err != nil {
		t.Fatalf("RemoveJump returned error: %v", err)
	}

	if len(exec.calls) != 2 {
		t.Fatalf("expected check and delete commands, got %d", len(exec.calls))
	}
	if runKey(exec.calls[1].command, exec.calls[1].args) != runKey(ipv4Binary, []string{"-w", iptablesWaitSeconds, "-t", "nat", "-D", "OUTPUT", "-j", "CANARY_DNAT"}) {
		t.Fatalf("unexpected delete command: %#v", exec.calls[1])
	}
}

func TestRemoveJumpNoOpWhenMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	exec := &fakeExecutor{
		responses: map[string]error{
			runKey(ipv4Binary, []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"}): &CommandError{
				Command: ipv4Binary,
				Args:    []string{"-w", iptablesWaitSeconds, "-t", "nat", "-C", "OUTPUT", "-j", "CANARY_DNAT"},
				Err:     fakeExitError{code: 1},
			},
		},
	}

	if err := RemoveJump(ctx, exec, "nat", "OUTPUT", "CANARY_DNAT", false, discardLogger()); err != nil {
		t.Fatalf("RemoveJump returned error: %v", err)
	}

	if len(exec.calls) != 1 {
		t.Fatalf("expected only the check command when rule missing, got %d", len(exec.calls))
	}
}

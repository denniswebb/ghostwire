package cmd

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/denniswebb/ghostwire/internal/iptables"
	"github.com/denniswebb/ghostwire/internal/metrics"
)

func TestJumpManagerOnTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		previous       string
		current        string
		setupExecutor  func(exec *mockExecutor)
		setupMetrics   func(m *metrics.Metrics)
		expectErr      bool
		expectedCalls  []string
		forbiddenArgs  []string
		expectedGauge  float64
		expectedErrors map[string]float64
		absentLabels   []string
		logSnippets    []string
	}{
		{
			name:     "transition to preview adds jump",
			previous: "active",
			current:  "preview",
			setupExecutor: func(exec *mockExecutor) {
				exec.runHook = func(command string, args []string) error {
					if containsArg(args, "-C") {
						return &iptables.CommandError{Command: command, Args: append([]string(nil), args...), Err: &exitErr{code: 1}}
					}
					return nil
				}
			},
			expectedCalls:  []string{"-C", "-I"},
			expectedGauge:  1,
			expectedErrors: map[string]float64{},
			absentLabels:   []string{metricErrorLabelIptables},
			logSnippets:    []string{"activating dnat jump", "level=INFO"},
		},
		{
			name:     "transition to preview no-op when jump exists",
			previous: "active",
			current:  "preview",
			setupExecutor: func(exec *mockExecutor) {
				exec.runHook = func(command string, args []string) error {
					if containsArg(args, "-C") {
						return nil
					}
					return nil
				}
			},
			expectedCalls:  []string{"-C"},
			forbiddenArgs:  []string{"-I"},
			expectedGauge:  1,
			expectedErrors: map[string]float64{},
			absentLabels:   []string{metricErrorLabelIptables},
			logSnippets:    []string{"activating dnat jump", "jump rule already present"},
		},
		{
			name:     "transition to active removes jump",
			previous: "preview",
			current:  "active",
			setupExecutor: func(exec *mockExecutor) {
				exec.runHook = func(command string, args []string) error {
					if containsArg(args, "-C") {
						return nil
					}
					return nil
				}
			},
			setupMetrics: func(m *metrics.Metrics) {
				m.SetJumpActive(true)
			},
			expectedCalls:  []string{"-C", "-D"},
			expectedGauge:  0,
			expectedErrors: map[string]float64{},
			absentLabels:   []string{metricErrorLabelIptables},
			logSnippets:    []string{"deactivating dnat jump", "level=INFO"},
		},
		{
			name:           "unrecognized transition ignored",
			previous:       "preview",
			current:        "shadow",
			setupExecutor:  func(exec *mockExecutor) {},
			expectedCalls:  nil,
			expectedGauge:  0,
			expectedErrors: map[string]float64{},
			absentLabels:   []string{metricErrorLabelIptables},
			logSnippets:    []string{"ignoring transition", "level=DEBUG"},
		},
		{
			name:     "add jump error increments metric",
			previous: "active",
			current:  "preview",
			setupExecutor: func(exec *mockExecutor) {
				exec.runHook = func(command string, args []string) error {
					if containsArg(args, "-C") {
						return &iptables.CommandError{Command: command, Args: append([]string(nil), args...), Err: &exitErr{code: 1}}
					}
					if containsArg(args, "-I") {
						return errors.New("boom")
					}
					return nil
				}
			},
			expectErr:     true,
			expectedCalls: []string{"-C", "-I"},
			expectedGauge: 0,
			expectedErrors: map[string]float64{
				metricErrorLabelIptables: 1,
			},
		},
		{
			name:     "remove jump error increments metric",
			previous: "preview",
			current:  "active",
			setupExecutor: func(exec *mockExecutor) {
				exec.runHook = func(_ string, args []string) error {
					if containsArg(args, "-C") {
						return nil
					}
					if containsArg(args, "-D") {
						return errors.New("remove failed")
					}
					return nil
				}
			},
			setupMetrics: func(m *metrics.Metrics) {
				m.SetJumpActive(true)
			},
			expectErr:     true,
			expectedCalls: []string{"-C", "-D"},
			expectedGauge: 1,
			expectedErrors: map[string]float64{
				metricErrorLabelIptables: 1,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			exec := &mockExecutor{}
			if tc.setupExecutor != nil {
				tc.setupExecutor(exec)
			}

			metricsCollector := metrics.NewMetrics()
			if tc.setupMetrics != nil {
				tc.setupMetrics(metricsCollector)
			}

			logger, buf := newTestLogger()

			jm := &jumpManager{
				executor:     exec,
				table:        "nat",
				hook:         "OUTPUT",
				chain:        "CANARY_DNAT",
				ipv6:         false,
				activeValue:  "active",
				previewValue: "preview",
				metrics:      metricsCollector,
				logger:       logger,
			}

			err := jm.OnTransition(context.Background(), tc.previous, tc.current)

			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			exec.assertCallsContain(t, tc.expectedCalls)

			for _, marker := range tc.forbiddenArgs {
				for _, call := range exec.calls {
					if containsArg(call.Args, marker) {
						t.Fatalf("did not expect call containing %q, got args %v", marker, call.Args)
					}
				}
			}

			body := scrapeMetrics(t, metricsCollector)
			gauge, foundGauge := findMetricValue(t, body, "ghostwire_jump_active", "")
			if !foundGauge {
				t.Fatal("expected jump gauge metric to be present")
			}
			if gauge != tc.expectedGauge {
				t.Fatalf("unexpected jump gauge: got %v want %v", gauge, tc.expectedGauge)
			}

			for label, want := range tc.expectedErrors {
				got, found := findMetricValue(t, body, "ghostwire_errors_total", `type="`+label+`"`)
				if !found {
					t.Fatalf("expected error metric for %s to be present", label)
				}
				if got != want {
					t.Fatalf("unexpected error counter for %s: got %v want %v", label, got, want)
				}
			}

			for _, label := range tc.absentLabels {
				_, found := findMetricValue(t, body, "ghostwire_errors_total", `type="`+label+`"`)
				if found {
					t.Fatalf("expected no error metric for %s", label)
				}
			}

			logs := buf.String()
			for _, snippet := range tc.logSnippets {
				if snippet == "" {
					continue
				}
				if !strings.Contains(logs, snippet) {
					t.Fatalf("expected logs to contain %q, got %q", snippet, logs)
				}
			}
		})
	}
}

func TestMetricsLabelReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		delegate         *stubLabelReader
		expectErr        bool
		expectValue      string
		expectErrorCount float64
		expectHealthy    bool
	}{
		{
			name:          "successful read marks healthy",
			delegate:      &stubLabelReader{value: "preview"},
			expectErr:     false,
			expectValue:   "preview",
			expectHealthy: true,
		},
		{
			name:             "error increments metric",
			delegate:         &stubLabelReader{err: errors.New("boom")},
			expectErr:        true,
			expectValue:      "",
			expectErrorCount: 1,
			expectHealthy:    false,
		},
		{
			name:          "empty value still marks labels read",
			delegate:      &stubLabelReader{value: ""},
			expectErr:     false,
			expectValue:   "",
			expectHealthy: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			metricsCollector := metrics.NewMetrics()
			health := metrics.NewHealthChecker()
			health.SetChainVerified()

			reader := &metricsLabelReader{
				delegate: tc.delegate,
				metrics:  metricsCollector,
				health:   health,
			}

			value, err := reader.GetLabel(context.Background(), "role")

			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if value != tc.expectValue {
				t.Fatalf("unexpected value: got %q want %q", value, tc.expectValue)
			}

			if healthy := health.IsHealthy(); healthy != tc.expectHealthy {
				t.Fatalf("unexpected health status: got %t want %t", healthy, tc.expectHealthy)
			}

			body := scrapeMetrics(t, metricsCollector)
			errorCount, found := findMetricValue(t, body, "ghostwire_errors_total", `type="`+metricErrorLabelRead+`"`)
			if tc.expectErrorCount == 0 {
				if found && errorCount != 0 {
					t.Fatalf("expected no label_read errors, got %v", errorCount)
				}
				if !found {
					return
				}
			} else {
				if !found {
					t.Fatalf("expected label_read error metric to be present")
				}
				if errorCount != tc.expectErrorCount {
					t.Fatalf("unexpected label_read error count: got %v want %v", errorCount, tc.expectErrorCount)
				}
			}
		})
	}
}

type mockExecutor struct {
	mu               sync.Mutex
	calls            []execCall
	runHook          func(command string, args []string) error
	chainExistsResp  bool
	chainExistsErr   error
	chainExists6Resp bool
	chainExists6Err  error
}

type execCall struct {
	Command string
	Args    []string
}

func (m *mockExecutor) Run(_ context.Context, command string, args ...string) error {
	m.mu.Lock()
	m.calls = append(m.calls, execCall{Command: command, Args: append([]string(nil), args...)})
	hook := m.runHook
	m.mu.Unlock()
	if hook != nil {
		return hook(command, args)
	}
	return nil
}

func (m *mockExecutor) ChainExists(context.Context, string, string) (bool, error) {
	return m.chainExistsResp, m.chainExistsErr
}

func (m *mockExecutor) ChainExists6(context.Context, string, string) (bool, error) {
	return m.chainExists6Resp, m.chainExists6Err
}

func (m *mockExecutor) assertCallsContain(t *testing.T, expected []string) {
	t.Helper()
	if len(expected) == 0 {
		if len(m.calls) != 0 {
			t.Fatalf("expected no calls, got %d", len(m.calls))
		}
		return
	}

	if len(m.calls) < len(expected) {
		t.Fatalf("expected at least %d calls, got %d", len(expected), len(m.calls))
	}

	for i, marker := range expected {
		if !containsArg(m.calls[i].Args, marker) {
			t.Fatalf("expected call %d to contain marker %q, got args %v", i, marker, m.calls[i].Args)
		}
	}
}

type exitErr struct {
	code int
}

func (e *exitErr) Error() string {
	return "exit"
}

func (e *exitErr) ExitCode() int {
	return e.code
}

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), buf
}

func scrapeMetrics(t *testing.T, m *metrics.Metrics) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("unexpected status scraping metrics: %d", rec.Code)
	}
	return rec.Body.String()
}

func findMetricValue(t *testing.T, body string, metric string, labelSelector string) (float64, bool) {
	t.Helper()
	target := metric
	if labelSelector != "" {
		target = target + "{" + labelSelector + "}"
	}
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, target) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		valueStr := fields[len(fields)-1]
		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			t.Fatalf("failed to parse metric value from %q: %v", line, err)
		}
		return value, true
	}
	return 0, false
}

type stubLabelReader struct {
	value string
	err   error
}

func (s *stubLabelReader) GetLabel(context.Context, string) (string, error) {
	return s.value, s.err
}

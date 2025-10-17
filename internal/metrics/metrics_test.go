package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewMetricsRegistersInstruments(t *testing.T) {
	t.Parallel()

	m := NewMetrics()
	if m == nil {
		t.Fatal("expected metrics instance")
	}

	// Prime the counter vector so the family appears in Gather results.
	m.IncrementError("bootstrap")

	families, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	if len(families) < 3 {
		t.Fatalf("expected at least 3 metric families, got %d", len(families))
	}

	names := map[string]struct{}{}
	for _, family := range families {
		names[family.GetName()] = struct{}{}
	}

	for _, expected := range []string{"ghostwire_jump_active", "ghostwire_errors_total", "ghostwire_dnat_rules"} {
		if _, ok := names[expected]; !ok {
			t.Fatalf("expected metric %q to be registered", expected)
		}
	}
}

func TestMetricsSetJumpActive(t *testing.T) {
	t.Parallel()

	m := NewMetrics()

	m.SetJumpActive(true)
	if got := testutil.ToFloat64(m.jumpState); got != 1 {
		t.Fatalf("expected gauge to be 1, got %v", got)
	}

	m.SetJumpActive(false)
	if got := testutil.ToFloat64(m.jumpState); got != 0 {
		t.Fatalf("expected gauge to be 0, got %v", got)
	}

	m.SetJumpActive(false)
	if got := testutil.ToFloat64(m.jumpState); got != 0 {
		t.Fatalf("expected gauge to remain 0, got %v", got)
	}
}

func TestMetricsIncrementError(t *testing.T) {
	t.Parallel()

	m := NewMetrics()

	m.IncrementError("label_read")
	m.IncrementError("label_read")
	m.IncrementError("iptables")

	if got := testutil.ToFloat64(m.errorsTotal.WithLabelValues("label_read")); got != 2 {
		t.Fatalf("expected label_read counter to be 2, got %v", got)
	}

	if got := testutil.ToFloat64(m.errorsTotal.WithLabelValues("iptables")); got != 1 {
		t.Fatalf("expected iptables counter to be 1, got %v", got)
	}

	if got := testutil.ToFloat64(m.errorsTotal.WithLabelValues("chain_verify")); got != 0 {
		t.Fatalf("expected chain_verify counter to be 0, got %v", got)
	}
}

func TestMetricsSetDNATRuleCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input int
	}{
		{name: "zero", input: 0},
		{name: "positive", input: 7},
		{name: "large", input: 123},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := NewMetrics()
			m.SetDNATRuleCount(tc.input)
			if got := testutil.ToFloat64(m.dnatRules); got != float64(tc.input) {
				t.Fatalf("expected gauge to be %d, got %v", tc.input, got)
			}
		})
	}
}

func TestMetricsHandler(t *testing.T) {
	t.Parallel()

	m := NewMetrics()
	m.SetJumpActive(true)
	m.IncrementError("label_read")
	m.IncrementError("label_read")
	m.IncrementError("chain_verify")
	m.SetDNATRuleCount(5)

	handler := m.Handler()
	if handler == nil {
		t.Fatal("expected handler to be non-nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain; version=0.0.4; charset=utf-8") {
		t.Fatalf("unexpected content type: %q", contentType)
	}

	body := rec.Body.String()
	for _, snippet := range []string{
		"# HELP ghostwire_jump_active",
		"# TYPE ghostwire_jump_active gauge",
		"ghostwire_jump_active 1",
		"ghostwire_errors_total{type=\"label_read\"} 2",
		"ghostwire_errors_total{type=\"chain_verify\"} 1",
		"ghostwire_dnat_rules 5",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected metrics output to contain %q, got %q", snippet, body)
		}
	}
}

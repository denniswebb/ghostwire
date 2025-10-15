package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics bundles Prometheus instruments for the watcher.
type Metrics struct {
	registry    *prometheus.Registry
	jumpState   prometheus.Gauge
	errorsTotal *prometheus.CounterVec
	dnatRules   prometheus.Gauge
}

// NewMetrics constructs a Metrics instance with an isolated registry.
func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()

	jumpState := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "ghostwire",
		Name:      "jump_active",
		Help:      "Whether the DNAT jump rule is active (1) or inactive (0).",
	})

	errorsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "ghostwire",
		Name:      "errors_total",
		Help:      "Total number of watcher errors by type.",
	}, []string{"type"})

	dnatRules := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "ghostwire",
		Name:      "dnat_rules",
		Help:      "Number of DNAT rules discovered from the audit map.",
	})

	registry.MustRegister(jumpState, errorsTotal, dnatRules)

	return &Metrics{
		registry:    registry,
		jumpState:   jumpState,
		errorsTotal: errorsTotal,
		dnatRules:   dnatRules,
	}
}

// SetJumpActive updates the jump activation gauge.
func (m *Metrics) SetJumpActive(active bool) {
	if active {
		m.jumpState.Set(1)
		return
	}
	m.jumpState.Set(0)
}

// IncrementError increments the error counter for the provided type label.
func (m *Metrics) IncrementError(errorType string) {
	m.errorsTotal.WithLabelValues(errorType).Inc()
}

// SetDNATRuleCount records the number of DNAT rules found in the audit map.
func (m *Metrics) SetDNATRuleCount(count int) {
	m.dnatRules.Set(float64(count))
}

// Handler exposes the Prometheus scrape handler bound to the registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

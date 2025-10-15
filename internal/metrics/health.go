package metrics

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/denniswebb/ghostwire/internal/logging"
)

// HealthChecker tracks readiness signals for the watcher sidecar.
type HealthChecker struct {
	mu            sync.RWMutex
	chainVerified bool
	labelsRead    bool
	logger        *slog.Logger
}

// NewHealthChecker returns a HealthChecker with a logger derived from the shared logging package.
func NewHealthChecker() *HealthChecker {
	logger := logging.GetLogger()
	if logger == nil {
		logger = slog.Default()
	}

	return &HealthChecker{logger: logger}
}

// SetChainVerified records that the DNAT chain existence has been confirmed.
func (h *HealthChecker) SetChainVerified() {
	h.mu.Lock()
	h.chainVerified = true
	h.mu.Unlock()
}

// SetLabelsRead records that pod labels have been successfully retrieved at least once.
func (h *HealthChecker) SetLabelsRead() {
	h.mu.Lock()
	h.labelsRead = true
	h.mu.Unlock()
}

// IsHealthy reports whether both readiness signals have been satisfied.
func (h *HealthChecker) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.chainVerified && h.labelsRead
}

// Handler produces an HTTP handler for the /healthz endpoint.
func (h *HealthChecker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		chainVerified := h.chainVerified
		labelsRead := h.labelsRead
		h.mu.RUnlock()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		if chainVerified && labelsRead {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK\n"))
			return
		}

		h.logger.Warn("health check not yet passing",
			slog.Bool("chain_verified", chainVerified),
			slog.Bool("labels_read", labelsRead),
		)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Service Unavailable\n"))
	})
}

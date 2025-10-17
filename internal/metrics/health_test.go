package metrics

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNewHealthCheckerInitialState(t *testing.T) {
	t.Parallel()

	h, _ := newHealthCheckerForTest()

	if h == nil {
		t.Fatal("expected health checker instance")
	}
	if h.chainVerified {
		t.Fatal("expected chainVerified to default to false")
	}
	if h.labelsRead {
		t.Fatal("expected labelsRead to default to false")
	}
	if h.logger == nil {
		t.Fatal("expected logger to be initialized")
	}
	if h.IsHealthy() {
		t.Fatal("expected IsHealthy to return false initially")
	}
}

func TestHealthCheckerSetters(t *testing.T) {
	t.Parallel()

	h, _ := newHealthCheckerForTest()

	h.SetChainVerified()
	if !h.chainVerified {
		t.Fatal("expected chainVerified to be true after SetChainVerified")
	}
	h.SetChainVerified()
	if !h.chainVerified {
		t.Fatal("expected chainVerified to remain true after repeated SetChainVerified")
	}

	if h.IsHealthy() {
		t.Fatal("expected IsHealthy to remain false without labelsRead")
	}

	h.SetLabelsRead()
	if !h.labelsRead {
		t.Fatal("expected labelsRead to be true after SetLabelsRead")
	}

	h.SetLabelsRead()
	if !h.labelsRead {
		t.Fatal("expected labelsRead to remain true after repeated SetLabelsRead")
	}

	if !h.IsHealthy() {
		t.Fatal("expected IsHealthy to return true once both signals set")
	}
}

func TestHealthCheckerHandlerStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configure  func(h *HealthChecker)
		wantStatus int
		wantBody   string
		expectWarn bool
	}{
		{
			name:       "unhealthy",
			configure:  func(*HealthChecker) {},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "Service Unavailable\n",
			expectWarn: true,
		},
		{
			name: "chain verified only",
			configure: func(h *HealthChecker) {
				h.SetChainVerified()
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "Service Unavailable\n",
			expectWarn: true,
		},
		{
			name: "labels read only",
			configure: func(h *HealthChecker) {
				h.SetLabelsRead()
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "Service Unavailable\n",
			expectWarn: true,
		},
		{
			name: "healthy",
			configure: func(h *HealthChecker) {
				h.SetChainVerified()
				h.SetLabelsRead()
			},
			wantStatus: http.StatusOK,
			wantBody:   "OK\n",
			expectWarn: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, buf := newHealthCheckerForTest()
			tc.configure(h)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

			h.Handler().ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("unexpected status: got %d want %d", rec.Code, tc.wantStatus)
			}

			if body := rec.Body.String(); body != tc.wantBody {
				t.Fatalf("unexpected body: got %q want %q", body, tc.wantBody)
			}

			if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
				t.Fatalf("unexpected content type: %q", ct)
			}

			logs := buf.String()
			if tc.expectWarn {
				if !strings.Contains(logs, "health check not yet passing") {
					t.Fatalf("expected warning log, got %q", logs)
				}
			} else if logs != "" {
				t.Fatalf("expected no logs when healthy, got %q", logs)
			}
		})
	}
}

func TestHealthCheckerConcurrentAccess(t *testing.T) {
	t.Parallel()

	h, _ := newHealthCheckerForTest()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				h.SetChainVerified()
			} else {
				h.SetLabelsRead()
			}
			_ = h.IsHealthy()
		}(i)
	}

	wg.Wait()

	h.SetChainVerified()
	h.SetLabelsRead()
	if !h.IsHealthy() {
		t.Fatal("expected healthy state after concurrent updates")
	}
}

func newHealthCheckerForTest() (*HealthChecker, *bytes.Buffer) {
	h := NewHealthChecker()
	buf := &bytes.Buffer{}
	h.logger = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return h, buf
}

package k8s

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewPollerValidation(t *testing.T) {
	t.Parallel()

	baseConfig := PollerConfig{
		LabelKey:          "role",
		ActiveValue:       "active",
		PreviewValue:      "preview",
		PollInterval:      10 * time.Millisecond,
		TransitionHandler: &recordingTransitionHandler{},
	}

	tests := []struct {
		name        string
		mutate      func(cfg *PollerConfig)
		expectError string
	}{
		{
			name: "missing label reader",
			mutate: func(cfg *PollerConfig) {
				cfg.LabelReader = nil
			},
			expectError: "label reader is required",
		},
		{
			name: "missing label key",
			mutate: func(cfg *PollerConfig) {
				cfg.LabelKey = ""
			},
			expectError: "label key is required",
		},
		{
			name: "missing active value",
			mutate: func(cfg *PollerConfig) {
				cfg.ActiveValue = ""
			},
			expectError: "active value is required",
		},
		{
			name: "missing preview value",
			mutate: func(cfg *PollerConfig) {
				cfg.PreviewValue = ""
			},
			expectError: "preview value is required",
		},
		{
			name: "active equals preview",
			mutate: func(cfg *PollerConfig) {
				cfg.PreviewValue = cfg.ActiveValue
			},
			expectError: "active and preview values must differ",
		},
		{
			name: "non positive poll interval",
			mutate: func(cfg *PollerConfig) {
				cfg.PollInterval = 0
			},
			expectError: "poll interval must be positive",
		},
		{
			name: "nil logger tolerated",
			mutate: func(cfg *PollerConfig) {
				cfg.Logger = nil
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := baseConfig
			cfg.LabelReader = newMockLabelReader(labelResponse{value: "active"})
			cfg.TransitionHandler = &recordingTransitionHandler{}
			if cfg.Logger == nil {
				cfg.Logger, _ = newBufferLogger()
			}
			tc.mutate(&cfg)

			poller, err := NewPoller(cfg)

			if tc.expectError != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.expectError)
				}
				if !strings.Contains(err.Error(), tc.expectError) {
					t.Fatalf("expected error to contain %q, got %v", tc.expectError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if poller == nil {
				t.Fatal("expected poller instance")
			}
		})
	}
}

func TestPollerRunScenarios(t *testing.T) {
	t.Parallel()

	type expectation struct {
		transitions []transitionCall
		logContains []string
	}

	tests := []struct {
		name       string
		responses  []labelResponse
		handlerErr []error
		expect     expectation
		polls      int
	}{
		{
			name: "initial recognized role triggers handler",
			responses: []labelResponse{
				{value: "active"},
			},
			expect: expectation{
				transitions: []transitionCall{{Previous: "", Current: "active"}},
				logContains: []string{"initialized role state", "level=DEBUG"},
			},
			polls: 1,
		},
		{
			name: "no change stays silent",
			responses: []labelResponse{
				{value: "active"},
				{value: "active"},
			},
			expect: expectation{
				transitions: []transitionCall{{Previous: "", Current: "active"}},
				logContains: []string{"role state unchanged", "level=DEBUG"},
			},
			polls: 2,
		},
		{
			name: "active to preview transition",
			responses: []labelResponse{
				{value: "active"},
				{value: "preview"},
			},
			expect: expectation{
				transitions: []transitionCall{
					{Previous: "", Current: "active"},
					{Previous: "active", Current: "preview"},
				},
				logContains: []string{"role transition detected", "level=INFO"},
			},
			polls: 2,
		},
		{
			name: "preview to active transition",
			responses: []labelResponse{
				{value: "preview"},
				{value: "active"},
			},
			expect: expectation{
				transitions: []transitionCall{
					{Previous: "", Current: "preview"},
					{Previous: "preview", Current: "active"},
				},
				logContains: []string{"role transition detected", "level=INFO"},
			},
			polls: 2,
		},
		{
			name: "unrecognized role transition ignored",
			responses: []labelResponse{
				{value: "active"},
				{value: "unknown"},
			},
			expect: expectation{
				transitions: []transitionCall{{Previous: "", Current: "active"}},
				logContains: []string{"role changed without recognized transition", "level=DEBUG"},
			},
			polls: 2,
		},
		{
			name: "unrecognized to recognized does not trigger handler",
			responses: []labelResponse{
				{value: "unknown"},
				{value: "preview"},
			},
			expect: expectation{
				transitions: nil,
				logContains: []string{"role changed without recognized transition", "level=DEBUG"},
			},
			polls: 2,
		},
		{
			name: "label read error logs warning and continues",
			responses: []labelResponse{
				{value: "active"},
				{err: errors.New("boom")},
				{value: "preview"},
			},
			expect: expectation{
				transitions: []transitionCall{
					{Previous: "", Current: "active"},
					{Previous: "active", Current: "preview"},
				},
				logContains: []string{"failed to read pod label", "level=WARN"},
			},
			polls: 3,
		},
		{
			name: "transition handler error logged",
			responses: []labelResponse{
				{value: "active"},
				{value: "preview"},
			},
			handlerErr: []error{nil, errors.New("handler boom")},
			expect: expectation{
				transitions: []transitionCall{
					{Previous: "", Current: "active"},
					{Previous: "active", Current: "preview"},
				},
				logContains: []string{"transition handler failed", "level=WARN"},
			},
			polls: 2,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := newMockLabelReader(tc.responses...)
			handler := &recordingTransitionHandler{responses: tc.handlerErr}
			logger, buf := newBufferLogger()

			poller, err := NewPoller(PollerConfig{
				LabelReader:       reader,
				LabelKey:          "role",
				ActiveValue:       "active",
				PreviewValue:      "preview",
				PollInterval:      5 * time.Millisecond,
				Logger:            logger,
				TransitionHandler: handler,
			})
			if err != nil {
				t.Fatalf("unexpected error creating poller: %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				poller.Run(ctx)
				close(done)
			}()

			reader.WaitForCalls(t, tc.polls, 500*time.Millisecond)
			cancel()
			<-done

			if got := handler.Transitions(); !equalTransitions(got, tc.expect.transitions) {
				t.Fatalf("unexpected transitions: got %#v want %#v", got, tc.expect.transitions)
			}

			logs := buf.String()
			for _, snippet := range tc.expect.logContains {
				if !strings.Contains(logs, snippet) {
					t.Fatalf("expected logs to contain %q, got %q", snippet, logs)
				}
			}
		})
	}
}

func TestPollerStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	reader := newMockLabelReader(labelResponse{value: "active"}, labelResponse{value: "active"}, labelResponse{value: "active"})
	handler := &recordingTransitionHandler{}
	logger, buf := newBufferLogger()

	poller, err := NewPoller(PollerConfig{
		LabelReader:       reader,
		LabelKey:          "role",
		ActiveValue:       "active",
		PreviewValue:      "preview",
		PollInterval:      5 * time.Millisecond,
		Logger:            logger,
		TransitionHandler: handler,
	})
	if err != nil {
		t.Fatalf("unexpected error creating poller: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	reader.WaitForCalls(t, 1, 200*time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("poller did not stop after context cancellation")
	}

	if !strings.Contains(buf.String(), "stopping label poller") {
		t.Fatalf("expected stop log, got %q", buf.String())
	}
}

func TestPollerGetCurrentRole(t *testing.T) {
	t.Parallel()

	reader := newMockLabelReader(labelResponse{value: "preview"}, labelResponse{value: "preview"})
	handler := &recordingTransitionHandler{}
	logger, _ := newBufferLogger()

	poller, err := NewPoller(PollerConfig{
		LabelReader:       reader,
		LabelKey:          "role",
		ActiveValue:       "active",
		PreviewValue:      "preview",
		PollInterval:      5 * time.Millisecond,
		Logger:            logger,
		TransitionHandler: handler,
	})
	if err != nil {
		t.Fatalf("unexpected error creating poller: %v", err)
	}

	if got := poller.GetCurrentRole(); got != "" {
		t.Fatalf("expected empty role before polling, got %q", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	reader.WaitForCalls(t, 1, 200*time.Millisecond)
	if got := poller.GetCurrentRole(); got != "preview" {
		t.Fatalf("expected role to be preview, got %q", got)
	}

	cancel()
	<-done
}

type labelResponse struct {
	value string
	err   error
}

type mockLabelReader struct {
	mu        sync.Mutex
	responses []labelResponse
	calls     int
	callCh    chan struct{}
}

func newMockLabelReader(responses ...labelResponse) *mockLabelReader {
	return &mockLabelReader{
		responses: responses,
		callCh:    make(chan struct{}, len(responses)+4),
	}
}

func (m *mockLabelReader) GetLabel(ctx context.Context, labelKey string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index := m.calls
	if index >= len(m.responses) {
		if len(m.responses) == 0 {
			m.calls++
			m.callCh <- struct{}{}
			return "", nil
		}
		index = len(m.responses) - 1
	}

	resp := m.responses[index]
	m.calls++
	m.callCh <- struct{}{}
	return resp.value, resp.err
}

func (m *mockLabelReader) WaitForCalls(t *testing.T, count int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for i := 0; i < count; i++ {
		select {
		case <-m.callCh:
		case <-deadline:
			t.Fatalf("timed out waiting for %d label reads", count)
		}
	}
}

type transitionCall struct {
	Previous string
	Current  string
}

type recordingTransitionHandler struct {
	mu        sync.Mutex
	calls     []transitionCall
	responses []error
}

func (h *recordingTransitionHandler) OnTransition(ctx context.Context, previous string, current string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.calls = append(h.calls, transitionCall{Previous: previous, Current: current})
	if len(h.responses) == 0 {
		return nil
	}

	err := h.responses[0]
	h.responses = h.responses[1:]
	return err
}

func (h *recordingTransitionHandler) Transitions() []transitionCall {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]transitionCall, len(h.calls))
	copy(out, h.calls)
	return out
}

func equalTransitions(a, b []transitionCall) bool {
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

func newBufferLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), buf
}

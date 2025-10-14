package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// LabelReader abstracts pod label retrieval for polling logic.
type LabelReader interface {
	GetLabel(ctx context.Context, labelKey string) (string, error)
}

// PollerConfig holds the dependencies and settings for the Poller.
type PollerConfig struct {
	LabelReader  LabelReader
	LabelKey     string
	ActiveValue  string
	PreviewValue string
	PollInterval time.Duration
	Logger       *slog.Logger
}

// Poller periodically checks a pod label and records role transitions.
type Poller struct {
	cfg          PollerConfig
	logger       *slog.Logger
	mu           sync.RWMutex
	lastRole     string
	observedRole bool
}

// NewPoller validates the configuration and returns a Poller ready to run.
func NewPoller(cfg PollerConfig) (*Poller, error) {
	if cfg.LabelReader == nil {
		return nil, fmt.Errorf("label reader is required")
	}
	if cfg.LabelKey == "" {
		return nil, fmt.Errorf("label key is required")
	}
	if cfg.ActiveValue == "" {
		return nil, fmt.Errorf("active value is required")
	}
	if cfg.PreviewValue == "" {
		return nil, fmt.Errorf("preview value is required")
	}
	if cfg.ActiveValue == cfg.PreviewValue {
		return nil, fmt.Errorf("active and preview values must differ")
	}
	if cfg.PollInterval <= 0 {
		return nil, fmt.Errorf("poll interval must be positive")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Poller{
		cfg:    cfg,
		logger: logger,
	}, nil
}

// Run executes the polling loop until the context is canceled.
func (p *Poller) Run(ctx context.Context) {
	p.logger.Info("starting label poller",
		slog.String("label_key", p.cfg.LabelKey),
		slog.String("poll_interval", p.cfg.PollInterval.String()),
	)

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer func() {
		ticker.Stop()
		p.logger.Info("stopping label poller",
			slog.String("label_key", p.cfg.LabelKey),
		)
	}()

	// Perform an initial check immediately so we capture the starting state.
	p.pollOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

// GetCurrentRole returns the last role value observed by the poller.
func (p *Poller) GetCurrentRole() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastRole
}

func (p *Poller) pollOnce(ctx context.Context) {
	labelValue, err := p.cfg.LabelReader.GetLabel(ctx, p.cfg.LabelKey)
	if err != nil {
		p.logger.Warn("failed to read pod label",
			slog.String("label_key", p.cfg.LabelKey),
			slog.Any("error", err),
		)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	previousValue := p.lastRole
	currentRecognized := p.isRecognizedRole(labelValue)
	previousRecognized := p.isRecognizedRole(previousValue)

	if !p.observedRole {
		p.lastRole = labelValue
		p.observedRole = true
		p.logger.Debug("initialized role state",
			slog.String("current_role", labelValue),
			slog.String("label_key", p.cfg.LabelKey),
			slog.Bool("recognized_role", currentRecognized),
		)
		return
	}

	if previousValue == labelValue {
		p.logger.Debug("role state unchanged",
			slog.String("current_role", labelValue),
			slog.String("label_key", p.cfg.LabelKey),
		)
		return
	}

	p.lastRole = labelValue

	if previousRecognized && currentRecognized {
		p.logger.Info("role transition detected",
			slog.String("previous_role", previousValue),
			slog.String("current_role", labelValue),
			slog.String("label_key", p.cfg.LabelKey),
		)
		return
	}

	p.logger.Debug("role changed without recognized transition",
		slog.String("previous_role", previousValue),
		slog.Bool("previous_recognized", previousRecognized),
		slog.String("current_role", labelValue),
		slog.Bool("current_recognized", currentRecognized),
		slog.String("label_key", p.cfg.LabelKey),
	)
}

func (p *Poller) isRecognizedRole(role string) bool {
	return role == p.cfg.ActiveValue || role == p.cfg.PreviewValue
}

package readiness

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"
)

type State string

const (
	StateDisabled State = "disabled"
	StatePending  State = "pending"
	StateReady    State = "ready"
	StateFailed   State = "failed"
)

type StaticConfig struct {
	Enabled bool
	APIKey  string
	BaseURL string
	Model   string
}

type Snapshot struct {
	Enabled   bool      `json:"enabled"`
	State     State     `json:"state"`
	LastError string    `json:"lastError,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitempty"`
}

type Pinger interface {
	Probe(ctx context.Context) error
}

type Checker struct {
	config     StaticConfig
	timeout    time.Duration
	retryCount int
	retryDelay time.Duration
	pinger     Pinger

	mu       sync.RWMutex
	snapshot Snapshot
}

func NewChecker(config StaticConfig, pinger Pinger, timeout time.Duration, retryCount int, retryDelay time.Duration) *Checker {
	state := StatePending
	if !config.Enabled {
		state = StateDisabled
	}
	return &Checker{
		config:     config,
		timeout:    timeout,
		retryCount: retryCount,
		retryDelay: retryDelay,
		pinger:     pinger,
		snapshot: Snapshot{
			Enabled: config.Enabled,
			State:   state,
		},
	}
}

func (c *Checker) Start(ctx context.Context) {
	go c.Run(ctx)
}

func (c *Checker) Run(ctx context.Context) {
	if !c.config.Enabled {
		c.setSnapshot(StateDisabled, "", time.Now().UTC())
		return
	}

	if err := c.validateStatic(); err != nil {
		c.setSnapshot(StateFailed, err.Error(), time.Now().UTC())
		return
	}

	c.setSnapshot(StatePending, "", time.Now().UTC())
	attemptLimit := c.retryCount + 1
	var lastErr error
	for attempt := 1; attempt <= attemptLimit; attempt++ {
		probeCtx, cancel := context.WithTimeout(ctx, c.timeout)
		err := c.pinger.Probe(probeCtx)
		cancel()
		if err == nil {
			c.setSnapshot(StateReady, "", time.Now().UTC())
			return
		}
		lastErr = err
		if attempt < attemptLimit {
			select {
			case <-ctx.Done():
				c.setSnapshot(StateFailed, ctx.Err().Error(), time.Now().UTC())
				return
			case <-time.After(c.retryDelay):
			}
		}
	}

	c.setSnapshot(StateFailed, lastErr.Error(), time.Now().UTC())
}

func (c *Checker) IsReady() bool {
	return c.Snapshot().State == StateReady
}

func (c *Checker) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *Checker) validateStatic() error {
	if c.config.APIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY 不能为空")
	}
	if c.config.BaseURL == "" {
		return fmt.Errorf("OPENAI_BASE_URL 不能为空")
	}
	if _, err := url.ParseRequestURI(c.config.BaseURL); err != nil {
		return fmt.Errorf("OPENAI_BASE_URL 不是有效 URL: %w", err)
	}
	if c.config.Model == "" {
		return fmt.Errorf("OPENAI_MODEL 不能为空")
	}
	if c.pinger == nil {
		return fmt.Errorf("AI 探活器未配置")
	}
	return nil
}

func (c *Checker) setSnapshot(state State, lastError string, checkedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot = Snapshot{
		Enabled:   c.config.Enabled,
		State:     state,
		LastError: lastError,
		CheckedAt: checkedAt,
	}
}

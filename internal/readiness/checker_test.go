package readiness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckerDisabled(t *testing.T) {
	t.Parallel()

	checker := NewChecker(StaticConfig{Enabled: false}, nil, time.Second, 0, 0)
	checker.Run(context.Background())
	if checker.Snapshot().State != StateDisabled {
		t.Fatalf("Snapshot().State = %q, want disabled", checker.Snapshot().State)
	}
}

func TestCheckerFailsStaticValidation(t *testing.T) {
	t.Parallel()

	checker := NewChecker(StaticConfig{Enabled: true, BaseURL: "https://example.com/v1", Model: "gpt-4o"}, stubPinger{}, time.Second, 0, 0)
	checker.Run(context.Background())
	snapshot := checker.Snapshot()
	if snapshot.State != StateFailed {
		t.Fatalf("Snapshot().State = %q, want failed", snapshot.State)
	}
	if !strings.Contains(snapshot.LastError, "OPENAI_API_KEY") {
		t.Fatalf("Snapshot().LastError = %q", snapshot.LastError)
	}
}

func TestCheckerBecomesReadyAfterSuccessfulProbe(t *testing.T) {
	t.Parallel()

	checker := NewChecker(StaticConfig{Enabled: true, APIKey: "k", BaseURL: "https://example.com/v1", Model: "gpt-4o"}, stubPinger{}, time.Second, 1, 0)
	checker.Run(context.Background())
	if snapshot := checker.Snapshot(); snapshot.State != StateReady {
		t.Fatalf("Snapshot().State = %q, want ready", snapshot.State)
	}
}

func TestCheckerFailsAfterRetries(t *testing.T) {
	t.Parallel()

	checker := NewChecker(StaticConfig{Enabled: true, APIKey: "k", BaseURL: "https://example.com/v1", Model: "gpt-4o"}, stubPinger{err: errors.New("boom")}, time.Second, 1, 0)
	checker.Run(context.Background())
	snapshot := checker.Snapshot()
	if snapshot.State != StateFailed {
		t.Fatalf("Snapshot().State = %q, want failed", snapshot.State)
	}
	if snapshot.LastError != "boom" {
		t.Fatalf("Snapshot().LastError = %q, want boom", snapshot.LastError)
	}
}

type stubPinger struct {
	err error
}

func (s stubPinger) Probe(ctx context.Context) error {
	return s.err
}

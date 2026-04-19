package service

import (
	"errors"
	"testing"
	"time"
)

func TestGenerationServiceUsesTodayInLocationWhenDateEmpty(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC+8", 8*60*60)
	service := NewGenerationService(location, staticGenerator{})
	result, err := service.Generate("")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.Date == "" {
		t.Fatal("Generate().Date is empty")
	}
}

func TestGenerationServiceMapsLegacyResultToGeneratedOrSkipped(t *testing.T) {
	t.Parallel()

	location := time.UTC
	tests := []struct {
		name    string
		backend Generator
		status  string
		wantErr bool
	}{
		{
			name:    "generated",
			backend: staticGenerator{result: GenerateExecutionResult{Generated: true, Attempts: 1, FilePath: "data/2026-04-18.md"}},
			status:  "generated",
		},
		{
			name:    "skipped",
			backend: staticGenerator{result: GenerateExecutionResult{Generated: false, Attempts: 1, FilePath: "data/2026-04-18.md"}},
			status:  "skipped",
		},
		{
			name:    "failed",
			backend: staticGenerator{result: GenerateExecutionResult{Attempts: 3}, err: errors.New("boom")},
			status:  "failed",
			wantErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewGenerationService(location, testCase.backend)
			result, err := service.Generate("2026-04-18")
			if testCase.wantErr && err == nil {
				t.Fatal("Generate() error = nil, want non-nil")
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if result.Status != testCase.status {
				t.Fatalf("Generate().Status = %q, want %q", result.Status, testCase.status)
			}
		})
	}
}

type staticGenerator struct {
	result GenerateExecutionResult
	err    error
}

func (s staticGenerator) Generate(date string) (GenerateExecutionResult, error) {
	if s.result.Date == "" {
		s.result.Date = date
	}
	return s.result, s.err
}

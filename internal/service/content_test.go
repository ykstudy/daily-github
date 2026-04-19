package service

import (
	"errors"
	"testing"

	"daily-github/internal/storage"
)

func TestContentServiceRejectsInvalidDateBeforeStorageAccess(t *testing.T) {
	t.Parallel()

	store := &trackingContentStore{}
	service := NewContentService(store)
	_, err := service.GetDailyContent("2026/04/18")
	if err == nil {
		t.Fatal("GetDailyContent() error = nil, want non-nil")
	}
	if store.called {
		t.Fatal("ReadDaily() was called for invalid date")
	}
}

func TestContentServiceReturnsMarkdownPayloadForExistingDate(t *testing.T) {
	t.Parallel()

	store := &trackingContentStore{
		result: storage.DailyContent{Date: "2026-04-18", Exists: true, Content: "# content", FilePath: "data/2026-04-18.md"},
	}
	service := NewContentService(store)
	result, err := service.GetDailyContent("2026-04-18")
	if err != nil {
		t.Fatalf("GetDailyContent() error = %v", err)
	}
	if !result.Exists {
		t.Fatal("GetDailyContent().Exists = false, want true")
	}
	if result.ContentType != "text/markdown" {
		t.Fatalf("GetDailyContent().ContentType = %q", result.ContentType)
	}
	if result.Content != "# content" {
		t.Fatalf("GetDailyContent().Content = %q", result.Content)
	}
}

type trackingContentStore struct {
	called bool
	result storage.DailyContent
	err    error
}

func (s *trackingContentStore) ReadDaily(date string) (storage.DailyContent, error) {
	s.called = true
	if s.err != nil {
		return storage.DailyContent{}, s.err
	}
	return s.result, nil
}

var _ = errors.New

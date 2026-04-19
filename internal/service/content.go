package service

import (
	"fmt"
	"time"

	"daily-github/internal/storage"
)

type ContentStore interface {
	ReadDaily(date string) (storage.DailyContent, error)
}

type ContentResult struct {
	Date        string `json:"date"`
	Exists      bool   `json:"exists"`
	Content     string `json:"content,omitempty"`
	ContentType string `json:"contentType"`
	FilePath    string `json:"filePath,omitempty"`
}

type ContentService struct {
	store ContentStore
}

func NewContentService(store ContentStore) *ContentService {
	return &ContentService{store: store}
}

func (s *ContentService) GetDailyContent(rawDate string) (ContentResult, error) {
	date, err := ValidateDate(rawDate)
	if err != nil {
		return ContentResult{}, err
	}

	content, err := s.store.ReadDaily(date)
	if err != nil {
		return ContentResult{}, err
	}

	return ContentResult{
		Date:        date,
		Exists:      content.Exists,
		Content:     content.Content,
		ContentType: "text/markdown",
		FilePath:    content.FilePath,
	}, nil
}

func ValidateDate(rawDate string) (string, error) {
	parsed, err := time.Parse("2006-01-02", rawDate)
	if err != nil {
		return "", fmt.Errorf("日期格式无效，需为 YYYY-MM-DD")
	}
	return parsed.Format("2006-01-02"), nil
}

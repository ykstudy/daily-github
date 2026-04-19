package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var dateFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)

type DailyContent struct {
	Date     string
	Exists   bool
	Content  string
	FilePath string
}

type MarkdownStore struct {
	DataDir string
}

func NewMarkdownStore(dataDir string) *MarkdownStore {
	return &MarkdownStore{DataDir: dataDir}
}

func (s *MarkdownStore) EnsureDataDir() error {
	if err := os.MkdirAll(s.DataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	return nil
}

func (s *MarkdownStore) PathForDate(date string) string {
	return filepath.Join(s.DataDir, date+".md")
}

func (s *MarkdownStore) ListDates() ([]string, error) {
	entries, err := os.ReadDir(s.DataDir)
	if err != nil {
		return nil, fmt.Errorf("读取数据目录失败: %w", err)
	}

	var dates []string
	for _, entry := range entries {
		if entry.IsDir() || !dateFilePattern.MatchString(entry.Name()) {
			continue
		}
		date := strings.TrimSuffix(entry.Name(), ".md")
		if _, err := time.Parse("2006-01-02", date); err != nil {
			continue
		}
		dates = append(dates, date)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates, nil
}

func (s *MarkdownStore) ReadDaily(date string) (DailyContent, error) {
	filePath := s.PathForDate(date)
	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return DailyContent{Date: date, FilePath: filePath, Exists: false}, nil
		}
		return DailyContent{}, fmt.Errorf("读取文件失败: %w", err)
	}

	return DailyContent{
		Date:     date,
		Exists:   true,
		Content:  string(content),
		FilePath: filePath,
	}, nil
}

func (s *MarkdownStore) WriteDaily(date string, content string) (string, error) {
	if err := s.EnsureDataDir(); err != nil {
		return "", err
	}

	filePath := s.PathForDate(date)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return filePath, nil
}

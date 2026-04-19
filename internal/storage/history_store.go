package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var repoURLPattern = regexp.MustCompile(`https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)`)

type RecommendationHistory struct {
	ProjectsByDate map[string][]string `json:"projectsByDate"`
}

type HistoryStore struct {
	DataDir       string
	MarkdownStore *MarkdownStore
}

func NewHistoryStore(dataDir string, markdownStore *MarkdownStore) *HistoryStore {
	if markdownStore == nil {
		markdownStore = NewMarkdownStore(dataDir)
	}
	return &HistoryStore{DataDir: dataDir, MarkdownStore: markdownStore}
}

func (s *HistoryStore) Path() string {
	return filepath.Join(s.DataDir, "recommendation-history.json")
}

func ExtractRecommendedRepos(markdown string) []string {
	matches := repoURLPattern.FindAllStringSubmatch(markdown, -1)
	unique := make(map[string]struct{})
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		unique[match[1]] = struct{}{}
	}

	repositories := make([]string, 0, len(unique))
	for repo := range unique {
		repositories = append(repositories, repo)
	}
	sort.Strings(repositories)
	return repositories
}

func ListHistoricalRepos(history RecommendationHistory) []string {
	unique := make(map[string]struct{})
	for _, repositories := range history.ProjectsByDate {
		for _, repo := range repositories {
			unique[repo] = struct{}{}
		}
	}

	result := make([]string, 0, len(unique))
	for repo := range unique {
		result = append(result, repo)
	}
	sort.Strings(result)
	return result
}

func (s *HistoryStore) Load() (RecommendationHistory, error) {
	history := RecommendationHistory{ProjectsByDate: make(map[string][]string)}
	content, err := os.ReadFile(s.Path())
	if err != nil {
		if !os.IsNotExist(err) {
			return history, fmt.Errorf("读取推荐历史失败: %w", err)
		}
	} else if len(content) > 0 {
		if err := json.Unmarshal(content, &history); err != nil {
			return history, fmt.Errorf("解析推荐历史失败: %w", err)
		}
		if history.ProjectsByDate == nil {
			history.ProjectsByDate = make(map[string][]string)
		}
	}

	changed, err := s.syncFromMarkdown(&history)
	if err != nil {
		return history, err
	}
	if changed {
		if err := s.Save(history); err != nil {
			return history, err
		}
	}

	return history, nil
}

func (s *HistoryStore) Save(history RecommendationHistory) error {
	if history.ProjectsByDate == nil {
		history.ProjectsByDate = make(map[string][]string)
	}
	content, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化推荐历史失败: %w", err)
	}
	if err := os.WriteFile(s.Path(), content, 0644); err != nil {
		return fmt.Errorf("写入推荐历史失败: %w", err)
	}
	return nil
}

func (s *HistoryStore) RecordDaily(date string, markdown string) error {
	repositories := ExtractRecommendedRepos(markdown)
	if len(repositories) == 0 {
		return fmt.Errorf("未能从生成结果中提取到任何 GitHub 仓库")
	}

	history, err := s.Load()
	if err != nil {
		return err
	}
	history.ProjectsByDate[date] = repositories
	return s.Save(history)
}

func (s *HistoryStore) syncFromMarkdown(history *RecommendationHistory) (bool, error) {
	dates, err := s.MarkdownStore.ListDates()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	changed := false
	for _, date := range dates {
		if existing := history.ProjectsByDate[date]; len(existing) > 0 {
			continue
		}
		content, err := s.MarkdownStore.ReadDaily(date)
		if err != nil {
			return changed, fmt.Errorf("读取历史 markdown 失败: %w", err)
		}
		if !content.Exists {
			continue
		}
		repositories := ExtractRecommendedRepos(content.Content)
		if len(repositories) == 0 {
			continue
		}
		history.ProjectsByDate[date] = repositories
		changed = true
	}

	return changed, nil
}

package service

import (
	"fmt"
	"time"
)

type Generator interface {
	Generate(date string) (GenerateExecutionResult, error)
}

type GenerateExecutionResult struct {
	Date      string
	FilePath  string
	Generated bool
	Attempts  int
}

type GenerationResult struct {
	Date      string `json:"date"`
	Status    string `json:"status"`
	Generated bool   `json:"generated"`
	Attempts  int    `json:"attempts"`
	Message   string `json:"message,omitempty"`
	FilePath  string `json:"filePath,omitempty"`
}

type GenerationService struct {
	location  *time.Location
	generator Generator
}

func NewGenerationService(location *time.Location, generator Generator) *GenerationService {
	return &GenerationService{location: location, generator: generator}
}

func (s *GenerationService) Generate(rawDate string) (GenerationResult, error) {
	date, err := ResolveDate(rawDate, s.location)
	if err != nil {
		return GenerationResult{}, err
	}

	execution, err := s.generator.Generate(date)
	result := GenerationResult{
		Date:     date,
		Attempts: execution.Attempts,
		FilePath: execution.FilePath,
	}
	if execution.Date != "" {
		result.Date = execution.Date
	}
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		return result, err
	}

	result.Generated = execution.Generated
	if execution.Generated {
		result.Status = "generated"
		result.Message = "generated"
	} else {
		result.Status = "skipped"
		result.Message = "skipped"
	}

	return result, nil
}

func ResolveDate(rawDate string, location *time.Location) (string, error) {
	trimmed := rawDate
	if trimmed == "" {
		return time.Now().In(location).Format("2006-01-02"), nil
	}
	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return "", fmt.Errorf("日期格式无效，需为 YYYY-MM-DD")
	}
	return parsed.Format("2006-01-02"), nil
}

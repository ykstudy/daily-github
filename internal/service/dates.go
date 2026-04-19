package service

type DateStore interface {
	ListDates() ([]string, error)
}

type DateListResult struct {
	Dates  []string `json:"dates"`
	Count  int      `json:"count"`
	Latest string   `json:"latest,omitempty"`
}

type DateService struct {
	store DateStore
}

func NewDateService(store DateStore) *DateService {
	return &DateService{store: store}
}

func (s *DateService) ListDates(prefix string) (DateListResult, error) {
	dates, err := s.store.ListDates()
	if err != nil {
		return DateListResult{}, err
	}

	filtered := make([]string, 0, len(dates))
	for _, date := range dates {
		if prefix == "" || len(date) >= len(prefix) && date[:len(prefix)] == prefix {
			filtered = append(filtered, date)
		}
	}

	result := DateListResult{Dates: filtered, Count: len(filtered)}
	if len(filtered) > 0 {
		result.Latest = filtered[0]
	}
	return result, nil
}

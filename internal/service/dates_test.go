package service

import (
	"reflect"
	"testing"
)

func TestDateServiceListDatesAppliesOptionalPrefix(t *testing.T) {
	t.Parallel()

	service := NewDateService(staticDateStore{dates: []string{"2026-04-18", "2026-04-17", "2026-03-01"}})
	result, err := service.ListDates("2026-04")
	if err != nil {
		t.Fatalf("ListDates() error = %v", err)
	}

	want := []string{"2026-04-18", "2026-04-17"}
	if !reflect.DeepEqual(result.Dates, want) {
		t.Fatalf("ListDates().Dates = %#v, want %#v", result.Dates, want)
	}
	if result.Count != 2 {
		t.Fatalf("ListDates().Count = %d, want 2", result.Count)
	}
	if result.Latest != "2026-04-18" {
		t.Fatalf("ListDates().Latest = %q, want 2026-04-18", result.Latest)
	}
}

type staticDateStore struct {
	dates []string
}

func (s staticDateStore) ListDates() ([]string, error) {
	return s.dates, nil
}

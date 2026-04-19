package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMarkdownStoreListDatesReturnsDescDatesOnly(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	files := map[string]string{
		"2026-04-18.md":               "a",
		"2026-04-17.md":               "b",
		"2026-04-18.md.bak1":          "ignored",
		"recommendation-history.json": "{}",
		"notes.txt":                   "ignored",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dataDir, "nested"), 0755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	store := NewMarkdownStore(dataDir)
	dates, err := store.ListDates()
	if err != nil {
		t.Fatalf("ListDates() error = %v", err)
	}

	want := []string{"2026-04-18", "2026-04-17"}
	if !reflect.DeepEqual(dates, want) {
		t.Fatalf("ListDates() = %#v, want %#v", dates, want)
	}
}

func TestMarkdownStoreReadDailyReturnsExistsFalseWhenFileMissing(t *testing.T) {
	t.Parallel()

	store := NewMarkdownStore(t.TempDir())
	result, err := store.ReadDaily("2026-04-18")
	if err != nil {
		t.Fatalf("ReadDaily() error = %v", err)
	}
	if result.Exists {
		t.Fatal("ReadDaily().Exists = true, want false")
	}
	if result.Content != "" {
		t.Fatalf("ReadDaily().Content = %q, want empty", result.Content)
	}
}

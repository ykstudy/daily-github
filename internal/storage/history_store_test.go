package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHistoryStoreLoadBackfillsFromMarkdown(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := NewMarkdownStore(dataDir)
	if _, err := store.WriteDaily("2026-04-18", "[repo](https://github.com/owner1/repo1)\n[repo](https://github.com/owner2/repo2)"); err != nil {
		t.Fatalf("WriteDaily() error = %v", err)
	}
	if _, err := store.WriteDaily("2026-04-17", "[repo](https://github.com/owner1/repo1)"); err != nil {
		t.Fatalf("WriteDaily() error = %v", err)
	}

	historyStore := NewHistoryStore(dataDir, store)
	history, err := historyStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want18 := []string{"owner1/repo1", "owner2/repo2"}
	if !reflect.DeepEqual(history.ProjectsByDate["2026-04-18"], want18) {
		t.Fatalf("history[2026-04-18] = %#v, want %#v", history.ProjectsByDate["2026-04-18"], want18)
	}
	wantAll := []string{"owner1/repo1", "owner2/repo2"}
	if got := ListHistoricalRepos(history); !reflect.DeepEqual(got, wantAll) {
		t.Fatalf("ListHistoricalRepos() = %#v, want %#v", got, wantAll)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "recommendation-history.json")); err != nil {
		t.Fatalf("history file not persisted: %v", err)
	}
}

func TestHistoryStoreLoadBackfillsIncompleteHistory(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := NewMarkdownStore(dataDir)
	if _, err := store.WriteDaily("2026-04-18", "[repo](https://github.com/owner1/repo1)"); err != nil {
		t.Fatalf("WriteDaily() error = %v", err)
	}
	if _, err := store.WriteDaily("2026-04-17", "[repo](https://github.com/owner2/repo2)"); err != nil {
		t.Fatalf("WriteDaily() error = %v", err)
	}

	historyFile := filepath.Join(dataDir, "recommendation-history.json")
	if err := os.WriteFile(historyFile, []byte(`{"projectsByDate":{"2026-04-18":["owner1/repo1"]}}`), 0644); err != nil {
		t.Fatalf("WriteFile(history) error = %v", err)
	}

	history, err := NewHistoryStore(dataDir, store).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := history.ProjectsByDate["2026-04-17"]; !reflect.DeepEqual(got, []string{"owner2/repo2"}) {
		t.Fatalf("history[2026-04-17] = %#v, want owner2/repo2", got)
	}
}

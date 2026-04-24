package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daily-github/internal/config"
	"daily-github/internal/service"
	"daily-github/internal/storage"
)

func TestNewIndexHandlerServesInjectedFileAtRoot(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "index.html")
	const marker = "daily github home"
	if err := os.WriteFile(indexPath, []byte("<html><body>"+marker+"</body></html>"), 0644); err != nil {
		t.Fatalf("WriteFile(index) error = %v", err)
	}

	handler := newIndexHandler(indexPath)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	handler(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if body := recorder.Body.String(); !strings.Contains(body, marker) {
		t.Fatalf("body = %q, want marker %q", body, marker)
	}
}

func TestNewIndexHandlerRejectsNonRootPath(t *testing.T) {
	handler := newIndexHandler(filepath.Join(t.TempDir(), "index.html"))
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	recorder := httptest.NewRecorder()

	handler(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestWithPathPrefixSupportsPrefixedAndLegacyRoutes(t *testing.T) {
	t.Parallel()

	inner := http.NewServeMux()
	inner.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "index")
	})
	inner.HandleFunc("/api/content/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.TrimPrefix(r.URL.Path, "/api/content/"))
	})

	handler := withPathPrefix(inner, webPathPrefix)

	tests := []struct {
		name       string
		path       string
		statusCode int
		body       string
	}{
		{name: "legacy root", path: "/", statusCode: http.StatusOK, body: "index"},
		{name: "prefixed root", path: webPathPrefix + "/", statusCode: http.StatusOK, body: "index"},
		{name: "prefixed content route", path: webPathPrefix + "/api/content/2026-04-22", statusCode: http.StatusOK, body: "2026-04-22"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != testCase.statusCode {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.statusCode)
			}
			if body := recorder.Body.String(); body != testCase.body {
				t.Fatalf("body = %q, want %q", body, testCase.body)
			}
		})
	}
}

func TestWithPathPrefixRedirectsBarePrefixToTrailingSlash(t *testing.T) {
	t.Parallel()

	handler := withPathPrefix(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), webPathPrefix)

	req := httptest.NewRequest(http.MethodGet, webPathPrefix, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusPermanentRedirect)
	}
	if location := recorder.Header().Get("Location"); location != webPathPrefix+"/" {
		t.Fatalf("Location = %q, want %q", location, webPathPrefix+"/")
	}
}

func TestResolveIndexPathPrefersWorkingDirectoryIndex(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("Chdir(restore) error = %v", chdirErr)
		}
	})

	workingDir := t.TempDir()
	indexPath := filepath.Join(workingDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html></html>"), 0644); err != nil {
		t.Fatalf("WriteFile(index) error = %v", err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("Chdir(%s) error = %v", workingDir, err)
	}

	resolved := resolveIndexPath()
	resolvedRealPath, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		t.Fatalf("EvalSymlinks(resolved) error = %v", err)
	}
	indexRealPath, err := filepath.EvalSymlinks(indexPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(indexPath) error = %v", err)
	}
	if resolvedRealPath != indexRealPath {
		t.Fatalf("resolveIndexPath() = %q, want %q", resolvedRealPath, indexRealPath)
	}
}

func TestNewTodayStatusHandlerReportsMissingTodayAndTokenRequirement(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC+8", 8*60*60)
	expectedDate := time.Now().In(location).Format("2006-01-02")
	store := &stubContentStore{
		result: storage.DailyContent{Date: expectedDate, Exists: false, FilePath: filepath.Join("data", expectedDate+".md")},
	}
	handler := newTodayStatusHandler(config.Config{
		Location:           location,
		ManualTriggerToken: "secret",
	}, service.NewContentService(store))

	req := httptest.NewRequest(http.MethodGet, "/api/today-status", nil)
	recorder := httptest.NewRecorder()

	handler(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if store.calledWith != expectedDate {
		t.Fatalf("ReadDaily() called with %q, want %q", store.calledWith, expectedDate)
	}

	var response todayStatusResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode(response) error = %v", err)
	}
	if response.Date != expectedDate {
		t.Fatalf("Date = %q, want %q", response.Date, expectedDate)
	}
	if response.Exists {
		t.Fatal("Exists = true, want false")
	}
	if !response.GenerationEnabled {
		t.Fatal("GenerationEnabled = false, want true")
	}
	if !response.TokenRequired {
		t.Fatal("TokenRequired = false, want true")
	}
}

func TestNewTodayStatusHandlerRejectsNonGetRequests(t *testing.T) {
	t.Parallel()

	handler := newTodayStatusHandler(config.Config{Location: time.UTC}, service.NewContentService(&stubContentStore{}))
	req := httptest.NewRequest(http.MethodPost, "/api/today-status", nil)
	recorder := httptest.NewRecorder()

	handler(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

type stubContentStore struct {
	calledWith string
	result     storage.DailyContent
	err        error
}

func (s *stubContentStore) ReadDaily(date string) (storage.DailyContent, error) {
	s.calledWith = date
	if s.err != nil {
		return storage.DailyContent{}, s.err
	}
	return s.result, nil
}

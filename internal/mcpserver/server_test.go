package mcpserver

import (
	"strings"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"daily-github/internal/config"
	"daily-github/internal/readiness"
	"daily-github/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewMuxUnauthorizedAndReadiness(t *testing.T) {
	t.Parallel()

	readyChecker := readiness.NewChecker(readiness.StaticConfig{Enabled: true, APIKey: "k", BaseURL: "https://example.com/v1", Model: "gpt-4o"}, readyStub{}, time.Second, 0, 0)
	readyChecker.Run(context.Background())

	server := httptest.NewServer(NewMux(config.MCPConfig{Path: "/mcp", BearerToken: "secret"}, Dependencies{
		Dates:      fakeDatesService{},
		Content:    fakeContentService{},
		Generation: fakeGenerationService{},
		Checker:    readyChecker,
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	unreadyChecker := readiness.NewChecker(readiness.StaticConfig{Enabled: true, APIKey: "k", BaseURL: "https://example.com/v1", Model: "gpt-4o"}, failingStub{}, time.Second, 0, 0)
	unreadyChecker.Run(context.Background())
	unreadyServer := httptest.NewServer(NewMux(config.MCPConfig{Path: "/mcp", BearerToken: "secret"}, Dependencies{
		Dates:      fakeDatesService{},
		Content:    fakeContentService{},
		Generation: fakeGenerationService{},
		Checker:    unreadyChecker,
	}))
	defer unreadyServer.Close()

	req, err := http.NewRequest(http.MethodPost, unreadyServer.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestMCPToolsFlow(t *testing.T) {
	t.Parallel()

	checker := readiness.NewChecker(readiness.StaticConfig{Enabled: true, APIKey: "k", BaseURL: "https://example.com/v1", Model: "gpt-4o"}, readyStub{}, time.Second, 0, 0)
	checker.Run(context.Background())

	server := httptest.NewServer(NewMux(config.MCPConfig{Path: "/mcp", BearerToken: "secret"}, Dependencies{
		Dates:      fakeDatesService{},
		Content:    fakeContentService{},
		Generation: fakeGenerationService{},
		Checker:    checker,
	}))
	defer server.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp",
		HTTPClient: &http.Client{Transport: authTransport{base: http.DefaultTransport, token: "secret"}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools.Tools) != 3 {
		t.Fatalf("len(Tools) = %d, want 3", len(tools.Tools))
	}

	listResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_dates", Arguments: map[string]any{"prefix": "2026-04"}})
	if err != nil {
		t.Fatalf("CallTool(list_dates) error = %v", err)
	}
	if listResult.IsError {
		t.Fatal("list_dates returned tool error")
	}
	listPayload := decodeStructuredContent(t, listResult.StructuredContent)
	if got := listPayload["count"]; got != float64(2) {
		t.Fatalf("list_dates count = %#v, want 2", got)
	}

	contentResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_daily_content", Arguments: map[string]any{"date": "2026-04-18"}})
	if err != nil {
		t.Fatalf("CallTool(get_daily_content) error = %v", err)
	}
	contentPayload := decodeStructuredContent(t, contentResult.StructuredContent)
	if got := contentPayload["exists"]; got != true {
		t.Fatalf("get_daily_content exists = %#v, want true", got)
	}

	generateResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "generate_daily_content", Arguments: map[string]any{"date": "2026-04-18"}})
	if err != nil {
		t.Fatalf("CallTool(generate_daily_content) error = %v", err)
	}
	generatePayload := decodeStructuredContent(t, generateResult.StructuredContent)
	if got := generatePayload["status"]; got != "skipped" {
		t.Fatalf("generate_daily_content status = %#v, want skipped", got)
	}
}

type fakeDatesService struct{}

func (fakeDatesService) ListDates(prefix string) (service.DateListResult, error) {
	result := service.DateListResult{Dates: []string{"2026-04-18", "2026-04-17"}, Count: 2, Latest: "2026-04-18"}
	if prefix == "" {
		return result, nil
	}
	return result, nil
}

type fakeContentService struct{}

func (fakeContentService) GetDailyContent(date string) (service.ContentResult, error) {
	return service.ContentResult{Date: date, Exists: true, Content: "# hello", ContentType: "text/markdown"}, nil
}

type fakeGenerationService struct{}

func (fakeGenerationService) Generate(date string) (service.GenerationResult, error) {
	return service.GenerationResult{Date: date, Status: "skipped", Generated: false, Attempts: 1, Message: "skipped"}, nil
}

type readyStub struct{}

func (readyStub) Probe(ctx context.Context) error { return nil }

type failingStub struct{}

func (failingStub) Probe(ctx context.Context) error { return context.DeadlineExceeded }

type authTransport struct {
	base  http.RoundTripper
	token string
}

func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func decodeStructuredContent(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return payload
}

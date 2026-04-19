package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadFromLookupUsesDefaultsAndParsesMCPFields(t *testing.T) {
	t.Parallel()

	lookup := mapLookup(map[string]string{
		"MCP_ENABLED":      "true",
		"MCP_BEARER_TOKEN": "secret",
		"MCP_PATH":         "mcp/v1",
	})

	config, err := LoadFromLookup(lookup)
	if err != nil {
		t.Fatalf("LoadFromLookup() error = %v", err)
	}

	if config.DataDir != defaultDataDir {
		t.Fatalf("DataDir = %q, want %q", config.DataDir, defaultDataDir)
	}
	if config.Web.Port != "18080" {
		t.Fatalf("Web.Port = %q, want 18080", config.Web.Port)
	}
	if !config.MCP.Enabled {
		t.Fatal("MCP.Enabled = false, want true")
	}
	if config.MCP.Port != "18081" {
		t.Fatalf("MCP.Port = %q, want 18081", config.MCP.Port)
	}
	if config.MCP.Path != "/mcp/v1" {
		t.Fatalf("MCP.Path = %q, want /mcp/v1", config.MCP.Path)
	}
	if config.MCP.ReadyTimeout != 15*time.Second {
		t.Fatalf("MCP.ReadyTimeout = %s, want 15s", config.MCP.ReadyTimeout)
	}
	if config.AI.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("AI.BaseURL = %q", config.AI.BaseURL)
	}
	if config.AI.Model != "gpt-4o" {
		t.Fatalf("AI.Model = %q", config.AI.Model)
	}
}

func TestLoadFromLookupReturnsErrorOnInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		env   map[string]string
		match string
	}{
		{
			name:  "invalid timezone",
			env:   map[string]string{"GENERATE_TIMEZONE": "Mars/Base"},
			match: "加载时区失败",
		},
		{
			name:  "invalid duration",
			env:   map[string]string{"SERVER_READ_TIMEOUT": "abc"},
			match: "不是有效的 duration",
		},
		{
			name:  "negative retry count",
			env:   map[string]string{"GENERATE_RETRY_COUNT": "-1"},
			match: "不能小于 0",
		},
		{
			name:  "missing bearer token when mcp enabled",
			env:   map[string]string{"MCP_ENABLED": "true"},
			match: "MCP_BEARER_TOKEN 不能为空",
		},
		{
			name:  "invalid base url",
			env:   map[string]string{"OPENAI_BASE_URL": "://bad-url"},
			match: "OPENAI_BASE_URL 不是有效 URL",
		},
		{
			name:  "mcp port equals web port",
			env:   map[string]string{"MCP_ENABLED": "true", "MCP_BEARER_TOKEN": "secret", "PORT": "18081", "MCP_PORT": "18081"},
			match: "MCP_PORT 不能与 PORT 相同",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFromLookup(mapLookup(testCase.env))
			if err == nil {
				t.Fatal("LoadFromLookup() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), testCase.match) {
				t.Fatalf("LoadFromLookup() error = %q, want substring %q", err.Error(), testCase.match)
			}
		})
	}
}

func mapLookup(values map[string]string) lookupEnvFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultDataDir = "data"
	defaultMCPPath = "/mcp"
)

type AIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

type HTTPConfig struct {
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type MCPConfig struct {
	Enabled         bool
	Port            string
	Path            string
	BearerToken     string
	ReadyTimeout    time.Duration
	ReadyRetryCount int
	ReadyRetryDelay time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
}

type Config struct {
	DataDir            string
	GenerateOnStartup  bool
	GenerateCron       string
	GenerateRetryCount int
	GenerateRetryDelay time.Duration
	GenerateTimezone   string
	ManualTriggerToken string
	Location           *time.Location
	ShutdownTimeout    time.Duration
	AI                 AIConfig
	Web                HTTPConfig
	MCP                MCPConfig
}

type lookupEnvFunc func(string) (string, bool)

func LoadDotEnv(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("检查环境文件失败: %w", err)
	}

	if err := godotenv.Load(path); err != nil {
		return fmt.Errorf("加载环境文件失败: %w", err)
	}

	return nil
}

func Load() (Config, error) {
	return LoadFromLookup(os.LookupEnv)
}

func LoadFromLookup(lookup lookupEnvFunc) (Config, error) {
	locationName := getEnv(lookup, "GENERATE_TIMEZONE", "Local")
	location, err := time.LoadLocation(locationName)
	if err != nil {
		return Config{}, fmt.Errorf("加载时区失败: %w", err)
	}

	readTimeout, err := getEnvDuration(lookup, "SERVER_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := getEnvDuration(lookup, "SERVER_WRITE_TIMEOUT", 120*time.Second)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := getEnvDuration(lookup, "SERVER_IDLE_TIMEOUT", 120*time.Second)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := getEnvDuration(lookup, "SERVER_SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	retryCount, err := getEnvInt(lookup, "GENERATE_RETRY_COUNT", 2)
	if err != nil {
		return Config{}, err
	}
	retryDelay, err := getEnvDuration(lookup, "GENERATE_RETRY_DELAY", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	mcpReadyTimeout, err := getEnvDuration(lookup, "MCP_READY_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	mcpReadyRetryCount, err := getEnvInt(lookup, "MCP_READY_RETRY_COUNT", 3)
	if err != nil {
		return Config{}, err
	}
	mcpReadyRetryDelay, err := getEnvDuration(lookup, "MCP_READY_RETRY_DELAY", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	mcpReadTimeout, err := getEnvDuration(lookup, "MCP_SERVER_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	mcpWriteTimeout, err := getEnvDuration(lookup, "MCP_SERVER_WRITE_TIMEOUT", 300*time.Second)
	if err != nil {
		return Config{}, err
	}
	mcpIdleTimeout, err := getEnvDuration(lookup, "MCP_SERVER_IDLE_TIMEOUT", 120*time.Second)
	if err != nil {
		return Config{}, err
	}

	config := Config{
		DataDir:            getEnv(lookup, "DATA_DIR", defaultDataDir),
		GenerateOnStartup:  getEnvBool(lookup, "GENERATE_ON_STARTUP", true),
		GenerateCron:       getEnv(lookup, "GENERATE_CRON", "5 0 * * *"),
		GenerateRetryCount: retryCount,
		GenerateRetryDelay: retryDelay,
		GenerateTimezone:   locationName,
		ManualTriggerToken: strings.TrimSpace(getEnv(lookup, "MANUAL_TRIGGER_TOKEN", "")),
		Location:           location,
		ShutdownTimeout:    shutdownTimeout,
		AI: AIConfig{
			APIKey:  strings.TrimSpace(getEnv(lookup, "OPENAI_API_KEY", "")),
			BaseURL: getEnv(lookup, "OPENAI_BASE_URL", "https://api.openai.com/v1"),
			Model:   getEnv(lookup, "OPENAI_MODEL", "gpt-4o"),
		},
		Web: HTTPConfig{
			Port:         getEnv(lookup, "PORT", "18080"),
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
		},
		MCP: MCPConfig{
			Enabled:         getEnvBool(lookup, "MCP_ENABLED", false),
			Port:            getEnv(lookup, "MCP_PORT", "18081"),
			Path:            normalizePath(getEnv(lookup, "MCP_PATH", defaultMCPPath)),
			BearerToken:     strings.TrimSpace(getEnv(lookup, "MCP_BEARER_TOKEN", "")),
			ReadyTimeout:    mcpReadyTimeout,
			ReadyRetryCount: mcpReadyRetryCount,
			ReadyRetryDelay: mcpReadyRetryDelay,
			ReadTimeout:     mcpReadTimeout,
			WriteTimeout:    mcpWriteTimeout,
			IdleTimeout:     mcpIdleTimeout,
		},
	}

	if err := validate(config); err != nil {
		return Config{}, err
	}

	return config, nil
}

func validate(config Config) error {
	if config.DataDir == "" {
		return fmt.Errorf("DATA_DIR 不能为空")
	}
	if config.GenerateCron == "" {
		return fmt.Errorf("GENERATE_CRON 不能为空")
	}
	if config.AI.BaseURL == "" {
		return fmt.Errorf("OPENAI_BASE_URL 不能为空")
	}
	if _, err := url.ParseRequestURI(config.AI.BaseURL); err != nil {
		return fmt.Errorf("OPENAI_BASE_URL 不是有效 URL: %w", err)
	}
	if config.AI.Model == "" {
		return fmt.Errorf("OPENAI_MODEL 不能为空")
	}
	if config.Web.Port == "" {
		return fmt.Errorf("PORT 不能为空")
	}
	if config.MCP.Enabled {
		if config.MCP.Port == "" {
			return fmt.Errorf("MCP_PORT 不能为空")
		}
		if config.MCP.BearerToken == "" {
			return fmt.Errorf("MCP_BEARER_TOKEN 不能为空")
		}
		if config.MCP.Path == "" || config.MCP.Path == "/" {
			return fmt.Errorf("MCP_PATH 不能为空或根路径")
		}
		if config.MCP.Port == config.Web.Port {
			return fmt.Errorf("MCP_PORT 不能与 PORT 相同")
		}
	}
	return nil
}

func normalizePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return defaultMCPPath
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return trimmed
}

func getEnv(lookup lookupEnvFunc, key string, fallback string) string {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func getEnvBool(lookup lookupEnvFunc, key string, fallback bool) bool {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}

	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getEnvDuration(lookup lookupEnvFunc, key string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("环境变量 %s 不是有效的 duration: %w", key, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("环境变量 %s 不能小于 0", key)
	}
	return duration, nil
}

func getEnvInt(lookup lookupEnvFunc, key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}

	var parsed int
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &parsed); err != nil {
		return 0, fmt.Errorf("环境变量 %s 不是有效整数: %w", key, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("环境变量 %s 不能小于 0", key)
	}
	return parsed, nil
}

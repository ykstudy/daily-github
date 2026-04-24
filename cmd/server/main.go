package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"daily-github/internal/ai"
	"daily-github/internal/config"
	"daily-github/internal/daily"
	"daily-github/internal/mcpserver"
	"daily-github/internal/readiness"
	"daily-github/internal/service"
	"daily-github/internal/storage"

	"github.com/robfig/cron/v3"
)

type manualGenerateRequest struct {
	Date string `json:"date"`
}

type todayStatusResponse struct {
	Date              string `json:"date"`
	Exists            bool   `json:"exists"`
	GenerationEnabled bool   `json:"generationEnabled"`
	TokenRequired     bool   `json:"tokenRequired"`
}

const webPathPrefix = "/daily-github"

func main() {
	if err := config.LoadDotEnv("../../.env"); err != nil {
		log.Fatalf("初始化环境配置失败: %v", err)
	}

	appConfig, err := config.Load()
	if err != nil {
		log.Fatalf("加载运行配置失败: %v", err)
	}

	markdownStore := storage.NewMarkdownStore(appConfig.DataDir)
	if err := markdownStore.EnsureDataDir(); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	historyStore := storage.NewHistoryStore(appConfig.DataDir, markdownStore)
	aiClient := ai.NewClient(appConfig.AI.APIKey, appConfig.AI.BaseURL, appConfig.AI.Model, nil)
	generator := daily.NewGenerator(markdownStore, historyStore, aiClient, appConfig.GenerateRetryCount, appConfig.GenerateRetryDelay)

	dateService := service.NewDateService(markdownStore)
	contentService := service.NewContentService(markdownStore)
	generationService := service.NewGenerationService(appConfig.Location, generator)

	rootContext, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	checker := readiness.NewChecker(readiness.StaticConfig{
		Enabled: appConfig.MCP.Enabled,
		APIKey:  appConfig.AI.APIKey,
		BaseURL: appConfig.AI.BaseURL,
		Model:   appConfig.AI.Model,
	}, aiClient, appConfig.MCP.ReadyTimeout, appConfig.MCP.ReadyRetryCount, appConfig.MCP.ReadyRetryDelay)
	checker.Start(rootContext)

	if appConfig.GenerateOnStartup {
		today := time.Now().In(appConfig.Location).Format("2006-01-02")
		if _, err := generationService.Generate(today); err != nil {
			log.Printf("⚠️ 启动时生成今日推荐失败: %v", err)
		}
	}

	scheduler, err := startScheduler(appConfig, generationService)
	if err != nil {
		log.Fatalf("启动定时任务失败: %v", err)
	}

	webServer := &http.Server{
		Addr:         ":" + appConfig.Web.Port,
		Handler:      newWebMux(appConfig, dateService, contentService, generationService),
		ReadTimeout:  appConfig.Web.ReadTimeout,
		WriteTimeout: appConfig.Web.WriteTimeout,
		IdleTimeout:  appConfig.Web.IdleTimeout,
	}

	serverErrors := make(chan error, 2)
	go func() {
		log.Printf("🌐 Web 服务已启动: http://0.0.0.0:%s", appConfig.Web.Port)
		serverErrors <- webServer.ListenAndServe()
	}()

	var mcpServer *http.Server
	if appConfig.MCP.Enabled {
		mcpServer = &http.Server{
			Addr:         ":" + appConfig.MCP.Port,
			Handler:      mcpserver.NewMux(appConfig.MCP, mcpserver.Dependencies{Dates: dateService, Content: contentService, Generation: generationService, Checker: checker}),
			ReadTimeout:  appConfig.MCP.ReadTimeout,
			WriteTimeout: appConfig.MCP.WriteTimeout,
			IdleTimeout:  appConfig.MCP.IdleTimeout,
		}
		go func() {
			log.Printf("🔌 MCP 服务已启动: http://0.0.0.0:%s%s", appConfig.MCP.Port, appConfig.MCP.Path)
			serverErrors <- mcpServer.ListenAndServe()
		}()
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("服务异常退出: %v", err)
		}
	case sig := <-signals:
		log.Printf("🛑 收到退出信号: %s", sig.String())
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), appConfig.ShutdownTimeout)
	defer cancelShutdown()
	cancelRoot()

	stopContext := scheduler.Stop()
	select {
	case <-stopContext.Done():
	case <-shutdownContext.Done():
	}

	if err := webServer.Shutdown(shutdownContext); err != nil {
		log.Printf("⚠️ Web 服务关闭异常: %v", err)
	}
	if mcpServer != nil {
		if err := mcpServer.Shutdown(shutdownContext); err != nil {
			log.Printf("⚠️ MCP 服务关闭异常: %v", err)
		}
	}
}

func startScheduler(appConfig config.Config, generationService *service.GenerationService) (*cron.Cron, error) {
	scheduler := cron.New(cron.WithLocation(appConfig.Location))
	_, err := scheduler.AddFunc(appConfig.GenerateCron, func() {
		date := time.Now().In(appConfig.Location).Format("2006-01-02")
		if _, err := generationService.Generate(date); err != nil {
			log.Printf("⚠️ 定时生成失败 (%s): %v", date, err)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("注册定时任务失败: %w", err)
	}
	scheduler.Start()
	log.Printf("⏰ 已启动定时生成: cron=%q timezone=%s", appConfig.GenerateCron, appConfig.GenerateTimezone)
	return scheduler, nil
}

func newWebMux(appConfig config.Config, dateService *service.DateService, contentService *service.ContentService, generationService *service.GenerationService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", newIndexHandler(resolveIndexPath()))
	mux.HandleFunc("/healthz", serveHealth)
	mux.HandleFunc("/api/today-status", newTodayStatusHandler(appConfig, contentService))
	mux.HandleFunc("/api/dates", func(w http.ResponseWriter, r *http.Request) {
		result, err := dateService.ListDates("")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result.Dates)
	})
	mux.HandleFunc("/api/content/", func(w http.ResponseWriter, r *http.Request) {
		date := strings.TrimPrefix(r.URL.Path, "/api/content/")
		result, err := contentService.GetDailyContent(date)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if !result.Exists {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "该日期的推荐内容不存在"})
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(result.Content))
	})
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		serveManualGenerate(appConfig, generationService, w, r)
	})
	return withPathPrefix(mux, webPathPrefix)
}

func withPathPrefix(handler http.Handler, prefix string) http.Handler {
	normalized := normalizeRequestPathPrefix(prefix)
	if normalized == "/" {
		return handler
	}

	prefixedRoot := normalized + "/"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == normalized:
			http.Redirect(w, r, prefixedRoot, http.StatusPermanentRedirect)
			return
		case strings.HasPrefix(r.URL.Path, prefixedRoot):
			clone := r.Clone(r.Context())
			urlCopy := *r.URL
			clone.URL = &urlCopy
			clone.URL.Path = strings.TrimPrefix(r.URL.Path, normalized)
			if clone.URL.Path == "" {
				clone.URL.Path = "/"
			}
			if r.URL.RawPath != "" {
				clone.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, normalized)
				if clone.URL.RawPath == "" {
					clone.URL.RawPath = "/"
				}
			}
			handler.ServeHTTP(w, clone)
			return
		default:
			handler.ServeHTTP(w, r)
		}
	})
}

func normalizeRequestPathPrefix(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" || trimmed == "/" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

func newIndexHandler(indexPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	}
}

func resolveIndexPath() string {
	var candidates []string

	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(workingDir, "index.html"))
	}
	if executablePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executablePath), "index.html"))
	}

	for _, candidate := range candidates {
		fileInfo, err := os.Stat(candidate)
		if err == nil && !fileInfo.IsDir() {
			return candidate
		}
	}

	return "index.html"
}

func newTodayStatusHandler(appConfig config.Config, contentService *service.ContentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET 请求"})
			return
		}

		today := time.Now().In(appConfig.Location).Format("2006-01-02")
		content, err := contentService.GetDailyContent(today)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		generationEnabled := strings.TrimSpace(appConfig.ManualTriggerToken) != ""
		writeJSON(w, http.StatusOK, todayStatusResponse{
			Date:              today,
			Exists:            content.Exists,
			GenerationEnabled: generationEnabled,
			TokenRequired:     generationEnabled,
		})
	}
}

func serveHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func serveManualGenerate(appConfig config.Config, generationService *service.GenerationService, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST 请求"})
		return
	}
	if appConfig.ManualTriggerToken == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "手动触发接口未启用"})
		return
	}
	if !validateManualTriggerToken(appConfig.ManualTriggerToken, r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "鉴权失败"})
		return
	}
	date, err := extractManualRequestDate(r, appConfig.Location)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := generationService.Generate(date)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "date": result.Date, "attempts": result.Attempts})
		return
	}
	statusCode := http.StatusOK
	if result.Generated {
		statusCode = http.StatusCreated
	}
	writeJSON(w, statusCode, result)
}

func validateManualTriggerToken(expected string, r *http.Request) bool {
	token := strings.TrimSpace(r.Header.Get("X-Trigger-Token"))
	if token == "" {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		token = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer"))
	}
	if len(token) != len(expected) {
		return false
	}
	return subtleCompare(token, expected)
}

func subtleCompare(left string, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func extractManualRequestDate(r *http.Request, location *time.Location) (string, error) {
	if queryDate := strings.TrimSpace(r.URL.Query().Get("date")); queryDate != "" {
		return service.ResolveDate(queryDate, location)
	}
	if r.ContentLength == 0 {
		return service.ResolveDate("", location)
	}
	var payload manualGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("请求体不是合法 JSON: %w", err)
	}
	return service.ResolveDate(payload.Date, location)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

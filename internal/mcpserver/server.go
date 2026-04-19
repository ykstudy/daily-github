package mcpserver

import (
	"net/http"
	"strings"

	"daily-github/internal/config"
	"daily-github/internal/readiness"
	"daily-github/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DateService interface {
	ListDates(prefix string) (service.DateListResult, error)
}

type ContentService interface {
	GetDailyContent(date string) (service.ContentResult, error)
}

type GenerationService interface {
	Generate(date string) (service.GenerationResult, error)
}

type Dependencies struct {
	Dates      DateService
	Content    ContentService
	Generation GenerationService
	Checker    *readiness.Checker
}

func NewMux(cfg config.MCPConfig, deps Dependencies) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "daily-github", Version: "v1.0.0"}, nil)
	handlers := &toolHandlers{
		dates:      deps.Dates,
		content:    deps.Content,
		generation: deps.Generation,
	}
	handlers.register(server)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})

	protected := chain(
		mcpHandler,
		readinessMiddleware(deps.Checker),
		bearerTokenMiddleware(cfg.BearerToken),
	)

	mux := http.NewServeMux()
	mux.Handle(cfg.Path, protected)
	mux.HandleFunc(readyzPath(cfg.Path), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if deps.Checker == nil {
			writeJSON(w, http.StatusOK, readiness.Snapshot{Enabled: false, State: readiness.StateDisabled})
			return
		}
		status := deps.Checker.Snapshot()
		httpStatus := http.StatusOK
		if status.State != readiness.StateReady {
			httpStatus = http.StatusServiceUnavailable
		}
		writeJSON(w, httpStatus, status)
	})
	return mux
}

func readyzPath(path string) string {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		trimmed = "/mcp"
	}
	return trimmed + "/readyz"
}

func chain(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	wrapped := handler
	for index := len(middlewares) - 1; index >= 0; index-- {
		wrapped = middlewares[index](wrapped)
	}
	return wrapped
}

package mcpserver

import (
	"context"

	"daily-github/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listDatesInput struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"optional date prefix filter"`
}

type getDailyContentInput struct {
	Date string `json:"date" jsonschema:"target date in YYYY-MM-DD"`
}

type generateDailyContentInput struct {
	Date string `json:"date,omitempty" jsonschema:"optional date in YYYY-MM-DD; defaults to today"`
}

type toolHandlers struct {
	dates      DateService
	content    ContentService
	generation GenerationService
}

func (h *toolHandlers) register(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_dates", Description: "List generated daily recommendation dates"}, h.listDates)
	mcp.AddTool(server, &mcp.Tool{Name: "get_daily_content", Description: "Get markdown content for a specified daily recommendation date"}, h.getDailyContent)
	mcp.AddTool(server, &mcp.Tool{Name: "generate_daily_content", Description: "Generate daily recommendation content for today or a specified date"}, h.generateDailyContent)
}

func (h *toolHandlers) listDates(ctx context.Context, req *mcp.CallToolRequest, input listDatesInput) (*mcp.CallToolResult, service.DateListResult, error) {
	_ = ctx
	_ = req
	result, err := h.dates.ListDates(input.Prefix)
	return nil, result, err
}

func (h *toolHandlers) getDailyContent(ctx context.Context, req *mcp.CallToolRequest, input getDailyContentInput) (*mcp.CallToolResult, service.ContentResult, error) {
	_ = ctx
	_ = req
	result, err := h.content.GetDailyContent(input.Date)
	return nil, result, err
}

func (h *toolHandlers) generateDailyContent(ctx context.Context, req *mcp.CallToolRequest, input generateDailyContentInput) (*mcp.CallToolResult, service.GenerationResult, error) {
	_ = ctx
	_ = req
	result, err := h.generation.Generate(input.Date)
	return nil, result, err
}

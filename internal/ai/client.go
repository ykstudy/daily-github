package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewClient(apiKey string, baseURL string, model string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Minute}
	}
	return &Client{
		APIKey:     strings.TrimSpace(apiKey),
		BaseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Model:      strings.TrimSpace(model),
		HTTPClient: httpClient,
	}
}

func (c *Client) Chat(ctx context.Context, messages []Message, maxTokens int) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("请设置 OPENAI_API_KEY，可放在 .env 文件或系统环境变量中")
	}
	if c.BaseURL == "" {
		return "", fmt.Errorf("OPENAI_BASE_URL 不能为空")
	}
	if c.Model == "" {
		return "", fmt.Errorf("OPENAI_MODEL 不能为空")
	}

	reqBody := chatRequest{
		Model:     c.Model,
		Messages:  messages,
		MaxTokens: maxTokens,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	url := c.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API 返回错误 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if chatResp.Error != nil {
		return "", fmt.Errorf("API 错误: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("API 未返回任何内容")
	}

	return chatResp.Choices[0].Message.Content, nil
}

func (c *Client) Probe(ctx context.Context) error {
	_, err := c.Chat(ctx, []Message{{Role: "user", Content: "reply with ok"}}, 8)
	return err
}

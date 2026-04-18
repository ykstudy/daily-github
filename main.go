package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"golang.org/x/net/html"
)

const defaultDataDir = "data"

var repoURLPattern = regexp.MustCompile(`https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)`)

type Config struct {
	DataDir            string
	Port               string
	GenerateOnStartup  bool
	GenerateCron       string
	GenerateRetryCount int
	GenerateRetryDelay time.Duration
	GenerateTimezone   string
	ManualTriggerToken string
	Location           *time.Location
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ShutdownTimeout    time.Duration
}

type GenerationResult struct {
	Date      string `json:"date"`
	FilePath  string `json:"filePath"`
	Generated bool   `json:"generated"`
	Attempts  int    `json:"attempts"`
}

type ManualGenerateRequest struct {
	Date string `json:"date"`
}

type TrendingRepository struct {
	Repo        string
	Description string
	Language    string
}

type PromptContext struct {
	TrendingByPeriod map[string][]TrendingRepository
	HistoricalRepos  []string
}

type RecommendationHistory struct {
	ProjectsByDate map[string][]string `json:"projectsByDate"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type ChatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

var generationMu sync.Mutex

const systemPrompt = `你是一个专业的 GitHub 开源项目分析师和推荐专家。
你对各类开源项目有深入的了解，能从技术架构、社区活跃度、实用价值等多维度进行分析。
你的推荐风格专业但易读，善于用结构化的方式呈现信息。`

func generateUserPrompt(date string, promptContext PromptContext) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("请为 %s 生成一份每日 GitHub 开源项目推荐报告。\n\n", date))
	builder.WriteString("## 本次推荐策略\n\n")
	builder.WriteString("1. GitHub Trending 的日榜、周榜、月榜是本次推荐的主要候选来源。\n")
	builder.WriteString("2. 优先从 Trending 候选中选择 4-6 个项目作为核心推荐。\n")
	builder.WriteString("3. 允许补充 1-3 个没有出现在 Trending 中、但客观上非常值得关注的真实项目，用于保留更广泛的推荐逻辑。\n")
	builder.WriteString("4. 不要把推荐范围完全限制在 Trending 页面项目中，但要明确体现 Trending 是主要依据。\n")
	builder.WriteString("5. 绝对不要重复推荐历史中已经出现过的 owner/repo。\n\n")

	builder.WriteString("## GitHub Trending 参考数据\n\n")
	builder.WriteString(formatTrendingPromptSection("daily", "日榜", promptContext.TrendingByPeriod["daily"]))
	builder.WriteString("\n")
	builder.WriteString(formatTrendingPromptSection("weekly", "周榜", promptContext.TrendingByPeriod["weekly"]))
	builder.WriteString("\n")
	builder.WriteString(formatTrendingPromptSection("monthly", "月榜", promptContext.TrendingByPeriod["monthly"]))
	builder.WriteString("\n")

	builder.WriteString("## 历史去重约束\n\n")
	if len(promptContext.HistoricalRepos) == 0 {
		builder.WriteString("当前没有历史推荐记录，可以正常推荐。\n\n")
	} else {
		builder.WriteString("以下项目已经推荐过，本次禁止再次推荐：\n")
		for _, repo := range promptContext.HistoricalRepos {
			builder.WriteString("- ")
			builder.WriteString(repo)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	builder.WriteString(fmt.Sprintf(`## 输出要求

### 文档结构
1. 以 "# 📅 GitHub 每日推荐 | %s" 作为标题
2. 紧接一段简短的今日推荐导语（2-3 句话概述今天的推荐主题）
3. 一个【今日速览】表格，快速索引所有推荐项目
4. 每个项目的详细分析卡片
5. 最后一个【今日总结】部分

### 推荐项目数量
推荐 6-8 个值得关注的 GitHub 开源项目，覆盖不同领域：
- 前端/UI
- 后端/基础设施
- AI/机器学习
- DevOps/工具
- 编程语言/框架
- 安全/数据

### 今日速览表格格式

| 项目 | 语言 | 领域 | 推荐程度 |
|------|------|------|----------|
| 项目名 | Go | 后端 | ⭐⭐⭐⭐⭐ |

### 每个项目的详细卡片格式

---

### {图标} 项目名称

> 一句话项目描述

| 属性 | 详情 |
|------|------|
| 📦 仓库 | [owner/repo](https://github.com/owner/repo) |
| ⭐ Stars | 约 xx.xk |
| 🔤 语言 | 主要编程语言 |
| 📄 协议 | MIT / Apache-2.0 等 |
| 🏷️ 标签 | 用逗号分隔的标签 |

**📝 项目简介**

用 2-3 句话描述项目是什么，解决什么问题。

**💡 推荐理由**

为什么值得关注？从技术价值、社区生态、学习意义等角度分析。

**✨ 核心特性**

- 特性一：简要说明
- 特性二：简要说明
- 特性三：简要说明

**🎯 适用场景**

描述什么人/什么项目适合使用。

**📊 推荐程度**

⭐⭐⭐⭐⭐ (X/5) — 一句话总结推荐理由

---

### 项目标题图标使用规则

根据项目特征在标题中使用对应图标：
- 🔥 热门/趋势项目（Stars 增长快、近期社区关注度高）
- 💡 创新/新颖项目（采用新技术或解决方案独特）
- 🛠️ 实用工具（开发者日常可用的工具类项目）
- 📚 学习/教程资源（适合学习和参考）
- 🚀 新兴/快速增长项目（成立时间较短但发展迅速）
- 🤖 AI/机器学习相关
- 🔒 安全相关
- 🎨 前端/设计相关
- ⚡ 高性能/基础设施

### 今日总结格式

## 📌 今日总结

用 3-4 句话总结今日推荐的主题和趋势，给出整体建议。

---

## 注意事项
- 推荐真实存在的知名 GitHub 项目
- Stars 数据需要是真实数据
- 确保涵盖不同编程语言和应用领域
- 描述要客观专业，避免过度营销
- Markdown 格式要规范，便于渲染
- 每个项目之间用 --- 分隔
- 明确优先使用 Trending 日榜、周榜、月榜中的热门项目，但允许补充少量非 Trending 项目
- 不要推荐任何已在历史去重名单中的 owner/repo`, date, date))

	return builder.String()
}

func formatTrendingPromptSection(period string, title string, repositories []TrendingRepository) string {
	var builder strings.Builder
	builder.WriteString("### GitHub Trending ")
	builder.WriteString(title)
	builder.WriteString("（")
	builder.WriteString(period)
	builder.WriteString("）\n")

	if len(repositories) == 0 {
		builder.WriteString("- 当前未抓取到该维度的 Trending 数据，请结合其他维度与一般推荐逻辑补充判断。\n")
		return builder.String()
	}

	for index, repository := range repositories {
		builder.WriteString(fmt.Sprintf("%d. %s", index+1, repository.Repo))
		if repository.Language != "" {
			builder.WriteString(" | ")
			builder.WriteString(repository.Language)
		}
		if repository.Description != "" {
			builder.WriteString(" | ")
			builder.WriteString(repository.Description)
		}
		builder.WriteString("\n")
	}

	return builder.String()
}

func callLLMAPI(prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("请设置 OPENAI_API_KEY，可放在 .env 文件或系统环境变量中")
	}

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	reqBody := ChatRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
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

	var chatResp ChatResponse
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

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateText(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	return strings.TrimSpace(value[:maxLen-3]) + "..."
}

func getAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func hasClass(node *html.Node, className string) bool {
	for _, item := range strings.Fields(getAttr(node, "class")) {
		if item == className {
			return true
		}
	}
	return false
}

func findFirstNode(node *html.Node, matcher func(*html.Node) bool) *html.Node {
	if matcher(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if matched := findFirstNode(child, matcher); matched != nil {
			return matched
		}
	}
	return nil
}

func collectNodes(node *html.Node, matcher func(*html.Node) bool, result *[]*html.Node) {
	if matcher(node) {
		*result = append(*result, node)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectNodes(child, matcher, result)
	}
}

func extractNodeText(node *html.Node) string {
	if node == nil {
		return ""
	}

	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteString(" ")
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(node)
	return normalizeText(builder.String())
}

func fetchTrendingRepositories(period string) ([]TrendingRepository, error) {
	url := fmt.Sprintf("https://github.com/trending?since=%s", period)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Trending 请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "daily-github-bot/1.0")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 Trending 页面失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Trending 页面返回异常状态: %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("解析 Trending HTML 失败: %w", err)
	}

	var articleNodes []*html.Node
	collectNodes(doc, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "article" && hasClass(node, "Box-row")
	}, &articleNodes)

	repositories := make([]TrendingRepository, 0, len(articleNodes))
	for _, article := range articleNodes {
		linkNode := findFirstNode(article, func(node *html.Node) bool {
			if node.Type != html.ElementNode || node.Data != "a" {
				return false
			}
			href := strings.TrimSpace(getAttr(node, "href"))
			return strings.HasPrefix(href, "/") && strings.Count(strings.Trim(href, "/"), "/") == 1
		})
		if linkNode == nil {
			continue
		}

		repo := strings.Trim(getAttr(linkNode, "href"), "/")
		if repo == "" {
			continue
		}

		descriptionNode := findFirstNode(article, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "p"
		})
		languageNode := findFirstNode(article, func(node *html.Node) bool {
			return node.Type == html.ElementNode && getAttr(node, "itemprop") == "programmingLanguage"
		})

		repositories = append(repositories, TrendingRepository{
			Repo:        repo,
			Description: truncateText(extractNodeText(descriptionNode), 160),
			Language:    extractNodeText(languageNode),
		})
	}

	if len(repositories) > 10 {
		repositories = repositories[:10]
	}

	return repositories, nil
}

func recommendationHistoryPath(dataDir string) string {
	return filepath.Join(dataDir, "recommendation-history.json")
}

func extractRecommendedRepos(markdown string) []string {
	matches := repoURLPattern.FindAllStringSubmatch(markdown, -1)
	unique := make(map[string]struct{})
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		unique[match[1]] = struct{}{}
	}

	repositories := make([]string, 0, len(unique))
	for repo := range unique {
		repositories = append(repositories, repo)
	}
	sort.Strings(repositories)
	return repositories
}

func saveRecommendationHistory(dataDir string, history RecommendationHistory) error {
	content, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化推荐历史失败: %w", err)
	}

	if err := os.WriteFile(recommendationHistoryPath(dataDir), content, 0644); err != nil {
		return fmt.Errorf("写入推荐历史失败: %w", err)
	}

	return nil
}

func syncRecommendationHistoryFromMarkdown(dataDir string, history *RecommendationHistory) (bool, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return false, fmt.Errorf("读取数据目录失败: %w", err)
	}

	changed := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}

		date := strings.TrimSuffix(name, ".md")
		if existing := history.ProjectsByDate[date]; len(existing) > 0 {
			continue
		}

		content, err := os.ReadFile(filepath.Join(dataDir, name))
		if err != nil {
			return changed, fmt.Errorf("读取历史 markdown 失败: %w", err)
		}

		repositories := extractRecommendedRepos(string(content))
		if len(repositories) == 0 {
			continue
		}

		history.ProjectsByDate[date] = repositories
		changed = true
	}

	return changed, nil
}

func loadRecommendationHistory(dataDir string) (RecommendationHistory, error) {
	history := RecommendationHistory{ProjectsByDate: make(map[string][]string)}

	content, err := os.ReadFile(recommendationHistoryPath(dataDir))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return history, fmt.Errorf("读取推荐历史失败: %w", err)
		}
	} else if len(content) > 0 {
		if err := json.Unmarshal(content, &history); err != nil {
			return history, fmt.Errorf("解析推荐历史失败: %w", err)
		}
		if history.ProjectsByDate == nil {
			history.ProjectsByDate = make(map[string][]string)
		}
	}

	changed, err := syncRecommendationHistoryFromMarkdown(dataDir, &history)
	if err != nil {
		return history, err
	}
	if changed {
		if err := saveRecommendationHistory(dataDir, history); err != nil {
			return history, err
		}
	}

	return history, nil
}

func listHistoricalRepos(history RecommendationHistory) []string {
	unique := make(map[string]struct{})
	for _, repositories := range history.ProjectsByDate {
		for _, repo := range repositories {
			unique[repo] = struct{}{}
		}
	}

	result := make([]string, 0, len(unique))
	for repo := range unique {
		result = append(result, repo)
	}
	sort.Strings(result)
	return result
}

func buildPromptContext(dataDir string) (PromptContext, error) {
	history, err := loadRecommendationHistory(dataDir)
	if err != nil {
		return PromptContext{}, err
	}

	trendingByPeriod := make(map[string][]TrendingRepository)
	for _, period := range []string{"daily", "weekly", "monthly"} {
		repositories, err := fetchTrendingRepositories(period)
		if err != nil {
			log.Printf("⚠️ 抓取 GitHub Trending %s 失败: %v", period, err)
			continue
		}
		trendingByPeriod[period] = repositories
	}

	return PromptContext{
		TrendingByPeriod: trendingByPeriod,
		HistoricalRepos:  listHistoricalRepos(history),
	}, nil
}

func recordDailyRecommendations(dataDir string, date string, markdown string) error {
	repositories := extractRecommendedRepos(markdown)
	if len(repositories) == 0 {
		return fmt.Errorf("未能从生成结果中提取到任何 GitHub 仓库")
	}

	history, err := loadRecommendationHistory(dataDir)
	if err != nil {
		return err
	}

	history.ProjectsByDate[date] = repositories
	return saveRecommendationHistory(dataDir, history)
}

func generateDaily(dataDir string, date string) (bool, error) {
	filePath := filepath.Join(dataDir, date+".md")

	if _, err := os.Stat(filePath); err == nil {
		content, readErr := os.ReadFile(filePath)
		if readErr == nil {
			if err := recordDailyRecommendations(dataDir, date, string(content)); err != nil {
				log.Printf("⚠️ 补录当日推荐历史失败 (%s): %v", date, err)
			}
		}
		log.Printf("📋 文件已存在，跳过生成: %s", filePath)
		return false, nil
	}

	log.Printf("🚀 开始生成 %s 的推荐内容...", date)

	promptContext, err := buildPromptContext(dataDir)
	if err != nil {
		return false, fmt.Errorf("构建推荐上下文失败: %w", err)
	}

	prompt := generateUserPrompt(date, promptContext)
	content, err := callLLMAPI(prompt)
	if err != nil {
		return false, fmt.Errorf("生成内容失败: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("写入文件失败: %w", err)
	}

	if err := recordDailyRecommendations(dataDir, date, content); err != nil {
		log.Printf("⚠️ 记录当日推荐历史失败 (%s): %v", date, err)
	}

	log.Printf("✅ 成功生成: %s", filePath)
	return true, nil
}

func generateDailyWithRetry(config Config, date string) (GenerationResult, error) {
	generationMu.Lock()
	defer generationMu.Unlock()

	result := GenerationResult{
		Date:     date,
		FilePath: filepath.Join(config.DataDir, date+".md"),
	}

	attemptLimit := config.GenerateRetryCount + 1
	var lastErr error

	for attempt := 1; attempt <= attemptLimit; attempt++ {
		generated, err := generateDaily(config.DataDir, date)
		if err == nil {
			result.Generated = generated
			result.Attempts = attempt
			return result, nil
		}

		lastErr = err
		log.Printf("⚠️ 生成失败 (%s)，第 %d/%d 次: %v", date, attempt, attemptLimit, err)

		if attempt < attemptLimit {
			log.Printf("⏳ %s 后重试生成: %s", config.GenerateRetryDelay, date)
			time.Sleep(config.GenerateRetryDelay)
		}
	}

	result.Attempts = attemptLimit
	return result, fmt.Errorf("重试后仍生成失败: %w", lastErr)
}

func serveDates(dataDir string, w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		http.Error(w, "读取数据目录失败", http.StatusInternalServerError)
		return
	}

	var dates []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			dates = append(dates, strings.TrimSuffix(entry.Name(), ".md"))
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dates)
}

func serveContent(dataDir string, w http.ResponseWriter, r *http.Request) {
	date := strings.TrimPrefix(r.URL.Path, "/api/content/")
	if date == "" {
		http.Error(w, "缺少日期参数", http.StatusBadRequest)
		return
	}

	if len(date) != 10 || date[4] != '-' || date[7] != '-' {
		http.Error(w, "日期格式无效", http.StatusBadRequest)
		return
	}

	content, err := os.ReadFile(filepath.Join(dataDir, date+".md"))
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "该日期的推荐内容不存在", http.StatusNotFound)
		} else {
			http.Error(w, "读取文件失败", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write(content)
}

func serveHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "index.html")
}

func loadEnvFile() error {
	const envFile = ".env"

	if _, err := os.Stat(envFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("检查 .env 文件失败: %w", err)
	}

	if err := godotenv.Load(envFile); err != nil {
		return fmt.Errorf("加载 .env 文件失败: %w", err)
	}

	log.Printf("📄 已加载配置文件: %s", envFile)
	return nil
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}

	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("环境变量 %s 不是有效的 duration: %w", key, err)
	}

	return duration, nil
}

func getEnvInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	var parsed int
	_, err := fmt.Sscanf(value, "%d", &parsed)
	if err != nil {
		return 0, fmt.Errorf("环境变量 %s 不是有效整数: %w", key, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("环境变量 %s 不能小于 0", key)
	}

	return parsed, nil
}

func parseRequestDate(raw string, location *time.Location) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().In(location).Format("2006-01-02"), nil
	}

	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return "", fmt.Errorf("日期格式无效，需为 YYYY-MM-DD")
	}

	return parsed.Format("2006-01-02"), nil
}

func extractManualRequestDate(r *http.Request, location *time.Location) (string, error) {
	if queryDate := strings.TrimSpace(r.URL.Query().Get("date")); queryDate != "" {
		return parseRequestDate(queryDate, location)
	}

	if r.ContentLength == 0 {
		return parseRequestDate("", location)
	}

	var payload ManualGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("请求体不是合法 JSON: %w", err)
	}

	return parseRequestDate(payload.Date, location)
}

func validateManualTriggerToken(config Config, r *http.Request) bool {
	if config.ManualTriggerToken == "" {
		return false
	}

	token := strings.TrimSpace(r.Header.Get("X-Trigger-Token"))
	if token == "" {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		token = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer"))
	}

	if len(token) != len(config.ManualTriggerToken) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(token), []byte(config.ManualTriggerToken)) == 1
}

func serveManualGenerate(config Config, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST 请求"})
		return
	}

	if config.ManualTriggerToken == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "手动触发接口未启用"})
		return
	}

	if !validateManualTriggerToken(config, r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "鉴权失败"})
		return
	}

	date, err := extractManualRequestDate(r, config.Location)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	result, err := generateDailyWithRetry(config, date)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":    err.Error(),
			"date":     date,
			"attempts": result.Attempts,
		})
		return
	}

	statusCode := http.StatusOK
	status := "skipped"
	if result.Generated {
		statusCode = http.StatusCreated
		status = "generated"
	}

	writeJSON(w, statusCode, map[string]any{
		"status":    status,
		"date":      result.Date,
		"attempts":  result.Attempts,
		"generated": result.Generated,
		"filePath":  result.FilePath,
	})
}

func loadConfig() (Config, error) {
	locationName := getEnv("GENERATE_TIMEZONE", "Local")
	location, err := time.LoadLocation(locationName)
	if err != nil {
		return Config{}, fmt.Errorf("加载时区失败: %w", err)
	}

	readTimeout, err := getEnvDuration("SERVER_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := getEnvDuration("SERVER_WRITE_TIMEOUT", 120*time.Second)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := getEnvDuration("SERVER_IDLE_TIMEOUT", 120*time.Second)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := getEnvDuration("SERVER_SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	retryCount, err := getEnvInt("GENERATE_RETRY_COUNT", 2)
	if err != nil {
		return Config{}, err
	}
	retryDelay, err := getEnvDuration("GENERATE_RETRY_DELAY", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	return Config{
		DataDir:            getEnv("DATA_DIR", defaultDataDir),
		Port:               getEnv("PORT", "18080"),
		GenerateOnStartup:  getEnvBool("GENERATE_ON_STARTUP", true),
		GenerateCron:       getEnv("GENERATE_CRON", "5 0 * * *"),
		GenerateRetryCount: retryCount,
		GenerateRetryDelay: retryDelay,
		GenerateTimezone:   locationName,
		ManualTriggerToken: strings.TrimSpace(os.Getenv("MANUAL_TRIGGER_TOKEN")),
		Location:           location,
		ReadTimeout:        readTimeout,
		WriteTimeout:       writeTimeout,
		IdleTimeout:        idleTimeout,
		ShutdownTimeout:    shutdownTimeout,
	}, nil
}

func startScheduler(config Config) (*cron.Cron, error) {
	scheduler := cron.New(cron.WithLocation(config.Location))

	_, err := scheduler.AddFunc(config.GenerateCron, func() {
		date := time.Now().In(config.Location).Format("2006-01-02")
		if _, err := generateDailyWithRetry(config, date); err != nil {
			log.Printf("⚠️ 定时生成失败 (%s): %v", date, err)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("注册定时任务失败: %w", err)
	}

	scheduler.Start()
	log.Printf("⏰ 已启动定时生成: cron=%q timezone=%s", config.GenerateCron, config.GenerateTimezone)
	return scheduler, nil
}

func main() {
	if err := loadEnvFile(); err != nil {
		log.Fatalf("初始化环境配置失败: %v", err)
	}

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("加载运行配置失败: %v", err)
	}

	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}

	if config.GenerateOnStartup {
		today := time.Now().In(config.Location).Format("2006-01-02")
		if _, err := generateDailyWithRetry(config, today); err != nil {
			log.Printf("⚠️ 启动时生成今日推荐失败: %v", err)
			log.Println("提示: 请确保在 .env 文件或系统环境变量中设置了 OPENAI_API_KEY")
			log.Println("提示: 可选设置 OPENAI_BASE_URL（默认 https://api.openai.com/v1）")
			log.Println("提示: 可选设置 OPENAI_MODEL（默认 gpt-4o）")
		}
	}

	scheduler, err := startScheduler(config)
	if err != nil {
		log.Fatalf("启动定时任务失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/healthz", serveHealth)
	mux.HandleFunc("/api/dates", func(w http.ResponseWriter, r *http.Request) {
		serveDates(config.DataDir, w, r)
	})
	mux.HandleFunc("/api/content/", func(w http.ResponseWriter, r *http.Request) {
		serveContent(config.DataDir, w, r)
	})
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		serveManualGenerate(config, w, r)
	})

	server := &http.Server{
		Addr:         ":" + config.Port,
		Handler:      mux,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		IdleTimeout:  config.IdleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("🌐 服务已启动: http://0.0.0.0:%s", config.Port)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务异常退出: %v", err)
		}
	case sig := <-signals:
		log.Printf("🛑 收到退出信号: %s", sig.String())
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	stopContext := scheduler.Stop()
	select {
	case <-stopContext.Done():
	case <-shutdownContext.Done():
	}

	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("⚠️ 服务关闭异常: %v", err)
	}
}

package daily

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"daily-github/internal/ai"
	"daily-github/internal/service"
	"daily-github/internal/storage"

	"golang.org/x/net/html"
)

const systemPrompt = `你是一个专业的 GitHub 开源项目分析师和推荐专家。
你对各类开源项目有深入的了解，能从技术架构、社区活跃度、实用价值等多维度进行分析。
你的推荐风格专业但易读，善于用结构化的方式呈现信息。`

type TrendingRepository struct {
	Repo        string
	Description string
	Language    string
}

type PromptContext struct {
	TrendingByPeriod map[string][]TrendingRepository
	HistoricalRepos  []string
}

type Generator struct {
	markdownStore *storage.MarkdownStore
	historyStore  *storage.HistoryStore
	aiClient      *ai.Client
	retryCount    int
	retryDelay    time.Duration
	trendingHTTP  *http.Client
	mu            sync.Mutex
}

func NewGenerator(markdownStore *storage.MarkdownStore, historyStore *storage.HistoryStore, aiClient *ai.Client, retryCount int, retryDelay time.Duration) *Generator {
	return &Generator{
		markdownStore: markdownStore,
		historyStore:  historyStore,
		aiClient:      aiClient,
		retryCount:    retryCount,
		retryDelay:    retryDelay,
		trendingHTTP:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (g *Generator) Generate(date string) (service.GenerateExecutionResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	result := service.GenerateExecutionResult{
		Date:     date,
		FilePath: g.markdownStore.PathForDate(date),
	}

	attemptLimit := g.retryCount + 1
	var lastErr error
	for attempt := 1; attempt <= attemptLimit; attempt++ {
		generated, err := g.generateDaily(date)
		if err == nil {
			result.Generated = generated
			result.Attempts = attempt
			return result, nil
		}

		lastErr = err
		log.Printf("⚠️ 生成失败 (%s)，第 %d/%d 次: %v", date, attempt, attemptLimit, err)
		if attempt < attemptLimit {
			log.Printf("⏳ %s 后重试生成: %s", g.retryDelay, date)
			time.Sleep(g.retryDelay)
		}
	}

	result.Attempts = attemptLimit
	return result, fmt.Errorf("重试后仍生成失败: %w", lastErr)
}

func (g *Generator) generateDaily(date string) (bool, error) {
	existing, err := g.markdownStore.ReadDaily(date)
	if err != nil {
		return false, err
	}
	if existing.Exists {
		if err := g.historyStore.RecordDaily(date, existing.Content); err != nil {
			log.Printf("⚠️ 补录当日推荐历史失败 (%s): %v", date, err)
		}
		log.Printf("📋 文件已存在，跳过生成: %s", existing.FilePath)
		return false, nil
	}

	log.Printf("🚀 开始生成 %s 的推荐内容...", date)
	promptContext, err := g.buildPromptContext()
	if err != nil {
		return false, fmt.Errorf("构建推荐上下文失败: %w", err)
	}
	prompt := generateUserPrompt(date, promptContext)
	content, err := g.aiClient.Chat(context.Background(), []ai.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}, 0)
	if err != nil {
		return false, fmt.Errorf("生成内容失败: %w", err)
	}

	filePath, err := g.markdownStore.WriteDaily(date, content)
	if err != nil {
		return false, err
	}
	if err := g.historyStore.RecordDaily(date, content); err != nil {
		log.Printf("⚠️ 记录当日推荐历史失败 (%s): %v", date, err)
	}
	log.Printf("✅ 成功生成: %s", filePath)
	return true, nil
}

func (g *Generator) buildPromptContext() (PromptContext, error) {
	history, err := g.historyStore.Load()
	if err != nil {
		return PromptContext{}, err
	}

	return PromptContext{
		HistoricalRepos: storage.ListHistoricalRepos(history),
	}, nil
}

func generateUserPrompt(date string, promptContext PromptContext) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("请为 %s 生成一份每日 GitHub 开源项目推荐报告。\n\n", date))
	builder.WriteString("## 本次推荐策略\n\n")
	builder.WriteString("1. 请自行获取或参考 GitHub Trending 的日榜(https://github.com/trending)、周榜(https://github.com/trending?since=weekly)、月榜(https://github.com/trending?since=monthly)，将其作为本次推荐的重要候选来源。\n")
	builder.WriteString("2. 优先选择近期在 GitHub 社区中热度明显上升、且客观上值得关注的真实项目。\n")
	builder.WriteString("3. 允许补充 1-3 个没有出现在 Trending 中、但确实值得推荐的真实项目，最近讨论又比较多，用于保留更广泛的推荐逻辑。\n")
	builder.WriteString("4. 如果无法确认某个项目是否来自 GitHub Trending，不要标注为 Trending 来源，应标注为补充推荐或基于公开信息参考。\n")
	builder.WriteString("5. 绝对不要重复推荐历史中已经出现过的 owner/repo。\n\n")

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

| 项目 | 语言 | 领域 | 来源 | 推荐程度 |
|------|------|------|------|----------|
| 项目名 | Go | 后端 | Trending 日榜 | ⭐⭐⭐⭐⭐ |

### 每个项目的详细卡片格式

---

### {图标} 项目名称

> 项目描述，用 1-2 句话概括项目的核心价值和特点。

| 属性 | 详情 |
|------|------|
| 📦 仓库 | [owner/repo](https://github.com/owner/repo) |
| 📡 来源 | Trending 参考 / 补充推荐（基于公开信息核验） |
| ⭐ Stars | 从 Github 获取真实的 Star 数 |
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
- ...

**🎯 适用场景**

描述什么人/什么项目适合使用。

**📊 推荐程度**

⭐⭐⭐⭐⭐ (X/5) — 总结推荐理由

---

### 项目标题图标使用规则

根据项目特征在标题中使用对应图标：
- 🔥 热门/趋势项目（Stars 增长快、近期社区关注度高）（重点）
- 💡 创新/新颖项目（采用新技术或解决方案独特）
- 🛠️ 实用工具（开发者日常可用的工具类项目）
- 📚 学习/教程资源（适合学习和参考）
- 🚀 新兴/快速增长项目（成立时间较短但发展迅速）（重点）
- 🤖 AI/机器学习相关（重点）
- 🔒 安全相关
- 🎨 前端/设计相关
- ⚡ 高性能/基础设施

### 今日总结格式

## 📌 今日总结

用 3-4 句话总结今日推荐的主题和趋势，给出整体建议。

---

## 注意事项
- 推荐真实存在的知名 GitHub 项目
- Stars、协议、标签等仓库元数据必须准确，不能凭空编造
- 确保涵盖不同编程语言和应用领域
- 描述要客观专业，避免过度营销
- Markdown 格式要规范，便于渲染
- 每个项目之间用 --- 分隔
- https://api.github.com/search/repositories?q=created:>%s&sort=stars&order=desc&per_page=10
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
		builder.WriteString("- 当前未抓取到该维度的 Trending 数据，请自行访问 Github。\n")
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

func (g *Generator) fetchTrendingRepositories(period string) ([]TrendingRepository, error) {
	url := fmt.Sprintf("https://github.com/trending?since=%s", period)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Trending 请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "daily-github-bot/1.0")

	resp, err := g.trendingHTTP.Do(req)
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
